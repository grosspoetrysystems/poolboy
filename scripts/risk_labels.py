#!/usr/bin/env python3
"""Derive advisory risk labels for a pull request from the paths it changes.

Advisory: labels sit alongside the author's Risk checkboxes and send nothing back
to the author. Admission is unaffected. The job summary shows the derived labels
next to the Risk boxes the author ticked, so a later step can compare them.

This script never checks out or runs pull request code. Paths come from the
GitHub API, and `.gitattributes` comes from a trusted default-branch checkout or
that branch's contents API response.
"""

from __future__ import annotations

import argparse
import base64
import fnmatch
import os
import re
import sys
import urllib.error
import urllib.parse
from pathlib import Path
from typing import Any

# Reuse the queue's GitHub client and the admission check's section parsing rather
# than writing second conventions for either.
sys.path.insert(0, str(Path(__file__).resolve().parent))
from pr_queue import GitHub
from validate_pr import _section

# Path-to-label rules. Adopters edit this table in their copy. A pattern with a
# slash matches the full path; a bare pattern matches the basename at any depth.
# fnmatch's `*` crosses `/`, so `.github/workflows/*` also matches nested files.
# Lockfiles by name, not "*lock*": that would also catch clock.ts or deadlock_test.go.
LOCKFILES = ("*.lock", "*.lockb", "package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "go.sum")

RULES: tuple[tuple[str, tuple[str, ...]], ...] = (
    (
        "risk:dependencies",
        (
            "package.json",
            *LOCKFILES,
            "go.mod",
            "requirements*.txt",
            "pyproject.toml",
            "Cargo.*",
            "Gemfile*",
            "*.csproj",
        ),
    ),
    (
        "risk:automation",
        (
            ".github/workflows/*",
            ".github/actions/*",
            "action.yml",
            "action.yaml",
            ".github/dependabot.yml",
            ".github/dependabot.yaml",
            "CODEOWNERS",
            ".github/release.yml",
            ".github/release.yaml",
            "release.yml",
            "release.yaml",
            "release.config.*",
            ".releaserc*",
            "AGENTS.md",
            "CLAUDE.md",
            ".cursorrules",
            ".github/copilot-instructions.md",
            ".pr_agent.toml",
        ),
    ),
)

# The template's Risk checkboxes that have a path-derived label. Authentication
# has no path signal, so it has no label; that stays the author's box.
RISK_BOXES = {
    "Dependencies or lockfiles": "risk:dependencies",
    "GitHub Actions, build, release, policy, or agent instructions": "risk:automation",
    "Generated, vendored, minified, or binary content": "risk:generated",
}

GENERATED = "risk:generated"
# "Split this": more changed lines or files than one review holds, with lockfiles
# and generated or vendored files excluded. Adopters edit these in their copy.
LARGE = "risk:large"
SPLIT_LINES = 1000
SPLIT_FILES = 50
RISK_PREFIX = "risk:"
# GitHub caps the changed-file list at 3000; at or above it, treat the rest as unknown.
FILE_CAP = 3000
# In the workflow this resolves inside the sparse trusted default-branch checkout.
GITATTRIBUTES = Path(__file__).resolve().parent.parent / ".gitattributes"


def _match(path: str, pattern: str) -> bool:
    """Match a full path for slash patterns and a basename otherwise."""
    candidate = path if "/" in pattern else path.rsplit("/", 1)[-1]
    return fnmatch.fnmatchcase(candidate, pattern)


def generated_patterns(gitattributes: str) -> list[str]:
    """Return fnmatch patterns marked generated or vendored in `.gitattributes`."""
    patterns: list[str] = []
    for raw in gitattributes.splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split()
        pattern, attributes = parts[0], parts[1:]
        if any(
            attribute in {
                "linguist-generated",
                "linguist-generated=true",
                "linguist-vendored",
                "linguist-vendored=true",
            }
            for attribute in attributes
        ):
            # fnmatch has no special `**`, and its `*` crosses `/`, which is the
            # useful approximation for gitattributes recursive patterns here.
            glob = pattern.lstrip("/").replace("**", "*")
            patterns.append(glob + "*" if glob.endswith("/") else glob)
    return patterns


def derive(paths: list[str], generated: list[str]) -> dict[str, list[str]]:
    """Map changed paths to labels, retaining the paths behind every label."""
    hits: dict[str, list[str]] = {}
    for path in paths:
        labels = {label for label, patterns in RULES if any(_match(path, pattern) for pattern in patterns)}
        if any(_match(path, pattern) for pattern in generated):
            labels.add(GENERATED)
        for label in labels:
            hits.setdefault(label, []).append(path)
    return {label: sorted(paths) for label, paths in sorted(hits.items())}


def large(files: dict[str, int], generated: list[str]) -> str | None:
    """Describe the change when it is too large to review as one, else None."""
    counted = {
        path: changes
        for path, changes in files.items()
        if not any(_match(path, pattern) for pattern in (*LOCKFILES, *generated))
    }
    lines = sum(counted.values())
    if lines > SPLIT_LINES or len(counted) > SPLIT_FILES:
        return f"{lines} changed lines in {len(counted)} files, lockfiles and generated files excluded"
    return None


def ticked_boxes(body: str) -> list[str]:
    """Return Risk checkbox labels ticked by the author.

    `_section` is the admission check's parser: it strips comments and template
    placeholders before this function reads the checkboxes.
    """
    section = _section(body, "Risk")
    if not section:
        return []
    return [
        match.group(1).strip()
        for line in section.splitlines()
        if (match := re.match(r"- \[[xX]\]\s+(.*)", line))
    ]


def comparison(derived: set[str], ticked: list[str], unknown: bool = False) -> str:
    """Describe agreement or differences between paths and the author's boxes."""
    boxed = {RISK_BOXES[text] for text in ticked if text in RISK_BOXES}
    derived = derived - {LARGE}  # no Risk box asks about size
    if derived == boxed:
        result = "Paths and the author's Risk boxes agree."
    else:
        only_derived = sorted(derived - boxed)
        only_boxed = sorted(boxed - derived)
        differences = []
        if only_derived:
            differences.append(f"derived from paths but not ticked: {', '.join(only_derived)}")
        if only_boxed:
            differences.append(f"ticked but no path signal: {', '.join(only_boxed)}")
        result = "Differ — " + "; ".join(differences) + "."
    return ("Unknown file data; " + result) if unknown else result


def _markdown_path(path: str) -> str:
    """Keep filenames readable in the summary table."""
    escaped = path.replace("`", "\\`").replace("|", "\\|")
    return f"`{escaped}`"


def summary(derived: dict[str, list[str]], body: str, unknown: bool) -> str:
    """Render the measurement record for ``GITHUB_STEP_SUMMARY``."""
    lines = ["### Advisory risk labels", ""]
    if derived:
        lines += ["| Label | Paths |", "| --- | --- |"]
        lines += [
            f"| `{label}` | {', '.join(paths if label == LARGE else (_markdown_path(path) for path in paths))} |"
            for label, paths in derived.items()
        ]
    else:
        lines.append("No risk labels derived from the changed paths.")
    if unknown:
        lines += [
            "",
            "> Changed-file data or the default-branch `.gitattributes` was missing or "
            "truncated, so these labels may be incomplete; absence of a label is not a finding.",
        ]
    ticked = ticked_boxes(body)
    lines += ["", f"**Author's Risk boxes ticked:** {'; '.join(ticked) if ticked else 'none'}"]
    lines += ["", comparison(set(derived), ticked, unknown)]
    return "\n".join(lines) + "\n"


def changed_paths(github: GitHub, number: int, pull: dict[str, Any] | None = None) -> tuple[dict[str, int], bool]:
    """Return known changed paths with their changed-line counts, and whether any file data is unknown.

    GitHub exposes at most 3,000 files for a pull request. Missing filenames,
    missing counts, and a shorter-than-advertised response are all unknown rather
    than evidence that a path did not change.
    """
    files = github.request("GET", f"pulls/{number}/files?per_page=100")
    if pull is None:
        pull = github.request("GET", f"pulls/{number}")
    unknown = False
    paths: dict[str, int] = {}
    if not isinstance(files, list):
        return paths, True
    for item in files:
        if not isinstance(item, dict) or not isinstance(item.get("filename"), str):
            unknown = True
            continue
        changes = item.get("changes")
        if type(changes) is not int:
            unknown = True
            changes = 0
        paths[item["filename"]] = changes
    expected = pull.get("changed_files") if isinstance(pull, dict) else None
    if type(expected) is not int:
        unknown = True
    elif len(paths) < expected:
        unknown = True
    if len(paths) >= FILE_CAP:
        unknown = True
    return paths, unknown


def read_gitattributes(github: GitHub, pull: dict[str, Any]) -> tuple[str, bool]:
    """Read `.gitattributes` from the trusted base, with an API fallback.

    The workflow checks out the default branch sparsely, so the local file is the
    normal path. Standalone runs fetch the base branch's file instead. A 404 means
    the repository has no attributes file; other malformed or unavailable data is
    unknown and prevents stale-label removal.
    """
    if GITATTRIBUTES.exists():
        return GITATTRIBUTES.read_text(encoding="utf-8"), False
    base = pull.get("base") or {}
    branch = base.get("ref") or ((base.get("repo") or {}).get("default_branch"))
    if not isinstance(branch, str) or not branch:
        return "", True
    endpoint = f"contents/.gitattributes?ref={urllib.parse.quote(branch, safe='')}"
    try:
        data = github.request("GET", endpoint)
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return "", False
        raise
    if not isinstance(data, dict) or not isinstance(data.get("content"), str):
        return "", True
    if data.get("encoding") != "base64":
        return data["content"], False
    try:
        return base64.b64decode("".join(data["content"].split())).decode("utf-8"), False
    except (ValueError, UnicodeDecodeError):
        return "", True


def set_labels(
    github: GitHub,
    number: int,
    derived: set[str],
    current: list[dict[str, Any]],
    complete: bool = True,
) -> None:
    """Set derived risk labels without touching queue or other labels.

    When file data is incomplete, add observed labels but retain existing risk
    labels: unknown absence is never a negative finding.
    """
    have = {
        label.get("name")
        for label in current
        if isinstance(label, dict) and isinstance(label.get("name"), str) and label["name"].startswith(RISK_PREFIX)
    }
    if complete:
        for label in sorted(have - derived):
            github.request("DELETE", f"issues/{number}/labels/{urllib.parse.quote(label, safe='')}")
    add = sorted(derived - have)
    if add:
        # GitHub creates an absent label when it is added to an issue. This is
        # intentionally more tolerant than pr_queue.require_labels: risk labels
        # are advisory and the job is not a required admission check.
        github.request("POST", f"issues/{number}/labels", {"labels": add})


def reconcile(github: GitHub, number: int) -> dict[str, list[str]]:
    pull = github.request("GET", f"pulls/{number}")
    if not isinstance(pull, dict):
        raise SystemExit(f"GitHub returned no pull request data for #{number}")
    paths, files_unknown = changed_paths(github, number, pull)
    gitattributes, attributes_unknown = read_gitattributes(github, pull)
    patterns = generated_patterns(gitattributes)
    derived = derive(list(paths), patterns)
    split = large(paths, patterns)
    if split:
        derived[LARGE] = [split]
    complete = not files_unknown and not attributes_unknown
    set_labels(github, number, set(derived), pull.get("labels") or [], complete)
    report = summary(derived, pull.get("body") or "", not complete)
    step_summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if step_summary:
        with open(step_summary, "a", encoding="utf-8") as handle:
            handle.write(report)
    else:
        print(report)
    state = "; unknown file data" if not complete else ""
    print(f"#{number}: {', '.join(sorted(derived)) or 'no risk labels'}{state}")
    return derived


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--pr", type=int, required=True, help="pull request to label")
    parser.add_argument("--dry-run", action="store_true", help="print writes instead of making them")
    args = parser.parse_args()

    github = GitHub(os.environ["GITHUB_REPOSITORY"], os.environ["GITHUB_TOKEN"], args.dry_run)
    reconcile(github, args.pr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
