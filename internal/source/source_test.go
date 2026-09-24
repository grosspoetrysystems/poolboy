package source

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grosspoetrysystems/poolboy/bundle"
)

func testBundle(root string) *bundle.Bundle {
	return &bundle.Bundle{
		Root:   root,
		Dir:    filepath.Join(root, "docs"),
		Output: "dist",
		Renders: []bundle.Render{
			{Template: "templates/reference.md.knap", Data: "data/reference.json", Output: "reference.md"},
		},
	}
}

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanFiltersBoundaryAndNestedIgnores(t *testing.T) {
	root := t.TempDir()
	b := testBundle(root)
	writeTestFile(t, root, ".gitignore", "*.log\nignored/\n")
	writeTestFile(t, root, ".poolboyignore", "custom/**\n!custom/keep.go\n")
	writeTestFile(t, root, "src/main.go", "package src\n")
	writeTestFile(t, root, "src/bin/cli.ts", "export const cli = true\n")
	writeTestFile(t, root, "src/debug.log", "ignored\n")
	writeTestFile(t, root, "src/nested/.gitignore", "*.txt\n!keep.txt\n")
	writeTestFile(t, root, "src/nested/drop.txt", "ignored\n")
	writeTestFile(t, root, "src/nested/keep.txt", "kept\n")
	writeTestFile(t, root, "ignored/keep.go", "package ignored\n")
	writeTestFile(t, root, "custom/drop.go", "package custom\n")
	writeTestFile(t, root, "custom/keep.go", "package custom\n")
	writeTestFile(t, root, ".env", "TOKEN=secret\n")
	writeTestFile(t, root, "tls/private.pem", "not a key\n")
	awsKey := "AKIA" + "1234567890ABCDEF"
	writeTestFile(t, root, "src/aws-real.go", "const key = \""+awsKey+"\"\n")
	writeTestFile(t, root, "src/blob.dat", "prefix\x00suffix")
	writeTestFile(t, root, "vendor/lib.go", "package vendor\n")
	writeTestFile(t, root, "node_modules/lib.js", "module.exports = {}\n")
	writeTestFile(t, root, ".substrate/audit.md", "internal\n")
	writeTestFile(t, root, "docs/index.md", "docs\n")
	writeTestFile(t, root, "bin/tool.go", "package tool\n")
	writeTestFile(t, root, "src/internal/build/keep.ts", "export const keep = true\n")
	writeTestFile(t, root, "build/generated.go", "package generated\n")
	writeTestFile(t, root, "dist/generated.md", "generated\n")
	writeTestFile(t, root, "templates/reference.md.knap", "template\n")
	writeTestFile(t, root, "data/reference.json", "{}\n")
	writeTestFile(t, root, "docs/reference.md", "generated\n")

	if err := os.Symlink(filepath.Join(root, "src/main.go"), filepath.Join(root, "src/link.go")); err != nil {
		t.Fatal(err)
	}
	oversized := bytes.Repeat([]byte("x"), int(maxFileBytes)+1)
	writeTestFile(t, root, "src/oversized.txt", string(oversized))

	inv, err := scanCurrent(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, excluded := range []string{
		"src/debug.log", "src/nested/drop.txt", "ignored/keep.go", "custom/drop.go",
		".env", "tls/private.pem", "src/aws-real.go", "src/blob.dat", "src/oversized.txt",
		"src/link.go", "vendor/lib.go", "node_modules/lib.js", ".substrate/audit.md",
		"bin/tool.go", "build/generated.go", "docs/index.md", "dist/generated.md", "templates/reference.md.knap", "data/reference.json",
	} {
		if _, ok := inv.Files[excluded]; ok {
			t.Errorf("excluded path present in inventory: %s", excluded)
		}
	}
	for _, included := range []string{"src/main.go", "src/bin/cli.ts", "src/internal/build/keep.ts", "src/nested/keep.txt", "custom/keep.go"} {
		if _, ok := inv.Files[included]; !ok {
			t.Errorf("safe path missing from inventory: %s", included)
		}
	}
}

func TestScanAcceptPreservesBaselineUntilReview(t *testing.T) {
	root := t.TempDir()
	b := testBundle(root)
	writeTestFile(t, root, "src/main.go", "package main\n")
	first, err := Scan(b, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != Version || len(first.Files) != 1 {
		t.Fatalf("initial inventory = %#v", first)
	}
	lockPath := filepath.Join(root, ".poolboy", lockName)
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "src/main.go", "package changed\n")
	if _, err := Scan(b, false); !errors.Is(err, ErrBaselineExists) {
		t.Fatalf("scan without accept error = %v", err)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("non-accepting scan replaced baseline")
	}
	changes, err := Drift(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != "src/main.go" || changes[0].Status != string(Modified) {
		t.Fatalf("drift = %#v", changes)
	}
	if _, err := Scan(b, true); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, updated) {
		t.Fatal("accepting scan did not replace baseline")
	}
}

func TestDriftReportsAddedRemovedModifiedWithoutWriting(t *testing.T) {
	root := t.TempDir()
	b := testBundle(root)
	writeTestFile(t, root, "src/keep.go", "package keep\n")
	writeTestFile(t, root, "src/remove.go", "package remove\n")
	if _, err := Scan(b, false); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(root, ".poolboy", lockName)
	baseline, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "src/keep.go", "package changed\n")
	if err := os.Remove(filepath.Join(root, "src/remove.go")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "src/add.go", "package add\n")
	changes, err := Drift(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 {
		t.Fatalf("drift = %#v", changes)
	}
	want := []Change{
		{Path: "src/add.go", Status: string(Added)},
		{Path: "src/keep.go", Status: string(Modified)},
		{Path: "src/remove.go", Status: string(Removed)},
	}
	for i := range want {
		if changes[i] != want[i] {
			t.Fatalf("drift[%d] = %#v, want %#v", i, changes[i], want[i])
		}
	}
	after, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseline, after) {
		t.Fatal("drift replaced baseline")
	}
}

func TestWriteLockDeterministicAndContained(t *testing.T) {
	root := t.TempDir()
	b := testBundle(root)
	inv := &Inventory{Files: map[string]Entry{
		"z.go": {Type: "go", Bytes: 1, SHA256: strings.Repeat("a", 64)},
		"a.go": {Type: "go", Bytes: 1, SHA256: strings.Repeat("b", 64)},
	}}
	if err := WriteLock(b, inv); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".poolboy", lockName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) || bytes.Index(data, []byte("\"a.go\"")) > bytes.Index(data, []byte("\"z.go\"")) {
		t.Fatalf("lock is not deterministic sorted JSON: %s", data)
	}
	if bytes.Contains(data, []byte(root)) || bytes.Contains(data, []byte("timestamp")) {
		t.Fatalf("lock contains host/timestamp data: %s", data)
	}
}

func TestCreateOnlyLockDoesNotReplaceBaseline(t *testing.T) {
	root := t.TempDir()
	b := testBundle(root)
	original := &Inventory{Files: map[string]Entry{
		"original.go": {Type: "go", Bytes: 1, SHA256: strings.Repeat("a", 64)},
	}}
	if err := WriteLock(b, original); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, ".poolboy", lockName))
	if err != nil {
		t.Fatal(err)
	}
	replacement := &Inventory{Files: map[string]Entry{
		"replacement.go": {Type: "go", Bytes: 1, SHA256: strings.Repeat("b", 64)},
	}}
	if err := writeLock(b, replacement, true); !errors.Is(err, ErrBaselineExists) {
		t.Fatalf("create-only write error = %v, want ErrBaselineExists", err)
	}
	after, err := os.ReadFile(filepath.Join(root, ".poolboy", lockName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("create-only write replaced baseline")
	}
}

func TestScanRejectsUnknownSettings(t *testing.T) {
	root := t.TempDir()
	b := testBundle(root)
	b.Unknown = []string{"z_typo", "a_typo"}
	if _, err := Scan(b, false); err == nil || !strings.Contains(err.Error(), "a_typo, z_typo") {
		t.Fatalf("scan error = %v, want sorted unknown settings", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".poolboy")); !os.IsNotExist(err) {
		t.Fatal("scan created state for invalid configuration")
	}
}

func TestAffectedResolvesProjectAndDocumentRelativeResources(t *testing.T) {
	root := t.TempDir()
	b := testBundle(root)
	writeTestFile(t, root, "src/routes.go", "package routes\n")
	writeTestFile(t, root, "lib/helper.go", "package lib\n")
	writeTestFile(t, root, "docs/index.md", "---\ntype: Reference\nsources:\n  - resource: src/routes.go\n  - resource: ../lib/helper.go\n---\n# Index\n")
	writeTestFile(t, root, "docs/nested/project.md", "---\ntype: Reference\nsources:\n  - resource: src/routes.go\n---\n# Project\n")
	writeTestFile(t, root, "docs/nested/local.md", "---\ntype: Reference\nsources:\n  - resource: ../../lib/helper.go\n---\n# Local\n")
	writeTestFile(t, root, "docs/no-source.md", "---\ntype: Reference\n---\n# None\n")

	routes, err := Affected(b, "src/routes.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(routes, ",") != "/index.md,/nested/project.md" {
		t.Fatalf("routes affected = %#v", routes)
	}
	helper, err := Affected(b, "lib/helper.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(helper, ",") != "/index.md,/nested/local.md" {
		t.Fatalf("helper affected = %#v", helper)
	}
}
