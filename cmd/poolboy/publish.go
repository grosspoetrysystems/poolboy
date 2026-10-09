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
	pin := fs.String("pin", "", "published Poolboy version the workflow runs (default: this build's version)")
	format := fs.String("format", "text", "output format: text|json|csv|tsv")
	if code := productFlags(fs, args); code >= 0 {
		return code
	}
	if fs.NArg() != 0 {
		return productError(errors.New("publish accepts no positional arguments"))
	}
	b, err := productBundle()
	if err != nil {
		return productError(err)
	}

	plan := publishPlan{
		Output:   b.Output,
		Workflow: publishWorkflowPath,
		Version:  strings.TrimPrefix(strings.TrimSpace(*pin), "v"),
	}
	if plan.Version == "" {
		plan.Version = strings.TrimPrefix(Version, "v")
	}
	plan.Released = releaseVersion.MatchString(plan.Version)
	workflowAbs := filepath.Join(b.Root, filepath.FromSlash(publishWorkflowPath))
	if _, err := os.Stat(workflowAbs); err == nil {
		plan.WorkflowExists = true
	} else if !os.IsNotExist(err) {
		return productError(err)
	}
	plan.Branch, plan.BranchSource = defaultBranch(b.Root)
	plan.Owner, plan.Repo = githubRemote(b.Root)
	if plan.Owner != "" {
		plan.URL = fmt.Sprintf("https://%s.github.io/%s/", strings.ToLower(plan.Owner), plan.Repo)
		plan.EnableCommand = fmt.Sprintf("gh api -X POST repos/%s/%s/pages -f build_type=workflow", plan.Owner, plan.Repo)
	}

	if *apply {
		if plan.WorkflowExists && !*force {
			return productError(fmt.Errorf("%s already exists; pass --force to overwrite", publishWorkflowPath))
		}
		// A workflow pinned to a version npm cannot resolve is a deploy that
		// never worked. Refuse rather than write one.
		if !plan.Released {
			return productError(fmt.Errorf("this build reports version %q, which is not a published release; pass --pin VERSION to choose the Poolboy the workflow runs", plan.Version))
		}
		// #nosec G301 -- workflow directories are user-facing repository content.
		if err := os.MkdirAll(filepath.Dir(workflowAbs), 0o755); err != nil {
			return productError(err)
		}
		// #nosec G306 -- a workflow is user-facing repository content.
		if err := os.WriteFile(workflowAbs, []byte(publishWorkflow(b.Output, plan.Version, plan.Branch)), 0o644); err != nil {
			return productError(err)
		}
		plan.Written = true
	}

	return productOutput(*format, plan.lines(), plan)
}

type publishPlan struct {
	Output         string `json:"output"`
	Workflow       string `json:"workflow"`
	WorkflowExists bool   `json:"workflow_exists"`
	Written        bool   `json:"written"`
	Version        string `json:"version"`
	Released       bool   `json:"version_is_release"`
	Owner          string `json:"github_owner,omitempty"`
	Repo           string `json:"github_repo,omitempty"`
	URL            string `json:"url,omitempty"`
	Branch         string `json:"branch"`
	BranchSource   string `json:"branch_source"`
	EnableCommand  string `json:"enable_command,omitempty"`
}

func (p publishPlan) lines() []string {
	lines := []string{"Publish target: GitHub Pages", "  corpus output: " + p.Output}
	if p.URL != "" {
		lines = append(lines, "  published at:  "+p.URL)
	} else {
		lines = append(lines, "  published at:  https://OWNER.github.io/REPO/ (no GitHub remote found)")
	}
	switch {
	case p.Written:
		lines = append(lines, "  workflow:      wrote "+p.Workflow+", pinned to Poolboy "+p.Version)
	case p.WorkflowExists:
		lines = append(lines, "  workflow:      "+p.Workflow+" already exists (--apply --force replaces it)")
	case !p.Released:
		lines = append(lines,
			"  workflow:      not present; --apply refused: this build reports version "+p.Version+",",
			"                 which is not a published release. Pass --pin VERSION.",
		)
	default:
		lines = append(lines, "  workflow:      not present; --apply writes "+p.Workflow+" pinned to Poolboy "+p.Version)
	}
	lines = append(lines,
		"",
		"Requires repository admin, once:",
		"  Settings -> Pages -> Build and deployment -> Source: GitHub Actions",
	)
	if p.EnableCommand != "" {
		lines = append(lines, "  or: "+p.EnableCommand)
	}
	lines = append(lines,
		"",
		"Pages is public hosting. Nothing is published until that setting is enabled",
		"and the workflow runs on "+p.Branch+" ("+p.BranchSource+").",
		"",
		"Not checkable from here: whether Pages is enabled, whether you hold admin,",
		"and whether the pinned Poolboy resolves on npm. Each fails in the run log.",
	)
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

// releaseVersion matches a version npm can resolve. A build that does not
// report one cannot pin a workflow, so `publish --apply` refuses instead of
// writing a deploy that never worked.
var releaseVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$`)
