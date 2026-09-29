package checkout

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grosspoetrysystems/poolboy/bundle"
)

func TestCheckinMergesDisjointChangesAndReportsSourceVariance(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	b := testBundle(t, root)
	writeFile(t, b.Dir, "index.md", "# Base\n")
	writeFile(t, b.Dir, "notes/other.md", "# Other\n")
	writeFile(t, root, "src/main.go", "package main\n")

	target := filepath.Join(parent, "editing")
	created, err := Create(b, target)
	if err != nil {
		t.Fatal(err)
	}
	if created.Documents != 2 || filepath.Base(created.Path) != "editing" {
		t.Fatalf("created = %#v", created)
	}
	writeFile(t, target, "index.md", "# Draft\n")
	writeFile(t, b.Dir, "notes/other.md", "# Workspace\n")
	writeFile(t, root, "src/main.go", "package main\n// changed\n")

	plan, err := Checkin(b, target, false)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.CanApply || plan.Applied || !plan.RequiresSourceReview {
		t.Fatalf("preview = %#v", plan)
	}
	if len(plan.DraftChanges) != 1 || len(plan.WorkspaceChanges) != 1 || len(plan.SourceChanges) != 1 || len(plan.Conflicts) != 0 {
		t.Fatalf("unexpected variance = %#v", plan)
	}
	if got := readFile(t, b.Dir, "index.md"); got != "# Base\n" {
		t.Fatalf("preview mutated corpus: %q", got)
	}

	plan, err = Checkin(b, target, true)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Applied {
		t.Fatalf("apply = %#v", plan)
	}
	if got := readFile(t, b.Dir, "index.md"); got != "# Draft\n" {
		t.Fatalf("draft not applied: %q", got)
	}
	if got := readFile(t, b.Dir, "notes/other.md"); got != "# Workspace\n" {
		t.Fatalf("concurrent workspace change lost: %q", got)
	}
	if _, err := os.Stat(statePath(root, created.ID)); !os.IsNotExist(err) {
		t.Fatalf("private checkout state still exists: %v", err)
	}
}

func TestCheckinRefusesConflictingAndGeneratedChanges(t *testing.T) {
	t.Run("both sides changed", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "project")
		b := testBundle(t, root)
		writeFile(t, b.Dir, "index.md", "# Base\n")
		target := filepath.Join(parent, "editing")
		if _, err := Create(b, target); err != nil {
			t.Fatal(err)
		}
		writeFile(t, target, "index.md", "# Draft\n")
		writeFile(t, b.Dir, "index.md", "# Workspace\n")

		plan, err := Checkin(b, target, true)
		if err != nil {
			t.Fatal(err)
		}
		if plan.CanApply || plan.Applied || len(plan.Conflicts) != 1 {
			t.Fatalf("conflict plan = %#v", plan)
		}
		if got := readFile(t, b.Dir, "index.md"); got != "# Workspace\n" {
			t.Fatalf("conflicting apply mutated corpus: %q", got)
		}
	})

	t.Run("generated document", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "project")
		b := testBundle(t, root)
		writeFile(t, b.Dir, "index.md", "# Generated\n")
		writeFile(t, root, ".poolboy/generated.json", `{"version":"0","files":{"index.md":{"data":"data/index.json","sha256":"owned","template":"templates/index.md.knap"}}}`)
		target := filepath.Join(parent, "editing")
		if _, err := Create(b, target); err != nil {
			t.Fatal(err)
		}
		writeFile(t, target, "index.md", "# Hand edited\n")

		plan, err := Checkin(b, target, false)
		if err != nil {
			t.Fatal(err)
		}
		if plan.CanApply || len(plan.Conflicts) != 1 {
			t.Fatalf("generated plan = %#v", plan)
		}
	})
}

func TestCheckoutBoundaries(t *testing.T) {
	t.Run("outside project and excludes output", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "project")
		b := testBundle(t, root)
		b.Dir = root
		writeFile(t, root, "index.md", "# Source\n")
		writeFile(t, root, "dist/published.md", "# Published\n")

		if _, err := Create(b, filepath.Join(root, "editing")); err == nil {
			t.Fatal("checkout inside project succeeded")
		}
		target := filepath.Join(parent, "editing")
		created, err := Create(b, target)
		if err != nil {
			t.Fatal(err)
		}
		if created.Documents != 1 {
			t.Fatalf("documents = %d, want 1", created.Documents)
		}
		if _, err := os.Stat(filepath.Join(target, "dist", "published.md")); !os.IsNotExist(err) {
			t.Fatalf("published output copied into checkout: %v", err)
		}
	})

	t.Run("untrusted marker id", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "project")
		b := testBundle(t, root)
		writeFile(t, b.Dir, "index.md", "# Source\n")
		target := filepath.Join(parent, "editing")
		if _, err := Create(b, target); err != nil {
			t.Fatal(err)
		}
		writeFile(t, target, markerName, `{"version":"0","id":"../../generated"}`)
		if _, err := Checkin(b, target, false); err == nil {
			t.Fatal("check-in accepted an untrusted marker id")
		}
	})
}

func TestApplyActionsRollsBackWhenWorkspaceChangesAfterPlanning(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.md", "# A base\n")
	writeFile(t, root, "b.md", "# B concurrent\n")
	current := map[string]document{
		"a.md": {Revision: revision([]byte("# A base\n")), data: []byte("# A base\n")},
		"b.md": {Revision: revision([]byte("# B base\n")), data: []byte("# B base\n")},
	}
	actions := []action{
		{path: "a.md", data: []byte("# A draft\n")},
		{path: "b.md", data: []byte("# B draft\n")},
	}

	if err := applyActions(root, actions, current); err == nil {
		t.Fatal("apply accepted a workspace change made after planning")
	}
	if got := readFile(t, root, "a.md"); got != "# A base\n" {
		t.Fatalf("first action was not rolled back: %q", got)
	}
	if got := readFile(t, root, "b.md"); got != "# B concurrent\n" {
		t.Fatalf("concurrent change was overwritten: %q", got)
	}
}

func testBundle(t *testing.T, root string) *bundle.Bundle {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &bundle.Bundle{Root: root, Dir: filepath.Join(root, "docs"), Output: "dist"}
}

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

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
