package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// publishWorkflowPath is where GitHub reads workflows from; the name is
// Poolboy's so a project can keep its own CI beside it.
const publishWorkflowPath = ".github/workflows/poolboy-docs.yml"

// cmdPublish reports what publishing this corpus on GitHub Pages requires and,
// with --apply, writes the workflow. It performs no network calls and changes
// no repository settings: enabling Pages is an admin action a maintainer takes
// deliberately, and this command's job is to say exactly which one.
func cmdPublish(args []string) int {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	apply := fs.Bool("apply", false, "write the publishing workflow into the project")
	force := fs.Bool("force", false, "overwrite an existing Poolboy publishing workflow")
	pin := fs.String("pin", "", "Poolboy version the workflow runs (default: this build's version)")
	format := fs.String("format", "text", "output format: text|json|csv|tsv")
	if code := productFlags(fs, args); code >= 0 {
		return code
	}
	if fs.NArg() != 0 {
		return productFailure(*format, actionableStep{
			Code:       "INVALID_ARGUMENT",
			Status:     "blocked",
			Owner:      "poolboy",
			Detail:     "publish accepts no positional arguments",
			NextAction: "remove positional arguments and rerun publish",
		})
	}
	b, err := productBundle()
	if err != nil {
		return productFailure(*format, actionableStep{
			Code:       "PROJECT_DISCOVERY_FAILED",
			Status:     "blocked",
			Owner:      "poolboy",
			Detail:     err.Error(),
			NextAction: "run publish from a Poolboy project root",
		})
	}

	plan := publishPlan{
		Output:   b.Output,
		Workflow: publishWorkflowPath,
		Version:  strings.TrimPrefix(strings.TrimSpace(*pin), "v"),
	}
	if plan.Version == "" {
		plan.Version = strings.TrimPrefix(Version, "v")
	}
	plan.VersionSyntaxValid = releaseVersion.MatchString(plan.Version)
	workflowAbs := filepath.Join(b.Root, filepath.FromSlash(publishWorkflowPath))
	if _, err := os.Stat(workflowAbs); err == nil {
		plan.WorkflowExists = true
	} else if !os.IsNotExist(err) {
		return productFailure(*format, actionableStep{
			Code:       "WORKFLOW_INSPECTION_FAILED",
			Status:     "blocked",
			Owner:      "poolboy",
			Detail:     err.Error(),
			NextAction: "inspect the workflow path and rerun publish",
		})

	}
	plan.Branch, plan.BranchSource = defaultBranch(b.Root)
	plan.Owner, plan.Repo = githubRemote(b.Root)
	if plan.Owner != "" {
		plan.URL = fmt.Sprintf("https://%s.github.io/%s/", strings.ToLower(plan.Owner), plan.Repo)
		plan.EnableCommand = fmt.Sprintf("gh api -X POST repos/%s/%s/pages -f build_type=workflow", plan.Owner, plan.Repo)
	}
	if *apply {
		if plan.WorkflowExists && !*force {
			return productFailure(*format, actionableStep{
				Code:       "WORKFLOW_EXISTS",
				Status:     "blocked",
				Owner:      "poolboy",
				Path:       plan.Workflow,
				Detail:     fmt.Sprintf("%s already exists; Poolboy will not overwrite it", publishWorkflowPath),
				NextAction: "review the existing workflow or pass --force to overwrite it deliberately",
			})
		}
		// A version string is only a local syntax gate. Whether the package
		// exists on npm remains an unverified workflow prerequisite.
		if !plan.VersionSyntaxValid {
			return productFailure(*format, actionableStep{
				Code:       "VERSION_SYNTAX_INVALID",
				Status:     "blocked",
				Owner:      "poolboy",
				Detail:     fmt.Sprintf("version %q does not match Poolboy release syntax; npm availability is not checked offline", plan.Version),
				NextAction: "pass --pin VERSION using a release-shaped version, then verify npm resolution in the workflow run",
			})
		}
		// #nosec G301 -- workflow directories are user-facing repository content.
		if err := os.MkdirAll(filepath.Dir(workflowAbs), 0o755); err != nil {
			return productFailure(*format, actionableStep{
				Code:       "WORKFLOW_WRITE_FAILED",
				Status:     "blocked",
				Owner:      "poolboy",
				Path:       plan.Workflow,
				Detail:     err.Error(),
				NextAction: "ensure the project workflow directory is writable and rerun publish",
			})
		}
		// #nosec G306 -- a workflow is user-facing repository content.
		if err := os.WriteFile(workflowAbs, []byte(publishWorkflow(b.Output, plan.Version, plan.Branch)), 0o644); err != nil {
			return productFailure(*format, actionableStep{
				Code:       "WORKFLOW_WRITE_FAILED",
				Status:     "blocked",
				Owner:      "poolboy",
				Path:       plan.Workflow,
				Detail:     err.Error(),
				NextAction: "ensure the project workflow path is writable and rerun publish",
			})
		}
		plan.Written = true
	}

	plan.Prerequisites = plan.prerequisites()
	return productOutput(*format, plan.lines(), plan)
}

type publishPlan struct {
	Output             string           `json:"output"`
	Workflow           string           `json:"workflow"`
	WorkflowExists     bool             `json:"workflow_exists"`
	Written            bool             `json:"written"`
	Version            string           `json:"version"`
	VersionSyntaxValid bool             `json:"version_syntax_valid"`
	Owner              string           `json:"github_owner,omitempty"`
	Repo               string           `json:"github_repo,omitempty"`
	URL                string           `json:"url,omitempty"`
	Branch             string           `json:"branch"`
	BranchSource       string           `json:"branch_source"`
	EnableCommand      string           `json:"enable_command,omitempty"`
	Prerequisites      []actionableStep `json:"prerequisites"`
}

func (p publishPlan) prerequisites() []actionableStep {
	workflow := actionableStep{
		Step:  "workflow",
		Owner: "poolboy",
	}
	switch {
	case p.Written:
		workflow.Status = "ready"
		workflow.Detail = fmt.Sprintf("wrote %s with Poolboy version %s", p.Workflow, p.Version)
		workflow.NextAction = "review the generated workflow before committing it"
	case p.WorkflowExists:
		workflow.Status = "unverified"
		workflow.Detail = fmt.Sprintf("%s already exists", p.Workflow)
		workflow.NextAction = "review the existing workflow or pass --force to overwrite it deliberately"
		if !p.VersionSyntaxValid {
			workflow.NextAction += "; replacement also requires --pin VERSION because this build's version does not match release syntax"
		}
	case !p.VersionSyntaxValid:
		workflow.Status = "blocked"
		workflow.Detail = fmt.Sprintf("version %q does not match release syntax", p.Version)
		workflow.NextAction = "pass --pin VERSION using a release-shaped version before --apply"
	default:
		workflow.Status = "ready"
		workflow.Detail = fmt.Sprintf("%s is ready to be written with Poolboy version %s", p.Workflow, p.Version)
		workflow.NextAction = "pass --apply after reviewing the plan"
	}

	branch := actionableStep{
		Step:       "trigger_branch",
		Owner:      "local Git metadata",
		Status:     "ready",
		Detail:     fmt.Sprintf("workflow runs on %s (%s)", p.Branch, p.BranchSource),
		NextAction: "review the resolved branch before committing the workflow",
	}
	if !strings.HasPrefix(p.BranchSource, "origin's default") {
		branch.Status = "unverified"
		branch.NextAction = "verify origin's default branch or edit the generated workflow before committing it"
	}

	pages := actionableStep{
		Step:   "pages_admin",
		Owner:  "GitHub administrator",
		Status: "unverified",
	}
	if p.Owner == "" {
		pages.Detail = "no GitHub remote found; Pages target and admin access cannot be derived locally"
		pages.NextAction = "resolve the GitHub owner/repository and enable Pages at Settings -> Pages -> Build and deployment -> Source: GitHub Actions"
	} else {
		pages.Detail = fmt.Sprintf("Pages enablement and admin access for %s are not checked offline", p.URL)
		pages.NextAction = "enable Pages at Settings -> Pages -> Build and deployment -> Source: GitHub Actions"
		if p.EnableCommand != "" {
			pages.NextAction += "; or run " + p.EnableCommand
		}
	}

	npm := actionableStep{
		Step:       "npm_resolution",
		Owner:      "npm/GitHub workflow",
		Status:     "unverified",
		Detail:     fmt.Sprintf("npm resolution of @grosspoetrysystems/poolboy@%s is not checked offline", p.Version),
		NextAction: "inspect the workflow run to confirm the exact Poolboy package resolves on npm",
	}

	return []actionableStep{
		{Step: "output", Owner: "poolboy", Status: "unverified", Detail: "configured corpus output is " + p.Output, NextAction: "run poolboy check and build; keep generated output out of the commit"},
		workflow,
		branch,
		pages,
		npm,
		{
			Step:       "commit_push",
			Owner:      "repository maintainer",
			Status:     "unverified",
			Detail:     "committing and pushing the workflow are outside Poolboy",
			NextAction: "review, commit, and push the workflow on the intended branch",
		},
		{
			Step:       "auto_merge",
			Owner:      "repository maintainer",
			Status:     "unverified",
			Detail:     "auto-merge policy is not checked or changed by Poolboy",
			NextAction: "follow the repository's merge policy; do not assume auto-merge is available",
		},
		{
			Step:       "branch_protection",
			Owner:      "repository maintainer",
			Status:     "unverified",
			Detail:     "branch protection and rulesets are not checked or changed by Poolboy",
			NextAction: "verify the repository rules and obtain maintainer authorization before merging",
		},
		{
			Step:       "required_ci",
			Owner:      "repository maintainer",
			Status:     "unverified",
			Detail:     "required CI status is not checked by Poolboy",
			NextAction: "inspect and fix required CI checks on the target branch before merging",
		},
	}
}

func (p publishPlan) lines() []string {
	lines := []string{"Publish target: GitHub Pages", "  corpus output: " + p.Output}
	if p.URL != "" {
		lines = append(lines, "  target URL:    "+p.URL)
	} else {
		lines = append(lines, "  target URL:    unavailable (no GitHub remote found)")
	}
	switch {
	case p.Written:
		lines = append(lines, "  workflow:      wrote "+p.Workflow+", pinned to Poolboy "+p.Version)
	case p.WorkflowExists:
		lines = append(lines, "  workflow:      "+p.Workflow+" already exists (--apply --force replaces it)")
	case !p.VersionSyntaxValid:
		lines = append(lines,
			"  workflow:      not present; --apply refused: this build reports version "+p.Version+",",
			"                 which does not match release syntax. Pass --pin VERSION.",
		)
	default:
		lines = append(lines, "  workflow:      not present; --apply writes "+p.Workflow+" pinned to Poolboy "+p.Version)
	}
	lines = append(lines, "", "Prerequisites:")
	steps := p.Prerequisites
	if steps == nil {
		steps = p.prerequisites()
	}
	for _, step := range steps {
		lines = append(lines, fmt.Sprintf("  %-18s [%s] %s", step.Step, step.Status, step.Detail))
		if step.NextAction != "" {
			lines = append(lines, "    next: "+step.NextAction)
		}
	}
	return lines
}

// githubRemote reads origin from the repository's config rather than shelling
// out, so it stays offline. An unrecognized or missing remote is not an error:
// the plan simply names no URL.
func githubRemote(root string) (owner, repo string) {
	dir, err := gitDir(root)
	if err != nil {
		return "", ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "config")) // #nosec G304 -- the discovered project's own Git config.
	if err != nil {
		return "", ""
	}
	match := githubRemotePattern.FindStringSubmatch(string(data))
	if match == nil {
		return "", ""
	}
	return match[1], strings.TrimSuffix(match[2], ".git")
}

// gitDir resolves the directory holding `config`. In a linked worktree `.git`
// is a file pointing at .git/worktrees/<name>, whose config lives in the main
// repository named by `commondir` — the shape every trial worktree has.
func gitDir(root string) (string, error) {
	path := filepath.Join(root, ".git")
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return path, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the discovered project's own .git pointer file.
	if err != nil {
		return "", err
	}
	pointer := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
	if pointer == "" {
		return "", errors.New("empty gitdir pointer")
	}
	if !filepath.IsAbs(pointer) {
		pointer = filepath.Join(root, pointer)
	}
	common, err := os.ReadFile(filepath.Join(pointer, "commondir")) // #nosec G304,G703 -- path comes from this repository's own .git pointer, read-only.
	if err != nil {
		return pointer, nil
	}
	shared := strings.TrimSpace(string(common))
	if !filepath.IsAbs(shared) {
		shared = filepath.Join(pointer, shared)
	}
	return filepath.Clean(shared), nil
}

var githubRemotePattern = regexp.MustCompile(`(?m)^\s*url\s*=\s*(?:https://github\.com/|git@github\.com:)([^/\s]+)/(\S+?)(?:\.git)?\s*$`)

// defaultBranch reports the branch the workflow should watch and where that
// came from. origin/HEAD is the only local record of the remote's default; it
// is a symbolic ref, so it lives in a loose file even when branches are
// packed. Absent it, say `main` and say why. It never adopts the checked-out
// branch: scaffolding from a topic branch would write a workflow that installs
// cleanly and silently never fires.
func defaultBranch(root string) (branch, source string) {
	dir, err := gitDir(root)
	if err != nil {
		return "main", "assumed; no Git repository found"
	}
	data, err := os.ReadFile(filepath.Join(dir, "refs", "remotes", "origin", "HEAD")) // #nosec G304 -- inside the resolved Git directory.
	if err != nil {
		return "main", "assumed; this checkout has no record of origin's default"
	}
	name, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "ref: refs/remotes/origin/")
	if !ok || name == "HEAD" {
		return "main", "assumed; origin/HEAD is unreadable"
	}
	return name, "origin's default branch"
}

func publishWorkflow(output, version, branch string) string {
	return `# Publishes the Poolboy corpus to GitHub Pages. Written by ` + "`poolboy publish --apply`" + `.
#
# Enabling Pages is a separate admin action:
#   Settings -> Pages -> Build and deployment -> Source: GitHub Actions
name: Poolboy docs

on:
  push:
    branches: [` + branch + `]
  workflow_dispatch:

permissions:
  contents: read
  pages: write
  id-token: write

env:
  # Pinned to the Poolboy that generated this file. Bump it deliberately; a
  # floating version changes your published bytes without a commit here.
  POOLBOY: "` + version + `"

concurrency:
  group: poolboy-pages
  cancel-in-progress: true

jobs:
  publish:
    runs-on: ubuntu-latest
    environment:
      name: github-pages
      url: ${{ steps.deploy.outputs.page_url }}
    steps:
      - uses: actions/checkout@v4
      - run: npx --yes "@grosspoetrysystems/poolboy@$POOLBOY" check
      - run: npx --yes "@grosspoetrysystems/poolboy@$POOLBOY" build
      - uses: actions/configure-pages@v5
      - uses: actions/upload-pages-artifact@v3
        with:
          path: ` + output + `
      - id: deploy
        uses: actions/deploy-pages@v4
`
}

// releaseVersion checks only the local syntax accepted for a workflow pin.
// npm availability remains an external prerequisite that Poolboy does not
// check while offline.
var releaseVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$`)
