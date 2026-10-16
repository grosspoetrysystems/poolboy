package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grosspoetrysystems/poolboy/internal/compiler"
	"github.com/grosspoetrysystems/poolboy/internal/renderer"
)

func affectedProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("poolboy.toml", "spec = \"0.2\"\ncorpus = \"docs\"\noutput = \"dist\"\n")
	write("src/routes.go", "package src\n")
	write("docs/index.md", "---\nokf_version: \"0.2\"\n---\n# Home\n")
	write("docs/note.md", "---\nokf_version: \"0.2\"\ntype: note\nsources:\n  - resource: src/routes.go\n---\n# Note\n")
	return root
}

func TestCmdAffectedReportsSourceConsumers(t *testing.T) {
	t.Cleanup(func() { rootDir = "" })
	rootDir = ""
	root := affectedProject(t)
	t.Chdir(root)

	out, code := capture(t, func() int {
		return cmdAffected([]string{"src/routes.go", "--format", "json"})
	})
	if code != 0 {
		t.Fatalf("affected exit=%d out=%q", code, out)
	}
	var paths []string
	if err := json.Unmarshal([]byte(out), &paths); err != nil {
		t.Fatalf("affected JSON: %v\n%s", err, out)
	}
	if len(paths) != 1 || paths[0] != "/note.md" {
		t.Fatalf("affected paths=%v, want [/note.md]", paths)
	}

	if _, code = capture(t, func() int { return cmdAffected([]string{"https://example.test/source"}) }); code != 2 {
		t.Fatalf("URL source exit=%d, want 2", code)
	}
	if _, code = capture(t, func() int { return cmdAffected(nil) }); code != 2 {
		t.Fatalf("affected without source exit=%d, want 2", code)
	}
	if _, code = capture(t, func() int { return cmdAffected([]string{"--unknown"}) }); code != 2 {
		t.Fatalf("affected unknown flag exit=%d, want 2", code)
	}
}

func TestProductCommandsRejectUsageAndDiscoveryFailures(t *testing.T) {
	t.Cleanup(func() { rootDir = "" })
	rootDir = ""
	t.Chdir(t.TempDir())

	cases := []struct {
		name string
		fn   func([]string) int
		args []string
	}{
		{"checkout missing target", cmdCheckout, nil},
		{"checkout extra target", cmdCheckout, []string{"one", "two"}},
		{"checkin missing target", cmdCheckin, nil},
		{"checkin extra target", cmdCheckin, []string{"one", "two"}},
		{"build unknown flag", cmdBuild, []string{"--unknown"}},
		{"build positional", cmdBuild, []string{"extra"}},
		{"health positional", cmdHealth, []string{"extra"}},
		{"scan positional", cmdScan, []string{"extra"}},
		{"drift positional", cmdDrift, []string{"extra"}},
	}
	for _, tc := range cases {
		if _, code := capture(t, func() int { return tc.fn(tc.args) }); code != 2 {
			t.Fatalf("%s exit=%d, want 2", tc.name, code)
		}
	}

	// With valid argument shape, project discovery is the next error boundary.
	for _, tc := range []struct {
		name string
		fn   func([]string) int
		args []string
	}{
		{"checkout discovery", cmdCheckout, []string{filepath.Join(t.TempDir(), "checkout")}},
		{"checkin discovery", cmdCheckin, []string{filepath.Join(t.TempDir(), "checkout")}},
		{"build discovery", cmdBuild, nil},
		{"health discovery", cmdHealth, nil},
		{"scan discovery", cmdScan, nil},
		{"drift discovery", cmdDrift, nil},
	} {
		if _, code := capture(t, func() int { return tc.fn(tc.args) }); code != 2 {
			t.Fatalf("%s exit=%d, want 2", tc.name, code)
		}
	}
}

func TestCmdHealthTextIncludesUnavailableAndFindings(t *testing.T) {
	t.Cleanup(func() { rootDir = "" })
	rootDir = ""
	root := affectedProject(t)
	t.Chdir(root)

	// A configured but absent source baseline is a meaningful health boundary.
	out, code := capture(t, func() int { return cmdHealth([]string{"--format", "text"}) })
	if code != 0 {
		t.Fatalf("health exit=%d out=%q", code, out)
	}
	if !strings.Contains(out, "unavailable\tsource_baseline") || !strings.Contains(out, "orphan\t/note.md") {
		t.Fatalf("health diagnostics missing from output: %q", out)
	}
}

func TestProductFailureAndBuildClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code string
		kind string
	}{
		{"generic", errors.New("build exploded"), "BUILD_FAILED", ""},
		{"node", &renderer.RenderError{Kind: renderer.KindAdapter, Code: "NODE_NOT_FOUND", Message: "node"}, "NODE_NOT_FOUND", "adapter"},
		{"timeout", &renderer.RenderError{Kind: renderer.KindAdapter, Code: "TIMEOUT", Message: "slow"}, "TIMEOUT", "adapter"},
		{"protocol", &renderer.RenderError{Kind: renderer.KindProtocol, Code: "PROCESS_EXIT", Message: "bad"}, "PROCESS_EXIT", "protocol"},
		{"template", &renderer.RenderError{
			Kind:   renderer.KindTemplate,
			Errors: []renderer.TemplateError{{Code: "UNKNOWN_FILTER", Message: "filter"}},
		}, "UNKNOWN_FILTER", "template"},
		{"publication", &compiler.PublicationError{Code: "OUTPUT_OCCUPIED", Path: "/tmp/dist", Err: errors.New("occupied")}, "OUTPUT_OCCUPIED", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyBuildFailure(tc.err)
			if got.Code != tc.code || got.Kind != tc.kind || got.Status != "blocked" {
				t.Fatalf("classification=%+v", got)
			}
		})
	}

	out, code := capture(t, func() int {
		return productFailure("json", actionableStep{Code: "TEST", Status: "blocked", Detail: "detail"})
	})
	if code != 2 || !strings.Contains(out, `"code": "TEST"`) {
		t.Fatalf("json product failure=%q code=%d", out, code)
	}
}

func TestCmdCheckinReportsConflictsAndInputErrors(t *testing.T) {
	t.Cleanup(func() { rootDir = "" })
	rootDir = ""
	root := writeBundle(t)
	t.Chdir(root)
	target := filepath.Join(t.TempDir(), "conflict")
	if _, code := capture(t, func() int {
		return cmdCheckout([]string{target, "--format", "json"})
	}); code != 0 {
		t.Fatalf("checkout exit=%d", code)
	}
	if err := os.WriteFile(filepath.Join(target, "guide.md"), []byte("# Draft change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "guide.md"), []byte("# Workspace change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := capture(t, func() int {
		return cmdCheckin([]string{target, "--apply", "--format", "json"})
	})
	if code != 1 || !strings.Contains(out, `"can_apply": false`) || !strings.Contains(out, `"conflicts":`) {
		t.Fatalf("conflict checkin output=%q code=%d", out, code)
	}
	data, err := os.ReadFile(filepath.Join(root, "guide.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# Workspace change\n" {
		t.Fatalf("conflict checkin mutated workspace: %q", data)
	}

	if _, code := capture(t, func() int {
		return cmdCheckin([]string{target, "--unknown"})
	}); code != 2 {
		t.Fatalf("checkin unknown flag exit=%d, want 2", code)
	}
	if _, code := capture(t, func() int {
		return cmdCheckin([]string{t.TempDir()})
	}); code != 2 {
		t.Fatalf("malformed checkout exit=%d, want 2", code)
	}
}

func TestCmdCheckinReportsQuarantinedSources(t *testing.T) {
	t.Cleanup(func() { rootDir = "" })
	rootDir = ""
	root := writeBundle(t)
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	target := filepath.Join(t.TempDir(), "review")
	if _, code := capture(t, func() int {
		return cmdCheckout([]string{target, "--format", "json"})
	}); code != 0 {
		t.Fatalf("checkout exit=%d", code)
	}
	out, code := capture(t, func() int {
		return cmdCheckin([]string{target, "--format", "json"})
	})
	if code != 0 ||
		!strings.Contains(out, `"source_quarantined"`) ||
		!strings.Contains(out, `".env"`) ||
		!strings.Contains(out, `"requires_source_review": true`) {
		t.Fatalf("quarantined checkin output=%q code=%d", out, code)
	}
}
