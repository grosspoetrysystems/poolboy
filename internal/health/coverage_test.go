package health

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/grosspoetrysystems/poolboy/bundle"
)

func TestResourcesFlattensSupportedShapes(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  []string
	}{
		{"map", map[string]any{"resource": "  src/a.go  "}, []string{"src/a.go"}},
		{"nested", []any{
			map[string]any{"resource": "src/a.go"},
			[]any{map[string]any{"resource": `"src/b.go"`}, 42},
		}, []string{"src/a.go", "src/b.go"}},
		{"strings", []string{"src/a.go", `src\b.go`, "  "}, []string{"src/a.go", "src/b.go"}},
		{"unsupported", 42, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resources(tc.value); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("resources(%#v) = %#v, want %#v", tc.value, got, tc.want)
			}
		})
	}
}

func TestProjectResourceFiltersExternalAndEscapingInputs(t *testing.T) {
	root := t.TempDir()
	document := filepath.Join(root, "docs", "index.md")
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"src/a.go", "src/a.go", true},
		{"/src/a.go", "src/a.go", true},
		{"https://example.test/a", "", false},
		{"%zz", "", false},
		{"../../outside.go", "", false},
		{".", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := projectResource(root, document, tc.raw)
		if got != tc.want || ok != tc.ok {
			t.Errorf("projectResource(%q) = %q, %v; want %q, %v", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSignalOrderIncludesUnknownSignals(t *testing.T) {
	want := map[string]int{
		"missing_sources":        0,
		"source_missing":         1,
		"source_not_in_baseline": 2,
		"source_changed":         3,
		"orphan":                 4,
		"unreachable":            5,
		"source_baseline":        6,
		"corpus":                 6,
	}
	for signal, expected := range want {
		if got := signalOrder(signal); got != expected {
			t.Errorf("signalOrder(%q) = %d, want %d", signal, got, expected)
		}
	}
}

func TestInspectReportsUnavailableCorpusWhenDirectoryMissing(t *testing.T) {
	root := t.TempDir()
	b := &bundle.Bundle{Root: root, Dir: filepath.Join(root, "missing-docs"), Output: "dist"}
	items, err := Inspect(b, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Signal != "source_baseline" || items[0].Status != Unavailable || items[1].Signal != "corpus" || items[1].Status != Unavailable {
		t.Fatalf("unavailable corpus = %#v", items)
	}
}

func TestCurrentReportsUnreadableBaseline(t *testing.T) {
	root := t.TempDir()
	b := &bundle.Bundle{Root: root, Dir: filepath.Join(root, "docs"), Output: "dist"}
	if err := os.MkdirAll(filepath.Join(root, ".poolboy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".poolboy", "sources.lock.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Current(b); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("unreadable source baseline error = %v", err)
	}
}
