package checkout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grosspoetrysystems/poolboy/bundle"
)

func TestCheckinRemovesDraftDeletedDocument(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	b := testBundle(t, root)
	writeFile(t, b.Dir, "index.md", "# Base\n")
	writeFile(t, b.Dir, "notes.md", "# Notes\n")
	target := filepath.Join(parent, "editing")
	created, err := Create(b, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(target, "notes.md")); err != nil {
		t.Fatal(err)
	}

	plan, err := Checkin(b, target, false)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.CanApply || len(plan.DraftChanges) != 1 || plan.DraftChanges[0].Status != "removed" {
		t.Fatalf("delete preview = %#v", plan)
	}
	plan, err = Checkin(b, target, true)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Applied {
		t.Fatalf("delete apply = %#v", plan)
	}
	if _, err := os.Stat(filepath.Join(b.Dir, "notes.md")); !os.IsNotExist(err) {
		t.Fatalf("deleted document remains: %v", err)
	}
	if _, err := os.Stat(statePath(root, created.ID)); !os.IsNotExist(err) {
		t.Fatalf("checkout state remains after delete apply: %v", err)
	}
}

func TestCheckinPlansCurrentGeneratedConflict(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	b := testBundle(t, root)
	writeFile(t, b.Dir, "index.md", "# Base\n")
	target := filepath.Join(parent, "editing")
	if _, err := Create(b, target); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, ".poolboy/generated.json", `{"version":"0","files":{"index.md":{"sha256":"current"}}}`)
	writeFile(t, target, "index.md", "# Draft\n")
	plan, err := Checkin(b, target, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CanApply || len(plan.Conflicts) != 1 || !strings.Contains(plan.Conflicts[0].Reason, "generated") {
		t.Fatalf("current-generated conflict plan = %#v", plan)
	}
}

func TestCheckinAppliesModifiedAndAddedDocuments(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	b := testBundle(t, root)
	writeFile(t, b.Dir, "index.md", "# Base\n")
	target := filepath.Join(parent, "editing")
	created, err := Create(b, target)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, target, "index.md", "# Draft\n")
	writeFile(t, target, "new.md", "# New\n")
	plan, err := Checkin(b, target, true)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Applied || len(plan.DraftChanges) != 2 {
		t.Fatalf("modified/add apply plan = %#v", plan)
	}
	for path, want := range map[string]string{"index.md": "# Draft\n", "new.md": "# New\n"} {
		data, err := os.ReadFile(filepath.Join(b.Dir, path))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Fatalf("%s = %q, want %q", path, data, want)
		}
	}
	if _, err := os.Stat(statePath(root, created.ID)); !os.IsNotExist(err) {
		t.Fatalf("checkout state remains after apply: %v", err)
	}
}

func TestCreateRefusesExistingTarget(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	b := testBundle(t, root)
	writeFile(t, b.Dir, "index.md", "# Base\n")
	target := filepath.Join(parent, "editing")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(b, target); err == nil || !strings.Contains(err.Error(), "target already exists") {
		t.Fatalf("existing target error = %v", err)
	}
}

func TestCreateCleansUpWhenStateDirectoryCreationFails(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	b := testBundle(t, root)
	writeFile(t, b.Dir, "index.md", "# Base\n")

	stateDir := filepath.Dir(statePath(b.Root, "sentinel"))
	if err := os.MkdirAll(filepath.Dir(stateDir), 0o755); err != nil {
		t.Fatal(err)
	}
	const sentinel = "preserve this state"
	if err := os.WriteFile(stateDir, []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(parent, "editing")
	if _, err := Create(b, target); err == nil || !strings.Contains(err.Error(), "create private state directory") {
		t.Fatalf("state directory creation error = %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("failed checkout target remains: %v", err)
	}
	got, err := os.ReadFile(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != sentinel {
		t.Fatalf("state sentinel changed: %q", got)
	}
}

func TestCreateRejectsUnsupportedGeneratedLedger(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	b := testBundle(t, root)
	writeFile(t, b.Dir, "index.md", "# Base\n")
	writeFile(t, root, ".poolboy/generated.json", `{"version":"9","files":{}}`)
	if _, err := Create(b, filepath.Join(parent, "editing")); err == nil || !strings.Contains(err.Error(), "unsupported generated ledger") {
		t.Fatalf("generated ledger error = %v", err)
	}
}

func TestCheckinRejectsMalformedAndMismatchedState(t *testing.T) {
	newCheckout := func(t *testing.T) (*bundle.Bundle, string, *Created) {
		t.Helper()
		parent := t.TempDir()
		root := filepath.Join(parent, "project")
		b := testBundle(t, root)
		writeFile(t, b.Dir, "index.md", "# Base\n")
		target := filepath.Join(parent, "editing")
		created, err := Create(b, target)
		if err != nil {
			t.Fatal(err)
		}
		return b, target, created
	}

	t.Run("missing marker", func(t *testing.T) {
		b, target, _ := newCheckout(t)
		if err := os.Remove(filepath.Join(target, markerName)); err != nil {
			t.Fatal(err)
		}
		if _, err := Checkin(b, target, false); err == nil || !strings.Contains(err.Error(), "read checkout marker") {
			t.Fatalf("missing marker error = %v", err)
		}
	})

	t.Run("unsupported marker", func(t *testing.T) {
		b, target, created := newCheckout(t)
		writeFile(t, target, markerName, `{"version":"9","id":"`+created.ID+`"}`)
		if _, err := Checkin(b, target, false); err == nil || !strings.Contains(err.Error(), "unsupported checkout marker") {
			t.Fatalf("unsupported marker error = %v", err)
		}
	})

	t.Run("malformed state", func(t *testing.T) {
		b, target, created := newCheckout(t)
		if err := os.WriteFile(statePath(b.Root, created.ID), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Checkin(b, target, false); err == nil || !strings.Contains(err.Error(), "read private checkout state") {
			t.Fatalf("malformed state error = %v", err)
		}
	})

	t.Run("state path mismatch", func(t *testing.T) {
		b, target, created := newCheckout(t)
		stateData, err := os.ReadFile(statePath(b.Root, created.ID))
		if err != nil {
			t.Fatal(err)
		}
		var st state
		if err := json.Unmarshal(stateData, &st); err != nil {
			t.Fatal(err)
		}
		st.Path = filepath.Join(filepath.Dir(st.Path), "other")
		updated, err := json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(statePath(b.Root, created.ID), updated, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Checkin(b, target, false); err == nil || !strings.Contains(err.Error(), "state does not match") {
			t.Fatalf("mismatched state error = %v", err)
		}
	})
	t.Run("missing state", func(t *testing.T) {
		b, target, created := newCheckout(t)
		if err := os.Remove(statePath(b.Root, created.ID)); err != nil {
			t.Fatal(err)
		}
		if _, err := Checkin(b, target, false); err == nil || !strings.Contains(err.Error(), "read private checkout state") {
			t.Fatalf("missing state error = %v", err)
		}
	})

	t.Run("draft symlink", func(t *testing.T) {
		b, target, _ := newCheckout(t)
		outside := filepath.Join(t.TempDir(), "outside.md")
		writeFile(t, filepath.Dir(outside), filepath.Base(outside), "# Outside\n")
		if err := os.Symlink(outside, filepath.Join(target, "linked.md")); err != nil {
			t.Fatal(err)
		}
		if _, err := Checkin(b, target, false); err == nil || !strings.Contains(err.Error(), "read draft") {
			t.Fatalf("draft symlink error = %v", err)
		}
	})

	t.Run("workspace symlink", func(t *testing.T) {
		b, target, _ := newCheckout(t)
		outside := filepath.Join(t.TempDir(), "outside.md")
		writeFile(t, filepath.Dir(outside), filepath.Base(outside), "# Outside\n")
		if err := os.Symlink(outside, filepath.Join(b.Dir, "linked.md")); err != nil {
			t.Fatal(err)
		}
		if _, err := Checkin(b, target, false); err == nil || !strings.Contains(err.Error(), "read workspace") {
			t.Fatalf("workspace symlink error = %v", err)
		}
	})
}

func TestCheckoutPathValidationAndExistingPath(t *testing.T) {
	root := t.TempDir()
	b := testBundle(t, root)
	if _, err := checkoutPath(b, " "); err == nil || !strings.Contains(err.Error(), "target directory is required") {
		t.Fatalf("empty target error = %v", err)
	}
	if _, err := checkoutPath(b, filepath.Join(root, "missing", "editing")); err == nil || !strings.Contains(err.Error(), "parent must exist") {
		t.Fatalf("missing parent error = %v", err)
	}
	if _, err := existingCheckoutPath(filepath.Join(root, "missing")); err == nil || !strings.Contains(err.Error(), "resolve checkout") {
		t.Fatalf("missing checkout error = %v", err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := existingCheckoutPath(file); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("file checkout error = %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if _, err := existingCheckoutPath(link); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("symlink-to-file checkout error = %v", err)
	}
}

func TestReadActionTargetRejectsUnsupportedPaths(t *testing.T) {
	root := t.TempDir()
	if _, ok, err := readActionTarget(root, "missing.md"); err != nil || ok {
		t.Fatalf("missing action target = %#v, %v", ok, err)
	}
	if _, _, err := readActionTarget(root, "bad\x00.md"); err == nil {
		t.Fatal("invalid action target path must error")
	}
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readActionTarget(root, "nested"); err == nil || !strings.Contains(err.Error(), "unsupported corpus path") {
		t.Fatalf("directory action target error = %v", err)
	}
	writeFile(t, root, "real.md", "# Real\n")
	if err := os.Symlink(filepath.Join(root, "real.md"), filepath.Join(root, "link.md")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readActionTarget(root, "link.md"); err == nil || !strings.Contains(err.Error(), "unsupported corpus path") {
		t.Fatalf("symlink action target error = %v", err)
	}
}

func TestApplyActionsRejectsUnsafeAndSymlinkPaths(t *testing.T) {
	root := t.TempDir()
	if err := applyActions(root, []action{{path: "../escape.md", data: []byte("bad")}}, nil); err == nil || !strings.Contains(err.Error(), "invalid corpus path") {
		t.Fatalf("traversal action error = %v", err)
	}
	if err := safeRelativePath(root, "bad\x00.md"); err == nil {
		t.Fatal("invalid path component must error")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "nested")); err != nil {
		t.Fatal(err)
	}
	if err := applyActions(root, []action{{path: "nested/file.md", data: []byte("bad")}}, nil); err == nil || !strings.Contains(err.Error(), "symlink path") {
		t.Fatalf("symlink action error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "file.md")); !os.IsNotExist(err) {
		t.Fatalf("unsafe action wrote outside root: %v", err)
	}
}

func TestReadDocumentsRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	b := testBundle(t, root)
	writeFile(t, b.Dir, "index.md", "# Base\n")
	target := filepath.Join(root, "outside.md")
	if err := os.WriteFile(target, []byte("# Outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(b.Dir, "linked.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := readDocuments(b, b.Dir); err == nil || !strings.Contains(err.Error(), "contains symlink") {
		t.Fatalf("symlink corpus error = %v", err)
	}
}

func TestReadDocumentsSkipsNonMarkdownAndHiddenDirectories(t *testing.T) {
	root := t.TempDir()
	b := testBundle(t, root)
	writeFile(t, b.Dir, "index.md", "# Index\n")
	writeFile(t, b.Dir, "notes.txt", "not corpus markdown")
	writeFile(t, b.Dir, ".hidden/private.md", "# Private\n")
	docs, err := readDocuments(b, b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("documents = %#v, want only index.md", docs)
	}
	if _, ok := docs["index.md"]; !ok {
		t.Fatalf("documents omitted index.md: %#v", docs)
	}
}

func TestReadDocumentsRejectsCaseCollision(t *testing.T) {
	root := t.TempDir()
	b := testBundle(t, root)
	writeFile(t, b.Dir, "a.md", "# Lower\n")
	writeFile(t, b.Dir, "A.md", "# Upper\n")
	lower, err := os.Stat(filepath.Join(b.Dir, "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	upper, err := os.Stat(filepath.Join(b.Dir, "A.md"))
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(lower, upper) {
		t.Skip("filesystem is case-insensitive; cannot create a collision")
	}
	if _, err := readDocuments(b, b.Dir); err == nil || !strings.Contains(err.Error(), "path collision") {
		t.Fatalf("case collision error = %v", err)
	}
}

func TestReadGeneratedAndJSONValidation(t *testing.T) {
	root := t.TempDir()
	generated, err := readGenerated(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(generated) != 0 {
		t.Fatalf("missing generated ledger = %#v", generated)
	}
	writeFile(t, root, ".poolboy/generated.json", `{"version":"0","files":{"index.md":{"sha256":"abc"}}}`)
	generated, err = readGenerated(root)
	if err != nil {
		t.Fatal(err)
	}
	if generated["index.md"].SHA256 != "abc" {
		t.Fatalf("generated revisions = %#v", generated)
	}
	writeFile(t, root, ".poolboy/generated.json", "{not json")
	if _, err := readGenerated(root); err == nil || !strings.Contains(err.Error(), "read generated ledger") {
		t.Fatalf("malformed generated ledger error = %v", err)
	}
	path := filepath.Join(root, "state.json")
	if err := os.WriteFile(path, []byte(`{"version":"0"} trailing`), 0o600); err != nil {
		t.Fatal(err)
	}
	var value map[string]string
	if err := readJSON(path, &value); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("trailing state error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":"0","id":"x","unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var mark marker
	if err := readJSON(path, &mark); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown state error = %v", err)
	}
}

func TestReadBoundedRejectsNonRegularAndValidID(t *testing.T) {
	root := t.TempDir()
	if _, err := readBounded(root, maxMarkdownBytes); err == nil || !strings.Contains(err.Error(), "unsupported Markdown file") {
		t.Fatalf("directory read error = %v", err)
	}
	cases := map[string]bool{
		"":                                    false,
		"0123456789abcdef0123456789abcdef":    true,
		"0123456789ABCDEF0123456789abcdef":    false,
		"0123456789abcdef0123456789abcdeg":    false,
		"../0123456789abcdef0123456789abcdef": false,
	}
	for id, want := range cases {
		if got := validID(id); got != want {
			t.Errorf("validID(%q) = %v, want %v", id, got, want)
		}
	}
	if !within(root, filepath.Join(root, "nested", "file")) || within(root, filepath.Join(filepath.Dir(root), "outside")) {
		t.Fatalf("within boundary result incorrect")
	}
	oversized := filepath.Join(root, "oversized.md")
	if err := os.WriteFile(oversized, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(oversized, maxMarkdownBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := readBounded(oversized, maxMarkdownBytes); err == nil || !strings.Contains(err.Error(), "unsupported Markdown file") {
		t.Fatalf("oversized Markdown error = %v", err)
	}
	state := filepath.Join(root, "state.json")
	if err := os.WriteFile(state, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(state, maxCorpusBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := readBoundedJSON(state); err == nil || !strings.Contains(err.Error(), "unsupported state file") {
		t.Fatalf("oversized state error = %v", err)
	}
	link := filepath.Join(root, "state-link.json")
	if err := os.Symlink(state, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readBoundedJSON(link); err == nil || !strings.Contains(err.Error(), "unsupported state file") {
		t.Fatalf("symlink state error = %v", err)
	}
}

func TestReadDocumentsReportsMissingRoot(t *testing.T) {
	root := t.TempDir()
	b := testBundle(t, root)
	if _, err := readDocuments(b, filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing corpus root must error")
	}
}

func TestReadActionTargetRejectsOversizedFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.md")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, maxMarkdownBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readActionTarget(root, "large.md"); err == nil || !strings.Contains(err.Error(), "unsupported Markdown file") {
		t.Fatalf("oversized action target error = %v", err)
	}
}

func TestWriteAtomicReportsRenameFailure(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "destination")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, []byte("data"), 0o644); err == nil {
		t.Fatal("atomic write to directory must error")
	}
}
