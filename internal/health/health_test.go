package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grosspoetrysystems/poolboy/bundle"
	"github.com/grosspoetrysystems/poolboy/index"
	"github.com/grosspoetrysystems/poolboy/internal/source"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInspectReportsDeterministicDocumentationSignals(t *testing.T) {
	root := t.TempDir()
	b := &bundle.Bundle{Root: root, Dir: filepath.Join(root, "docs"), Output: "dist", IgnoreOrphans: []string{"parked.md"}}
	writeFile(t, root, "src/changed.go", "before\n")
	writeFile(t, root, "src/untracked.go", "present\n")
	writeFile(t, root, "src/removed.go", "present\n")
	writeFile(t, root, "docs/index.md", "---\nokf_version: \"0.1\"\nsources:\n  - resource: src/changed.go\n---\n# Home\n[grounded](/grounded.md)\n[external](/external.md)\n")
	writeFile(t, root, "docs/grounded.md", "---\ntype: note\nsources:\n  - resource: src/changed.go\n---\n# Grounded\n")
	writeFile(t, root, "docs/external.md", "---\ntype: note\nsources:\n  - resource: https://example.test/spec\n---\n# External\n")
	writeFile(t, root, "docs/no-sources.md", "---\ntype: note\n---\n# No sources\n")
	writeFile(t, root, "docs/untracked.md", "---\ntype: note\nsources:\n  - resource: src/untracked.go\n---\n# Untracked\n")
	writeFile(t, root, "docs/missing.md", "---\ntype: note\nsources:\n  - resource: src/removed.go\n---\n# Missing\n")
	writeFile(t, root, "docs/parked.md", "---\ntype: note\nsources:\n  - resource: src/changed.go\n---\n# Parked\n")

	baseline, err := source.Current(b)
	if err != nil {
		t.Fatal(err)
	}
	delete(baseline.Files, "src/untracked.go")
	writeFile(t, root, "src/changed.go", "after\n")
	if err := os.Remove(filepath.Join(root, "src/removed.go")); err != nil {
		t.Fatal(err)
	}
	current, err := source.Current(b)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := index.Build(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Entries) != 7 {
		t.Fatalf("indexed %d entries: %#v", len(idx.Entries), idx.Entries)
	}

	items, err := Inspect(b, baseline, current)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(items))
	for i, item := range items {
		got[i] = strings.Join([]string{item.Document, item.Signal, item.Source, item.Status}, "|")
	}
	want := []string{
		"/grounded.md|source_changed|src/changed.go|finding",
		"/index.md|source_changed|src/changed.go|finding",
		"/missing.md|source_missing|src/removed.go|finding",
		"/missing.md|orphan||finding",
		"/no-sources.md|missing_sources||finding",
		"/no-sources.md|orphan||finding",
		"/parked.md|source_changed|src/changed.go|finding",
		"/untracked.md|source_not_in_baseline|src/untracked.go|finding",
		"/untracked.md|orphan||finding",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("health items:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestCurrentReportsUnavailableInputsWithoutMaskingFindings(t *testing.T) {
	root := t.TempDir()
	b := &bundle.Bundle{Root: root, Dir: filepath.Join(root, "docs"), Output: "dist"}
	writeFile(t, root, "docs/index.md", "---\nokf_version: \"0.1\"\n---\n# Home\n")

	items, err := Current(b)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(items))
	for i, item := range items {
		got[i] = strings.Join([]string{item.Document, item.Signal, item.Status}, "|")
	}
	want := []string{
		"|source_baseline|unavailable",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("health items:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	b.Dir = filepath.Join(root, "absent")
	current, err := source.Current(b)
	if err != nil {
		t.Fatal(err)
	}
	items, err = Inspect(b, nil, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Signal != "source_baseline" || items[0].Status != Unavailable || items[1].Signal != "corpus" || items[1].Status != Unavailable {
		t.Fatalf("missing inputs reported as %#v", items)
	}
}

func TestAcceptedScanRetainsPreAcceptHealthEvidence(t *testing.T) {
	root := t.TempDir()
	b := &bundle.Bundle{Root: root, Dir: filepath.Join(root, "docs"), Output: "dist"}
	writeFile(t, root, "src/main.go", "before\n")
	writeFile(t, root, "docs/index.md", "---\nokf_version: \"0.1\"\nsources:\n  - resource: src/main.go\n---\n# Home\n")
	if _, err := source.Scan(b, false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "src/main.go", "after\n")

	previous, current, err := source.ScanEvidence(b, true)
	if err != nil {
		t.Fatal(err)
	}
	items, err := Inspect(b, previous, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Signal != "source_changed" || items[0].Source != "src/main.go" {
		t.Fatalf("pre-accept health = %#v", items)
	}
	items, err = Current(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("post-accept health = %#v, want clean accepted baseline", items)
	}
}
