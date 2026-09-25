package sign

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	sigbundle "github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	sigverify "github.com/sigstore/sigstore-go/pkg/verify"
)

const (
	sigstoreBundleName = "graph.json.sigstore.json"
	githubOIDCIssuer   = "https://token.actions.githubusercontent.com"
	trustVersion       = "1"
	defaultChannel     = "stable"
	defaultReleaseAge  = 72 * time.Hour
)

// VerifyOptions selects trust state, publisher identity, channel, and approval policy.
type VerifyOptions struct {
	LockPath          string
	TOFU              bool
	Identity          string
	Channel           string
	MinimumReleaseAge time.Duration
	MigrateIdentity   bool
	OverrideAge       bool
	Now               func() time.Time
}

// TrustRecord is the exact approved publisher, channel, release, and corpus hash set.
type TrustRecord struct {
	Version           string            `json:"version"`
	Origin            string            `json:"origin"`
	Mode              string            `json:"mode"`
	Identity          string            `json:"identity,omitempty"`
	Key               string            `json:"key,omitempty"`
	Channel           string            `json:"channel"`
	ManifestSHA256    string            `json:"manifest_sha256"`
	SignedAt          time.Time         `json:"signed_at,omitempty"`
	MinimumReleaseAge string            `json:"minimum_release_age,omitempty"`
	AgeOverride       bool              `json:"age_override,omitempty"`
	Files             map[string]string `json:"files"`
}

// Candidate describes an authenticated release and its difference from approval.
type Candidate struct {
	Record   TrustRecord
	Added    []string
	Changed  []string
	Removed  []string
	Checked  int
	Pending  bool
	Previous *TrustRecord
	verified bool
}

type localTrust struct {
	Version string                 `json:"version"`
	Records map[string]TrustRecord `json:"records"`
}

type manifestEntry struct {
	SHA256 string `json:"sha256"`
}

type signedManifest struct {
	LLMS      manifestEntry            `json:"llms"`
	Files     map[string]manifestEntry `json:"files"`
	Artifacts map[string]manifestEntry `json:"artifacts"`
}

// Verify authenticates release metadata and fully verifies only an approved digest.
func Verify(target string, opts VerifyOptions) (*Candidate, error) {
	record, found, err := loadTrust(target, opts)
	if err != nil {
		return nil, err
	}
	candidate, src, graph, err := inspect(target, opts, record, found)
	if err != nil {
		return nil, err
	}
	if !found || record.ManifestSHA256 != candidate.Record.ManifestSHA256 {
		candidate.Pending = true
		return candidate, nil
	}
	checked, err := verifyFull(src, graph)
	if err != nil {
		return nil, err
	}
	candidate.Checked = checked
	return candidate, nil
}

// PrepareApproval authenticates and fully verifies a candidate before review.
func PrepareApproval(target string, opts VerifyOptions) (*Candidate, error) {
	record, found, err := loadTrust(target, opts)
	if err != nil {
		return nil, err
	}
	candidate, src, graph, err := inspect(target, opts, record, found)
	if err != nil {
		return nil, err
	}
	checked, err := verifyFull(src, graph)
	if err != nil {
		return nil, err
	}
	candidate.Checked = checked
	candidate.verified = true
	age := candidate.Record.MinimumReleaseAge
	if age != "" && !candidate.Record.SignedAt.IsZero() {
		minimum, err := time.ParseDuration(age)
		if err != nil {
			return nil, fmt.Errorf("invalid minimum release age %q: %w", age, err)
		}
		now := time.Now
		if opts.Now != nil {
			now = opts.Now
		}
		remaining := candidate.Record.SignedAt.Add(minimum).Sub(now())
		if remaining > 0 && !opts.OverrideAge {
			return nil, verifyErrf("release %s is quarantined for %s more (signed %s; minimum age %s)", candidate.Record.ManifestSHA256, remaining.Round(time.Minute), candidate.Record.SignedAt.Format(time.RFC3339), minimum)
		}
		candidate.Record.AgeOverride = remaining > 0 && opts.OverrideAge
	}
	return candidate, nil
}

// CommitApproval persists a candidate produced by PrepareApproval.
func CommitApproval(candidate *Candidate, opts VerifyOptions) error {
	if candidate == nil || !candidate.verified || candidate.Record.ManifestSHA256 == "" || candidate.Checked == 0 {
		return errors.New("approval requires a fully verified candidate")
	}
	candidate.Record.Version = trustVersion
	if opts.LockPath != "" {
		return writeJSONAtomic(opts.LockPath, candidate.Record, 0o644)
	}
	path, err := knownPublishersPath()
	if err != nil {
		return err
	}
	store, err := readLocalTrust(path)
	if err != nil {
		return err
	}
	store.Records[trustKey(candidate.Record.Origin, candidate.Record.Channel)] = candidate.Record
	return writeJSONAtomic(path, store, 0o600)
}

func inspect(target string, opts VerifyOptions, trusted TrustRecord, found bool) (*Candidate, *source, []byte, error) {
	src, err := newSource(target)
	if err != nil {
		return nil, nil, nil, err
	}
	if found && opts.Channel != "" && opts.Channel != trusted.Channel {
		return nil, nil, nil, fmt.Errorf("trust lock channel is %q, not %q", trusted.Channel, opts.Channel)
	}
	graph, err := src.fetch(GraphName)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load %s: %w", GraphName, err)
	}
	digestBytes := sha256.Sum256(graph)
	digest := hex.EncodeToString(digestBytes[:])
	channel := opts.Channel
	if channel == "" {
		channel = trusted.Channel
	}
	if channel == "" {
		channel = defaultChannel
	}

	mode := trusted.Mode
	if !found {
		if opts.TOFU {
			mode = "tofu"
		} else if opts.Identity != "" {
			mode = "sigstore"
		}
	}
	if opts.MigrateIdentity && !found {
		return nil, nil, nil, errors.New("identity migration requires an existing trust record")
	}
	if opts.MigrateIdentity {
		switch {
		case opts.TOFU:
			mode = "tofu"
		case opts.Identity != "":
			mode = "sigstore"
		default:
			return nil, nil, nil, errors.New("identity migration requires --identity or --tofu")
		}
	}
	if mode == "" {
		return nil, nil, nil, errors.New("no trust policy: pass --lock, --identity, or explicit --tofu")
	}

	record := TrustRecord{Version: trustVersion, Origin: src.origin, Mode: mode, Channel: channel, ManifestSHA256: digest}
	switch mode {
	case "tofu":
		sig, err := src.fetch(SigName)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("load %s: %w", SigName, err)
		}
		pub, err := src.fetch(PubName)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("load %s: %w", PubName, err)
		}
		key, err := VerifyGraph(graph, sig, pub)
		if err != nil {
			return nil, nil, nil, err
		}
		if found && !opts.MigrateIdentity && trusted.Key != key {
			return nil, nil, nil, verifyErrf("PUBLISHER KEY CHANGED for %s\n  approved key: %s\n  offered key:  %s", src.origin, trusted.Key, key)
		}
		record.Key = key
	case "sigstore":
		identity := trusted.Identity
		if opts.Identity != "" {
			if found && !opts.MigrateIdentity && opts.Identity != trusted.Identity {
				return nil, nil, nil, errors.New("changing the expected identity requires --migrate-identity")
			}
			identity = opts.Identity
		}
		if identity == "" {
			return nil, nil, nil, errors.New("sigstore verification requires an expected --identity")
		}
		bundleBytes, err := src.fetch(sigstoreBundleName)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("load %s: %w", sigstoreBundleName, err)
		}
		signedAt, err := verifySigstore(graph, bundleBytes, identity)
		if err != nil {
			return nil, nil, nil, err
		}
		if found && !opts.MigrateIdentity && !trusted.SignedAt.IsZero() && signedAt.Before(trusted.SignedAt) {
			return nil, nil, nil, verifyErrf("RELEASE ROLLBACK for %s\n  approved signing time: %s\n  offered signing time:  %s", src.origin, trusted.SignedAt.Format(time.RFC3339), signedAt.Format(time.RFC3339))
		}
		record.Identity = identity
		record.SignedAt = signedAt
		minimum := opts.MinimumReleaseAge
		if found && opts.MinimumReleaseAge == 0 {
			parsed, err := time.ParseDuration(trusted.MinimumReleaseAge)
			if trusted.MinimumReleaseAge != "" && err != nil {
				return nil, nil, nil, fmt.Errorf("invalid locked minimum release age %q: %w", trusted.MinimumReleaseAge, err)
			}
			minimum = parsed
		}
		if minimum == 0 && channel == defaultChannel {
			minimum = defaultReleaseAge
		}
		if minimum > 0 {
			record.MinimumReleaseAge = minimum.String()
		}
	default:
		return nil, nil, nil, fmt.Errorf("unsupported trust mode %q", mode)
	}

	files, err := manifestFiles(graph)
	if err != nil {
		return nil, nil, nil, err
	}
	record.Files = files
	candidate := &Candidate{Record: record}
	if found {
		previous := trusted
		candidate.Previous = &previous
	}
	if found {
		candidate.Added, candidate.Changed, candidate.Removed = diffFiles(trusted.Files, files)
	} else {
		candidate.Added = sortedKeys(files)
	}
	return candidate, src, graph, nil
}

func verifySigstore(graph, bundleBytes []byte, identity string) (time.Time, error) {
	var b sigbundle.Bundle
	if err := b.UnmarshalJSON(bundleBytes); err != nil {
		return time.Time{}, verifyErrf("invalid %s: %v", sigstoreBundleName, err)
	}
	trustedRoot, err := root.FetchTrustedRoot()
	if err != nil {
		return time.Time{}, fmt.Errorf("load Sigstore trusted root: %w", err)
	}
	verifier, err := sigverify.NewVerifier(trustedRoot, sigverify.WithSignedCertificateTimestamps(1), sigverify.WithTransparencyLog(1), sigverify.WithObserverTimestamps(1))
	if err != nil {
		return time.Time{}, err
	}
	certIdentity, err := sigverify.NewShortCertificateIdentity(githubOIDCIssuer, "", identity, "")
	if err != nil {
		return time.Time{}, err
	}
	digest := sha256.Sum256(graph)
	result, err := verifier.Verify(&b, sigverify.NewPolicy(sigverify.WithArtifactDigest("sha256", digest[:]), sigverify.WithCertificateIdentity(certIdentity)))
	if err != nil {
		return time.Time{}, verifyErrf("Sigstore verification failed for %s: %v", identity, err)
	}
	var signedAt time.Time
	for _, timestamp := range result.VerifiedTimestamps {
		if timestamp.Type == "Tlog" && (signedAt.IsZero() || timestamp.Timestamp.Before(signedAt)) {
			signedAt = timestamp.Timestamp.UTC()
		}
	}
	if signedAt.IsZero() {
		return time.Time{}, verifyErrf("Sigstore bundle has no verified transparency-log timestamp")
	}
	return signedAt, nil
}

func manifestFiles(graph []byte) (map[string]string, error) {
	var manifest signedManifest
	if err := json.Unmarshal(graph, &manifest); err != nil {
		return nil, fmt.Errorf("parse %s: %w", GraphName, err)
	}
	if err := validateManifestDigest("llms.txt", manifest.LLMS.SHA256); err != nil {
		return nil, verifyErrf("%s does not authenticate llms.txt: %v", GraphName, err)
	}
	files := make(map[string]string, len(manifest.Files)+len(manifest.Artifacts)+1)
	files["llms.txt"] = manifest.LLMS.SHA256
	add := func(path string, entry manifestEntry) error {
		if err := validateFetchName(path); err != nil {
			return err
		}
		switch path {
		case GraphName, SigName, PubName, sigstoreBundleName, "llms.txt":
			return fmt.Errorf("reserved publication path %q", path)
		}
		if _, exists := files[path]; exists {
			return fmt.Errorf("duplicate publication path %q", path)
		}
		if err := validateManifestDigest(path, entry.SHA256); err != nil {
			return err
		}
		files[path] = entry.SHA256
		return nil
	}
	for path, entry := range manifest.Files {
		if !strings.HasPrefix(path, "/") {
			return nil, verifyErrf("invalid Markdown path %q", path)
		}
		if err := add(strings.TrimPrefix(path, "/"), entry); err != nil {
			return nil, verifyErrf("invalid %s: %v", GraphName, err)
		}
	}
	for path, entry := range manifest.Artifacts {
		if err := add(path, entry); err != nil {
			return nil, verifyErrf("invalid %s: %v", GraphName, err)
		}
	}
	return files, nil
}

func validateManifestDigest(path, digest string) error {
	if len(digest) != sha256.Size*2 {
		return fmt.Errorf("%s has invalid SHA-256", path)
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || hex.EncodeToString(decoded) != digest {
		return fmt.Errorf("%s has invalid lowercase SHA-256", path)
	}
	return nil
}

func loadTrust(target string, opts VerifyOptions) (TrustRecord, bool, error) {
	if opts.LockPath != "" {
		// #nosec G304 -- the operator explicitly selected this trust lock.
		data, err := os.ReadFile(opts.LockPath)
		if errors.Is(err, os.ErrNotExist) {
			return TrustRecord{}, false, nil
		}
		if err != nil {
			return TrustRecord{}, false, err
		}
		var record TrustRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return TrustRecord{}, false, fmt.Errorf("read %s: %w", opts.LockPath, err)
		}
		if record.Version != trustVersion {
			return TrustRecord{}, false, fmt.Errorf("unsupported trust lock version %q", record.Version)
		}
		return record, true, nil
	}
	path, err := knownPublishersPath()
	if err != nil {
		return TrustRecord{}, false, err
	}
	store, err := readLocalTrust(path)
	if err != nil {
		return TrustRecord{}, false, err
	}
	src, err := newSource(target)
	if err != nil {
		return TrustRecord{}, false, err
	}
	channel := opts.Channel
	if channel == "" {
		channel = defaultChannel
	}
	record, ok := store.Records[trustKey(src.origin, channel)]
	return record, ok, nil
}

func readLocalTrust(path string) (localTrust, error) {
	store := localTrust{Version: trustVersion, Records: map[string]TrustRecord{}}
	// #nosec G304 -- path is the selected Poolboy trust store.
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return store, err
	}
	if err := json.Unmarshal(data, &store); err == nil && store.Version == trustVersion && store.Records != nil {
		return store, nil
	}
	var legacy map[string]string
	if err := json.Unmarshal(data, &legacy); err != nil {
		return store, fmt.Errorf("read %s: %w", path, err)
	}
	for origin, key := range legacy {
		record := TrustRecord{Version: trustVersion, Origin: origin, Mode: "tofu", Key: key, Channel: defaultChannel, Files: map[string]string{}}
		store.Records[trustKey(origin, defaultChannel)] = record
	}
	return store, nil
}

func knownPublishersPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "poolboy", "known_publishers.json"), nil
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".poolboy-trust-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func trustKey(origin, channel string) string { return origin + "\n" + channel }

func diffFiles(old, current map[string]string) (added, changed, removed []string) {
	for path, digest := range current {
		if prior, ok := old[path]; !ok {
			added = append(added, path)
		} else if prior != digest {
			changed = append(changed, path)
		}
	}
	for path := range old {
		if _, ok := current[path]; !ok {
			removed = append(removed, path)
		}
	}
	sort.Strings(added)
	sort.Strings(changed)
	sort.Strings(removed)
	return
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
