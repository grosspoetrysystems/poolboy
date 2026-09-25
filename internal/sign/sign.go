// Package sign authenticates Poolboy publication manifests with either
// Sigstore workflow identity or an explicit Ed25519 TOFU fallback, then
// verifies every corpus byte named by the manifest.
package sign

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Reserved publication file names.
const (
	Algorithm = "ed25519"
	GraphName = "graph.json"
	SigName   = "graph.json.sig"
	PubName   = "poolboy.pub"
)

// maxFetchBytes bounds a single verification read so a hostile origin cannot
// exhaust memory. 64 MiB matches the corpus ceiling.
const maxFetchBytes = 64 << 20

// Signature is the on-disk graph.json.sig payload. Field order is locked
// (alg, key, sig) so the file is byte-deterministic.
type Signature struct {
	Alg string `json:"alg"`
	Key string `json:"key"`
	Sig string `json:"sig"`
}

// VerifyError marks a negative verification result (bad provenance, rollback,
// publisher change, or content mismatch) rather than an IO or usage error.
type VerifyError struct{ msg string }

func (e *VerifyError) Error() string { return e.msg }

func verifyErrf(format string, a ...any) error {
	return &VerifyError{msg: fmt.Sprintf(format, a...)}
}

// GenerateSeed creates a fresh keypair, returning the 32-byte ed25519 seed and
// the base64 (std) public key.
func GenerateSeed() (seed []byte, pub string, err error) {
	public, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	return priv.Seed(), base64.StdEncoding.EncodeToString(public), nil
}

// WriteSeedFile writes a base64 (std) ed25519 seed to path at mode 0600. It
// refuses to overwrite an existing file unless force is set. Private keys never
// belong in a publication; callers point this at a key file, not the output.
func WriteSeedFile(path string, seed []byte, force bool) error {
	if len(seed) != ed25519.SeedSize {
		return fmt.Errorf("seed must be %d bytes, got %d", ed25519.SeedSize, len(seed))
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !force {
		flags |= os.O_EXCL
	}
	// #nosec G304 -- key path is explicitly supplied by the operator.
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists; pass --force to overwrite", path)
		}
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	_, werr := f.WriteString(base64.StdEncoding.EncodeToString(seed) + "\n")
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// LoadSeedFile reads and decodes a base64 (std) ed25519 seed file into a
// private key.
func LoadSeedFile(path string) (ed25519.PrivateKey, error) {
	// #nosec G304 -- key path is operator-supplied, not attacker-controlled.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSeed(string(data))
}

// ParseSeed decodes a base64 (std) 32-byte ed25519 seed into a private key.
func ParseSeed(text string) (ed25519.PrivateKey, error) {
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil {
		return nil, fmt.Errorf("signing key is not valid base64: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("signing key must decode to %d bytes, got %d", ed25519.SeedSize, len(seed))
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// Graph signs graphBytes and returns the graph.json.sig bytes (JSON +
// trailing newline) and the poolboy.pub bytes (base64 public key + newline).
func Graph(graphBytes []byte, priv ed25519.PrivateKey) (sig, pub []byte, err error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, nil, fmt.Errorf("private key must be %d bytes, got %d", ed25519.PrivateKeySize, len(priv))
	}
	public, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, nil, errors.New("private key is not ed25519")
	}
	key := base64.StdEncoding.EncodeToString(public)
	payload := Signature{
		Alg: Algorithm,
		Key: key,
		Sig: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, graphBytes)),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	return append(data, '\n'), []byte(key + "\n"), nil
}

// VerifyGraph checks that pubBytes matches sig.key and that the signature
// verifies over graphBytes. It returns the base64 public key on success.
func VerifyGraph(graphBytes, sigBytes, pubBytes []byte) (string, error) {
	var payload Signature
	if err := json.Unmarshal(sigBytes, &payload); err != nil {
		return "", verifyErrf("invalid %s: %v", SigName, err)
	}
	if payload.Alg != Algorithm {
		return "", verifyErrf("unsupported signature algorithm %q (want %q)", payload.Alg, Algorithm)
	}
	pubText := strings.TrimSpace(string(pubBytes))
	if payload.Key != pubText {
		return "", verifyErrf("signature key does not match %s:\n  %s key: %s\n  signature key: %s", PubName, PubName, pubText, payload.Key)
	}
	pubKey, err := base64.StdEncoding.DecodeString(pubText)
	if err != nil || len(pubKey) != ed25519.PublicKeySize {
		return "", verifyErrf("invalid public key in %s", PubName)
	}
	rawSig, err := base64.StdEncoding.DecodeString(payload.Sig)
	if err != nil || len(rawSig) != ed25519.SignatureSize {
		return "", verifyErrf("invalid signature encoding in %s", SigName)
	}
	if !ed25519.Verify(ed25519.PublicKey(pubKey), graphBytes, rawSig) {
		return "", verifyErrf("signature does not verify over %s (manifest was modified or signed by a different key)", GraphName)
	}
	return pubText, nil
}

// Dir reads <dir>/graph.json, signs it, and writes <dir>/graph.json.sig and
// <dir>/poolboy.pub. It never writes the private key into the publication.
func Dir(dir string, priv ed25519.PrivateKey) error {
	graphPath := filepath.Join(dir, GraphName)
	// #nosec G304 -- dir is the operator's own resolved publication output.
	graph, err := os.ReadFile(graphPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no %s in %s; run poolboy build first", GraphName, dir)
		}
		return err
	}
	sig, pub, err := Graph(graph, priv)
	if err != nil {
		return err
	}
	// #nosec G703 -- dir is the resolved publication output selected by the operator.
	if err := os.WriteFile(filepath.Join(dir, SigName), sig, 0o644); err != nil { //nolint:gosec // published metadata is intentionally world-readable
		return err
	}
	// #nosec G703 -- dir is the resolved publication output selected by the operator.
	return os.WriteFile(filepath.Join(dir, PubName), pub, 0o644) //nolint:gosec // published metadata is intentionally world-readable
}

// verifyFull checks every byte named by the signed manifest, including the
// llms.txt discovery entrypoint.
func verifyFull(src *source, graph []byte) (int, error) {
	files, err := manifestFiles(graph)
	if err != nil {
		return 0, err
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for i, path := range paths {
		if err := src.checkHash(path, files[path]); err != nil {
			return i, err
		}
	}
	return len(paths), nil
}

// source loads named files from a publication and reports a normalized origin.
type source struct {
	origin string
	dir    string // set for local directories
	base   string // set for http(s) origins, trailing slash trimmed
	client *http.Client
}

func newSource(target string) (*source, error) {
	if u, ok := parseHTTPURL(target); ok {
		if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return nil, errors.New("verify URL must not include a query, fragment, or userinfo")
		}
		scheme := strings.ToLower(u.Scheme)
		host := strings.ToLower(u.Host)
		path := strings.TrimRight(u.EscapedPath(), "/")
		base := scheme + "://" + host + path
		return &source{
			origin: base,
			base:   base,
			client: &http.Client{
				Timeout: 30 * time.Second,
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
			},
		}, nil
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory or http(s) URL", target)
	}
	return &source{origin: abs, dir: abs}, nil
}

func parseHTTPURL(target string) (*url.URL, bool) {
	u, err := url.Parse(target)
	if err != nil || u.Host == "" {
		return nil, false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, false
	}
	u.Scheme = scheme
	return u, true
}

func (s *source) fetch(name string) ([]byte, error) {
	if err := validateFetchName(name); err != nil {
		return nil, err
	}
	if s.dir != "" {
		// #nosec G304 -- name is a manifest-relative path after validation.
		f, err := os.Open(filepath.Join(s.dir, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		return readBounded(f)
	}
	// #nosec G107 -- base is an operator-supplied publication URL.
	resp, err := s.client.Get(s.base + "/" + escapeHTTPPath(name))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", name, resp.Status)
	}
	return readBounded(resp.Body)
}

func escapeHTTPPath(name string) string {
	parts := strings.Split(name, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func validateFetchName(name string) error {
	if name == "" || strings.Contains(name, "\\") {
		return fmt.Errorf("invalid publication path %q", name)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	if filepath.IsAbs(filepath.FromSlash(name)) || clean != name || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("invalid publication path %q", name)
	}
	return nil
}

func readBounded(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxFetchBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFetchBytes {
		return nil, fmt.Errorf("publication file exceeds %d bytes", maxFetchBytes)
	}
	return data, nil
}

func (s *source) checkHash(name, want string) error {
	data, err := s.fetch(name)
	if err != nil {
		return fmt.Errorf("load %s: %w", name, err)
	}
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if actual != want {
		return verifyErrf("%s hash mismatch:\n  manifest: %s\n  actual:   %s", name, want, actual)
	}
	return nil
}
