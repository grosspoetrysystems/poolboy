package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// publishProject is a minimal bundle with a GitHub origin, written the way a
// real checkout stores it: config on disk, no git binary involved.
func publishProject(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("poolboy.toml", "spec = \"0.2\"\ncorpus = \"docs\"\noutput = \"site\"\n")
	write("docs/index.md", "---\nokf_version: \"0.2\"\n---\n# Docs\n")
	if remote != "" {
		write(".git/config", "[remote \"origin\"]\n\turl = "+remote+"\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n")
	}
	return dir
}

func TestPublishPlansWithoutTouchingTheRepository(t *testing.T) {
	dir := publishProject(t, "git@github.com:acme/widget.git")
	t.Chdir(dir)

	out, code := capture(t, func() int { return cmdPublish(nil) })
	if code != 0 {
		t.Fatalf("publish exit %d: %s", code, out)
	}
	for _, want := range []string{
		"https://acme.github.io/widget/",
		"gh api -X POST repos/acme/widget/pages -f build_type=workflow",
		"site",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan missing %q:\n%s", want, out)
		}
	}
	// A plan writes nothing: publishing is an explicit second step.
	if _, err := os.Stat(filepath.Join(dir, ".github")); !os.IsNotExist(err) {
		t.Errorf("bare publish created .github: %v", err)
	}
}

func TestPublishApplyWritesWorkflowForConfiguredOutput(t *testing.T) {
	dir := publishProject(t, "https://github.com/acme/widget")
	t.Chdir(dir)

	// A source build reports no published version, so --apply must refuse
	// rather than write a workflow npm cannot resolve.
	if _, code := capture(t, func() int { return cmdPublish([]string{"--apply"}) }); code != 2 {
		t.Fatalf("unreleased --apply exit = %d, want 2", code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".github")); !os.IsNotExist(err) {
		t.Fatalf("refused --apply still wrote a workflow: %v", err)
	}

	if out, code := capture(t, func() int { return cmdPublish([]string{"--apply", "--pin", "0.2.1"}) }); code != 0 {
		t.Fatalf("publish --apply exit %d: %s", code, out)
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(publishWorkflowPath)))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	// The workflow must upload what the bundle actually builds, not a guess.
	if !strings.Contains(workflow, "path: site") {
		t.Errorf("workflow does not upload the configured output:\n%s", workflow)
	}
	if !strings.Contains(workflow, `POOLBOY: "0.2.1"`) {
		t.Errorf("workflow is not pinned to the requested version:\n%s", workflow)
	}

	// Rewriting is refused without --force, so a tuned workflow survives.
	if _, code := capture(t, func() int { return cmdPublish([]string{"--apply", "--pin", "0.2.1"}) }); code != 2 {
		t.Errorf("second --apply exit = %d, want 2", code)
	}
	if _, code := capture(t, func() int { return cmdPublish([]string{"--apply", "--force", "--pin", "0.2.1"}) }); code != 0 {
		t.Errorf("--apply --force exit = %d", code)
	}
}

// A linked worktree keeps its config in the main repository, which is exactly
// where trial checkouts run.
func TestPublishResolvesRemoteFromLinkedWorktree(t *testing.T) {
	main := publishProject(t, "git@github.com:acme/widget.git")
	worktree := publishProject(t, "")
	gitdir := filepath.Join(main, ".git", "worktrees", "trial")
	if err := os.MkdirAll(gitdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitdir, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(worktree)

	out, code := capture(t, func() int { return cmdPublish(nil) })
	if code != 0 {
		t.Fatalf("publish exit %d: %s", code, out)
	}
	if !strings.Contains(out, "https://acme.github.io/widget/") {
		t.Errorf("worktree did not resolve the main repository's remote:\n%s", out)
	}
}

func TestPublishWithoutGitHubRemoteStillPlans(t *testing.T) {
	t.Chdir(publishProject(t, "git@gitlab.com:acme/widget.git"))

	out, code := capture(t, func() int { return cmdPublish(nil) })
	if code != 0 {
		t.Fatalf("publish exit %d: %s", code, out)
	}
	if !strings.Contains(out, "no GitHub remote found") || strings.Contains(out, "gh api") {
		t.Errorf("non-GitHub remote should yield no URL or enable command:\n%s", out)
	}
}

// A repository that does not use `main` must get a workflow that triggers;
// a guessed branch installs cleanly and then never runs.
func TestPublishUsesTheRepositoryDefaultBranch(t *testing.T) {
	dir := publishProject(t, "git@github.com:acme/widget.git")
	if err := os.MkdirAll(filepath.Join(dir, ".git", "refs", "remotes", "origin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "refs", "remotes", "origin", "HEAD"), []byte("ref: refs/remotes/origin/trunk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	out, code := capture(t, func() int { return cmdPublish([]string{"--apply", "--pin", "0.2.1"}) })
	if code != 0 {
		t.Fatalf("publish exit %d: %s", code, out)
	}
	if !strings.Contains(out, "runs on trunk") {
		t.Errorf("plan does not name the real branch:\n%s", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(publishWorkflowPath)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "branches: [trunk]") {
		t.Errorf("workflow watches the wrong branch:\n%s", data)
	}
}

// Without a recorded remote default, say `main` and say it is an assumption —
// adopting whatever branch happens to be checked out writes a workflow that
// never fires.
func TestPublishMarksAnAssumedBranch(t *testing.T) {
	dir := publishProject(t, "git@github.com:acme/widget.git")
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/my-feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	out, code := capture(t, func() int { return cmdPublish([]string{"--apply", "--pin", "0.2.1"}) })
	if code != 0 {
		t.Fatalf("publish exit %d: %s", code, out)
	}
	if strings.Contains(out, "my-feature") {
		t.Errorf("plan adopted the checked-out branch:\n%s", out)
	}
	if !strings.Contains(out, "main (assumed") {
		t.Errorf("plan does not flag the branch as assumed:\n%s", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(publishWorkflowPath)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "branches: [main]") {
		t.Errorf("workflow watches the wrong branch:\n%s", data)
	}
}

// The gate that decides whether a workflow can be pinned at all.
func TestReleaseVersionGate(t *testing.T) {
	for _, version := range []string{"dev", "", "0.0.0-SNAPSHOT-5e0a027 ", "latest"} {
		if releaseVersion.MatchString(version) {
			t.Errorf("%q accepted as a published release", version)
		}
	}
	for _, version := range []string{"0.2.1", "v1.0.0", "1.2.3-rc.1"} {
		if !releaseVersion.MatchString(version) {
			t.Errorf("%q rejected as a published release", version)
		}
	}
}
func TestPublishJSONPrerequisitesAndOfflineRefusals(t *testing.T) {
	dir := publishProject(t, "https://github.com/acme/widget")
	t.Chdir(dir)

	out, code := capture(t, func() int { return cmdPublish([]string{"--format", "json"}) })
	if code != 0 {
		t.Fatalf("publish json exit %d: %s", code, out)
	}
	var plan publishPlan
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("publish json: %v\n%s", err, out)
	}
	if plan.VersionSyntaxValid || len(plan.Prerequisites) == 0 {
		t.Fatalf("plan = %+v", plan)
	}
	for _, name := range []string{"pages_admin", "npm_resolution", "auto_merge", "branch_protection", "required_ci"} {
		found := false
		for _, step := range plan.Prerequisites {
			if step.Step == name && step.Status == "unverified" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing unverified prerequisite %q: %+v", name, plan.Prerequisites)
		}
	}
	text, code := capture(t, func() int { return cmdPublish(nil) })
	if code != 0 {
		t.Fatalf("publish text exit %d: %s", code, text)
	}
	for _, want := range []string{"pages_admin", "npm_resolution", "auto_merge", "branch_protection", "required_ci", "unverified"} {
		if !strings.Contains(text, want) {
			t.Errorf("text plan missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(out, `"version_is_release"`) || strings.Contains(out, `"released": true`) {
		t.Fatalf("publish claimed an npm release from syntax: %s", out)
	}

	if _, code := capture(t, func() int { return cmdPublish([]string{"--apply", "--pin", "0.2.1"}) }); code != 0 {
		t.Fatalf("initial apply exit = %d", code)
	}
	workflowPath := filepath.Join(dir, filepath.FromSlash(publishWorkflowPath))
	before, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	out, code = capture(t, func() int {
		return cmdPublish([]string{"--apply", "--pin", "0.2.1", "--format", "json"})
	})
	if code != 2 {
		t.Fatalf("existing workflow exit = %d, want 2; output=%s", code, out)
	}
	var failure actionableStep
	if err := json.Unmarshal([]byte(out), &failure); err != nil {
		t.Fatalf("existing workflow failure JSON: %v\n%s", err, out)
	}
	if failure.Code != "WORKFLOW_EXISTS" || failure.Status != "blocked" || !strings.Contains(failure.NextAction, "--force") {
		t.Fatalf("existing workflow failure = %+v", failure)
	}
	after, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("refused workflow replacement changed the file")
	}

	other := publishProject(t, "https://github.com/acme/widget")
	t.Chdir(other)
	out, code = capture(t, func() int {
		return cmdPublish([]string{"--apply", "--format", "json"})
	})
	if code != 2 {
		t.Fatalf("invalid version exit = %d, want 2; output=%s", code, out)
	}
	if err := json.Unmarshal([]byte(out), &failure); err != nil {
		t.Fatalf("invalid version failure JSON: %v\n%s", err, out)
	}
	if failure.Code != "VERSION_SYNTAX_INVALID" || failure.Status != "blocked" || !strings.Contains(failure.NextAction, "--pin") {
		t.Fatalf("invalid version failure = %+v", failure)
	}
	if _, err := os.Stat(filepath.Join(other, ".github")); !os.IsNotExist(err) {
		t.Fatal("refused invalid version created workflow files")
	}
}

func TestPublishJSONReportsWorkflowPrerequisiteStates(t *testing.T) {
	t.Cleanup(func() { rootDir = "" })
	rootDir = ""

	existingRoot := publishProject(t, "https://github.com/acme/widget")
	existingWorkflow := filepath.Join(existingRoot, filepath.FromSlash(publishWorkflowPath))
	if err := os.MkdirAll(filepath.Dir(existingWorkflow), 0o755); err != nil {
		t.Fatal(err)
	}
	const existingContents = "name: hand-written workflow\n"
	if err := os.WriteFile(existingWorkflow, []byte(existingContents), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(existingRoot)
	out, code := capture(t, func() int {
		return cmdPublish([]string{"--format", "json", "--pin", "dev"})
	})
	if code != 0 {
		t.Fatalf("existing-workflow plan exit = %d: %s", code, out)
	}
	var existingPlan publishPlan
	if err := json.Unmarshal([]byte(out), &existingPlan); err != nil {
		t.Fatalf("existing-workflow plan JSON: %v\n%s", err, out)
	}
	if !existingPlan.WorkflowExists || existingPlan.Written || existingPlan.VersionSyntaxValid {
		t.Fatalf("existing-workflow plan = %+v", existingPlan)
	}
	var existingStep actionableStep
	for _, step := range existingPlan.Prerequisites {
		if step.Step == "workflow" {
			existingStep = step
			break
		}
	}
	if existingStep.Status != "unverified" || existingStep.Owner != "poolboy" ||
		!strings.Contains(existingStep.NextAction, "--pin VERSION") {
		t.Fatalf("existing-workflow prerequisite = %+v", existingStep)
	}
	after, err := os.ReadFile(existingWorkflow)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != existingContents {
		t.Fatalf("plan changed existing workflow: %q", after)
	}

	readyRoot := publishProject(t, "https://github.com/acme/widget")
	t.Chdir(readyRoot)
	out, code = capture(t, func() int {
		return cmdPublish([]string{"--format", "json", "--pin", "0.2.1"})
	})
	if code != 0 {
		t.Fatalf("release-pin plan exit = %d: %s", code, out)
	}
	var readyPlan publishPlan
	if err := json.Unmarshal([]byte(out), &readyPlan); err != nil {
		t.Fatalf("release-pin plan JSON: %v\n%s", err, out)
	}
	if readyPlan.WorkflowExists || readyPlan.Written || !readyPlan.VersionSyntaxValid ||
		readyPlan.Version != "0.2.1" {
		t.Fatalf("release-pin plan = %+v", readyPlan)
	}
	var readyStep actionableStep
	for _, step := range readyPlan.Prerequisites {
		if step.Step == "workflow" {
			readyStep = step
			break
		}
	}
	if readyStep.Status != "ready" || readyStep.Owner != "poolboy" {
		t.Fatalf("release-pin prerequisite = %+v", readyStep)
	}
	if _, err := os.Stat(filepath.Join(readyRoot, filepath.FromSlash(publishWorkflowPath))); !os.IsNotExist(err) {
		t.Fatalf("plan-only release pin created workflow: %v", err)
	}
}

func TestPublishRejectsUsageAndWorkflowWriteFailure(t *testing.T) {
	t.Cleanup(func() { rootDir = "" })
	rootDir = ""
	t.Chdir(t.TempDir())
	out, code := capture(t, func() int {
		return cmdPublish([]string{"--format", "json"})
	})
	if code != 2 || !strings.Contains(out, `"code": "PROJECT_DISCOVERY_FAILED"`) {
		t.Fatalf("publish outside project=%q code=%d", out, code)
	}

	root := publishProject(t, "https://github.com/acme/widget")
	t.Chdir(root)
	out, code = capture(t, func() int {
		return cmdPublish([]string{"--format", "json", "--pin", "0.2.1", "extra"})
	})
	if code != 2 || !strings.Contains(out, `"code": "INVALID_ARGUMENT"`) {
		t.Fatalf("publish positional=%q code=%d", out, code)
	}

	if err := os.MkdirAll(filepath.Join(root, ".github"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".github", "workflows"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code = capture(t, func() int {
		return cmdPublish([]string{"--apply", "--pin", "0.2.1", "--format", "json"})
	})
	if code != 2 || (!strings.Contains(out, `"code": "WORKFLOW_INSPECTION_FAILED"`) && !strings.Contains(out, `"code": "WORKFLOW_WRITE_FAILED"`)) {
		t.Fatalf("publish workflow failure=%q code=%d", out, code)
	}
}
