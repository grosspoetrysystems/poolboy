package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdCheckboxesDoneAndAllFilters(t *testing.T) {
	rootDir = ""
	dir := writeBundle(t)
	guide := filepath.Join(dir, "guide.md")
	content, err := os.ReadFile(guide)
	if err != nil {
		t.Fatal(err)
	}
	content = append(content, []byte("- [x] already done\n")...)
	if err := os.WriteFile(guide, content, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	out, code := capture(t, func() int { return cmdCheckboxes([]string{"--done", "--format", "json"}) })
	if code != 0 || !strings.Contains(out, "already done") || strings.Contains(out, "try the CLI") {
		t.Fatalf("done checkboxes=%q code=%d", out, code)
	}
	out, code = capture(t, func() int { return cmdCheckboxes([]string{"--all", "--format", "json"}) })
	if code != 0 || !strings.Contains(out, "already done") || !strings.Contains(out, "try the CLI") {
		t.Fatalf("all checkboxes=%q code=%d", out, code)
	}
	if _, code = capture(t, func() int {
		return cmdCheckboxes([]string{"guide.md", "flat.md"})
	}); code != 2 {
		t.Fatalf("two checkbox targets exit=%d, want 2", code)
	}
}

func TestCmdUnresolvedAndOrphansReportEntries(t *testing.T) {
	rootDir = ""
	dir := writeBundle(t)
	indexPath := filepath.Join(dir, "index.md")
	index, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	index = append(index, []byte("\n[missing](/missing.md)\n")...)
	if err := os.WriteFile(indexPath, index, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	out, code := capture(t, func() int { return cmdUnresolved([]string{"--format", "json"}) })
	if code != 0 || !strings.Contains(out, "missing.md") {
		t.Fatalf("unresolved=%q code=%d", out, code)
	}
	out, code = capture(t, func() int { return cmdOrphans([]string{"--format", "json"}) })
	if code != 0 || !strings.Contains(out, "flat.md") {
		t.Fatalf("orphans=%q code=%d", out, code)
	}
	if _, code = capture(t, func() int { return cmdBacklinks([]string{"missing.md"}) }); code != 2 {
		t.Fatalf("backlinks missing target exit=%d, want 2", code)
	}
}

func TestCmdTidyConvertsWikilinks(t *testing.T) {
	rootDir = ""
	dir := writeBundle(t)
	path := filepath.Join(dir, "wiki.md")
	if err := os.WriteFile(path, []byte("---\ntype: note\n---\nSee [[guide]].\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	out, code := capture(t, func() int { return cmdTidy([]string{"--wikilinks"}) })
	if code != 0 || !strings.Contains(out, "convert wikilink") {
		t.Fatalf("wikilink tidy=%q code=%d", out, code)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "[guide](./guide.md)") {
		t.Fatalf("wikilink was not converted: %q", body)
	}
}

func TestCommandsReportProjectDiscoveryErrors(t *testing.T) {
	t.Cleanup(func() { rootDir = "" })
	t.Chdir(t.TempDir())

	for _, tc := range []struct {
		name string
		fn   func([]string) int
		args []string
	}{
		{"tags", cmdTags, nil},
		{"properties", cmdProperties, nil},
		{"property", cmdProperty, []string{"type"}},
		{"checkboxes", cmdCheckboxes, nil},
		{"unresolved", cmdUnresolved, nil},
		{"orphans", cmdOrphans, nil},
		{"check", cmdCheck, nil},
		{"tidy", cmdTidy, nil},
		{"read", cmdRead, []string{"index.md"}},
		{"outline", cmdOutline, []string{"index.md"}},
		{"search", cmdSearch, []string{"query"}},
		{"table", cmdTable, []string{"index.md"}},
		{"move", cmdMove, []string{"a.md", "b.md"}},
		{"links", cmdLinks, []string{"index.md"}},
		{"backlinks", cmdBacklinks, []string{"index.md"}},
	} {
		if _, code := capture(t, func() int { return tc.fn(tc.args) }); code != 2 {
			t.Errorf("%s exit=%d, want 2", tc.name, code)
		}
	}
}
