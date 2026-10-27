package compiler

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grosspoetrysystems/poolboy/bundle"
)

// --- graph.go pure link validation -----------------------------------------

func TestValidateFragment(t *testing.T) {
	cases := []struct {
		target  string
		wantErr bool
	}{
		{"doc.md", false},         // no fragment
		{"doc.md#section", false}, // valid fragment
		{"doc.md#", false},        // empty fragment
		{"doc.md#a\nb", true},     // newline in fragment
		{"doc.md#a\rb", true},     // carriage return in fragment
		{"doc.md#a\xffb", true},   // invalid UTF-8 fragment
	}
	for _, c := range cases {
		err := validateFragment("from.md", c.target)
		if (err != nil) != c.wantErr {
			t.Errorf("validateFragment(%q) err=%v, wantErr=%v", c.target, err, c.wantErr)
		}
	}
}

func TestValidateExternalURL(t *testing.T) {
	for _, scheme := range []string{"javascript", "data", "file", "vbscript", "about", "blob", "JavaScript"} {
		if err := validateExternalURL("from.md", scheme+":payload"); err == nil {
			t.Errorf("scheme %q should be rejected", scheme)
		}
	}
	for _, ok := range []string{"https://example.com", "http://example.com", "mailto:a@b.c"} {
		if err := validateExternalURL("from.md", ok); err != nil {
			t.Errorf("scheme %q should be allowed: %v", ok, err)
		}
	}
}

func TestValidateRawInternalTarget(t *testing.T) {
	cases := []struct {
		from, target string
		wantErr      bool
	}{
		{"a.md", "#frag", false},         // fragment-only, empty path part
		{"a.md", "b.md", false},          // plain sibling
		{"a.md", "back\\slash.md", true}, // backslash
		{"a.md", "nul\x00.md", true},     // NUL byte
		{"a.md", "%zz.md", true},         // invalid percent escape
		{"a.md", "/root.md", true},       // root-relative after decode
		{"a.md", "../escape.md", true},   // traversal from depth-0 file
		{"deep/a.md", "../b.md", false},  // traversal allowed within depth
		{"a.md", "sub/./c.md", false},    // dot segment kept harmless
	}
	for _, c := range cases {
		err := validateRawInternalTarget(c.from, c.target)
		if (err != nil) != c.wantErr {
			t.Errorf("validateRawInternalTarget(%q,%q) err=%v, wantErr=%v", c.from, c.target, err, c.wantErr)
		}
	}
}

func TestRejectUnsupportedLinks(t *testing.T) {
	cases := []struct {
		body    string
		wantErr bool
	}{
		{"plain [inline](x.md)\n", false},
		{"[text][ref]\n\n[ref]: x.md\n", true}, // reference definition + reference link
		{`<a href="x.md">x</a>`, true},         // HTML href
		{"a [[wiki]] link", true},              // wikilink
		{"no links here", false},
	}
	for _, c := range cases {
		err := rejectUnsupportedLinks("doc.md", c.body)
		if (err != nil) != c.wantErr {
			t.Errorf("rejectUnsupportedLinks(%q) err=%v, wantErr=%v", c.body, err, c.wantErr)
		}
	}
}

func TestHasReferenceLink(t *testing.T) {
	if hasReferenceLink("[text][ref]") != true {
		t.Error("reference link not detected")
	}
	if hasReferenceLink("[text][ref](inline.md)") != false {
		t.Error("inline link with adjacent brackets misread as reference link")
	}
	if hasReferenceLink("no brackets") != false {
		t.Error("plain text misread as reference link")
	}
}

func TestLinkPath(t *testing.T) {
	if got := linkPath("a.md#frag"); got != "a.md" {
		t.Errorf("linkPath stripped fragment = %q", got)
	}
	if got := linkPath("a.md"); got != "a.md" {
		t.Errorf("linkPath without fragment = %q", got)
	}
}

func TestSourcesFromMarkdown(t *testing.T) {
	if got := sourcesFromMarkdown([]byte("# no frontmatter\n")); got != nil {
		t.Errorf("no frontmatter should yield nil, got %v", got)
	}
	if got := sourcesFromMarkdown([]byte("---\ntitle: X\n---\nbody\n")); got != nil {
		t.Errorf("frontmatter without sources should yield nil, got %v", got)
	}
	if got := sourcesFromMarkdown([]byte("---\nsources: scalar\n---\nbody\n")); got != nil {
		t.Errorf("non-list sources should yield nil, got %v", got)
	}
	// Non-map list entries are skipped; map entries keep only typed fields.
	doc := "---\nsources:\n  - url: https://example.com\n    trusted: true\n  - just a string\n  - {}\n---\nbody\n"
	got := sourcesFromMarkdown([]byte(doc))
	if len(got) != 1 {
		t.Fatalf("expected one clean source, got %v", got)
	}
	if got[0]["url"] != "https://example.com" || got[0]["trusted"] != true {
		t.Errorf("source fields not preserved: %v", got[0])
	}
}

func TestFrontmatterBlock(t *testing.T) {
	if got := frontmatterBlock("\ufeff---\ntitle: X\n---\n"); got != "title: X" {
		t.Errorf("BOM-prefixed block = %q", got)
	}
	if got := frontmatterBlock("no fence\n"); got != "" {
		t.Errorf("missing fence should be empty, got %q", got)
	}
	if got := frontmatterBlock("---no newline"); got != "" {
		t.Errorf("single-line should be empty, got %q", got)
	}
	if got := frontmatterBlock("---\nunterminated\n"); got != "" {
		t.Errorf("unterminated block should be empty, got %q", got)
	}
}

// --- validate.go helpers ----------------------------------------------------

func TestCleanRelative(t *testing.T) {
	cases := []struct {
		value   string
		wantErr bool
	}{
		{"", true},
		{"/abs", true},
		{"a/../b", true}, // traversal part
		{".", true},      // resolves to current dir
		{"sub/doc.md", false},
		{"./sub/doc.md", false},
	}
	for _, c := range cases {
		_, err := cleanRelative("field", c.value)
		if (err != nil) != c.wantErr {
			t.Errorf("cleanRelative(%q) err=%v, wantErr=%v", c.value, err, c.wantErr)
		}
	}
	if _, err := checkedRelative("field", "ok.md"); err != nil {
		t.Errorf("checkedRelative valid path errored: %v", err)
	}
	if _, err := checkedRelative("field", ""); err == nil {
		t.Error("checkedRelative empty should error")
	}
}

func TestSameOrWithin(t *testing.T) {
	if !sameOrWithin("/a/b", "/a/b") {
		t.Error("identical paths should be within")
	}
	if !sameOrWithin("/a", "/a/b/c") {
		t.Error("child should be within parent")
	}
	if sameOrWithin("/a/b", "/a/c") {
		t.Error("sibling should not be within")
	}
}

func TestRejectPathCollision(t *testing.T) {
	if err := rejectPathCollision("/a/one", "/b/two"); err != nil {
		t.Errorf("distinct roots collided: %v", err)
	}
	if err := rejectPathCollision("/a", "/a/nested"); err == nil {
		t.Error("overlapping roots should collide")
	}
}

func TestReservedProjectPath(t *testing.T) {
	proj := "/project"
	if !reservedProjectPath(proj, filepath.Join(proj, ".git", "x")) {
		t.Error(".git path should be reserved")
	}
	if !reservedProjectPath(proj, filepath.Join(proj, ".poolboy")) {
		t.Error(".poolboy path should be reserved")
	}
	if reservedProjectPath(proj, filepath.Join(proj, "docs", "a.md")) {
		t.Error("ordinary path wrongly reserved")
	}
	if reservedProjectPath(proj, "/elsewhere/.git") {
		t.Error("path outside project wrongly reserved")
	}
}

func TestHiddenDirectory(t *testing.T) {
	if !hiddenDirectory(".drafts/doc.md") {
		t.Error("hidden parent not detected")
	}
	if hiddenDirectory("docs/doc.md") {
		t.Error("ordinary parent flagged hidden")
	}
	if hiddenDirectory("doc.md") {
		t.Error("file with no parent flagged hidden")
	}
}

func TestCanonicalPathKeyFolding(t *testing.T) {
	// Case folding plus Unicode composition collapse to one key.
	composed := canonicalPathKey("/Café.md")
	decomposed := canonicalPathKey("/cafe\u0301.md")
	if composed != decomposed {
		t.Errorf("accent folding mismatch: %q vs %q", composed, decomposed)
	}
	if got, ok := composeLatin('n', '\u0303'); !ok || got != 'ñ' {
		t.Errorf("composeLatin n+tilde = %q,%v", got, ok)
	}
	if _, ok := composeLatin('z', '\u0301'); ok {
		t.Error("composeLatin should not compose unknown pair")
	}
}

func TestNormalizeMarkdown(t *testing.T) {
	if got := string(normalizeMarkdown([]byte("a\r\nb\rc"))); got != "a\nb\nc\n" {
		t.Errorf("line endings not normalized: %q", got)
	}
	if got := string(normalizeMarkdown([]byte(""))); got != "\n" {
		t.Errorf("empty input should gain newline: %q", got)
	}
	if got := string(normalizeMarkdown([]byte("already\n"))); got != "already\n" {
		t.Errorf("trailing newline duplicated: %q", got)
	}
}

func TestCheckContext(t *testing.T) {
	if err := checkContext(context.Background()); err != nil {
		t.Errorf("live context errored: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := checkContext(ctx); err == nil {
		t.Error("canceled context should error")
	}
}

func TestSortedPaths(t *testing.T) {
	got := sortedPaths(map[string]document{"/b.md": {}, "/a.md": {}, "/c.md": {}})
	want := []string{"/a.md", "/b.md", "/c.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("sortedPaths = %v, want %v", got, want)
	}
}

func TestExistingDir(t *testing.T) {
	dir := t.TempDir()
	if err := existingDir(dir, "root"); err != nil {
		t.Errorf("real dir errored: %v", err)
	}
	if err := existingDir(filepath.Join(dir, "missing"), "root"); err == nil {
		t.Error("missing dir should error")
	}
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := existingDir(file, "root"); err == nil {
		t.Error("file should not pass existingDir")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := existingDir(link, "root"); err == nil {
		t.Error("symlink should be rejected")
	}
}

func TestNoSymlinkPath(t *testing.T) {
	root := t.TempDir()
	if err := noSymlinkPath(root, filepath.Join(root, "a", "b.md")); err != nil {
		t.Errorf("non-existent leaf should be allowed: %v", err)
	}
	if err := noSymlinkPath(root, filepath.Join(root, "..", "escape")); err == nil {
		t.Error("escaping target should error")
	}
	sub := filepath.Join(root, "sub")
	if err := os.Symlink(root, sub); err != nil {
		t.Fatal(err)
	}
	if err := noSymlinkPath(root, filepath.Join(sub, "x.md")); err == nil {
		t.Error("symlink component should error")
	}
}

func TestReadBoundedRegular(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.md")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := readBoundedRegular(file, 1<<20, "md")
	if err != nil || string(data) != "hello" {
		t.Fatalf("read = %q, %v", data, err)
	}
	if _, err := readBoundedRegular(filepath.Join(dir, "missing"), 1<<20, "md"); err == nil {
		t.Error("missing file should error")
	}
	if _, err := readBoundedRegular(file, 2, "md"); err == nil {
		t.Error("oversize file should error")
	}
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readBoundedRegular(link, 1<<20, "md"); err == nil {
		t.Error("symlink should be rejected")
	}
	if _, err := readBoundedRegular(dir, 1<<20, "md"); err == nil {
		t.Error("directory should be rejected")
	}
}

func TestCheckDocumentBudget(t *testing.T) {
	if err := checkDocumentBudget(map[string]document{"/a.md": {data: []byte("x")}}); err != nil {
		t.Errorf("small corpus errored: %v", err)
	}
}

func TestValidateRootsBoundaries(t *testing.T) {
	newBundle := func() (*bundle.Bundle, string, string) {
		root := t.TempDir()
		corpus := filepath.Join(root, "docs")
		if err := os.MkdirAll(corpus, 0o755); err != nil {
			t.Fatal(err)
		}
		return &bundle.Bundle{Root: root, Dir: corpus, Output: "dist", Spec: "0.2"}, root, corpus
	}

	if got, err := validateRoots(nil); err == nil || got.project != "" {
		t.Errorf("nil bundle roots = %+v, %v", got, err)
	}
	if b, _, _ := newBundle(); func() error { b.Root = ""; _, err := validateRoots(b); return err }() == nil {
		t.Error("missing project root should be rejected")
	}
	if b, _, _ := newBundle(); func() error {
		b.Dir = filepath.Join(b.Root, "missing")
		_, err := validateRoots(b)
		return err
	}() == nil {
		t.Error("missing corpus should be rejected")
	}
	if b, root, _ := newBundle(); func() error {
		b.Dir = filepath.Join(filepath.Dir(root), "outside")
		_, err := validateRoots(b)
		return err
	}() == nil {
		t.Error("corpus outside project should be rejected")
	}
	if b, _, _ := newBundle(); func() error {
		b.Output = ""
		_, err := validateRoots(b)
		return err
	}() == nil {
		t.Error("missing output should be rejected")
	}
	if b, root, _ := newBundle(); func() error {
		b.Output = filepath.Join(root, "absolute")
		_, err := validateRoots(b)
		return err
	}() == nil {
		t.Error("absolute output should be rejected")
	}
	for _, output := range []string{"../outside", ".poolboy", "docs"} {
		b, _, _ := newBundle()
		b.Output = output
		if _, err := validateRoots(b); err == nil {
			t.Errorf("output %q should be rejected", output)
		}
	}

	if b, root, _ := newBundle(); func() error {
		path := filepath.Join(root, "dist")
		if err := os.WriteFile(path, []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := validateRoots(b)
		return err
	}() == nil {
		t.Error("file output should be rejected")
	}
	if b, root, _ := newBundle(); func() error {
		if err := os.Symlink(filepath.Join(root, "elsewhere"), filepath.Join(root, "dist")); err != nil {
			t.Fatal(err)
		}
		_, err := validateRoots(b)
		return err
	}() == nil {
		t.Error("symlink output should be rejected")
	}
	b, _, _ := newBundle()
	got, err := validateRoots(b)
	if err != nil || got.project == "" || got.corpus == "" || got.output == "" || got.bundle != b {
		t.Fatalf("valid roots = %+v, %v", got, err)
	}
}

func TestValidateMappingsBoundaries(t *testing.T) {
	root := t.TempDir()
	corpus := filepath.Join(root, "docs")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "template.knap"), []byte("{{ .Name }}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data.json"), []byte(`{"Name":"docs"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &bundle.Bundle{Root: root, Dir: corpus, Output: "dist", Renders: []bundle.Render{{
		Template: "template.knap", Data: "data.json", Output: "generated.md",
	}}}
	r := roots{project: root, corpus: corpus, output: filepath.Join(root, "dist"), bundle: b}
	if err := validateMappings(b, r); err != nil {
		t.Fatalf("valid mapping errored: %v", err)
	}

	reject := func(name string, mutate func(*bundle.Bundle)) {
		t.Helper()
		bundleCopy := *b
		bundleCopy.Renders = append([]bundle.Render(nil), b.Renders...)
		mutate(&bundleCopy)
		if err := validateMappings(&bundleCopy, r); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}
	reject("missing fields", func(v *bundle.Bundle) {
		v.Renders = []bundle.Render{{}}
	})
	reject("template traversal", func(v *bundle.Bundle) {
		v.Renders[0].Template = "../template.knap"
	})
	reject("data traversal", func(v *bundle.Bundle) {
		v.Renders[0].Data = "../data.json"
	})
	reject("output traversal", func(v *bundle.Bundle) {
		v.Renders[0].Output = "../generated.md"
	})
	reject("hidden output", func(v *bundle.Bundle) {
		v.Renders[0].Output = ".drafts/generated.md"
	})
	reject("non-Markdown output", func(v *bundle.Bundle) {
		v.Renders[0].Output = "generated.txt"
	})
	reject("template publication collision", func(v *bundle.Bundle) {
		v.Renders[0].Template = "dist/template.knap"
	})
	reject("data publication collision", func(v *bundle.Bundle) {
		v.Renders[0].Data = "dist/data.json"
	})
	reject("missing template", func(v *bundle.Bundle) {
		v.Renders[0].Template = "missing.knap"
	})
	reject("missing data", func(v *bundle.Bundle) {
		v.Renders[0].Data = "missing.json"
	})
	reject("ignored output", func(v *bundle.Bundle) {
		v.Ignore = []string{"generated.md"}
	})
	reject("duplicate output", func(v *bundle.Bundle) {
		v.Renders = append(v.Renders, bundle.Render{
			Template: "template.knap", Data: "data.json", Output: "GENERATED.md",
		})
	})
	t.Run("publication overlap", func(t *testing.T) {
		bundleCopy := *b
		bundleCopy.Renders = append([]bundle.Render(nil), b.Renders...)
		bundleCopy.Renders[0].Output = "dist/generated.md"
		overlapRoots := r
		overlapRoots.output = filepath.Join(corpus, "dist")
		if err := validateMappings(&bundleCopy, overlapRoots); err == nil {
			t.Error("publication overlap should be rejected")
		}
	})
	symlink := filepath.Join(corpus, "symlink.md")
	if err := os.Symlink(filepath.Join(root, "template.knap"), symlink); err != nil {
		t.Fatal(err)
	}
	reject("symlink output", func(v *bundle.Bundle) {
		v.Renders[0].Output = "symlink.md"
	})
}

// --- build.go helpers -------------------------------------------------------

func TestUTF16Length(t *testing.T) {
	if got := utf16Length("abc"); got != 3 {
		t.Errorf("ascii length = %d", got)
	}
	if got := utf16Length("a😀b"); got != 4 { // emoji is a surrogate pair
		t.Errorf("surrogate length = %d, want 4", got)
	}
}

func TestCloneDocuments(t *testing.T) {
	src := map[string]document{"/a.md": {path: "/a.md", data: []byte("x")}}
	out := cloneDocuments(src)
	out["/b.md"] = document{}
	if _, ok := src["/b.md"]; ok {
		t.Error("clone mutation leaked to source")
	}
}

func TestCandidateCollisions(t *testing.T) {
	if err := checkCandidateCollisions(map[string]document{"/A.md": {}, "/a.md": {}}); err == nil {
		t.Error("case-folded collision not detected")
	}
	if err := checkCandidateCollisions(map[string]document{"/a.md": {}, "/b.md": {}}); err != nil {
		t.Errorf("distinct docs collided: %v", err)
	}
	docs := map[string]document{"/existing.md": {}}
	if err := ensureCandidatePath(docs, "/EXISTING.md"); err == nil {
		t.Error("ensureCandidatePath missed case-folded collision")
	}
	if err := ensureCandidatePath(docs, "/new.md"); err != nil {
		t.Errorf("ensureCandidatePath false positive: %v", err)
	}
}

func TestWriteAtomicAndRestoreBackups(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c.md")
	if err := writeAtomic(nested, []byte("v1"), 0o644); err != nil {
		t.Fatalf("writeAtomic: %v", err)
	}
	if data, _ := os.ReadFile(nested); string(data) != "v1" {
		t.Fatalf("writeAtomic content = %q", data)
	}

	// restoreBackups: an edited file reverts, a newly created file is removed.
	edited := filepath.Join(dir, "edited.md")
	if err := os.WriteFile(edited, []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(dir, "created.md")
	if err := os.WriteFile(created, []byte("created"), 0o644); err != nil {
		t.Fatal(err)
	}
	backups := []fileBackup{
		{path: edited, data: []byte("original"), exists: true},
		{path: created, exists: false},
	}
	if err := restoreBackups(backups); err != nil {
		t.Fatalf("restoreBackups: %v", err)
	}
	if data, _ := os.ReadFile(edited); string(data) != "original" {
		t.Errorf("edited file not restored: %q", data)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("created file not removed: %v", err)
	}
}

func TestSwapPublicationRestoreAndCommit(t *testing.T) {
	// restore reinstates the previous publication after a swap.
	t.Run("restore", func(t *testing.T) {
		dir := t.TempDir()
		output := filepath.Join(dir, "out")
		if err := os.MkdirAll(output, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, "old.txt"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		stage := filepath.Join(dir, "stage")
		if err := os.MkdirAll(stage, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stage, "new.txt"), []byte("new"), 0o644); err != nil {
			t.Fatal(err)
		}
		sw, err := swapPublication(stage, output)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(output, "new.txt")); err != nil {
			t.Fatalf("staged content not published: %v", err)
		}
		if err := sw.restore(); err != nil {
			t.Fatalf("restore: %v", err)
		}
		if data, _ := os.ReadFile(filepath.Join(output, "old.txt")); string(data) != "old" {
			t.Errorf("previous publication not restored: %q", data)
		}
		if err := sw.restore(); err != nil { // idempotent
			t.Errorf("second restore errored: %v", err)
		}
	})

	// commit drops the backup when there is no prior publication to retain.
	t.Run("commit-fresh", func(t *testing.T) {
		dir := t.TempDir()
		output := filepath.Join(dir, "out")
		stage := filepath.Join(dir, "stage")
		if err := os.MkdirAll(stage, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stage, "new.txt"), []byte("new"), 0o644); err != nil {
			t.Fatal(err)
		}
		sw, err := swapPublication(stage, output)
		if err != nil {
			t.Fatal(err)
		}
		if err := sw.commit(); err != nil {
			t.Errorf("commit with no backup errored: %v", err)
		}
	})
	t.Run("commit-replaces previous", func(t *testing.T) {
		dir := t.TempDir()
		output := filepath.Join(dir, "out")
		if err := os.MkdirAll(output, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, "old.txt"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		stage := filepath.Join(dir, "stage")
		if err := os.MkdirAll(stage, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stage, "new.txt"), []byte("new"), 0o644); err != nil {
			t.Fatal(err)
		}
		sw, err := swapPublication(stage, output)
		if err != nil {
			t.Fatal(err)
		}
		if err := sw.commit(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(output, "old.txt")); !os.IsNotExist(err) {
			t.Errorf("committed publication retained old file: %v", err)
		}
		if data, err := os.ReadFile(filepath.Join(output, "new.txt")); err != nil || string(data) != "new" {
			t.Errorf("committed publication missing new file: %q, %v", data, err)
		}
	})

	t.Run("failed publish restores previous", func(t *testing.T) {
		dir := t.TempDir()
		output := filepath.Join(dir, "out")
		if err := os.MkdirAll(output, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, "old.txt"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := swapPublication(filepath.Join(dir, "missing-stage"), output)
		if err == nil {
			t.Fatal("missing stage should fail publication")
		}
		if data, readErr := os.ReadFile(filepath.Join(output, "old.txt")); readErr != nil || string(data) != "old" {
			t.Errorf("failed publication lost previous output: %q, %v", data, readErr)
		}
	})
}

func TestStageLedgerUnsafeState(t *testing.T) {
	t.Run("state symlink", func(t *testing.T) {
		project := t.TempDir()
		target := filepath.Join(project, "state")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(project, ".poolboy")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := stageLedger(project, []byte("{}")); err == nil {
			t.Error("state symlink should be rejected")
		}
	})
	t.Run("state file", func(t *testing.T) {
		project := t.TempDir()
		if err := os.WriteFile(filepath.Join(project, ".poolboy"), []byte("state"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := stageLedger(project, []byte("{}")); err == nil {
			t.Error("state file should not be replaced by a directory")
		}
	})
}

func TestStageLedger(t *testing.T) {
	project := t.TempDir()
	path, err := stageLedger(project, []byte("{}"))
	if err != nil {
		t.Fatalf("stageLedger: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "{}" {
		t.Errorf("staged ledger = %q", data)
	}
}

// --- ledger.go --------------------------------------------------------------

func writeLedger(t *testing.T, project, content string) {
	t.Helper()
	state := filepath.Join(project, ".poolboy")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "generated.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadLedger(t *testing.T) {
	validHash := strings.Repeat("a", 64)

	t.Run("missing yields empty", func(t *testing.T) {
		l, err := loadLedger(t.TempDir())
		if err != nil || l.Version != ledgerVersion || len(l.Files) != 0 {
			t.Fatalf("empty ledger = %+v, %v", l, err)
		}
	})

	t.Run("valid", func(t *testing.T) {
		project := t.TempDir()
		writeLedger(t, project, `{"version":"0","files":{"a.md":{"sha256":"`+validHash+`"}}}`)
		l, err := loadLedger(project)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := l.Files["a.md"]; !ok {
			t.Errorf("entry missing: %+v", l.Files)
		}
	})

	bad := map[string]string{
		"malformed json":  `{not json`,
		"multiple values": `{} {}`,
		"bad version":     `{"version":"9","files":{}}`,
		"bad path":        `{"files":{"../x.md":{"sha256":"` + validHash + `"}}}`,
		"non-md path":     `{"files":{"x.txt":{"sha256":"` + validHash + `"}}}`,
		"short hash":      `{"files":{"x.md":{"sha256":"abc"}}}`,
		"non-hex hash":    `{"files":{"x.md":{"sha256":"` + strings.Repeat("z", 64) + `"}}}`,
	}
	for name, content := range bad {
		t.Run(name, func(t *testing.T) {
			project := t.TempDir()
			writeLedger(t, project, content)
			if _, err := loadLedger(project); err == nil {
				t.Errorf("expected error for %s", name)
			}
		})
	}

	t.Run("state is a file", func(t *testing.T) {
		project := t.TempDir()
		if err := os.WriteFile(filepath.Join(project, ".poolboy"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadLedger(project); err == nil {
			t.Error("state file should error")
		}
	})
	t.Run("case collision", func(t *testing.T) {
		project := t.TempDir()
		writeLedger(t, project, `{"files":{"A.md":{"sha256":"`+validHash+`"},"a.md":{"sha256":"`+validHash+`"}}}`)
		if _, err := loadLedger(project); err == nil {
			t.Error("case-folded ledger paths should collide")
		}
	})
	t.Run("state symlink", func(t *testing.T) {
		project := t.TempDir()
		target := filepath.Join(project, "state")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(project, ".poolboy")); err != nil {
			t.Fatal(err)
		}
		if _, err := loadLedger(project); err == nil {
			t.Error("state symlink should error")
		}
	})
	t.Run("ledger symlink", func(t *testing.T) {
		project := t.TempDir()
		state := filepath.Join(project, ".poolboy")
		if err := os.Mkdir(state, 0o700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(project, "ledger.json")
		if err := os.WriteFile(target, []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(state, "generated.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := loadLedger(project); err == nil {
			t.Error("ledger symlink should error")
		}
	})
}

func TestEncodeAndDesiredLedger(t *testing.T) {
	data, err := encodeLedger(ledger{})
	if err != nil || !strings.Contains(string(data), `"files": {}`) {
		t.Fatalf("encode empty ledger = %q, %v", data, err)
	}
	l := desiredLedger([]rendered{{rel: "a.md", dataPath: "d.json", templatePath: "t.knap", data: []byte("x")}})
	entry, ok := l.Files["a.md"]
	if !ok || entry.Data != "d.json" || entry.Template != "t.knap" || entry.SHA256 == "" {
		t.Fatalf("desiredLedger entry = %+v", l.Files)
	}
}

func TestPlanLedger(t *testing.T) {
	hash := hashBytes([]byte("gen"))
	old := ledger{Files: map[string]ledgerEntry{"stale.md": {SHA256: hash}, "keep.md": {SHA256: hash}}}
	next := ledger{Files: map[string]ledgerEntry{"keep.md": {SHA256: hash}}}
	existing := map[string]document{
		"/stale.md": {data: []byte("gen")},
		"/keep.md":  {data: []byte("gen")},
	}
	stale, err := planLedger(existing, old, next)
	if err != nil {
		t.Fatal(err)
	}
	if !stale["stale.md"] || stale["keep.md"] {
		t.Errorf("stale set = %v", stale)
	}

	// Editing a tracked generated file that stays wanted is rejected.
	edited := map[string]document{"/keep.md": {data: []byte("changed")}}
	if _, err := planLedger(edited, next, next); err == nil {
		t.Error("edited generated file should be rejected")
	}

	// A render target that overwrites handwritten (untracked) Markdown is rejected.
	handwritten := map[string]document{"/keep.md": {data: []byte("hand")}}
	if _, err := planLedger(handwritten, ledger{Files: map[string]ledgerEntry{}}, next); err == nil {
		t.Error("overwriting handwritten Markdown should be rejected")
	}
	if _, err := planLedger(map[string]document{"/stale.md": {data: []byte("changed")}}, old, next); err == nil {
		t.Error("edited stale generated file should be rejected")
	}
}

func TestVerifyRenderTargets(t *testing.T) {
	newCase := func(t *testing.T) (string, rendered, ledger) {
		t.Helper()
		corpus := t.TempDir()
		data := []byte("generated\n")
		return corpus, rendered{rel: "generated.md"}, ledger{
			Files: map[string]ledgerEntry{"generated.md": {SHA256: hashBytes(data)}},
		}
	}

	t.Run("missing target is allowed", func(t *testing.T) {
		corpus, render, old := newCase(t)
		if err := verifyRenderTargets(corpus, map[string]document{}, old, []rendered{render}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("unchanged tracked target", func(t *testing.T) {
		corpus, render, old := newCase(t)
		path := filepath.Join(corpus, "generated.md")
		if err := os.WriteFile(path, []byte("generated\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		existing := map[string]document{"/generated.md": {data: []byte("generated\n")}}
		if err := verifyRenderTargets(corpus, existing, old, []rendered{render}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("edited tracked target", func(t *testing.T) {
		corpus, render, old := newCase(t)
		if err := os.WriteFile(filepath.Join(corpus, "generated.md"), []byte("changed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := verifyRenderTargets(corpus, nil, old, []rendered{render}); err == nil {
			t.Error("edited generated target should be rejected")
		}
	})
	t.Run("indexed target disagrees with disk", func(t *testing.T) {
		corpus, render, old := newCase(t)
		if err := os.WriteFile(filepath.Join(corpus, "generated.md"), []byte("generated\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		existing := map[string]document{"/generated.md": {data: []byte("different\n")}}
		if err := verifyRenderTargets(corpus, existing, old, []rendered{render}); err == nil {
			t.Error("indexed/disk disagreement should be rejected")
		}
	})
	t.Run("untracked existing target", func(t *testing.T) {
		corpus, render, _ := newCase(t)
		if err := os.WriteFile(filepath.Join(corpus, "generated.md"), []byte("generated\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := verifyRenderTargets(corpus, map[string]document{}, ledger{Files: map[string]ledgerEntry{}}, []rendered{render}); err == nil {
			t.Error("untracked target should be rejected")
		}
	})
	t.Run("invalid target forms", func(t *testing.T) {
		t.Run("directory", func(t *testing.T) {
			corpus, render, old := newCase(t)
			if err := os.Mkdir(filepath.Join(corpus, "generated.md"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := verifyRenderTargets(corpus, nil, old, []rendered{render}); err == nil {
				t.Error("directory target should be rejected")
			}
		})
		t.Run("invalid UTF-8", func(t *testing.T) {
			corpus, render, old := newCase(t)
			if err := os.WriteFile(filepath.Join(corpus, "generated.md"), []byte{0xff}, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := verifyRenderTargets(corpus, nil, old, []rendered{render}); err == nil {
				t.Error("invalid UTF-8 target should be rejected")
			}
		})
		t.Run("symlink", func(t *testing.T) {
			corpus, render, old := newCase(t)
			if err := os.Symlink(filepath.Join(corpus, "other.md"), filepath.Join(corpus, "generated.md")); err != nil {
				t.Fatal(err)
			}
			if err := verifyRenderTargets(corpus, nil, old, []rendered{render}); err == nil {
				t.Error("symlink target should be rejected")
			}
		})
	})
}

func TestRenderDocumentsBoundaries(t *testing.T) {
	t.Run("canceled before rendering", func(t *testing.T) {
		_, b, rendererPath := fixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := renderDocuments(ctx, b, rendererPath); err == nil {
			t.Error("canceled render should fail")
		}
	})
	t.Run("data must be a JSON object", func(t *testing.T) {
		for _, value := range []string{"[]", "null"} {
			_, b, rendererPath := fixture(t)
			if err := os.WriteFile(filepath.Join(b.Root, "data", "endpoints.json"), []byte(value), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := renderDocuments(context.Background(), b, rendererPath); err == nil {
				t.Errorf("data %s should be rejected", value)
			}
		}
	})
	t.Run("malformed data", func(t *testing.T) {
		_, b, rendererPath := fixture(t)
		if err := os.WriteFile(filepath.Join(b.Root, "data", "endpoints.json"), []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := renderDocuments(context.Background(), b, rendererPath); err == nil {
			t.Error("malformed JSON should be rejected")
		}
	})
	t.Run("invalid template encoding", func(t *testing.T) {
		_, b, rendererPath := fixture(t)
		if err := os.WriteFile(filepath.Join(b.Root, "templates", "endpoints.md.knap"), []byte{0xff}, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := renderDocuments(context.Background(), b, rendererPath); err == nil {
			t.Error("invalid template UTF-8 should be rejected")
		}
	})
	t.Run("template UTF-16 limit", func(t *testing.T) {
		_, b, rendererPath := fixture(t)
		data := []byte(strings.Repeat("x", maxOutputUTF16+1))
		if err := os.WriteFile(filepath.Join(b.Root, "templates", "endpoints.md.knap"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := renderDocuments(context.Background(), b, rendererPath); err == nil {
			t.Error("oversized UTF-16 template should be rejected")
		}
	})
}

func TestValidateDocumentsBoundaries(t *testing.T) {
	valid := []byte("---\nokf_version: \"0.2\"\ntype: Concept\ntitle: Valid\n---\n# Valid\n")
	if err := validateDocuments(context.Background(), map[string]document{"/valid.md": {data: valid}}); err != nil {
		t.Fatalf("valid document errored: %v", err)
	}
	invalid := map[string]document{"/invalid.md": {data: []byte("not OKF")}}
	err := validateDocuments(context.Background(), invalid)
	if err == nil || !strings.Contains(err.Error(), "invalid OKF") {
		t.Fatalf("invalid document error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := validateDocuments(ctx, map[string]document{"/valid.md": {data: valid}}); err == nil {
		t.Error("canceled document validation should fail")
	}
	if err := validateDocuments(context.Background(), nil); err != nil {
		t.Fatalf("empty document set errored: %v", err)
	}
}

// --- publication.go ---------------------------------------------------------

func TestPublicationErrorUnwrap(t *testing.T) {
	inner := errors.New("inner")
	pub := &PublicationError{Code: "X", Path: "/p", Err: inner}
	if !errors.Is(pub, inner) {
		t.Error("Unwrap did not expose inner error")
	}
	if pub.Error() != "inner" {
		t.Errorf("Error() = %q", pub.Error())
	}
}

func TestPublicationGraphPath(t *testing.T) {
	if rel, err := publicationGraphPath("/sub/a.md"); err != nil || rel != filepath.FromSlash("sub/a.md") {
		t.Errorf("valid graph path = %q, %v", rel, err)
	}
	for _, bad := range []string{"no-slash.md", "/back\\slash.md", "/frag.md#x", "/not-md.txt", "/../escape.md"} {
		if _, err := publicationGraphPath(bad); err == nil {
			t.Errorf("graph path %q should be invalid", bad)
		}
	}
}

func TestPublicationArtifactPath(t *testing.T) {
	if rel, err := publicationArtifactPath("assets/style.css"); err != nil || rel != filepath.FromSlash("assets/style.css") {
		t.Errorf("valid artifact = %q, %v", rel, err)
	}
	for _, bad := range []string{"", "/abs.css", "back\\slash.css", "has#.css", "has?.css", "has%.css", ".hidden/x.css", "graph.json", "llms.txt", "doc.md"} {
		if _, err := publicationArtifactPath(bad); err == nil {
			t.Errorf("artifact path %q should be invalid", bad)
		}
	}
}

func TestValidateExistingPublicationBasics(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	if err := validateExistingPublication(missing); err != nil {
		t.Errorf("missing publication errored: %v", err)
	}

	regular := filepath.Join(root, "regular")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateExistingPublication(regular); err == nil {
		t.Error("regular publication path should be rejected")
	}

	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := validateExistingPublication(link); err == nil {
		t.Error("symlink publication path should be rejected")
	}

	bad := func(name, graph string) {
		t.Helper()
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "graph.json"), []byte(graph), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := validateExistingPublication(dir); err == nil {
			t.Errorf("publication %s should be rejected", name)
		}
	}
	bad("missing-graph", "not json")
	bad("multiple-graphs", `{"version":"0","root":"/index.md","files":{"/index.md":{"bytes":1,"sha256":"`+strings.Repeat("a", 64)+`"}}} {}`)
	bad("incompatible-graph", `{"version":"9","root":"/index.md","files":{"/index.md":{"bytes":1,"sha256":"`+strings.Repeat("a", 64)+`"}}}`)
}

// --- landing.go -------------------------------------------------------------

func TestValidateLandingBaseURL(t *testing.T) {
	got, err := validateLandingBaseURL("https://docs.example.com/guide/")
	if err != nil || got != "https://docs.example.com/guide" {
		t.Errorf("normalized base URL = %q, %v", got, err)
	}
	for _, bad := range []string{
		"https://x/a?b",   // query
		"https://x/a#f",   // fragment
		"ftp://x/a",       // scheme
		"https://",        // no host
		"https://u:p@x/a", // userinfo
		"mailto:a@b",      // opaque
		"https://x/../a",  // traversal
		"https://x/./a",   // dot segment
	} {
		if _, err := validateLandingBaseURL(bad); err == nil {
			t.Errorf("base URL %q should be invalid", bad)
		}
	}
}

func TestValidateDownloadFilename(t *testing.T) {
	if err := validateDownloadFilename("acme-docs.zip"); err != nil {
		t.Errorf("valid filename errored: %v", err)
	}
	for _, bad := range []string{"", ".", "..", "a/b.zip", "a\\b.zip", "a?.zip", "..secret.zip", "nul\x00.zip", "docs.tar", "tab\t.zip"} {
		if err := validateDownloadFilename(bad); err == nil {
			t.Errorf("filename %q should be invalid", bad)
		}
	}
}

func TestLandingSlug(t *testing.T) {
	if got := landingSlug("  Acme Docs! v2  "); got != "acme-docs-v2" {
		t.Errorf("slug = %q", got)
	}
	if got := landingSlug("***"); got != "corpus" {
		t.Errorf("empty slug fallback = %q", got)
	}
}

func TestResolveLandingStyle(t *testing.T) {
	v := func(s string) *string { return &s }
	if _, err := resolveLandingStyle(bundle.LandingStyle{}); err != nil {
		t.Errorf("default style errored: %v", err)
	}
	bad := map[string]bundle.LandingStyle{
		"color":       {Background: v("red")},
		"font url":    {Font: v("url(x)")},
		"font braces": {Font: v("a{b}")},
		"radius form": {BorderRadius: v("4em")},
		"radius max":  {BorderRadius: v("65px")},
	}
	for name, style := range bad {
		if _, err := resolveLandingStyle(style); err == nil {
			t.Errorf("style %s should be invalid", name)
		}
	}
}

func TestLandingValueHelpers(t *testing.T) {
	v := "x"
	if landingRequired(nil, "fb") != "fb" || landingRequired(&v, "fb") != "x" {
		t.Error("landingRequired")
	}
	if landingDefault(nil, "fb") != "fb" || landingDefault(&v, "fb") != "x" {
		t.Error("landingDefault")
	}
	sp := "  y  "
	if landingValue(nil, "fb") != "fb" || landingValue(&sp, "fb") != "y" {
		t.Error("landingValue should trim")
	}
}

func TestLandingLocalPath(t *testing.T) {
	root := t.TempDir()
	if _, err := landingLocalPath(root, "assets/logo.svg", "logo"); err != nil {
		t.Errorf("valid local path errored: %v", err)
	}
	for _, bad := range []string{"http://x/logo.svg", "/abs/logo.svg", "../out.svg", ".git/logo.svg"} {
		if _, err := landingLocalPath(root, bad, "logo"); err == nil {
			t.Errorf("local path %q should be invalid", bad)
		}
	}
}

func TestResolveLandingBoundaries(t *testing.T) {
	root := t.TempDir()
	r := roots{project: root, corpus: root, output: filepath.Join(root, "dist")}

	if _, err := resolveLanding(nil, r); err == nil {
		t.Error("nil bundle should be rejected")
	}

	blankName := &bundle.Bundle{Root: root, Dir: root}
	if _, err := resolveLanding(blankName, r); err != nil {
		t.Fatalf("blank bundle name should fall back to corpus name: %v", err)
	}

	blankTitle := "  "
	b := &bundle.Bundle{Root: root, Dir: root, Name: "Docs", Landing: bundle.Landing{Title: &blankTitle}}
	if _, err := resolveLanding(b, r); err == nil {
		t.Error("blank title should be rejected")
	}

	blankPrompt := "  "
	b = &bundle.Bundle{Root: root, Dir: root, Name: "Docs", Landing: bundle.Landing{Prompt: &blankPrompt}}
	if _, err := resolveLanding(b, r); err == nil {
		t.Error("blank prompt should be rejected")
	}

	emptyBase := ""
	b = &bundle.Bundle{Root: root, Dir: root, Name: "Docs", Landing: bundle.Landing{BaseURL: &emptyBase}}
	if _, err := resolveLanding(b, r); err != nil {
		t.Fatalf("empty base URL should use no base URL: %v", err)
	}

	emptyFilename := ""
	b = &bundle.Bundle{Root: root, Dir: root, Name: "Docs", Landing: bundle.Landing{DownloadFilename: &emptyFilename}}
	if _, err := resolveLanding(b, r); err == nil {
		t.Error("empty download filename should be rejected")
	}
}

func TestReadLandingLogo(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("rest")...)
	jpeg := []byte{0xff, 0xd8, 0xff, 0x00}
	webp := append([]byte("RIFF0000WEBP"), []byte("rest")...)

	cases := []struct {
		name     string
		data     []byte
		wantMIME string
		wantErr  bool
	}{
		{"logo.svg", []byte(`<svg xmlns="x"></svg>`), "image/svg+xml", false},
		{"bad.svg", []byte(`<html></html>`), "", true},
		{"remote.svg", []byte(`<svg><image href="//evil/x"/></svg>`), "", true},
		{"logo.png", png, "image/png", false},
		{"bad.png", []byte("notpng!!"), "", true},
		{"logo.webp", webp, "image/webp", false},
		{"bad.webp", []byte("RIFF0000XXXX"), "", true},
		{"logo.jpg", jpeg, "image/jpeg", false},
		{"bad.jpg", []byte{0x00, 0x01, 0x02}, "", true},
		{"logo.gif", []byte("GIF89a"), "", true}, // unsupported extension
	}
	for _, c := range cases {
		path := write(c.name, c.data)
		_, mime, err := readLandingLogo(path)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v wantErr=%v", c.name, err, c.wantErr)
			continue
		}
		if !c.wantErr && mime != c.wantMIME {
			t.Errorf("%s: mime=%q want %q", c.name, mime, c.wantMIME)
		}
	}
}

func TestReadLandingSite(t *testing.T) {
	t.Run("valid nested assets", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "assets", "style.css"), []byte("body{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		assets, err := readLandingSite(root)
		if err != nil {
			t.Fatal(err)
		}
		if string(assets["index.html"]) != "<html>" || string(assets["assets/style.css"]) != "body{}" {
			t.Errorf("assets = %#v", assets)
		}
	})

	t.Run("missing index", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "style.css"), []byte("body{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readLandingSite(root); err == nil {
			t.Error("site without index.html should be rejected")
		}
	})

	t.Run("private path", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".private"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".private", "x.css"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readLandingSite(root); err == nil {
			t.Error("private site path should be rejected")
		}
	})

	t.Run("reserved and invalid artifacts", func(t *testing.T) {
		for name := range map[string]string{"graph.json": "reserved", "bad%.css": "invalid"} {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("index"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := readLandingSite(root); err == nil {
				t.Errorf("site artifact %q should be rejected", name)
			}
		}
	})

	t.Run("symlink and case collision", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("index"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "index.html"), filepath.Join(root, "alias.html")); err != nil {
			t.Fatal(err)
		}
		if _, err := readLandingSite(root); err == nil {
			t.Error("site symlink should be rejected")
		}

		root = t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("index"), 0o644); err != nil {
			t.Fatal(err)
		}
		a := filepath.Join(root, "A.css")
		lower := filepath.Join(root, "a.css")
		if err := os.WriteFile(a, []byte("a"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lower, []byte("b"), 0o644); err != nil {
			t.Fatal(err)
		}
		first, err := os.Stat(a)
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.Stat(lower)
		if err != nil {
			t.Fatal(err)
		}
		if os.SameFile(first, second) {
			t.Skip("case-insensitive filesystem aliases collision candidates")
		}
		if _, err := readLandingSite(root); err == nil {
			t.Error("case-folded site paths should collide")
		}
	})
}

func TestLandingArtifactCollisions(t *testing.T) {
	docs := map[string]document{"/guide.md": {data: []byte("guide")}}
	if err := checkLandingArtifactCollisions(map[string][]byte{
		"index.html": []byte("index"),
		"corpus.zip": []byte("zip"),
	}, docs); err != nil {
		t.Fatalf("valid artifacts errored: %v", err)
	}
	for name, artifacts := range map[string]map[string][]byte{
		"reserved metadata":  {"graph.json": []byte("x")},
		"invalid path":       {".private/x.css": []byte("x")},
		"document collision": {"guide.md": []byte("x")},
		"case collision":     {"A.css": []byte("a"), "a.css": []byte("b")},
	} {
		if err := checkLandingArtifactCollisions(artifacts, docs); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}
}

func TestLimitedBuffer(t *testing.T) {
	var b limitedBuffer
	b.limit = 2
	if n, err := b.Write([]byte("ab")); err != nil || n != 2 {
		t.Fatalf("first write = %d,%v", n, err)
	}
	if _, err := b.Write([]byte("c")); err == nil {
		t.Error("write past archive limit should fail")
	}
}

// --- Build-driven link and landing edges ------------------------------------

func TestBuildRejectsBrokenAndForbiddenLinks(t *testing.T) {
	t.Run("broken link", func(t *testing.T) {
		_, b, rendererPath := fixture(t)
		if err := os.WriteFile(filepath.Join(b.Dir, "broken.md"), []byte("---\ntype: Concept\n---\n[gone](nowhere.md)\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Build(context.Background(), b, rendererPath, BuildOptions{}); err == nil || !strings.Contains(err.Error(), "broken link") {
			t.Fatalf("broken link error = %v", err)
		}
	})
	t.Run("forbidden external scheme", func(t *testing.T) {
		_, b, rendererPath := fixture(t)
		if err := os.WriteFile(filepath.Join(b.Dir, "evil.md"), []byte("---\ntype: Concept\n---\n[x](javascript:alert(1))\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Build(context.Background(), b, rendererPath, BuildOptions{}); err == nil || !strings.Contains(err.Error(), "forbidden") {
			t.Fatalf("forbidden scheme error = %v", err)
		}
	})
	t.Run("root-relative link", func(t *testing.T) {
		_, b, rendererPath := fixture(t)
		if err := os.WriteFile(filepath.Join(b.Dir, "abs.md"), []byte("---\ntype: Concept\n---\n[x](/root.md)\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Build(context.Background(), b, rendererPath, BuildOptions{}); err == nil || !strings.Contains(err.Error(), "root-relative") {
			t.Fatalf("root-relative error = %v", err)
		}
	})
}

func TestBuildEmbedsConfiguredLogo(t *testing.T) {
	root, b := landingProject(t)
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("pixels")...)
	if err := os.WriteFile(filepath.Join(root, "logo.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	logo := "logo.png"
	b.Landing = bundle.Landing{Logo: &logo}
	if err := Build(context.Background(), b, "", BuildOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "data:image/png;base64,") {
		t.Fatalf("landing page missing embedded logo: %s", data)
	}
}

func TestBuildRebuildRemovesStaleGenerated(t *testing.T) {
	root, b, rendererPath := fixture(t)
	if err := Build(context.Background(), b, rendererPath, BuildOptions{}); err != nil {
		t.Fatal(err)
	}
	generated := filepath.Join(root, "docs", "reference", "endpoints.md")
	if _, err := os.Stat(generated); err != nil {
		t.Fatalf("generated file missing after first build: %v", err)
	}
	// Remove the authored link before dropping the render so the candidate corpus
	// remains valid after the stale generated file is deleted.
	if err := os.WriteFile(filepath.Join(b.Dir, "index.md"), []byte("---\nokf_version: \"0.2\"\n---\n# Fixture\n\n[Architecture](architecture.md)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.Dir, "architecture.md"), []byte("---\ntype: Concept\ntitle: Architecture\n---\n# Architecture\n\nStandalone.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b.Renders = nil
	if err := Build(context.Background(), b, rendererPath, BuildOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(generated); !os.IsNotExist(err) {
		t.Fatalf("stale generated file not removed: %v", err)
	}
}

func TestBuildInputBoundaries(t *testing.T) {
	if err := Build(context.Background(), nil, "", BuildOptions{}); err == nil {
		t.Error("nil bundle should be rejected")
	}
	t.Run("unsupported spec", func(t *testing.T) {
		_, b, rendererPath := fixture(t)
		b.Spec = "0.1"
		if err := Build(context.Background(), b, rendererPath, BuildOptions{}); err == nil || !strings.Contains(err.Error(), "unsupported Poolboy spec") {
			t.Fatalf("unsupported spec error = %v", err)
		}
	})
	t.Run("unknown settings are sorted", func(t *testing.T) {
		_, b, rendererPath := fixture(t)
		b.Unknown = []string{"zeta", "alpha"}
		err := Build(context.Background(), b, rendererPath, BuildOptions{})
		if err == nil || !strings.Contains(err.Error(), "alpha, zeta") {
			t.Fatalf("unknown settings error = %v", err)
		}
	})
	t.Run("background context", func(t *testing.T) {
		_, b, rendererPath := fixture(t)
		if err := Build(context.Background(), b, rendererPath, BuildOptions{}); err != nil {
			t.Fatalf("nil context build errored: %v", err)
		}
	})
}
func TestBuildFailureTransitions(t *testing.T) {
	t.Run("canceled before validation", func(t *testing.T) {
		_, b, rendererPath := fixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := Build(ctx, b, rendererPath, BuildOptions{}); err == nil {
			t.Error("canceled build should fail before validation")
		}
	})
	t.Run("occupied output", func(t *testing.T) {
		root, b, rendererPath := fixture(t)
		if err := os.MkdirAll(filepath.Join(root, b.Output), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, b.Output, "foreign.txt"), []byte("foreign"), 0o644); err != nil {
			t.Fatal(err)
		}
		b.Renders = nil
		if err := Build(context.Background(), b, rendererPath, BuildOptions{}); err == nil {
			t.Error("non-Poolboy output should be rejected")
		}
	})
	t.Run("missing renderer companion", func(t *testing.T) {
		root, b, _ := fixture(t)
		if err := Build(context.Background(), b, filepath.Join(root, "missing-renderer"), BuildOptions{}); err == nil {
			t.Error("missing renderer should be rejected")
		}
	})
	t.Run("relative renderer path", func(t *testing.T) {
		_, b, rendererPath := fixture(t)
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		relative, err := filepath.Rel(cwd, rendererPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := Build(context.Background(), b, relative, BuildOptions{}); err == nil || !strings.Contains(err.Error(), "must be absolute") {
			t.Errorf("relative renderer error = %v", err)
		}
	})
	t.Run("renderer process failure", func(t *testing.T) {
		root, b, _ := fixture(t)
		path := filepath.Join(root, "renderer-fail.sh")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := Build(context.Background(), b, path, BuildOptions{}); err == nil {
			t.Error("renderer process failure should abort build")
		}
	})
	t.Run("invalid rendered document", func(t *testing.T) {
		root, b, _ := fixture(t)
		path := filepath.Join(root, "renderer-invalid.sh")
		script := "#!/bin/sh\nprintf '%s\\n' '{\"ok\":true,\"output\":\"plain text\",\"warnings\":[]}'\n"
		if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := Build(context.Background(), b, path, BuildOptions{}); err == nil || !strings.Contains(err.Error(), "invalid OKF output") {
			t.Errorf("invalid renderer output error = %v", err)
		}
	})
	t.Run("invalid generated ledger", func(t *testing.T) {
		_, b, rendererPath := fixture(t)
		b.Renders = nil
		writeLedger(t, b.Root, "{not json")
		if err := Build(context.Background(), b, rendererPath, BuildOptions{}); err == nil {
			t.Error("invalid generated ledger should abort build")
		}
	})
	t.Run("corpus symlink", func(t *testing.T) {
		_, b, rendererPath := fixture(t)
		if err := os.Symlink(filepath.Join(b.Dir, "architecture.md"), filepath.Join(b.Dir, "alias.md")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		b.Renders = nil
		if err := Build(context.Background(), b, rendererPath, BuildOptions{}); err == nil {
			t.Error("corpus symlink should abort build")
		}
	})
}

func TestRenderDocumentsErrorTransitions(t *testing.T) {
	t.Run("missing template and data", func(t *testing.T) {
		_, b, rendererPath := fixture(t)
		b.Renders[0].Template = "missing.knap"
		if _, err := renderDocuments(context.Background(), b, rendererPath); err == nil {
			t.Error("missing template should fail")
		}
		_, b, rendererPath = fixture(t)
		b.Renders[0].Data = "missing.json"
		if _, err := renderDocuments(context.Background(), b, rendererPath); err == nil {
			t.Error("missing data should fail")
		}
	})
	t.Run("renderer output size", func(t *testing.T) {
		root, b, _ := fixture(t)
		path := filepath.Join(root, "renderer-large.sh")
		output := strings.Repeat("x", maxOutputUTF16+1)
		script := "#!/bin/sh\nprintf '%s\\n' '{\"ok\":true,\"output\":\"" + output + "\",\"warnings\":[]}'\n"
		if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := renderDocuments(context.Background(), b, path); err == nil {
			t.Error("oversized renderer output should fail")
		}
	})
}

func TestCollectCorpusBoundaries(t *testing.T) {
	t.Run("filters non-documents and ignored subtrees", func(t *testing.T) {
		root := t.TempDir()
		corpus := filepath.Join(root, "docs")
		output := filepath.Join(corpus, "generated")
		if err := os.MkdirAll(filepath.Join(corpus, ".private"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(output, 0o755); err != nil {
			t.Fatal(err)
		}
		write := func(path, data string) {
			t.Helper()
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write(filepath.Join(corpus, "index.md"), "# Index\n")
		write(filepath.Join(corpus, "ignored.md"), "# Ignored\n")
		write(filepath.Join(corpus, "notes.txt"), "not Markdown")
		write(filepath.Join(corpus, ".private", "hidden.md"), "# Hidden\n")
		write(filepath.Join(output, "staged.md"), "# Staged\n")
		b := &bundle.Bundle{Root: root, Dir: corpus, Ignore: []string{"ignored.md"}}
		docs, err := collectCorpus(roots{project: root, corpus: corpus, output: output, bundle: b})
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) != 1 {
			t.Fatalf("filtered corpus = %#v", docs)
		}
		if _, ok := docs["/index.md"]; !ok {
			t.Errorf("index missing from corpus: %#v", docs)
		}
	})
	t.Run("symlink is rejected", func(t *testing.T) {
		root := t.TempDir()
		corpus := filepath.Join(root, "docs")
		if err := os.MkdirAll(corpus, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(corpus, "missing.md"), filepath.Join(corpus, "link.md")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		b := &bundle.Bundle{Root: root, Dir: corpus}
		if _, err := collectCorpus(roots{project: root, corpus: corpus, bundle: b}); err == nil {
			t.Error("corpus symlink should be rejected")
		}
	})
	t.Run("invalid UTF-8 is rejected", func(t *testing.T) {
		root := t.TempDir()
		corpus := filepath.Join(root, "docs")
		if err := os.MkdirAll(corpus, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(corpus, "bad.md"), []byte{0xff}, 0o644); err != nil {
			t.Fatal(err)
		}
		b := &bundle.Bundle{Root: root, Dir: corpus}
		if _, err := collectCorpus(roots{project: root, corpus: corpus, bundle: b}); err == nil {
			t.Error("invalid UTF-8 should be rejected")
		}
	})
}

func TestMaterializeGeneratedBoundaries(t *testing.T) {
	t.Run("writes and rolls back", func(t *testing.T) {
		project := t.TempDir()
		corpus := filepath.Join(project, "docs")
		if err := os.MkdirAll(corpus, 0o755); err != nil {
			t.Fatal(err)
		}
		item := rendered{rel: "nested/generated.md", data: []byte("# Generated\n")}
		next := desiredLedger([]rendered{item})
		rollback, err := materializeGenerated(project, corpus, nil, nil, next, []rendered{item})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(corpus, "nested", "generated.md")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("generated file missing: %v", err)
		}
		if err := rollback(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("rollback left generated file: %v", err)
		}
	})
	t.Run("missing render output", func(t *testing.T) {
		project := t.TempDir()
		corpus := filepath.Join(project, "docs")
		if err := os.MkdirAll(corpus, 0o755); err != nil {
			t.Fatal(err)
		}
		next := ledger{Files: map[string]ledgerEntry{"missing.md": {SHA256: strings.Repeat("a", 64)}}}
		if _, err := materializeGenerated(project, corpus, nil, nil, next, nil); err == nil {
			t.Error("missing render output should fail")
		}
	})
	t.Run("state symlink", func(t *testing.T) {
		project := t.TempDir()
		corpus := filepath.Join(project, "docs")
		if err := os.MkdirAll(corpus, 0o755); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(project, "state")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(project, ".poolboy")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := materializeGenerated(project, corpus, nil, nil, ledger{}, nil); err == nil {
			t.Error("state symlink should fail materialization")
		}
	})
	t.Run("blocked parent", func(t *testing.T) {
		project := t.TempDir()
		corpus := filepath.Join(project, "docs")
		if err := os.MkdirAll(corpus, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(corpus, "blocked"), []byte("file"), 0o644); err != nil {
			t.Fatal(err)
		}
		item := rendered{rel: "blocked/generated.md", data: []byte("x")}
		if _, err := materializeGenerated(project, corpus, nil, nil, desiredLedger([]rendered{item}), []rendered{item}); err == nil {
			t.Error("file parent should fail materialization")
		}
	})
}

func TestValidateExistingPublicationOwned(t *testing.T) {
	makePublication := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		pub := filepath.Join(root, "dist")
		if err := os.MkdirAll(filepath.Join(pub, "assets"), 0o755); err != nil {
			t.Fatal(err)
		}
		index := []byte("# Index\n")
		llms := []byte("## Reading order\n")
		style := []byte("body{}\n")
		write := func(path string, data []byte) {
			t.Helper()
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write(filepath.Join(pub, "index.md"), index)
		write(filepath.Join(pub, "llms.txt"), llms)
		write(filepath.Join(pub, "assets", "style.css"), style)
		manifest := graph{
			Version: "0",
			Root:    "/index.md",
			LLMS:    graphArtifact{Bytes: len(llms), SHA256: hashBytes(llms)},
			Files: map[string]graphFile{
				"/index.md": {Bytes: len(index), SHA256: hashBytes(index)},
			},
			Artifacts: map[string]graphArtifact{
				"assets/style.css": {Bytes: len(style), SHA256: hashBytes(style)},
			},
		}
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(pub, "graph.json"), data)
		return pub
	}

	t.Run("complete owned publication", func(t *testing.T) {
		if err := validateExistingPublication(makePublication(t)); err != nil {
			t.Fatalf("owned publication errored: %v", err)
		}
	})
	t.Run("unknown file", func(t *testing.T) {
		pub := makePublication(t)
		if err := os.WriteFile(filepath.Join(pub, "extra.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := validateExistingPublication(pub); err == nil {
			t.Error("unknown publication file should be rejected")
		}
	})
	t.Run("tampered metadata and artifact", func(t *testing.T) {
		pub := makePublication(t)
		if err := os.WriteFile(filepath.Join(pub, "llms.txt"), []byte("tampered"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := validateExistingPublication(pub); err == nil {
			t.Error("tampered llms should be rejected")
		}
		pub = makePublication(t)
		if err := os.WriteFile(filepath.Join(pub, "assets", "style.css"), []byte("tampered"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := validateExistingPublication(pub); err == nil {
			t.Error("tampered artifact should be rejected")
		}
	})
	t.Run("missing owned files", func(t *testing.T) {
		pub := makePublication(t)
		if err := os.Remove(filepath.Join(pub, "assets", "style.css")); err != nil {
			t.Fatal(err)
		}
		if err := validateExistingPublication(pub); err == nil {
			t.Error("missing artifact should be rejected")
		}
	})
}

func TestLandingArtifactBoundaries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := buildLandingArtifacts(ctx, landingConfig{}, nil); err == nil {
		t.Error("canceled landing build should fail")
	}

	assets := map[string][]byte{"index.html": []byte("index")}
	got, err := buildLandingArtifacts(context.Background(), landingConfig{assets: assets}, map[string]document{"/guide.md": {data: []byte("# Guide\n")}})
	if err != nil {
		t.Fatalf("asset landing build errored: %v", err)
	}
	assets["index.html"][0] = 'X'
	if string(got["index.html"]) != "index" {
		t.Error("landing assets were not copied")
	}

	if _, err := renderLanding(landingConfig{title: "Docs", prompt: strings.Repeat("x", maxOutputBytes)}); err == nil {
		t.Error("oversized landing HTML should fail")
	}
	if err := checkLandingArtifactCollisions(map[string][]byte{"index.html": make([]byte, maxLandingFileBytes+1)}, nil); err == nil {
		t.Error("oversized landing artifact should fail")
	}

	root := t.TempDir()
	if _, err := readLandingSite(filepath.Join(root, "missing")); err == nil {
		t.Error("missing landing site should fail")
	}
	file := filepath.Join(root, "site")
	if err := os.WriteFile(file, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readLandingSite(file); err == nil {
		t.Error("regular landing site path should fail")
	}

	if err := writeLandingArtifacts(root, map[string][]byte{"bad%.css": []byte("x")}); err == nil {
		t.Error("invalid landing artifact path should fail")
	}
	if err := os.WriteFile(filepath.Join(root, "blocked"), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeLandingArtifacts(root, map[string][]byte{"blocked/file.css": []byte("x")}); err == nil {
		t.Error("blocked landing artifact parent should fail")
	}
}
