package source

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/grosspoetrysystems/poolboy/bundle"
)

func TestValidProjectPath(t *testing.T) {
	for _, p := range []string{"a.go", "src/main.go", "a/b/c.txt", ".poolboyignore"} {
		if !validProjectPath(p) {
			t.Errorf("validProjectPath(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"", "/abs.go", "a\\b.go", "a//b.go", "./a.go", "../a.go", "a/../b.go", "a/./b.go", "."} {
		if validProjectPath(p) {
			t.Errorf("validProjectPath(%q) = true, want false", p)
		}
	}
}

func TestProjectRelativeBoundaries(t *testing.T) {
	root := t.TempDir()
	if got, err := projectRelative(root, "", true); err != nil || got != "." {
		t.Errorf("empty = (%q, %v), want (., nil)", got, err)
	}
	if got, err := projectRelative(root, root, true); err != nil || got != "." {
		t.Errorf("root allowDot = (%q, %v), want (., nil)", got, err)
	}
	if _, err := projectRelative(root, root, false); err == nil || !strings.Contains(err.Error(), "names project root") {
		t.Errorf("root !allowDot err = %v, want names-project-root", err)
	}
	if _, err := projectRelative(root, "../escape", false); err == nil || !strings.Contains(err.Error(), "escapes project root") {
		t.Errorf("escape err = %v, want escapes-project-root", err)
	}
	if got, err := projectRelative(root, "sub/x.go", false); err != nil || got != "sub/x.go" {
		t.Errorf("relative = (%q, %v), want (sub/x.go, nil)", got, err)
	}
}

func TestHasURLScheme(t *testing.T) {
	for _, v := range []string{"http://x", "https://a", "mailto:a", "x+y.z-1:v"} {
		if !hasURLScheme(v) {
			t.Errorf("hasURLScheme(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", ":x", "noscheme", "/usr:x", "4bad:x", "a b:x", "a/b:c"} {
		if hasURLScheme(v) {
			t.Errorf("hasURLScheme(%q) = true, want false", v)
		}
	}
}

func TestSourceType(t *testing.T) {
	cases := map[string]string{
		"Dockerfile":       "dockerfile",
		"Containerfile":    "dockerfile",
		"Makefile":         "make",
		"GNUmakefile":      "make",
		".gitignore":       "ignore",
		".poolboyignore":   "ignore",
		"a.go":             "go",
		"dir/nested/x.ts":  "typescript",
		"weird.unknownext": "other",
		"noext":            "other",
		"sub/Makefile":     "make",
	}
	for path, want := range cases {
		if got := sourceType(path); got != want {
			t.Errorf("sourceType(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestIsBinary(t *testing.T) {
	if !isBinary([]byte("ab\x00cd")) {
		t.Error("NUL within probe window must be detected as binary")
	}
	if isBinary([]byte("plain text, no NUL")) {
		t.Error("plain text must not be binary")
	}
	// A NUL beyond the probe window is not inspected, so the file reads as text.
	beyond := append(make([]byte, binaryProbeBytes+16), 0)
	for i := range beyond[:binaryProbeBytes+16] {
		beyond[i] = 'a'
	}
	if isBinary(beyond) {
		t.Error("NUL beyond probe window must not count as binary")
	}
}

func TestMarshalLock(t *testing.T) {
	if _, err := marshalLock(nil); err == nil {
		t.Error("nil inventory must error")
	}
	if _, err := marshalLock(&Inventory{Version: "9", Files: map[string]Entry{}}); err == nil || !strings.Contains(err.Error(), "unsupported inventory version") {
		t.Errorf("bad version err = %v", err)
	}
	if _, err := marshalLock(&Inventory{Files: map[string]Entry{"../x": {}}}); err == nil || !strings.Contains(err.Error(), "invalid inventory path") {
		t.Errorf("invalid path err = %v", err)
	}
	data, err := marshalLock(&Inventory{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version": "0"`) || !strings.HasSuffix(string(data), "\n") {
		t.Errorf("default marshal = %s", data)
	}
}

func TestProjectRootRejects(t *testing.T) {
	if _, err := projectRoot(nil); err == nil {
		t.Error("nil bundle must error")
	}
	if _, err := projectRoot(&bundle.Bundle{}); err == nil {
		t.Error("empty root must error")
	}
	if _, err := projectRoot(&bundle.Bundle{Root: "rel/path"}); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("relative root err = %v", err)
	}
	if _, err := projectRoot(&bundle.Bundle{Root: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Error("missing root must error")
	}
	root := t.TempDir()
	file := filepath.Join(root, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := projectRoot(&bundle.Bundle{Root: file}); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("file root err = %v", err)
	}
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := projectRoot(&bundle.Bundle{Root: link}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("symlink root err = %v", err)
	}
}

func TestStatePathsRejectsNonDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".poolboy"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := statePaths(testBundle(root), false); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Errorf("file .poolboy err = %v", err)
	}

	root2 := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root2, ".poolboy")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, err := statePaths(testBundle(root2), false); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Errorf("symlink .poolboy err = %v", err)
	}
}

func TestReadLockErrors(t *testing.T) {
	root := t.TempDir()
	b := testBundle(root)
	if _, err := ReadLock(b); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing lock err = %v, want fs.ErrNotExist", err)
	}
	dir := filepath.Join(root, ".poolboy")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, lockName)

	if err := os.WriteFile(lock, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLock(b); err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Errorf("bad json err = %v", err)
	}

	if err := os.WriteFile(lock, []byte(`{"version":"9","files":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLock(b); err == nil || !strings.Contains(err.Error(), "unsupported inventory version") {
		t.Errorf("wrong version err = %v", err)
	}

	if err := os.WriteFile(lock, []byte(`{"version":"0","files":{"../escape":{"type":"go","bytes":1,"sha256":"x"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLock(b); err == nil || !strings.Contains(err.Error(), "invalid inventory path") {
		t.Errorf("invalid path err = %v", err)
	}

	if err := os.WriteFile(lock, []byte(`{"version":"0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	inv, err := ReadLock(b)
	if err != nil || inv.Files == nil {
		t.Errorf("nil files = (%v, %v), want non-nil map", inv, err)
	}

	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "file.txt"), lock); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := ReadLock(b); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Errorf("symlink lock err = %v", err)
	}
}

func TestDriftEvidenceNoBaseline(t *testing.T) {
	root := t.TempDir()
	b := testBundle(root)
	writeTestFile(t, root, "src/main.go", "package main\n")
	if _, err := Drift(b); err == nil || !strings.Contains(err.Error(), "no baseline") {
		t.Errorf("drift without baseline err = %v", err)
	}
}

func TestAffectedInputAndCorpusErrors(t *testing.T) {
	root := t.TempDir()
	b := testBundle(root)
	writeTestFile(t, root, "docs/index.md", "---\ntype: Reference\n---\n# Index\n")

	if _, err := Affected(b, ""); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("empty source err = %v", err)
	}
	if _, err := Affected(b, "https://example.com/x"); err == nil || !strings.Contains(err.Error(), "not a URL") {
		t.Errorf("url source err = %v", err)
	}

	// A relative corpus dir is joined to the project root.
	rel := testBundle(root)
	rel.Dir = "docs"
	writeTestFile(t, root, "src/routes.go", "package routes\n")
	writeTestFile(t, root, "docs/ref.md", "---\ntype: Reference\nsources:\n  - resource: src/routes.go\n---\n# Ref\n")
	affected, err := Affected(rel, "src/routes.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(affected, ",") != "/ref.md" {
		t.Errorf("relative-dir affected = %#v", affected)
	}

	// A missing corpus directory is an error, not an empty result.
	missing := t.TempDir()
	if _, err := Affected(testBundle(missing), "src/x.go"); err == nil || !strings.Contains(err.Error(), "corpus") {
		t.Errorf("missing corpus err = %v", err)
	}

	// A corpus path that is a regular file is rejected.
	fileCorpus := t.TempDir()
	if err := os.WriteFile(filepath.Join(fileCorpus, "docs"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Affected(testBundle(fileCorpus), "src/x.go"); err == nil || !strings.Contains(err.Error(), "not a real directory") {
		t.Errorf("file corpus err = %v", err)
	}
}

func TestWriteQuarantineRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, ".poolboyignore")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := writeQuarantine(testBundle(root), []Exclusion{{Path: ".env", Reason: "environment_credentials"}})
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Errorf("symlink .poolboyignore err = %v", err)
	}
}

func TestReadIgnorePatternsRejectsSymlinkAndEscape(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "target"), filepath.Join(root, ".gitignore")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := readIgnorePatterns(root, root); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("symlink ignore err = %v", err)
	}
	if _, err := readIgnorePatterns(filepath.Dir(root), root); err == nil || !strings.Contains(err.Error(), "escapes project root") {
		t.Errorf("escape ignore dir err = %v", err)
	}
}

func TestBuildExclusionsLandingAndRenderPaths(t *testing.T) {
	root := t.TempDir()
	logo := "assets/logo.png"
	site := "site"
	b := testBundle(root)
	b.Landing.Logo = &logo
	b.Landing.SiteDir = &site
	dirs, files, err := buildExclusions(b, root)
	if err != nil {
		t.Fatal(err)
	}
	if !files["assets/logo.png"] {
		t.Errorf("landing logo not excluded: %#v", files)
	}
	if !dirs["site"] {
		t.Errorf("landing site_dir not excluded: %#v", dirs)
	}
	// Render output is corpus-relative and excluded under the corpus directory.
	if !files["docs/reference.md"] {
		t.Errorf("render output not excluded: %#v", files)
	}

	abs := testBundle(root)
	abs.Renders = []bundle.Render{{Output: "/absolute.md"}}
	if _, _, err := buildExclusions(abs, root); err == nil || !strings.Contains(err.Error(), "corpus-relative") {
		t.Errorf("absolute render output err = %v", err)
	}

	esc := testBundle(root)
	esc.Output = "../escape"
	if _, _, err := buildExclusions(esc, root); err == nil || !strings.Contains(err.Error(), "output path") {
		t.Errorf("escaping output err = %v", err)
	}
}

func TestScanRecordsGitRevision(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull,
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	writeTestFile(t, root, "src/main.go", "package main\n")
	run("add", ".")
	run("commit", "-m", "initial")

	inv, err := Scan(testBundle(root), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Revision) != 40 {
		t.Errorf("revision = %q, want 40-char commit id", inv.Revision)
	}
}

func TestScanRejectsSymlinkBaseline(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".poolboy"), 0o700); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(root, ".poolboy", lockName)
	if err := os.Symlink(filepath.Join(root, "missing-lock"), lock); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Scan(testBundle(root), true); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Errorf("symlink baseline err = %v", err)
	}
}

func TestScanSurfacesUnreadableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	root := t.TempDir()
	writeTestFile(t, root, "src/keep.go", "package keep\n")
	locked := filepath.Join(root, "src", "locked")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "src/locked/x.go", "package locked\n")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(locked, 0o755) }()

	_, err := Current(testBundle(root))
	if err == nil {
		t.Skip("filesystem allowed reading the unreadable directory")
	}
	if !strings.Contains(err.Error(), "src/locked") {
		t.Errorf("unreadable dir err = %v, want path-qualified failure", err)
	}
}
func TestSecretNameAndContentReasons(t *testing.T) {
	for name, want := range map[string]string{
		".ENV":               "environment_credentials",
		".env.local":         "environment_credentials",
		".env.example":       "",
		".env.sample":        "",
		".env.template":      "",
		".env.defaults":      "",
		"CREDENTIALS.JSON":   "credential_filename",
		"id_ED25519":         "private_key_filename",
		"tls/private.P12":    "private_key_container",
		"ordinary-secret.go": "",
		"certificate.pem":    "",
	} {
		if got := secretNameReason(name); got != want {
			t.Errorf("secretNameReason(%q) = %q, want %q", name, got, want)
		}
	}

	for _, kind := range []string{"", "RSA ", "EC ", "OPENSSH "} {
		data := "-----BEGIN " + kind + "PRIVATE KEY-----\nkey\n-----END " + kind + "PRIVATE KEY-----\n"
		if got := secretContentReason([]byte(data)); got != "private_key_block" {
			t.Errorf("secretContentReason(%q) = %q", kind, got)
		}
	}
	pgp := "-----BEGIN PGP PRIVATE KEY BLOCK-----\nkey\n-----END PGP PRIVATE KEY BLOCK-----\n"
	if got := secretContentReason([]byte(pgp)); got != "private_key_block" {
		t.Errorf("secretContentReason(PGP) = %q", got)
	}
	for _, data := range []string{
		"-----BEGIN PRIVATE KEY-----\nmissing end",
		"-----BEGIN CERTIFICATE-----\npublic\n-----END CERTIFICATE-----",
		"ordinary source",
	} {
		if got := secretContentReason([]byte(data)); got != "" {
			t.Errorf("secretContentReason(%q) = %q, want empty", data, got)
		}
	}
	if got := lowerASCII("AZ09-é"); got != "az09-é" {
		t.Errorf("lowerASCII = %q", got)
	}
	if hasSuffixASCII("short", "long") {
		t.Error("a longer suffix must not match")
	}
}

func TestFrontmatterResourcesTraversal(t *testing.T) {
	if got, err := frontmatterResources("---\ntype: note\n---\nbody\n"); err != nil || got != nil {
		t.Errorf("missing sources = %#v, %v; want nil, nil", got, err)
	}
	content := "---\n" +
		"sources:\n" +
		"  - resource: src/main.go\n" +
		"  - name: no-resource\n" +
		"  - resource: ' ./local.md '\n" +
		"  - resource: ''\n" +
		"---\nbody\n"
	got, err := frontmatterResources(content)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"src/main.go", "./local.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("resources = %#v, want %#v", got, want)
	}
	if _, err := frontmatterResources("---\nsources: [unterminated\n---\n"); err == nil {
		t.Error("malformed sources YAML must return an error")
	}
}

func TestCanonicalInputAndResourceCandidatesBoundaries(t *testing.T) {
	root := t.TempDir()
	document := filepath.Join(root, "docs", "page.md")
	if got, err := canonicalInput(root, " src/main.go "); err != nil || got != "src/main.go" {
		t.Errorf("canonical project input = (%q, %v)", got, err)
	}
	if _, err := canonicalInput(root, ""); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("empty canonical input err = %v", err)
	}
	if _, err := canonicalInput(root, "https://example.test/spec"); err == nil || !strings.Contains(err.Error(), "not a URL") {
		t.Errorf("URL canonical input err = %v", err)
	}
	if _, err := canonicalInput(root, "../../outside"); err == nil || !strings.Contains(err.Error(), "escapes project root") {
		t.Errorf("escaping canonical input err = %v", err)
	}

	for raw, want := range map[string]string{
		"src/main.go": "src/main.go",
		"./local.md":  "docs/local.md",
		"/root.md":    "root.md",
	} {
		got := resourceCandidates(root, document, raw)
		if len(got) != 1 || got[0] != want {
			t.Errorf("resourceCandidates(%q) = %#v, want [%q]", raw, got, want)
		}
	}
	for _, raw := range []string{"", "https://example.test/spec", "../../outside"} {
		if got := resourceCandidates(root, document, raw); got != nil {
			t.Errorf("resourceCandidates(%q) = %#v, want nil", raw, got)
		}
	}
}

func TestQuarantinePatternMatchesLiteralPath(t *testing.T) {
	raw := "path with[chars]*?!#name"
	pattern := gitignore.ParsePattern(quarantinePattern(raw), nil)
	if !ignored([]gitignore.Pattern{pattern}, []string{raw}, false) {
		t.Errorf("quarantine pattern does not match its literal path: %q", quarantinePattern(raw))
	}
	if ignored([]gitignore.Pattern{pattern}, []string{"path with[chars]X?!#name"}, false) {
		t.Error("quarantine pattern matched a path with a different literal character")
	}
}
func TestAffectedSkipsExcludedAndNonDocumentEntries(t *testing.T) {
	root := t.TempDir()
	b := testBundle(root)
	writeTestFile(t, root, "src/routes.go", "package routes\n")
	writeTestFile(t, root, "docs/real.md", "---\nsources:\n  - resource: src/routes.go\n---\n# Real\n")
	writeTestFile(t, root, "docs/not-markdown.txt", "sources: src/routes.go\n")
	writeTestFile(t, root, "docs/.git/hidden.md", "---\nsources:\n  - resource: src/routes.go\n---\n")
	writeTestFile(t, root, "docs/node_modules/hidden.md", "---\nsources:\n  - resource: src/routes.go\n---\n")
	if err := os.Symlink(filepath.Join(root, "docs", "real.md"), filepath.Join(root, "docs", "linked.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	affected, err := Affected(b, "src/routes.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(affected, ",") != "/real.md" {
		t.Errorf("affected = %#v, want only real document", affected)
	}
}

func TestAffectedRejectsInvalidRootAndCorpus(t *testing.T) {
	if _, err := Affected(&bundle.Bundle{Root: "relative"}, "src/main.go"); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("relative root err = %v", err)
	}

	root := t.TempDir()
	b := testBundle(root)
	b.Dir = "../outside"
	if _, err := Affected(b, "src/main.go"); err == nil || !strings.Contains(err.Error(), "corpus path") {
		t.Errorf("escaping corpus err = %v", err)
	}
}

func TestCurrentRejectsInvalidRootAndExclusions(t *testing.T) {
	if _, err := Current(&bundle.Bundle{Root: "relative"}); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("relative root err = %v", err)
	}

	root := t.TempDir()
	b := testBundle(root)
	b.Output = "../outside"
	if _, err := Current(b); err == nil || !strings.Contains(err.Error(), "output path") {
		t.Errorf("escaping output err = %v", err)
	}
}

func TestBuildExclusionsRejectsOutOfTreeInputs(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name string
		edit func(*bundle.Bundle)
		want string
	}{
		{
			name: "render template",
			edit: func(b *bundle.Bundle) {
				b.Renders = []bundle.Render{{Template: "../template.md"}}
			},
			want: "render template",
		},
		{
			name: "render data",
			edit: func(b *bundle.Bundle) {
				b.Renders = []bundle.Render{{Data: "../data.json"}}
			},
			want: "render data",
		},
		{
			name: "render output",
			edit: func(b *bundle.Bundle) {
				b.Renders = []bundle.Render{{Output: "../../rendered.md"}}
			},
			want: "render output",
		},
		{
			name: "landing logo",
			edit: func(b *bundle.Bundle) {
				logo := "../logo.png"
				b.Landing.Logo = &logo
			},
			want: "landing logo",
		},
		{
			name: "landing site directory",
			edit: func(b *bundle.Bundle) {
				siteDir := "../site"
				b.Landing.SiteDir = &siteDir
			},
			want: "landing site_dir",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := testBundle(root)
			tc.edit(b)
			if _, _, err := buildExclusions(b, root); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("buildExclusions err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestWriteLockRejectsNilInventory(t *testing.T) {
	if err := WriteLock(testBundle(t.TempDir()), nil); err == nil || !strings.Contains(err.Error(), "inventory is required") {
		t.Errorf("nil inventory err = %v", err)
	}
}

func TestScannerRejectsSymlinkAndNonRegularFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "directory")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s := &scanner{entries: make(map[string]Entry), quarantined: make(map[string]Exclusion)}
	if err := s.addFile(dir, []string{"directory"}); err != nil {
		t.Fatalf("directory addFile error = %v", err)
	}

	link := filepath.Join(root, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := s.addFile(link, []string{"link"}); err != nil {
		t.Fatalf("symlink addFile error = %v", err)
	}
	if len(s.entries) != 0 {
		t.Fatalf("non-regular files entered inventory: %#v", s.entries)
	}
}

func TestScannerPathBoundaryHelpers(t *testing.T) {
	if got := displayPath(nil); got != "." {
		t.Errorf("displayPath(nil) = %q, want .", got)
	}
	if got := displayPath([]string{"src", "main.go"}); got != "src/main.go" {
		t.Errorf("displayPath = %q", got)
	}
	for _, tc := range []struct {
		rel  []string
		name string
		want bool
	}{
		{nil, "anything", false},
		{[]string{"build"}, "build", true},
		{[]string{"src"}, "vendor", true},
		{[]string{"src"}, "ordinary", false},
	} {
		if got := (&scanner{}).skipDir(tc.rel, tc.name); got != tc.want {
			t.Errorf("skipDir(%v, %q) = %v, want %v", tc.rel, tc.name, got, tc.want)
		}
	}
}

func TestCurrentPropagatesIgnoreFileErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "missing-ignore"), filepath.Join(root, ".gitignore")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Current(testBundle(root)); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("Current symlink ignore err = %v", err)
	}
}

func TestScanAndDriftRejectMalformedBaseline(t *testing.T) {
	root := t.TempDir()
	b := testBundle(root)
	if err := os.Mkdir(filepath.Join(root, ".poolboy"), 0o700); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(root, ".poolboy", lockName)
	if err := os.WriteFile(lock, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(b, true); err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Errorf("accepting scan malformed baseline err = %v", err)
	}
	if _, err := Drift(b); err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Errorf("drift malformed baseline err = %v", err)
	}
}

func TestScanPropagatesCurrentError(t *testing.T) {
	root := t.TempDir()
	b := testBundle(root)
	b.Output = "../outside"
	if _, err := Scan(b, false); err == nil || !strings.Contains(err.Error(), "output path") {
		t.Errorf("scan output error = %v", err)
	}
	root = t.TempDir()
	b = testBundle(root)
	b.Dir = "../outside"
	if _, err := Scan(b, false); err == nil || !strings.Contains(err.Error(), "corpus path") {
		t.Errorf("scan corpus error = %v", err)
	}
}
