package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grosspoetrysystems/poolboy/bundle"
)

func TestDiscoveryLinksUsesAdmittedFilesWithoutBaseline(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/index.md", "[remote](https://corpus.example/docs/)\n")
	write("src/projects.json", "{\n  \"docs\": \"https://source.example/project/\"\n}\n")
	write(".env", "https://secret.example/\n")
	if err := os.Symlink(filepath.Join(root, "src", "projects.json"), filepath.Join(root, "src", "alias.json")); err != nil {
		t.Fatal(err)
	}

	links, err := DiscoveryLinks(&bundle.Bundle{Root: root, Dir: "docs", Output: "dist"})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Path != "src/projects.json" || links[0].Line != 2 || links[0].URL != "https://source.example/project/" {
		t.Fatalf("links = %#v", links)
	}
}

// Discovery must inherit the inventory admission boundary exactly: an ignored,
// quarantined, binary, dependency or symlinked file is never read for links,
// so a URL that only exists inside one cannot reach the network.
func TestDiscoveryLinksExcludesIgnoredPrivateAndSymlinkedInputs(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".gitignore", "generated/\n*.log\n")
	writeTestFile(t, root, "src/app.ts", "const docs = \"https://admitted.example/docs/\"\n")
	writeTestFile(t, root, "generated/bundle.js", "https://generated.example/\n")
	writeTestFile(t, root, "src/debug.log", "https://logged.example/\n")
	writeTestFile(t, root, ".env", "ENDPOINT=https://environment.example/\n")
	writeTestFile(t, root, "src/credentials.json", "{\"url\":\"https://credential.example/\"}\n")
	writeTestFile(t, root, "src/leaked.txt", "-----BEGIN "+"PRIVATE KEY-----\nhttps://privatekey.example/\n-----END "+"PRIVATE KEY-----\n")
	writeTestFile(t, root, "src/image.bin", "\x00\x01https://binary.example/\n")
	writeTestFile(t, root, "node_modules/dep/index.js", "https://dependency.example/\n")
	if err := os.Symlink(filepath.Join(root, "src", "app.ts"), filepath.Join(root, "src", "alias.ts")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	links, err := DiscoveryLinks(&bundle.Bundle{Root: root, Dir: "docs", Output: "dist"})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Path != "src/app.ts" || links[0].Line != 1 || links[0].URL != "https://admitted.example/docs/" {
		t.Fatalf("links = %#v", links)
	}
	for _, link := range links {
		for _, excluded := range []string{
			"generated.example", "logged.example", "environment.example", "credential.example",
			"privatekey.example", "binary.example", "dependency.example",
		} {
			if strings.Contains(link.URL, excluded) {
				t.Errorf("excluded input leaked into discovery: %s", link.URL)
			}
		}
	}
}

// Discovery is read-only evidence: it reports exact file and one-based line
// provenance, skips the corpus directory, and never creates or refreshes the
// accepted source baseline.
func TestDiscoveryLinksReportsProvenanceWithoutTouchingBaseline(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/catalogue.ts", "// header\nexport const a = \"https://one.example/docs/\"\nexport const b = \"https://two.example/docs/\"\n// see https://four.example/docs/.\n")
	writeTestFile(t, root, "docs/index.md", "See [remote](https://corpus.example/docs/).\n")

	links, err := DiscoveryLinks(&bundle.Bundle{Root: root, Dir: "docs", Output: "dist"})
	if err != nil {
		t.Fatal(err)
	}
	want := []DiscoveryLink{
		{URL: "https://one.example/docs/", Path: "src/catalogue.ts", Line: 2},
		{URL: "https://two.example/docs/", Path: "src/catalogue.ts", Line: 3},
		{URL: "https://four.example/docs/", Path: "src/catalogue.ts", Line: 4},
	}
	if len(links) != len(want) {
		t.Fatalf("links = %#v, want %#v", links, want)
	}
	for i, expected := range want {
		if links[i] != expected {
			t.Errorf("link %d = %#v, want %#v", i, links[i], expected)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".poolboy")); !os.IsNotExist(err) {
		t.Errorf("discovery created the state directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".poolboy", lockName)); !os.IsNotExist(err) {
		t.Errorf("discovery wrote the accepted baseline: %v", err)
	}
}

func TestDiscoveryLinksExcludesCorpusStagingTrees(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/app.ts", "const api = \"https://api.example/v1/\"\n")
	writeTestFile(t, root, ".github/workflows/ci.yml", "run: curl https://ci.example/hook/\n")
	writeTestFile(t, root, "docs/index.md", "[remote](https://corpus.example/docs/)\n")
	writeTestFile(t, root, ".poolboy-corpus-build-live/index.md", "[remote](https://staged-corpus.example/docs/)\n")
	writeTestFile(t, root, ".poolboy-publication-live/index.md", "[remote](https://staged-publication.example/docs/)\n")
	writeTestFile(t, root, ".poolboy-output-old-live/index.md", "[remote](https://staged-output.example/docs/)\n")
	writeTestFile(t, root, "src/.poolboy-corpus-build-stale/index.md", "[remote](https://staged-corpus-nested.example/docs/)\n")
	writeTestFile(t, root, "src/.poolboy-publication-stale/index.md", "[remote](https://staged-publication-nested.example/docs/)\n")
	writeTestFile(t, root, "src/.poolboy-output-old-stale/index.md", "[remote](https://staged-output-nested.example/docs/)\n")

	links, err := DiscoveryLinks(&bundle.Bundle{Root: root, Dir: "docs", Output: "dist"})
	if err != nil {
		t.Fatal(err)
	}
	want := []DiscoveryLink{
		{URL: "https://ci.example/hook/", Path: ".github/workflows/ci.yml", Line: 1},
		{URL: "https://api.example/v1/", Path: "src/app.ts", Line: 1},
	}
	if len(links) != len(want) {
		t.Fatalf("links = %#v, want %#v", links, want)
	}
	for i, expected := range want {
		if links[i] != expected {
			t.Errorf("link %d = %#v, want %#v", i, links[i], expected)
		}
	}
}

func TestDiscoveryLinksEmitsLiteralDelimitedURLsAndSkipsInterpolation(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/notes.md", ""+
		"Plain https://keep.example/ok link.\n"+
		"Code inline (`https://back.example/tick`).\n"+
		"Quote \u201chttps://curly.example/q\u201dfollow.\n"+
		"Template https://tmpl.example/{HELICOPTER}/doc here.\n"+
		"Interp https://interp.example/${name} here.\n"+
		"Encoded https://enc.example/a%20b?q=1&r=2 ok.\n")

	links, err := DiscoveryLinks(&bundle.Bundle{Root: root, Dir: "docs", Output: "dist"})
	if err != nil {
		t.Fatal(err)
	}
	want := []DiscoveryLink{
		{URL: "https://keep.example/ok", Path: "src/notes.md", Line: 1},
		{URL: "https://back.example/tick", Path: "src/notes.md", Line: 2},
		{URL: "https://curly.example/q", Path: "src/notes.md", Line: 3},
		{URL: "https://enc.example/a%20b?q=1&r=2", Path: "src/notes.md", Line: 6},
	}
	if len(links) != len(want) {
		t.Fatalf("links = %#v, want %#v", links, want)
	}
	for i, expected := range want {
		if links[i] != expected {
			t.Errorf("link %d = %#v, want %#v", i, links[i], expected)
		}
	}
}
