package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeLinesHandlesNoopAndMissingTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gitignore")
	all := strings.TrimSpace(starter[".gitignore"]) + "\n"
	if err := os.WriteFile(path, []byte(all), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := mergeLines(path, starter[".gitignore"])
	if err != nil || changed {
		t.Fatalf("complete merge changed=%v err=%v", changed, err)
	}

	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err = mergeLines(path, "one\ntwo")
	if err != nil || !changed {
		t.Fatalf("newline merge changed=%v err=%v", changed, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing\none\ntwo\n" {
		t.Fatalf("merged ignore = %q", data)
	}
}

func TestWriteSkipsNonRegularGitignoreAndReportsParentErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".gitignore"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(dir, false); err != nil {
		t.Fatalf("directory .gitignore should be preserved: %v", err)
	}

	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(filepath.Join(blocked, "project"), false); err == nil {
		t.Fatal("scaffold under a file unexpectedly succeeded")
	}
}

func TestWriteReportsStarterFileWriteFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "poolboy.toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(dir, true); err == nil {
		t.Fatal("scaffold should report a starter path that is a directory")
	}
}

func TestMergeLinesReportsMissingFile(t *testing.T) {
	if changed, err := mergeLines(filepath.Join(t.TempDir(), "missing"), "entry"); err == nil || changed {
		t.Fatalf("missing merge changed=%v err=%v", changed, err)
	}
}
