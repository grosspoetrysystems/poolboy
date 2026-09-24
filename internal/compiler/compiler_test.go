package compiler

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/grosspoetrysystems/poolboy/bundle"
)

func TestBuildDeterministicAndMaterializesGeneratedCorpus(t *testing.T) {
	root, b, rendererPath := fixture(t)
	if err := Build(context.Background(), b, rendererPath); err != nil {
		t.Fatal(err)
	}
	first := snapshot(t, filepath.Join(root, "dist"))
	generated, err := os.ReadFile(filepath.Join(root, "docs", "reference", "endpoints.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), "type: Reference") {
		t.Fatalf("generated Markdown was not materialized: %s", generated)
	}
	if err := Build(context.Background(), b, rendererPath); err != nil {
		t.Fatal(err)
	}
	second := snapshot(t, filepath.Join(root, "dist"))
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated builds differ:\nfirst=%v\nsecond=%v", first, second)
	}
	graph := string(second["graph.json"])
	if !strings.Contains(graph, `"/reference/endpoints.md"`) || !strings.Contains(graph, `"/index.md"`) {
		t.Fatalf("graph missing canonical nodes: %s", graph)
	}
}
func TestBuildDefaultDirectCorpusExcludesPublicationSubtree(t *testing.T) {
	root := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.md", "---\nokf_version: \"0.2\"\n---\n# Direct\n\n[Architecture](architecture.md)\n")
	write("architecture.md", "---\ntype: Concept\n---\n# Architecture\n\n[Home](index.md)\n")
	b := &bundle.Bundle{Root: root, Dir: root, Name: "Direct", Output: "dist", Spec: "0.2"}
	if err := Build(context.Background(), b, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "/dist/") {
		t.Fatalf("publication subtree leaked into graph: %s", data)
	}
}

func TestBuildIgnoresUnsupportedSyntaxInCode(t *testing.T) {
	_, b, rendererPath := fixture(t)
	code := "---\ntype: Concept\n---\n" +
		"Inline `[[render]]` is documentation.\n\n" +
		"```toml\n[[render]]\noutput = \"dist\"\n```\n\n" +
		"```markdown\n[reference][docs]\n[docs]: index.md\n<a href=\"index.md\">home</a>\n[[wikilinks]]\n```\n"
	if err := os.WriteFile(filepath.Join(b.Dir, "syntax.md"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Build(context.Background(), b, rendererPath); err != nil {
		t.Fatalf("code examples rejected: %v", err)
	}

	actual := "---\ntype: Concept\n---\n[reference][docs]\n[docs]: index.md\n"
	if err := os.WriteFile(filepath.Join(b.Dir, "actual.md"), []byte(actual), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Build(context.Background(), b, rendererPath); err == nil ||
		!strings.Contains(err.Error(), "unsupported reference-style Markdown link") {
		t.Fatalf("prose reference link error = %v", err)
	}
}

func TestBuildFailurePreservesPreviousPublication(t *testing.T) {
	root, b, rendererPath := fixture(t)
	if err := Build(context.Background(), b, rendererPath); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, filepath.Join(root, "dist"))
	bad := `#!/bin/sh
cat >/dev/null
printf '%s\n' '{"ok":true,"output":"---\ntitle: Missing type\n---\n# bad\n","warnings":[]}'
`
	if err := os.WriteFile(rendererPath, []byte(bad), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Build(context.Background(), b, rendererPath); err == nil {
		t.Fatal("invalid rendered OKF unexpectedly built")
	}
	after := snapshot(t, filepath.Join(root, "dist"))
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("failed build changed publication:\nbefore=%v\nafter=%v", before, after)
	}
}
func TestBuildRejectsHiddenGeneratedTargetWithoutOverwrite(t *testing.T) {
	_, b, rendererPath := fixture(t)
	hidden := filepath.Join(b.Dir, ".drafts", "endpoints.md")
	if err := os.MkdirAll(filepath.Dir(hidden), 0o755); err != nil {
		t.Fatal(err)
	}
	before := []byte("---\ntype: Handwritten\n---\n# owned\n")
	if err := os.WriteFile(hidden, before, 0o644); err != nil {
		t.Fatal(err)
	}
	b.Renders[0].Output = ".drafts/endpoints.md"
	if err := Build(context.Background(), b, rendererPath); err == nil || !strings.Contains(err.Error(), "hidden") {
		t.Fatalf("hidden output error = %v", err)
	}
	after, err := os.ReadFile(hidden)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("hidden handwritten output changed: %q", after)
	}
}

func TestBuildRejectsEscapingLinksBeforeNormalization(t *testing.T) {
	_, b, rendererPath := fixture(t)
	if err := os.WriteFile(filepath.Join(b.Dir, "escape.md"), []byte("---\ntype: Concept\n---\n[escape](../../outside.md)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Build(context.Background(), b, rendererPath); err == nil || !strings.Contains(err.Error(), "escapes corpus") {
		t.Fatalf("escaping link error = %v", err)
	}
}

func TestBuildRejectsSymlinkAndOverlappingOutput(t *testing.T) {
	_, b, rendererPath := fixture(t)
	if err := os.Symlink(filepath.Join(b.Dir, "index.md"), filepath.Join(b.Dir, "alias.md")); err != nil {
		t.Fatal(err)
	}
	if err := Build(context.Background(), b, rendererPath); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink error = %v", err)
	}
	_, overlap, overlapRenderer := fixture(t)
	overlap.Output = "docs"
	if err := Build(context.Background(), overlap, overlapRenderer); err == nil || !strings.Contains(err.Error(), "contains corpus") {
		t.Fatalf("overlap error = %v", err)
	}
}

func TestBuildRejectsMissingRootAndHandwrittenOverwrite(t *testing.T) {
	root, b, rendererPath := fixture(t)
	b.Root = filepath.Join(root, "missing")
	if err := Build(context.Background(), b, rendererPath); err == nil || !strings.Contains(err.Error(), "project root") {
		t.Fatalf("missing root error = %v", err)
	}
	root, b, rendererPath = fixture(t)
	if err := os.MkdirAll(filepath.Join(b.Dir, "reference"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, ".poolboy")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.Dir, "reference", "endpoints.md"), []byte("---\ntype: Handwritten\n---\n# owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Build(context.Background(), b, rendererPath); err == nil || !strings.Contains(err.Error(), "handwritten") {
		t.Fatalf("handwritten overwrite error = %v", err)
	}
}
func TestBuildProtectsReservedDirectories(t *testing.T) {
	for _, name := range []string{".git", ".substrate", ".poolboy"} {
		root, b, rendererPath := fixture(t)
		path := filepath.Join(root, name, "sentinel")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("keep\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		b.Output = name
		if err := Build(context.Background(), b, rendererPath); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("%s output error = %v", name, err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "keep\n" {
			t.Fatalf("%s sentinel changed: %q", name, got)
		}
	}
}

func TestBuildRefusesUnknownExistingPublicationFiles(t *testing.T) {
	root, b, rendererPath := fixture(t)
	if err := Build(context.Background(), b, rendererPath); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "dist", "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Build(context.Background(), b, rendererPath); err == nil || !strings.Contains(err.Error(), "unknown file") {
		t.Fatalf("unknown publication file error = %v", err)
	}
	got, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep\n" {
		t.Fatalf("publication sentinel changed: %q", got)
	}
}

func fixture(t *testing.T) (string, *bundle.Bundle, string) {
	t.Helper()
	root := t.TempDir()
	corpus := filepath.Join(root, "docs")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(corpus, "index.md"), "---\nokf_version: \"0.2\"\n---\n# Fixture\n\n[Endpoints](reference/endpoints.md)\n\n- [ ] [Review architecture](architecture.md)\n")
	write(filepath.Join(corpus, "architecture.md"), "---\ntype: Concept\ntitle: Architecture\n---\n# Architecture\n\nThe generated [reference](reference/endpoints.md) is deterministic.\n")
	write(filepath.Join(root, "templates", "endpoints.md.knap"), "---\ntype: Reference\ntitle: Endpoint reference\n---\n# Endpoints\n")
	write(filepath.Join(root, "data", "endpoints.json"), "{\"title\":\"Endpoints\"}\n")
	rendererPath := filepath.Join(root, "renderer-stub.sh")
	write(rendererPath, `#!/bin/sh
cat >/dev/null
printf '%s\n' '{"ok":true,"output":"---\ntype: Reference\ntitle: Endpoint reference\n---\n# Endpoints\n\nSee [architecture](../architecture.md).\n","warnings":[]}'
`)
	if err := os.Chmod(rendererPath, 0o700); err != nil {
		t.Fatal(err)
	}
	return root, &bundle.Bundle{
		Root:    root,
		Dir:     corpus,
		Name:    "Fixture",
		Output:  "dist",
		Spec:    "0.2",
		Renders: []bundle.Render{{Template: "templates/endpoints.md.knap", Data: "data/endpoints.json", Output: "reference/endpoints.md"}},
	}, rendererPath
}

func snapshot(t *testing.T, root string) map[string][]byte {
	t.Helper()
	got := map[string][]byte{}
	var paths []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		got[rel] = data
	}
	return got
}

func TestBuildLandingSubstitutesPromptURL(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.md"), []byte("---\nokf_version: \"0.2\"\n---\n# Direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &bundle.Bundle{Root: root, Dir: root, Name: "Direct", Output: "dist", Spec: "0.2"}
	if err := Build(context.Background(), b, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	if !strings.Contains(page, `replaceAll("{{url}}",base)`) {
		t.Fatalf("landing page cannot substitute the prompt URL: %s", page)
	}
}

func landingProject(t *testing.T) (string, *bundle.Bundle) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.md"), []byte("---\nokf_version: \"0.2\"\n---\n# Direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, &bundle.Bundle{Root: root, Dir: root, Name: "Acme Docs", Output: "dist", Spec: "0.2"}
}

func TestBuildLandingAppliesConfiguredText(t *testing.T) {
	root, b := landingProject(t)
	value := func(s string) *string { return &s }
	b.Landing = bundle.Landing{
		Title:                value("Acme"),
		Description:          value("Ask an agent."),
		SecondaryDescription: value(""),
		Prompt:               value("Read {{url}}/llms.txt."),
		BaseURL:              value("https://docs.example.com/guide"),
		DownloadFilename:     value("acme-docs.zip"),
		Mark:                 value("🩳"),
	}
	if err := Build(context.Background(), b, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	for _, want := range []string{"Acme", "Ask an agent.", "Read {{url}}/llms.txt.", "https://docs.example.com/guide", `download="acme-docs.zip"`, "🩳"} {
		if !strings.Contains(page, want) {
			t.Fatalf("landing page missing %q: %s", want, page)
		}
	}
	if strings.Contains(page, "Documentation built for agents.") {
		t.Fatalf("configured description did not replace the default: %s", page)
	}
}

func TestBuildRejectsInvalidLandingConfigBeforeWriting(t *testing.T) {
	value := func(s string) *string { return &s }
	for name, landing := range map[string]bundle.Landing{
		"base_url":          {BaseURL: value("ftp://example.com")},
		"download_filename": {DownloadFilename: value("../escape.zip")},
		"style":             {Style: bundle.LandingStyle{Background: value("red")}},
		"logo":              {Logo: value("../outside.svg")},
	} {
		t.Run(name, func(t *testing.T) {
			root, b := landingProject(t)
			b.Landing = landing
			if err := Build(context.Background(), b, ""); err == nil {
				t.Fatal("expected invalid landing configuration to fail the build")
			}
			if _, err := os.Stat(filepath.Join(root, "dist")); !os.IsNotExist(err) {
				t.Fatalf("invalid configuration published output: %v", err)
			}
		})
	}
}

func TestBuildCustomSiteDirReplacesGeneratedLanding(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "index.md"), []byte("---\nokf_version: \"0.2\"\n---\n# Direct\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &bundle.Bundle{Root: root, Dir: docs, Name: "Acme Docs", Output: "dist", Spec: "0.2"}
	site := filepath.Join(root, "website")
	if err := os.MkdirAll(site, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "index.html"), []byte("<!doctype html><title>Custom</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	value := "website"
	b.Landing = bundle.Landing{SiteDir: &value}
	if err := Build(context.Background(), b, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Custom") || strings.Contains(string(data), "Bring your own agent") {
		t.Fatalf("custom site did not replace the generated landing: %s", data)
	}
	if _, err := os.Stat(filepath.Join(root, "dist", "corpus.zip")); err != nil {
		t.Fatalf("custom site dropped the Markdown download: %v", err)
	}
}
