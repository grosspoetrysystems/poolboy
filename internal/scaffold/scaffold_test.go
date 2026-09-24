package scaffold

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/grosspoetrysystems/poolboy/parse"
)

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	written, err := Write(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	// the ignore file lands as .gitignore, never a bare "gitignore"
	if slices.Contains(written, "gitignore") || !slices.Contains(written, ".gitignore") {
		t.Errorf("written = %v (want .gitignore, not gitignore)", written)
	}
	for _, f := range []string{"poolboy.toml", "landing.example.toml", ".gitignore", "docs/index.md", "docs/getting-started.md", "templates/example.md.knap", "data/example.json"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	toml, _ := os.ReadFile(filepath.Join(dir, "poolboy.toml"))
	if !strings.Contains(string(toml), "corpus = \"docs\"") || !strings.Contains(string(toml), "[[render]]") {
		t.Errorf("scaffolded poolboy.toml should describe docs and a render:\n%s", toml)
	}
	if slices.Contains(written, "CLAUDE.md") {
		t.Errorf("docs scaffold should not emit generic agent files: %v", written)
	}
	// a non-empty target is refused without force, allowed with it
	if _, err := Write(dir, false); err == nil {
		t.Errorf("re-write without force should error")
	}
	if _, err := Write(dir, true); err != nil {
		t.Errorf("force re-write: %v", err)
	}
}

// A lone .git directory does not make the target "non-empty": a freshly
// `git init`'d (or empty-cloned) repo is a normal init target.
func TestWriteToleratesGitDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(dir, false); err != nil {
		t.Errorf("init into a .git-only dir should succeed without force: %v", err)
	}

	// .git alongside real content still requires --force
	dir2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir2, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "notes.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(dir2, false); err == nil {
		t.Errorf("init into a dir with .git + content should still require force")
	}
}

// TestScaffoldIsOKFConformant locks the OKF v0.2 MUST-level rules on the docs
// project `poolboy init` emits.
func TestScaffoldIsOKFConformant(t *testing.T) {
	assertOKFConformant(t)
}

func assertOKFConformant(t *testing.T) {
	dir := t.TempDir()
	if _, err := Write(dir, false); err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fm, _ := parse.Frontmatter(string(raw))
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		switch d.Name() {
		case "index.md", "log.md":
			if rel == filepath.ToSlash(filepath.Join("docs", "index.md")) {
				if v := parse.String(fm, "okf_version"); v != "0.2" {
					t.Errorf("docs/index.md okf_version = %q, want \"0.2\"", v)
				}
				if len(fm) != 1 {
					t.Errorf("docs/index.md frontmatter must carry only okf_version, got %v", fm)
				}
			} else if len(fm) != 0 {
				t.Errorf("%s is reserved and must have no frontmatter", rel)
			}
		default:
			if parse.String(fm, "type") == "" {
				t.Errorf("%s: missing required non-empty type", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
