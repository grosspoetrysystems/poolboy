package sign

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type boundedTestReader struct {
	remaining int64
}

func (r *boundedTestReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	for i := range n {
		p[i] = 'x'
	}
	r.remaining -= int64(n)
	return n, nil
}

func TestParseSeedRejectsMalformedInput(t *testing.T) {
	if _, err := ParseSeed("not valid base64!!!"); err == nil || !strings.Contains(err.Error(), "not valid base64") {
		t.Fatalf("bad base64 error = %v", err)
	}
	short := base64.StdEncoding.EncodeToString([]byte("too short"))
	if _, err := ParseSeed(short); err == nil || !strings.Contains(err.Error(), "must decode to") {
		t.Fatalf("short seed error = %v", err)
	}
	seed, _, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSeed(base64.StdEncoding.EncodeToString(seed)); err != nil {
		t.Fatalf("valid seed rejected: %v", err)
	}
}

func TestWriteSeedFileRejectsWrongLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k")
	if err := WriteSeedFile(path, []byte{1, 2, 3}, false); err == nil || !strings.Contains(err.Error(), "seed must be") {
		t.Fatalf("short seed write error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rejected seed must not create a file: %v", err)
	}
}

func TestWriteSeedFileReportsPathError(t *testing.T) {
	seed, _, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "missing", "key")
	if err := WriteSeedFile(path, seed, false); err == nil {
		t.Fatal("seed write with missing parent must error")
	}
}

func TestLoadSeedFileErrors(t *testing.T) {
	if _, err := LoadSeedFile(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing seed file must error")
	}
	path := filepath.Join(t.TempDir(), "bad")
	if err := os.WriteFile(path, []byte("not base64!!!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSeedFile(path); err == nil || !strings.Contains(err.Error(), "base64") {
		t.Fatalf("malformed seed file error = %v", err)
	}
}

func TestGraphRejectsMalformedPrivateKey(t *testing.T) {
	if _, _, err := Graph([]byte("payload"), ed25519.PrivateKey("short")); err == nil || !strings.Contains(err.Error(), "private key must be") {
		t.Fatalf("short key error = %v", err)
	}
}

func TestReadBoundedRejectsOversize(t *testing.T) {
	if _, err := readBounded(&boundedTestReader{remaining: maxFetchBytes + 1}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized publication error = %v", err)
	}
}

func TestVerifyGraphRejectsMalformedSignatures(t *testing.T) {
	seed, _, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	graph := []byte("payload")
	sig, pub, err := Graph(graph, ed25519.NewKeyFromSeed(seed))
	if err != nil {
		t.Fatal(err)
	}
	remarshal := func(mutate func(*Signature)) []byte {
		var s Signature
		if err := json.Unmarshal(sig, &s); err != nil {
			t.Fatal(err)
		}
		mutate(&s)
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	cases := []struct {
		name    string
		graph   []byte
		sig     []byte
		pub     []byte
		wantSub string
	}{
		{"invalid json", graph, []byte("{not json"), pub, "invalid " + SigName},
		{"unsupported alg", graph, remarshal(func(s *Signature) { s.Alg = "rsa" }), pub, "unsupported signature algorithm"},
		{"key mismatch", graph, sig, []byte("a-different-key"), "signature key does not match"},
		{"invalid public key", graph, remarshal(func(s *Signature) { s.Key = "!!!" }), []byte("!!!"), "invalid public key"},
		{"wrong-length public key", graph, remarshal(func(s *Signature) { s.Key = base64.StdEncoding.EncodeToString([]byte("short")) }), []byte(base64.StdEncoding.EncodeToString([]byte("short"))), "invalid public key"},
		{"invalid signature encoding", graph, remarshal(func(s *Signature) { s.Sig = "!!!" }), pub, "invalid signature encoding"},
		{"wrong-length signature", graph, remarshal(func(s *Signature) { s.Sig = base64.StdEncoding.EncodeToString([]byte("short")) }), pub, "invalid signature encoding"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := VerifyGraph(tc.graph, tc.sig, tc.pub); err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error = %v, want %q", err, tc.wantSub)
			}
		})
	}
}

func TestDirRefusesMissingGraph(t *testing.T) {
	seed, _, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	if err := Dir(t.TempDir(), ed25519.NewKeyFromSeed(seed)); err == nil || !strings.Contains(err.Error(), "no "+GraphName) {
		t.Fatalf("missing graph error = %v", err)
	}
}

func TestDirRejectsInvalidSigningKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, GraphName), []byte("graph"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Dir(dir, ed25519.PrivateKey("short")); err == nil || !strings.Contains(err.Error(), "private key must be") {
		t.Fatalf("invalid signing key error = %v", err)
	}
}

func TestNewSourceRejectsBadTargets(t *testing.T) {
	for _, target := range []string{"https://host/path?x=1", "https://host/path#fragment", "https://user@host/path"} {
		if _, err := newSource(target); err == nil || !strings.Contains(err.Error(), "must not include a query, fragment, or userinfo") {
			t.Fatalf("rejected URL %q error = %v", target, err)
		}
	}
	if _, err := newSource(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("missing local directory must error")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := newSource(file); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file target error = %v", err)
	}
	dir := t.TempDir()
	src, err := newSource(dir)
	if err != nil {
		t.Fatalf("valid directory rejected: %v", err)
	}
	if src.dir == "" || src.origin == "" {
		t.Fatalf("local source = %#v", src)
	}
}

func TestValidateFetchName(t *testing.T) {
	bad := []string{"", "a\\b", "/abs.md", "..", "../escape.md", "dir/../../escape.md"}
	for _, name := range bad {
		if err := validateFetchName(name); err == nil {
			t.Errorf("validateFetchName(%q) = nil, want error", name)
		}
	}
	for _, name := range []string{"index.md", "a/b/c.md", "llms.txt"} {
		if err := validateFetchName(name); err != nil {
			t.Errorf("validateFetchName(%q) = %v, want nil", name, err)
		}
	}
}

func TestFetchLocalRejectsInvalidName(t *testing.T) {
	src, err := newSource(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.fetch("../escape"); err == nil {
		t.Fatal("fetch must reject a traversal path")
	}
}

func TestFetchHTTPReportsNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer server.Close()
	src, err := newSource(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.fetch("missing.md"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("non-200 fetch error = %v", err)
	}
}

func TestFetchLocalReportsMissingFile(t *testing.T) {
	src, err := newSource(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.fetch("missing.md"); err == nil {
		t.Fatal("missing local publication file must error")
	}
}

func TestVerifyRejectsAuthenticatedMalformedManifest(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	seed, _, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	graph := []byte("{not json")
	sig, pub, err := Graph(graph, ed25519.NewKeyFromSeed(seed))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{GraphName: graph, SigName: sig, PubName: pub} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Verify(dir, VerifyOptions{TOFU: true}); err == nil || !strings.Contains(err.Error(), "parse "+GraphName) {
		t.Fatalf("malformed manifest error = %v", err)
	}
}

func TestVerifyFullRejectsMalformedManifest(t *testing.T) {
	src, err := newSource(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyFull(src, []byte("{not json")); err == nil || !strings.Contains(err.Error(), "parse "+GraphName) {
		t.Fatalf("malformed full-verification error = %v", err)
	}
}

func TestInspectMigrationRequiresExplicitPolicy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	seed, _, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	writeSignedPublication(t, dir, ed25519.NewKeyFromSeed(seed), "first")
	candidate, err := PrepareApproval(dir, VerifyOptions{TOFU: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitApproval(candidate, VerifyOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(dir, VerifyOptions{MigrateIdentity: true}); err == nil || !strings.Contains(err.Error(), "requires --identity or --tofu") {
		t.Fatalf("migration policy error = %v", err)
	}
}

func TestVerifySigstoreRejectsMalformedBundle(t *testing.T) {
	if _, err := verifySigstore([]byte("graph"), []byte("not json"), "identity"); err == nil || !strings.Contains(err.Error(), "invalid "+sigstoreBundleName) {
		t.Fatalf("malformed Sigstore bundle error = %v", err)
	}
}

func TestManifestFilesRejectsInvalidManifests(t *testing.T) {
	marshal := func(m map[string]any) []byte {
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	entry := map[string]any{"sha256": validDigest}
	cases := []struct {
		name    string
		graph   []byte
		wantSub string
	}{
		{"malformed json", []byte("{not json"), "parse " + GraphName},
		{"bad llms digest", marshal(map[string]any{"llms": map[string]any{"sha256": "short"}}), "does not authenticate llms.txt"},
		{"markdown path not rooted", marshal(map[string]any{
			"llms":  entry,
			"files": map[string]any{"index.md": entry},
		}), "invalid Markdown path"},
		{"reserved artifact path", marshal(map[string]any{
			"llms":      entry,
			"artifacts": map[string]any{GraphName: entry},
		}), "reserved publication path"},
		{"duplicate path", marshal(map[string]any{
			"llms":      entry,
			"files":     map[string]any{"/doc.md": entry},
			"artifacts": map[string]any{"doc.md": entry},
		}), "duplicate publication path"},
		{"bad file digest", marshal(map[string]any{
			"llms":  entry,
			"files": map[string]any{"/doc.md": map[string]any{"sha256": "nothex"}},
		}), "invalid"},
		{"uppercase file digest", marshal(map[string]any{
			"llms":  entry,
			"files": map[string]any{"/doc.md": map[string]any{"sha256": strings.Repeat("A", 64)}},
		}), "lowercase"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := manifestFiles(tc.graph); err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error = %v, want %q", err, tc.wantSub)
			}
		})
	}

	ok, err := manifestFiles(marshal(map[string]any{
		"llms":      entry,
		"files":     map[string]any{"/doc.md": entry},
		"artifacts": map[string]any{"asset.bin": entry},
	}))
	if err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	for _, want := range []string{"llms.txt", "doc.md", "asset.bin"} {
		if _, exists := ok[want]; !exists {
			t.Errorf("manifest missing %q: %#v", want, ok)
		}
	}
}

func TestInspectRequiresTrustPolicy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	seed, _, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	writeSignedPublication(t, dir, ed25519.NewKeyFromSeed(seed), "first")

	if _, err := Verify(dir, VerifyOptions{}); err == nil || !strings.Contains(err.Error(), "no trust policy") {
		t.Fatalf("no-policy error = %v", err)
	}
	if _, err := Verify(dir, VerifyOptions{MigrateIdentity: true, TOFU: true}); err == nil || !strings.Contains(err.Error(), "migration requires an existing trust record") {
		t.Fatalf("migration-without-record error = %v", err)
	}
}

func TestInspectSigstorePolicyFailures(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	seed, _, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	writeSignedPublication(t, dir, ed25519.NewKeyFromSeed(seed), "first")
	writeLock := func(t *testing.T, identity string) string {
		t.Helper()
		lock := filepath.Join(t.TempDir(), "poolboy.lock")
		body := `{"version":"1","origin":"ignored","mode":"sigstore","identity":"` + identity + `","channel":"stable"}`
		if err := os.WriteFile(lock, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return lock
	}

	if _, err := Verify(dir, VerifyOptions{LockPath: writeLock(t, "")}); err == nil || !strings.Contains(err.Error(), "requires an expected") {
		t.Fatalf("missing Sigstore identity error = %v", err)
	}
	if _, err := Verify(dir, VerifyOptions{LockPath: writeLock(t, "old"), Identity: "new"}); err == nil || !strings.Contains(err.Error(), "changing the expected identity") {
		t.Fatalf("identity migration refusal = %v", err)
	}
	if _, err := Verify(dir, VerifyOptions{LockPath: writeLock(t, "expected")}); err == nil || !strings.Contains(err.Error(), "load "+sigstoreBundleName) {
		t.Fatalf("missing Sigstore bundle error = %v", err)
	}
	missingLock := filepath.Join(t.TempDir(), "missing.lock")
	if _, err := Verify(dir, VerifyOptions{LockPath: missingLock, Identity: "expected"}); err == nil || !strings.Contains(err.Error(), "load "+sigstoreBundleName) {
		t.Fatalf("explicit identity policy error = %v", err)
	}
}

func TestInspectSigstoreMigrationRejectsMalformedBundle(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, GraphName), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sigstoreBundleName), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(t.TempDir(), "poolboy.lock")
	record, err := json.Marshal(TrustRecord{
		Version:  trustVersion,
		Mode:     "sigstore",
		Identity: "old",
		Channel:  defaultChannel,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, record, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Verify(dir, VerifyOptions{LockPath: lock, Identity: "new", MigrateIdentity: true})
	if err == nil || !strings.Contains(err.Error(), "invalid "+sigstoreBundleName) {
		t.Fatalf("malformed migrated Sigstore bundle error = %v", err)
	}
}

func TestInspectTofuMigrationRevalidatesPublisher(t *testing.T) {
	dir := t.TempDir()
	seed, pub, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	writeSignedPublication(t, dir, ed25519.NewKeyFromSeed(seed), "first")
	lock := filepath.Join(t.TempDir(), "poolboy.lock")
	record, err := json.Marshal(TrustRecord{
		Version:        trustVersion,
		Mode:           "tofu",
		Key:            pub,
		Channel:        defaultChannel,
		ManifestSHA256: "old",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, record, 0o644); err != nil {
		t.Fatal(err)
	}
	candidate, err := Verify(dir, VerifyOptions{LockPath: lock, TOFU: true, MigrateIdentity: true})
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Pending || candidate.Record.Key != pub {
		t.Fatalf("migrated TOFU candidate = %#v", candidate)
	}
}

func TestInspectTofuFetchAndSignatureFailures(t *testing.T) {
	t.Run("missing signature", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, GraphName), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Verify(dir, VerifyOptions{LockPath: filepath.Join(t.TempDir(), "missing.lock"), TOFU: true})
		if err == nil || !strings.Contains(err.Error(), "load "+SigName) {
			t.Fatalf("missing signature error = %v", err)
		}
	})
	t.Run("missing public key", func(t *testing.T) {
		dir := t.TempDir()
		for name, data := range map[string][]byte{GraphName: []byte("{}"), SigName: []byte("{}")} {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_, err := Verify(dir, VerifyOptions{LockPath: filepath.Join(t.TempDir(), "missing.lock"), TOFU: true})
		if err == nil || !strings.Contains(err.Error(), "load "+PubName) {
			t.Fatalf("missing public key error = %v", err)
		}
	})
	t.Run("invalid signature", func(t *testing.T) {
		dir := t.TempDir()
		for name, data := range map[string][]byte{GraphName: []byte("{}"), SigName: []byte("{}"), PubName: []byte("key")} {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_, err := Verify(dir, VerifyOptions{LockPath: filepath.Join(t.TempDir(), "missing.lock"), TOFU: true})
		if err == nil || !strings.Contains(err.Error(), "unsupported signature algorithm") {
			t.Fatalf("invalid signature error = %v", err)
		}
	})
}

func TestLoadTrustRejectsUnreadableLock(t *testing.T) {
	dir := t.TempDir()
	if _, err := Verify(dir, VerifyOptions{LockPath: t.TempDir()}); err == nil {
		t.Fatal("directory lock path must error")
	}
}

func TestCommitApprovalReportsLockWriteFailure(t *testing.T) {
	candidate := &Candidate{
		Record:   TrustRecord{ManifestSHA256: validDigest},
		Checked:  1,
		verified: true,
	}
	if err := CommitApproval(candidate, VerifyOptions{LockPath: t.TempDir()}); err == nil {
		t.Fatal("approval written over a directory lock path")
	}
}

func TestDirReportsPublicationWriteFailures(t *testing.T) {
	seed, _, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{SigName, PubName} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, GraphName), []byte("graph"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := Dir(dir, ed25519.NewKeyFromSeed(seed)); err == nil {
				t.Fatalf("signing with directory %s should error", name)
			}
		})
	}
}

func TestPrepareApprovalReportsMissingManifestFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	seed, _, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	writeSignedPublication(t, dir, ed25519.NewKeyFromSeed(seed), "first")
	if err := os.Remove(filepath.Join(dir, "index.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareApproval(dir, VerifyOptions{TOFU: true}); err == nil || !strings.Contains(err.Error(), "load index.md") {
		t.Fatalf("missing manifest file error = %v", err)
	}
}

func TestPrepareApprovalPropagatesTrustAndInspectionErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := PrepareApproval(dir, VerifyOptions{LockPath: dir}); err == nil {
		t.Fatal("directory trust lock must error")
	}
	target := filepath.Join(t.TempDir(), "missing-publication")
	lock := filepath.Join(t.TempDir(), "missing.lock")
	if _, err := PrepareApproval(target, VerifyOptions{LockPath: lock}); err == nil {
		t.Fatal("missing publication target must error")
	}
}

func TestLoadTrustLockErrors(t *testing.T) {
	dir := t.TempDir()
	seed, _, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	writeSignedPublication(t, dir, ed25519.NewKeyFromSeed(seed), "first")

	writeLock := func(t *testing.T, body string) string {
		t.Helper()
		lock := filepath.Join(t.TempDir(), "poolboy.lock")
		if err := os.WriteFile(lock, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return lock
	}

	badVersion := writeLock(t, `{"version":"9","origin":"x","mode":"tofu","channel":"stable"}`)
	if _, err := Verify(dir, VerifyOptions{LockPath: badVersion}); err == nil || !strings.Contains(err.Error(), "unsupported trust lock version") {
		t.Fatalf("bad-version lock error = %v", err)
	}
	badJSON := writeLock(t, "{not json")
	if _, err := Verify(dir, VerifyOptions{LockPath: badJSON}); err == nil {
		t.Fatal("malformed lock must error")
	}

	bogus := writeLock(t, `{"version":"1","origin":"x","mode":"bogus","channel":"stable"}`)
	if _, err := Verify(dir, VerifyOptions{LockPath: bogus}); err == nil || !strings.Contains(err.Error(), "unsupported trust mode") {
		t.Fatalf("bogus-mode error = %v", err)
	}
}

func TestReadLocalTrustHandlesLegacyAndMalformed(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(legacy, []byte(`{"version":"0","https://pub.example":"a2V5"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := readLocalTrust(legacy)
	if err != nil {
		t.Fatal(err)
	}
	record, ok := store.Records[trustKey("https://pub.example", defaultChannel)]
	if !ok || record.Mode != "tofu" || record.Key != "a2V5" {
		t.Fatalf("legacy migration = %#v", store.Records)
	}

	missing := readLocalTrustMust(t, filepath.Join(dir, "absent.json"))
	if len(missing.Records) != 0 {
		t.Fatalf("absent store should be empty: %#v", missing.Records)
	}

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLocalTrust(bad); err == nil {
		t.Fatal("malformed trust store must error")
	}
}

func readLocalTrustMust(t *testing.T, path string) localTrust {
	t.Helper()
	store, err := readLocalTrust(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestDiffFilesReportsAddedChangedRemoved(t *testing.T) {
	added, changed, removed := diffFiles(
		map[string]string{"keep": "1", "edit": "1", "drop": "1"},
		map[string]string{"keep": "1", "edit": "2", "new": "9"},
	)
	if strings.Join(added, ",") != "new" {
		t.Errorf("added = %v, want [new]", added)
	}
	if strings.Join(changed, ",") != "edit" {
		t.Errorf("changed = %v, want [edit]", changed)
	}
	if strings.Join(removed, ",") != "drop" {
		t.Errorf("removed = %v, want [drop]", removed)
	}
}

func TestKnownPublishersPathUsesHomeWithoutXDG(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	path, err := knownPublishersPath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(path), "poolboy/known_publishers.json") {
		t.Fatalf("path = %q", path)
	}
}
