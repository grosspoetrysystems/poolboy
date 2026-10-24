package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEntryDiskBoundaries(t *testing.T) {
	idx := build(t, map[string]string{
		"a.md": "---\ntype: note\ntimestamp: 2024-01-02T03:04:05Z\n---\nbody\n",
	})
	e, err := idx.Resolve("/a.md")
	if err != nil {
		t.Fatal(err)
	}
	wantTime, _ := time.Parse(time.RFC3339, "2024-01-02T03:04:05Z")
	if got := e.SortTime(); !got.Equal(wantTime) {
		t.Errorf("timestamp SortTime = %v, want %v", got, wantTime)
	}
	if raw, err := e.Raw(); err != nil || !strings.Contains(raw, "timestamp:") {
		t.Errorf("Raw = (%q, %v)", raw, err)
	}
	if body, err := e.Body(); err != nil || body != "body\n" {
		t.Errorf("Body = (%q, %v), want body", body, err)
	}

	// An invalid timestamp falls back to the current file mtime, while malformed
	// frontmatter is surfaced by Body instead of being silently returned.
	e.fm["timestamp"] = "not-a-timestamp"
	mtime := time.Date(2022, 6, 7, 8, 9, 10, 123456789, time.UTC)
	if err := os.Chtimes(e.abs, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(e.abs)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.SortTime(); !got.Equal(info.ModTime()) {
		t.Errorf("mtime SortTime = %v, want %v", got, info.ModTime())
	}
	if err := os.WriteFile(e.abs, []byte("---\ntype: [\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Body(); err == nil || !strings.Contains(err.Error(), "invalid YAML") {
		t.Errorf("malformed Body err = %v", err)
	}

	if err := os.Remove(e.abs); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Raw(); err == nil {
		t.Error("Raw on a removed entry must fail")
	}
	if _, err := e.Body(); err == nil {
		t.Error("Body on a removed entry must fail")
	}
	if got := e.SortTime(); !got.IsZero() {
		t.Errorf("missing-file SortTime = %v, want zero", got)
	}
}

func TestIndexResourceSpellingBoundaries(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "docs", "page.md")
	target := filepath.Join(root, "lib", "api.md")
	if got := sourceResourceValues([]any{
		map[string]any{"resource": "root.md"},
		[]any{map[string]any{"resource": "nested.md"}, "ignored"},
	}); strings.Join(got, ",") != "root.md,nested.md" {
		t.Errorf("sourceResourceValues = %#v", got)
	}
	for raw, want := range map[string]string{
		"./api.md":  "../lib/api.md",
		"../api.md": "../lib/api.md",
		"/api.md":   "/lib/api.md",
		"api.md":    "lib/api.md",
	} {
		if got := spellResource(raw, from, target, root); got != want {
			t.Errorf("spellResource(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := relativeResource(from, filepath.Join(root, "docs", "other.md")); got != "./other.md" {
		t.Errorf("same-directory relativeResource = %q", got)
	}
	if got := resourceRelative(root, target); got != "lib/api.md" {
		t.Errorf("resourceRelative = %q", got)
	}
	for _, value := range []string{"http://example.test/x", "mailto:someone@example.test", "x+y.z-1:v"} {
		if !hasResourceScheme(value) {
			t.Errorf("hasResourceScheme(%q) = false", value)
		}
	}
	for _, value := range []string{"", ":x", "4bad:x", "a/b:c", "plain"} {
		if hasResourceScheme(value) {
			t.Errorf("hasResourceScheme(%q) = true", value)
		}
	}
	if got := stripAnchor("/docs/page.md#intro"); got != "/docs/page.md" {
		t.Errorf("stripAnchor = %q", got)
	}
	if got := anchorOf("/docs/page.md#intro"); got != "#intro" {
		t.Errorf("anchorOf = %q", got)
	}
	if got := anchorOf("/docs/page.md"); got != "" {
		t.Errorf("anchorOf without anchor = %q", got)
	}
}

func TestSafeMovePathBoundaries(t *testing.T) {
	root := t.TempDir()
	if err := safeMovePath(root, root, "destination"); err != nil {
		t.Errorf("bundle root should be allowed: %v", err)
	}
	if err := safeMovePath(root, filepath.Join(root, "new", "file.md"), "destination"); err != nil {
		t.Errorf("missing destination leaf should be allowed: %v", err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside.md")
	if err := safeMovePath(root, outside, "destination"); err == nil || !strings.Contains(err.Error(), "escapes the bundle") {
		t.Errorf("outside destination err = %v", err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := safeMovePath(root, filepath.Join(link, "file.md"), "destination"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("symlink destination err = %v", err)
	}
}

func TestWriteHelperBoundaries(t *testing.T) {
	for _, value := range []string{"", " leading", "trailing ", "a:b", "#tag", "[x]", "1.2", "true", "-item", "?item"} {
		if !needsQuote(value) {
			t.Errorf("needsQuote(%q) = false", value)
		}
	}
	for _, value := range []string{"plain", "/docs/a.md", "v1-2"} {
		if needsQuote(value) {
			t.Errorf("needsQuote(%q) = true", value)
		}
	}
	for _, line := range []string{"", " # comment", "  nested: value", "- item", "no colon", ": value"} {
		if _, _, ok := rootMappingLine(line); ok {
			t.Errorf("rootMappingLine(%q) unexpectedly matched", line)
		}
	}
	if key, value, ok := rootMappingLine("key: value\r"); !ok || key != "key" || value != " value" {
		t.Errorf("rootMappingLine = (%q, %q, %v)", key, value, ok)
	}
	for _, lines := range [][]string{nil, {}, {"body"}} {
		if open, closed := frontmatterFence(lines); open != -1 || closed != -1 {
			t.Errorf("frontmatterFence(%q) = (%d, %d)", lines, open, closed)
		}
	}
	if open, closed := frontmatterFence([]string{"---", "body"}); open != 0 || closed != -1 {
		t.Errorf("unterminated frontmatterFence = (%d, %d)", open, closed)
	}
	if open, closed := frontmatterFence([]string{"---\r", "type: note", "---\r"}); open != 0 || closed != 2 {
		t.Errorf("CRLF frontmatterFence = (%d, %d)", open, closed)
	}
	if _, err := setFrontmatterValue("body\n", "status", "todo", true); err != ErrNoFrontmatter {
		t.Errorf("reserved scalar without frontmatter err = %v", err)
	}
	if _, err := setFrontmatterList("body\n", "tags", []string{"x"}, true); err != ErrNoFrontmatter {
		t.Errorf("reserved list without frontmatter err = %v", err)
	}
	if _, err := setFrontmatterValue("---\nstatus: todo\nbody\n", "status", "done", false); err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("unterminated scalar err = %v", err)
	}
	if _, err := setFrontmatterList("---\ntags: [x]\nbody\n", "tags", []string{"y"}, false); err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("unterminated list err = %v", err)
	}
	body := "plain body\n"
	if got := unsetFrontmatterValue(body, "status"); got != body {
		t.Errorf("unset without frontmatter = %q", got)
	}
}

func TestMutationsRejectRemovedEntry(t *testing.T) {
	idx := build(t, map[string]string{
		"a.md": "---\ntype: task\nstatus: todo\n---\nbody\n",
	})
	e, err := idx.Resolve("/a.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetFields(nil); err == nil || !strings.Contains(err.Error(), "no fields") {
		t.Errorf("empty SetFields err = %v", err)
	}
	if err := os.Remove(e.abs); err != nil {
		t.Fatal(err)
	}
	operations := []struct {
		name string
		run  func() error
	}{
		{"SetField", func() error { return e.SetField("status", "done") }},
		{"SetFieldList", func() error { return e.SetFieldList("tags", []string{"x"}) }},
		{"UnsetField", func() error { return e.UnsetField("status") }},
		{"SetCheckbox", func() error { return e.SetCheckbox(1, true) }},
	}
	for _, op := range operations {
		if err := op.run(); err == nil {
			t.Errorf("%s on a removed entry must fail", op.name)
		}
	}
}

func TestMoveRefusesSymlinkAndUnavailableSources(t *testing.T) {
	t.Run("symlink destination component", func(t *testing.T) {
		idx := build(t, map[string]string{
			"a.md": "---\ntype: note\n---\nbody\n",
		})
		link := filepath.Join(idx.Bundle.Dir, "linked")
		if err := os.Symlink(t.TempDir(), link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := idx.Move("/a.md", "/linked/moved.md", false, false); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Errorf("symlink destination err = %v", err)
		}
	})

	for _, tc := range []struct {
		name        string
		replace     func(string) error
		skipOnError bool
	}{
		{
			name: "symlink source",
			replace: func(path string) error {
				if err := os.Remove(path); err != nil {
					return err
				}
				return os.Symlink(filepath.Join(filepath.Dir(path), "target.md"), path)
			},
			skipOnError: true,
		},
		{
			name:    "missing source",
			replace: os.Remove,
		},
		{
			name: "non-regular source",
			replace: func(path string) error {
				if err := os.Remove(path); err != nil {
					return err
				}
				return os.Mkdir(path, 0o755)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx := build(t, map[string]string{
				"a.md": "---\ntype: note\n---\nbody\n",
			})
			source := filepath.Join(idx.Bundle.Dir, "a.md")
			if err := tc.replace(source); err != nil {
				if tc.skipOnError {
					t.Skipf("symlinks unavailable: %v", err)
				}
				t.Fatal(err)
			}
			if _, err := idx.Move("/a.md", "/moved.md", false, false); err == nil {
				t.Fatalf("%s move unexpectedly succeeded", tc.name)
			}
			if _, err := os.Lstat(filepath.Join(idx.Bundle.Dir, "moved.md")); !os.IsNotExist(err) {
				t.Fatalf("refused move created a destination: %v", err)
			}
		})
	}
}

func TestMoveMutationBoundaries(t *testing.T) {
	t.Run("space-preserving link rewrite", func(t *testing.T) {
		idx := build(t, map[string]string{
			"index.md":    "---\ntype: index\n---\n[page](</old file.md>)\n",
			"old file.md": "---\ntype: note\n---\nbody\n",
		})
		if _, err := idx.Move("/old file.md", "/new file.md", false, false); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(idx.Bundle.Dir, "index.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), "[page](<./new file.md>)") {
			t.Errorf("spaced destination link = %q", got)
		}
	})

	t.Run("file parent blocks destination", func(t *testing.T) {
		idx := build(t, map[string]string{
			"a.md": "---\ntype: note\n---\nbody\n",
		})
		parent := filepath.Join(idx.Bundle.Dir, "blocked")
		if err := os.WriteFile(parent, []byte("not a directory\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := idx.Move("/a.md", "/blocked/sub.md", false, false); err == nil {
			t.Fatal("move through regular-file parent unexpectedly succeeded")
		}
		if _, err := os.Stat(filepath.Join(idx.Bundle.Dir, "a.md")); err != nil {
			t.Errorf("source changed after blocked move: %v", err)
		}
	})
}
