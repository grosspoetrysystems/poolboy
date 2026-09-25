package sign

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGraphRoundTrip(t *testing.T) {
	seed, wantKey, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	graph := []byte(`{"version":"0","root":"/index.md"}`)
	sig, pub, err := Graph(graph, priv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(sig), "\n") || string(pub) != wantKey+"\n" {
		t.Fatalf("signature/publication files are missing their required trailing newline")
	}
	gotKey, err := VerifyGraph(graph, sig, pub)
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != wantKey {
		t.Fatalf("verified key=%q, want %q", gotKey, wantKey)
	}
}

func TestVerifyGraphRejectsTamperedManifest(t *testing.T) {
	seed, _, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	sig, pub, err := Graph([]byte("original"), ed25519.NewKeyFromSeed(seed))
	if err != nil {
		t.Fatal(err)
	}
	_, err = VerifyGraph([]byte("tampered"), sig, pub)
	if err == nil || !strings.Contains(err.Error(), "does not verify") {
		t.Fatalf("tampered graph error=%v, want signature verification failure", err)
	}
}

func TestApprovalPinsExactReleaseAndRejectsChangedKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	seed, _, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	writeSignedPublication(t, dir, ed25519.NewKeyFromSeed(seed), "first")

	candidate, err := Verify(dir, VerifyOptions{TOFU: true})
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Pending {
		t.Fatal("first release was trusted without approval")
	}
	candidate, err = PrepareApproval(dir, VerifyOptions{TOFU: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitApproval(candidate, VerifyOptions{}); err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(dir, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if verified.Pending || verified.Checked != 2 {
		t.Fatalf("approved release pending=%v checked=%d, want false/2", verified.Pending, verified.Checked)
	}
	if err := os.WriteFile(filepath.Join(dir, "llms.txt"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(dir, VerifyOptions{}); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("tampered llms error=%v, want hash failure", err)
	}
	writeSignedPublication(t, dir, ed25519.NewKeyFromSeed(seed), "first")

	writeSignedPublication(t, dir, ed25519.NewKeyFromSeed(seed), "second")
	changed, err := Verify(dir, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !changed.Pending || len(changed.Changed) != 1 || changed.Changed[0] != "index.md" {
		t.Fatalf("changed release=%+v, want pending index.md change", changed)
	}

	otherSeed, _, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	writeSignedPublication(t, dir, ed25519.NewKeyFromSeed(otherSeed), "third")
	if _, err := Verify(dir, VerifyOptions{}); err == nil || !strings.Contains(err.Error(), "PUBLISHER KEY CHANGED") {
		t.Fatalf("changed publisher error=%v, want hard failure", err)
	}
}

func TestProjectLockIsAuthoritativeForChannel(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(t.TempDir(), "poolboy.lock")
	seed, _, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	writeSignedPublication(t, dir, ed25519.NewKeyFromSeed(seed), "first")
	opts := VerifyOptions{LockPath: lock, TOFU: true}
	candidate, err := PrepareApproval(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitApproval(candidate, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(dir, VerifyOptions{LockPath: lock, Channel: "beta"}); err == nil || !strings.Contains(err.Error(), "channel") {
		t.Fatalf("channel mismatch error=%v, want lock refusal", err)
	}
}

func TestManifestRejectsLLMSCollision(t *testing.T) {
	digest := strings.Repeat("0", sha256.Size*2)
	graph, err := json.Marshal(map[string]any{
		"llms":      map[string]any{"sha256": digest},
		"files":     map[string]any{"/index.md": map[string]any{"sha256": digest}},
		"artifacts": map[string]any{"llms.txt": map[string]any{"sha256": digest}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifestFiles(graph); err == nil || !strings.Contains(err.Error(), "reserved publication path") {
		t.Fatalf("collision error=%v, want reserved path refusal", err)
	}
}

func TestCommitApprovalRejectsUnverifiedCandidate(t *testing.T) {
	candidate := &Candidate{
		Record:  TrustRecord{ManifestSHA256: strings.Repeat("0", sha256.Size*2)},
		Checked: 1,
	}
	if err := CommitApproval(candidate, VerifyOptions{LockPath: filepath.Join(t.TempDir(), "poolboy.lock")}); err == nil {
		t.Fatal("unverified candidate was committed")
	}
}

func writeSignedPublication(t *testing.T, dir string, priv ed25519.PrivateKey, body string) {
	t.Helper()
	llms := []byte("# Corpus\n")
	index := []byte(body)
	entry := func(data []byte) map[string]any {
		sum := sha256.Sum256(data)
		return map[string]any{"bytes": len(data), "sha256": hex.EncodeToString(sum[:])}
	}
	graph, err := json.Marshal(map[string]any{
		"version": "0",
		"root":    "/index.md",
		"llms":    entry(llms),
		"files":   map[string]any{"/index.md": entry(index)},
	})
	if err != nil {
		t.Fatal(err)
	}
	graph = append(graph, '\n')
	for name, data := range map[string][]byte{GraphName: graph, "llms.txt": llms, "index.md": index} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Dir(dir, priv); err != nil {
		t.Fatal(err)
	}
}

func TestWriteSeedFileRefusesOverwriteUnlessForced(t *testing.T) {
	seed, _, err := GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "poolboy.key")
	if err := WriteSeedFile(path, seed, false); err != nil {
		t.Fatal(err)
	}
	if err := WriteSeedFile(path, seed, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("second write error=%v, want --force guidance", err)
	}
	if err := WriteSeedFile(path, seed, true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("seed mode=%o, want 600", info.Mode().Perm())
	}
	loaded, err := LoadSeedFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded.Seed()) != string(seed) {
		t.Fatal("forced write changed the seed")
	}
}
