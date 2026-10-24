package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/grosspoetrysystems/poolboy/internal/sign"
)

// signProject writes a minimal discoverable bundle whose output directory holds
// a graph.json ready for `poolboy sign`, mirroring what a build leaves behind.
func signProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("poolboy.toml", "spec = \"0.2\"\ncorpus = \"docs\"\noutput = \"dist\"\n")
	write("docs/index.md", "---\nokf_version: \"0.2\"\n---\n# Docs\n")
	write("dist/graph.json", "{\"version\":\"0\",\"root\":\"/index.md\"}\n")
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// signedPublication writes a complete, signable+verifiable publication into dir
// using only the exported sign API (the internal fixture is package-private).
func signedPublication(t *testing.T, dir string, priv ed25519.PrivateKey, body string) {
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
	for name, data := range map[string][]byte{sign.GraphName: graph, "llms.txt": llms, "index.md": index} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := sign.Dir(dir, priv); err != nil {
		t.Fatal(err)
	}
}

func newPrivateKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	seed, _, err := sign.GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	return ed25519.NewKeyFromSeed(seed)
}

func TestCmdKeygenWritesProtectedKeyOutsideProject(t *testing.T) {
	t.Cleanup(func() { rootDir = "" })
	t.Chdir(t.TempDir()) // no project here, so publication refusal does not apply
	keyPath := filepath.Join(t.TempDir(), "poolboy.key")

	out, code := capture(t, func() int { return cmdKeygen([]string{"--out", keyPath}) })
	if code != 0 {
		t.Fatalf("keygen exit=%d out=%q", code, out)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("keygen did not print the public key")
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode=%o, want 600", info.Mode().Perm())
	}

	// A second write without --force must refuse; --force overwrites.
	if _, code = capture(t, func() int { return cmdKeygen([]string{"--out", keyPath}) }); code != 2 {
		t.Fatalf("overwrite without --force exit=%d, want 2", code)
	}
	if _, code = capture(t, func() int { return cmdKeygen([]string{"--out", keyPath, "--force"}) }); code != 0 {
		t.Fatalf("forced overwrite exit=%d, want 0", code)
	}
}

func TestCmdKeygenRejectsKeyInsidePublication(t *testing.T) {
	t.Cleanup(func() { rootDir = "" })
	root := signProject(t)
	t.Chdir(root)

	for _, inside := range []string{filepath.Join("docs", "k.key"), filepath.Join("dist", "k.key")} {
		if _, code := capture(t, func() int { return cmdKeygen([]string{"--out", inside}) }); code != 2 {
			t.Fatalf("keygen into %s exit=%d, want refusal 2", inside, code)
		}
	}
	if _, code := capture(t, func() int { return cmdKeygen([]string{"extra"}) }); code != 2 {
		t.Fatalf("keygen with positional arg exit=%d, want 2", code)
	}
}

func TestCmdKeygenProtectsRootSelectedOrUnreadableProject(t *testing.T) {
	for _, tc := range []struct{ name, mode string }{
		{"--root from outside the project", "root"},
		{"unreadable poolboy.toml", "broken"},
		{"destination project from outside cwd", "destination"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() { rootDir = "" })
			root := signProject(t)
			switch tc.mode {
			case "broken":
				if err := os.WriteFile(filepath.Join(root, "poolboy.toml"), []byte("corpus = [\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Chdir(root)
			case "root":
				t.Chdir(t.TempDir())
				rootDir = root
			default:
				t.Chdir(t.TempDir())
			}
			target := filepath.Join(root, "docs", "k.key")
			out, code := capture(t, func() int { return cmdKeygen([]string{"--out", target}) })
			if code != 2 || out != "" {
				t.Errorf("keygen exit=%d, public key emitted=%t; want refusal", code, out != "")
			}
			if _, err := os.Lstat(target); !os.IsNotExist(err) {
				t.Fatalf("protected key target must remain absent, lstat error=%v", err)
			}
		})
	}
}

func TestCmdKeygenRejectsSymlinkDestinationsInPublication(t *testing.T) {
	for _, tc := range []struct {
		name, linkTarget, suffix, protected string
		force                               bool
	}{
		{"project alias into corpus", ".", "docs/new.key", "docs/new.key", false},
		{"publication parent alias", "dist", "new.key", "dist/new.key", false},
		{"forced publication leaf", "dist/graph.json", "", "dist/graph.json", true},
		{"dangling corpus leaf", "docs/new.key", "", "docs/new.key", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := signProject(t)
			t.Chdir(root)
			target := filepath.Join(root, filepath.FromSlash(tc.protected))
			before, beforeErr := os.ReadFile(target)
			if beforeErr != nil && !os.IsNotExist(beforeErr) {
				t.Fatal(beforeErr)
			}
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(filepath.Join(root, filepath.FromSlash(tc.linkTarget)), alias); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			args := []string{"--out", filepath.Join(alias, filepath.FromSlash(tc.suffix))}
			if tc.force {
				args = append(args, "--force")
			}
			out, code := capture(t, func() int { return cmdKeygen(args) })
			if code != 2 || out != "" {
				t.Errorf("keygen exit=%d, public key emitted=%t; want refusal without a public key", code, out != "")
			}
			after, err := os.ReadFile(target)
			if beforeErr == nil {
				if err != nil || string(after) != string(before) {
					t.Fatal("keygen modified the protected publication file")
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("protected key target must remain absent, read error=%v", err)
			}
		})
	}
}

func TestCmdKeygenRejectsParentTraversalAfterSymlink(t *testing.T) {
	root := signProject(t)
	t.Chdir(root)
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(outside, "alias")
	if err := os.Symlink(filepath.Join(root, "dist"), alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// Preserve "..": the writer follows the symlink before traversing its parent.
	keyPath := alias + string(filepath.Separator) + filepath.FromSlash("../docs/new.key")
	out, code := capture(t, func() int { return cmdKeygen([]string{"--out", keyPath}) })
	if code != 2 || out != "" {
		t.Errorf("keygen exit=%d, public key emitted=%t; want refusal", code, out != "")
	}
	if _, err := os.Lstat(filepath.Join(root, "docs", "new.key")); !os.IsNotExist(err) {
		t.Fatalf("protected key target must remain absent, lstat error=%v", err)
	}
}

func TestCmdKeygenRejectsRelativePathFromSymlinkedCwd(t *testing.T) {
	root := signProject(t)
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Chdir(alias)
	t.Setenv("PWD", alias)
	out, code := capture(t, func() int {
		return cmdKeygen([]string{"--out", filepath.Join("docs", "new.key")})
	})
	if code != 2 || out != "" {
		t.Errorf("keygen exit=%d, public key emitted=%t; want refusal", code, out != "")
	}
	if _, err := os.Lstat(filepath.Join(root, "docs", "new.key")); !os.IsNotExist(err) {
		t.Fatalf("protected key target must remain absent, lstat error=%v", err)
	}
}

func TestCmdKeygenAllowsUnpublishedDestinations(t *testing.T) {
	for _, tc := range []struct {
		name            string
		alias, cwdAlias bool
	}{
		{"relative leaf", false, false},
		{"external parent alias", true, false},
		{"parent path from aliased cwd", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := signProject(t)
			t.Chdir(root)
			keyPath := "poolboy.key"
			target := filepath.Join(root, keyPath)
			if tc.alias {
				external := t.TempDir()
				alias := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(external, alias); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				keyPath = filepath.Join(alias, "poolboy.key")
				target = filepath.Join(external, "poolboy.key")
			}
			if tc.cwdAlias {
				external, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				target = filepath.Join(external, "poolboy.key")
				keyPath, err = filepath.Rel(root, target)
				if err != nil {
					t.Fatal(err)
				}
				alias := filepath.Join(t.TempDir(), "project")
				if err := os.Symlink(root, alias); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				t.Chdir(alias)
				t.Setenv("PWD", alias)
			}
			out, code := capture(t, func() int { return cmdKeygen([]string{"--out", keyPath}) })
			if code != 0 {
				t.Fatalf("keygen exit=%d, want 0", code)
			}
			priv, err := sign.LoadSeedFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(out) != base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)) {
				t.Fatal("printed public key does not match the persisted private key")
			}
			out, code = capture(t, func() int { return cmdKeygen([]string{"--out", keyPath}) })
			if code != 2 || out != "" {
				t.Fatalf("overwrite without --force exit=%d, public key emitted=%t", code, out != "")
			}
			after, err := sign.LoadSeedFile(target)
			if err != nil || !priv.Equal(after) {
				t.Fatal("refused overwrite changed the existing private key")
			}
		})
	}
}

func TestCmdKeygenUsesFilesystemDirectoryIdentity(t *testing.T) {
	for _, dir := range []string{"docs", "dist"} {
		t.Run(dir, func(t *testing.T) {
			root := signProject(t)
			t.Chdir(root)
			protected, err := os.Stat(filepath.Join(root, dir))
			if err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(root, strings.ToUpper(dir))
			if err := os.Mkdir(alias, 0o755); err != nil && !os.IsExist(err) {
				t.Fatal(err)
			}
			aliased, err := os.Stat(alias)
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(alias, "new.key")
			out, code := capture(t, func() int { return cmdKeygen([]string{"--out", target}) })
			if os.SameFile(protected, aliased) {
				if code != 2 || out != "" {
					t.Fatalf("aliased destination exit=%d, public key emitted=%t; want refusal", code, out != "")
				}
				if _, err := os.Lstat(target); !os.IsNotExist(err) {
					t.Fatalf("protected key target must remain absent, lstat error=%v", err)
				}
				return
			}
			if code != 0 {
				t.Fatalf("distinct destination exit=%d, want 0", code)
			}
			priv, err := sign.LoadSeedFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(out) != base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)) {
				t.Fatal("printed public key does not match the persisted private key")
			}
		})
	}
}

func TestCmdSignSignsPublicationWithKeyAndEnv(t *testing.T) {
	t.Cleanup(func() {
		rootDir = ""
	})
	rootDir = ""
	root := signProject(t)
	t.Chdir(root)

	seed, _, err := sign.GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "poolboy.key")
	if err := sign.WriteSeedFile(keyPath, seed, false); err != nil {
		t.Fatal(err)
	}

	out, code := capture(t, func() int { return cmdSign([]string{"--key", keyPath}) })
	if code != 0 || !strings.Contains(out, "signed publication:") {
		t.Fatalf("sign --key exit=%d out=%q", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, "dist", sign.SigName)); err != nil {
		t.Fatalf("signature not written: %v", err)
	}

	// Reset graph signing artifacts and sign again through the environment seed.
	t.Setenv("POOLBOY_SIGNING_KEY", base64.StdEncoding.EncodeToString(seed))
	out, code = capture(t, func() int { return cmdSign(nil) })
	if code != 0 || !strings.Contains(out, "signed publication:") {
		t.Fatalf("sign via env exit=%d out=%q", code, out)
	}
}

func TestCmdSignErrorContracts(t *testing.T) {
	t.Cleanup(func() {
		rootDir = ""
	})
	rootDir = ""
	root := signProject(t)
	t.Chdir(root)
	t.Setenv("POOLBOY_SIGNING_KEY", "")
	_ = os.Unsetenv("POOLBOY_SIGNING_KEY")

	cases := []struct {
		name string
		args []string
	}{
		{"missing key", nil},
		{"bad key path", []string{"--key", filepath.Join(t.TempDir(), "nope.key")}},
		{"positional", []string{"extra"}},
	}
	for _, c := range cases {
		if _, code := capture(t, func() int { return cmdSign(c.args) }); code != 2 {
			t.Fatalf("%s: sign exit=%d, want 2", c.name, code)
		}
	}

	// Discovery failure: no project at the working directory.
	t.Chdir(t.TempDir())
	if _, code := capture(t, func() int {
		return cmdSign([]string{"--key", filepath.Join(t.TempDir(), "nope.key")})
	}); code != 2 {
		t.Fatalf("sign outside project exit=%d, want 2", code)
	}
}

func TestCmdVerifyPendingThenApproved(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	signedPublication(t, dir, newPrivateKey(t), "first")

	out, code := capture(t, func() int { return cmdVerify([]string{dir, "--tofu"}) })
	if code != 1 || !strings.Contains(out, "manifest: sha256:") {
		t.Fatalf("pending verify exit=%d out=%q", code, out)
	}

	candidate, err := sign.PrepareApproval(dir, sign.VerifyOptions{TOFU: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := sign.CommitApproval(candidate, sign.VerifyOptions{}); err != nil {
		t.Fatal(err)
	}

	out, code = capture(t, func() int { return cmdVerify([]string{dir}) })
	if code != 0 || !strings.Contains(out, "verified sha256:") {
		t.Fatalf("approved verify exit=%d out=%q", code, out)
	}
}

func TestCmdVerifyRejectsChangedPublisherAndBadTarget(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	signedPublication(t, dir, newPrivateKey(t), "first")

	candidate, err := sign.PrepareApproval(dir, sign.VerifyOptions{TOFU: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := sign.CommitApproval(candidate, sign.VerifyOptions{}); err != nil {
		t.Fatal(err)
	}

	// Re-sign with a different publisher key: a hard negative verification (code 1).
	signedPublication(t, dir, newPrivateKey(t), "first")
	if _, code := capture(t, func() int { return cmdVerify([]string{dir}) }); code != 1 {
		t.Fatalf("changed publisher verify exit=%d, want 1", code)
	}

	// Usage error and non-signature failure.
	if _, code := capture(t, func() int { return cmdVerify(nil) }); code != 2 {
		t.Fatalf("verify without target exit=%d, want 2", code)
	}
	if _, code := capture(t, func() int {
		return cmdVerify([]string{t.TempDir(), "--tofu"})
	}); code != 2 {
		t.Fatalf("verify empty dir exit=%d, want 2", code)
	}
}

func TestCmdApprovePrintsCandidateAndGuardsTerminal(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	oldStdin := os.Stdin
	stdin, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = stdin
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = stdin.Close()
		_ = writer.Close()
	})
	dir := t.TempDir()
	signedPublication(t, dir, newPrivateKey(t), "first")

	// stdin is not a terminal under `go test`, so approval refuses after printing
	// the candidate for review.
	out, code := capture(t, func() int {
		return cmdApprove([]string{dir, "--tofu", "--minimum-release-age", "72h", "--override-age"})
	})
	if code != 2 || !strings.Contains(out, "manifest: sha256:") {
		t.Fatalf("approve non-terminal exit=%d out=%q", code, out)
	}
}

func TestCmdApproveErrorContracts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	signedPublication(t, dir, newPrivateKey(t), "first")

	if _, code := capture(t, func() int { return cmdApprove(nil) }); code != 2 {
		t.Fatalf("approve without target exit=%d, want 2", code)
	}
	if _, code := capture(t, func() int {
		return cmdApprove([]string{dir, "--tofu", "--minimum-release-age", "notaduration"})
	}); code != 2 {
		t.Fatalf("approve bad age exit=%d, want 2", code)
	}
	if _, code := capture(t, func() int {
		return cmdApprove([]string{t.TempDir(), "--tofu"})
	}); code != 2 {
		t.Fatalf("approve unverifiable target exit=%d, want 2", code)
	}
	if _, code := capture(t, func() int {
		return cmdApprove([]string{dir, "--tofu", "--migrate-identity"})
	}); code != 2 {
		t.Fatalf("approve identity migration without existing record exit=%d, want 2", code)
	}
}

func TestPrintCandidateShowsTrustAndChanges(t *testing.T) {
	candidate := &sign.Candidate{
		Record: sign.TrustRecord{
			Mode:           "sigstore",
			Origin:         "https://example.test/site",
			Channel:        "stable",
			ManifestSHA256: strings.Repeat("a", 64),
			Identity:       "https://github.com/example/repo/.github/workflows/release.yml",
			SignedAt:       time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC),
		},
		Previous: &sign.TrustRecord{
			ManifestSHA256: strings.Repeat("b", 64),
			SignedAt:       time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
		},
		Added:   []string{"new.md"},
		Changed: []string{"changed.md"},
		Removed: []string{"old.md"},
	}
	out, err := captureOutput(t, func() error { return printCandidate(candidate) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"publisher mode: sigstore",
		"approved manifest: sha256:" + strings.Repeat("b", 64),
		"identity: https://github.com/example/repo/.github/workflows/release.yml",
		"added (1): new.md",
		"changed (1): changed.md",
		"removed (1): old.md",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("candidate output missing %q: %s", want, out)
		}
	}
}

func captureOutput(t *testing.T, f func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	callErr := f()
	if closeErr := w.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	os.Stdout = old
	data, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(data), callErr
}

func TestSigningCommandsRejectInvalidFlagsAndAge(t *testing.T) {
	t.Cleanup(func() { rootDir = "" })
	t.Chdir(t.TempDir())
	if _, code := capture(t, func() int { return cmdKeygen([]string{"--unknown"}) }); code != 2 {
		t.Fatalf("keygen unknown flag exit=%d, want 2", code)
	}
	if _, code := capture(t, func() int { return cmdSign([]string{"--unknown"}) }); code != 2 {
		t.Fatalf("sign unknown flag exit=%d, want 2", code)
	}

	dir := t.TempDir()
	signedPublication(t, dir, newPrivateKey(t), "first")
	if _, code := capture(t, func() int {
		return cmdApprove([]string{dir, "--tofu", "--minimum-release-age", "-1h"})
	}); code != 2 {
		t.Fatalf("approve negative age exit=%d, want 2", code)
	}
}
