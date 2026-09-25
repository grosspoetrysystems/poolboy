package sign

import (
	"crypto/ed25519"
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

func TestPinFirstUseThenRejectsChangedKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	origin := "https://example.test/docs"
	first := strings.Repeat("a", 43)
	second := strings.Repeat("b", 43)
	pinned, err := Pin(origin, first)
	if err != nil {
		t.Fatal(err)
	}
	if !pinned {
		t.Fatal("first verification did not report a new pin")
	}
	pinned, err = Pin(origin, first)
	if err != nil {
		t.Fatal(err)
	}
	if pinned {
		t.Fatal("repeat verification reported a new pin")
	}
	_, err = Pin(origin, second)
	if err == nil || !strings.Contains(err.Error(), "PUBLISHER KEY CHANGED") || !strings.Contains(err.Error(), first) || !strings.Contains(err.Error(), second) {
		t.Fatalf("changed publisher error=%v, want both keys", err)
	}
	path, err := KnownPublishersPath()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("known publishers mode=%o, want 600", info.Mode().Perm())
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
