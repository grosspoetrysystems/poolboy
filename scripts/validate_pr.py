#!/usr/bin/env python3
"""Validate Poolboy's pull request metadata contract."""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable

from pr_queue import GitHub

MaintainerLookup = Callable[[str], bool]

REQUIRED_SECTIONS = ("Change", "Verification", "Risk")
PLACEHOLDERS = (
    "<problem, smallest solution, and user-visible effect>",
    "Linear: <Linear work item URL or `inline`>",
    "Commands and results:",
    "<commands and results; say what you couldn't run or test. Project CI verifies independently.>",
    "<commands and concise result; project CI verifies independently>",  # earlier wording, still in open pull requests
    "Check every area this pull request changes:",
    "Details and mitigations:",
)
DEPENDABOT_LOGIN = "dependabot[bot]"
DEPENDABOT_BRANCH = re.compile(r"dependabot/(?:github_actions|go_modules|npm_and_yarn)/\S+")
DEPENDABOT_TITLE = re.compile(r"(?:Bump|chore\(deps(?:-dev)?\): bump)\s+\S", re.IGNORECASE)


@dataclass(frozen=True)
class Refusal:
    code: str
    message: str


def _mapping(value: Any) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def _repo_id(value: Any) -> int | None:
    return value if type(value) is int and value > 0 else None


def _event_shape_refusal(event: Any) -> str | None:
    if not isinstance(event, dict):
        return "event JSON must be an object."
    pull_request = event.get("pull_request")
    if pull_request is not None and not isinstance(pull_request, dict):
        return "event `pull_request` must be an object."
    if isinstance(pull_request, dict):
        body = pull_request.get("body")
        if body is not None and not isinstance(body, str):
            return "event `pull_request.body` must be a string or null."
        for field in ("base", "head", "user"):
            value = pull_request.get(field)
            if value is not None and not isinstance(value, dict):
                return f"event `pull_request.{field}` must be an object."
    for field in ("repository", "sender"):
        value = event.get(field)
        if value is not None and not isinstance(value, dict):
            return f"event `{field}` must be an object."
    return None


def _metadata_exempt(
    event: dict[str, Any],
    mode: str,
    solo_maintainer_id: int,
    is_maintainer: MaintainerLookup | None = None,
) -> bool:
    pull_request = _mapping(event.get("pull_request"))
    author = _mapping(pull_request.get("user"))
    base_repository = _mapping(_mapping(pull_request.get("base")).get("repo"))
    head_repository = _mapping(_mapping(pull_request.get("head")).get("repo"))
    repository = _mapping(event.get("repository"))
    owner = _mapping(base_repository.get("owner"))
    sender = _mapping(event.get("sender"))
    repository_id = _repo_id(repository.get("id"))
    author_id = _repo_id(author.get("id"))
    sender_id = _repo_id(sender.get("id"))

    trusted_same_repository_author = (
        repository_id is not None
        and owner.get("type") in {"User", "Organization"}
        and author.get("type") == "User"
        and sender.get("type") == "User"
        and author_id is not None
        and sender_id == author_id
        and _repo_id(base_repository.get("id")) == repository_id
        and _repo_id(head_repository.get("id")) == repository_id
    )
    if not trusted_same_repository_author:
        return False

    if mode == "team":
        return bool(is_maintainer and is_maintainer(author.get("login", "")))

    if mode != "solo" or author_id != solo_maintainer_id:
        return False

    return owner.get("type") == "Organization" or _repo_id(owner.get("id")) == solo_maintainer_id



def _repository_bot(event: dict[str, Any]) -> bool:
    """A bot pushing a branch of this repository. GitHub allows that only with write access an administrator
    granted, so the grant is the authorization; there is no list to keep."""
    pull_request = _mapping(event.get("pull_request"))
    author = _mapping(pull_request.get("user"))
    sender = _mapping(event.get("sender"))
    repository_id = _repo_id(_mapping(event.get("repository")).get("id"))
    base_repository = _mapping(_mapping(pull_request.get("base")).get("repo"))
    head_repository = _mapping(_mapping(pull_request.get("head")).get("repo"))
    author_id = _repo_id(author.get("id"))
    sender_id = _repo_id(sender.get("id"))
    return (
        author.get("type") == "Bot"
        and bool(author.get("login"))
        and sender.get("login") == author.get("login")
        and author_id is not None
        and sender_id == author_id
        and repository_id is not None
        and _repo_id(base_repository.get("id")) == repository_id
        and _repo_id(head_repository.get("id")) == repository_id
    )


def _dependabot_refusals(event: dict[str, Any]) -> list[Refusal]:
    """Keep Poolboy's supported Dependabot target, branch, and title contract."""
    pull_request = _mapping(event.get("pull_request"))
    base = _mapping(pull_request.get("base"))
    head = _mapping(pull_request.get("head"))
    head_ref = head.get("ref") if isinstance(head.get("ref"), str) else ""
    title = pull_request.get("title") if isinstance(pull_request.get("title"), str) else ""
    refusals: list[Refusal] = []
    if base.get("ref") != "main":
        refusals.append(Refusal("REFUSE_DEPENDABOT_UNAUTHORIZED", "Dependabot PR must target main."))
    if DEPENDABOT_BRANCH.fullmatch(head_ref) is None:
        refusals.append(
            Refusal(
                "REFUSE_DEPENDABOT_UNAUTHORIZED",
                "Dependabot PR branch must match a configured dependency ecosystem.",
            )
        )
    if DEPENDABOT_TITLE.match(title) is None:
        refusals.append(
            Refusal(
                "REFUSE_DEPENDABOT_UNAUTHORIZED",
                "Dependabot PR title must use its standard dependency bump format.",
            )
        )
    return refusals


def _section(body: str, name: str) -> str | None:
    """Return section content without placeholders or Change references."""
    match = re.search(rf"^## {re.escape(name)}[ \t]*$(?P<text>.*?)(?=^## |\Z)", body, re.MULTILINE | re.DOTALL)
    if not match:
        return None
    lines = [line.strip() for line in match.group("text").splitlines()]
    return "\n".join(
        line for line in lines
        if line and line not in PLACEHOLDERS and not line.startswith("```")
        and not (name == "Change" and line.startswith("Linear:"))
    )


def validate_body(body: str) -> list[Refusal]:
    if not isinstance(body, str):
        return [Refusal("REFUSE_EVENT_INVALID", "pull request body must be text.")]

    body = re.sub(r"<!--.*?(?:-->|\Z)", "", body, flags=re.DOTALL)
    refusals: list[Refusal] = []
    sections = {name: _section(body, name) for name in REQUIRED_SECTIONS}

    missing = [f"## {name}" for name, text in sections.items() if text is None]
    if missing:
        refusals.append(
            Refusal(
                "REFUSE_TEMPLATE_INCOMPLETE",
                "Missing required sections: " + ", ".join(missing) + ". Copy them from the pull request template.",
            )
        )

    if sections["Change"] == "":
        refusals.append(
            Refusal("REFUSE_CHANGE_EMPTY", "Under ## Change, describe the problem, the fix, and its visible effect.")
        )
    if sections["Verification"] == "":
        refusals.append(
            Refusal(
                "REFUSE_VERIFICATION_EMPTY",
                "Under ## Verification, list the commands or steps you ran and what they showed.",
            )
        )
    if sections["Risk"] is not None and not re.search(r"^- \[[xX]\] ", sections["Risk"], re.MULTILINE):
        refusals.append(
            Refusal(
                "REFUSE_RISK_UNCHECKED",
                "Under ## Risk, check every area this pull request changes, or check `None of the above`.",
            )
        )
    return refusals


def _commit_pages(commits: list[Any]) -> list[dict[str, Any]]:
    """Accept the commits endpoint's array, or `gh api --paginate --slurp` pages of it."""
    return [commit for item in commits for commit in (item if isinstance(item, list) else [item])]


def validate_signoff(commits: list[Any]) -> list[Refusal]:
    """Every non-merge commit carries a `Signed-off-by` with its author's email (DCO 1.1)."""
    unsigned = []
    for commit in _commit_pages(commits):
        if len(commit.get("parents") or []) > 1:
            continue  # merge commits, such as GitHub's "Update branch", carry no sign-off
        details = commit.get("commit") or {}
        email = ((details.get("author") or {}).get("email") or "").lower()
        signers = re.findall(r"^Signed-off-by: .*<([^>]+)>\s*$", details.get("message") or "", re.MULTILINE | re.IGNORECASE)
        if not email or email not in {signer.lower() for signer in signers}:
            unsigned.append(str(commit.get("sha") or "?")[:7])
    if not unsigned:
        return []
    return [
        Refusal(
            "REFUSE_SIGNOFF_MISSING",
            f"Commits without a `Signed-off-by` matching their author's email: {', '.join(unsigned)}. "
            "Run `git rebase --signoff` on your branch and force-push.",
        )
    ]


def validate_open_prs(open_prs: int, limit: int) -> list[Refusal]:
    if limit <= 0 or open_prs <= limit:
        return []
    return [
        Refusal(
            "REFUSE_OPEN_PR_LIMIT",
            f"You have {open_prs} open non-draft pull requests here; the limit is {limit}. "
            "Convert one to a draft or close it, then edit this description to re-run the check.",
        )
    ]


def validate_shape(pull_request: dict[str, Any], default_branch: str, max_lines: int, max_files: int) -> list[Refusal]:
    """Outside contributions target the default branch and stay small enough to review. A limit of 0 is off."""
    refusals = []
    base = (pull_request.get("base") or {}).get("ref")
    if default_branch and base and base != default_branch:
        refusals.append(
            Refusal(
                "REFUSE_BASE_BRANCH",
                f"This pull request targets `{base}`; contributions go to `{default_branch}`. "
                "Change the base branch with **Edit** next to the title.",
            )
        )
    lines = (pull_request.get("additions") or 0) + (pull_request.get("deletions") or 0)
    files = pull_request.get("changed_files") or 0
    over = []
    if max_lines and lines > max_lines:
        over.append(f"{lines} changed lines (limit {max_lines})")
    if max_files and files > max_files:
        over.append(f"{files} changed files (limit {max_files})")
    if over:
        refusals.append(
            Refusal(
                "REFUSE_TOO_LARGE",
                f"This pull request has {' and '.join(over)}. Split it into smaller pull requests, "
                "or ask a maintainer in an issue first.",
            )
        )
    return refusals


def _limit(value: str, default: int) -> int:
    return int(value) if re.fullmatch(r"[0-9]+", value) else default


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("event", type=Path, help="GitHub pull request event JSON")
    parser.add_argument("--mode", default="contributor", help="repository mode: solo, contributor, or team")
    parser.add_argument(
        "--solo-maintainer-id",
        default="",
        help="immutable numeric GitHub user ID authorized in solo mode",
    )
    parser.add_argument(
        "--commits",
        type=Path,
        help="JSON from the pull request commits endpoint; checks DCO sign-off when given",
    )
    parser.add_argument(
        "--open-prs",
        type=int,
        default=0,
        help="the author's open non-draft pull requests in this repository, this one included",
    )
    parser.add_argument(
        "--open-pr-limit",
        default="2",
        help="most open non-draft pull requests an outside contributor may have; 0 disables",
    )
    parser.add_argument("--max-changed-lines", default="5000", help="outside contributors' size limit; 0 disables")
    parser.add_argument("--max-changed-files", default="300", help="outside contributors' file limit; 0 disables")
    args = parser.parse_args()

    try:
        event = json.loads(args.event.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        print(f"REFUSE_EVENT_INVALID: {error}")
        return 2

    shape_error = _event_shape_refusal(event)
    if shape_error:
        print(f"REFUSE_EVENT_INVALID: {shape_error}")
        return 2

    if (_mapping(event.get("pull_request")).get("draft")):
        print("Draft: admission runs when the pull request is marked ready for review.")
        return 0

    mode = args.mode if args.mode in {"solo", "contributor", "team"} else "contributor"
    solo_maintainer_id = int(args.solo_maintainer_id) if re.fullmatch(r"[1-9][0-9]*", args.solo_maintainer_id) else 0
    token = os.environ.get("GITHUB_TOKEN")
    repository = os.environ.get("GITHUB_REPOSITORY")
    github = GitHub(repository, token) if token and repository else None
    is_maintainer: MaintainerLookup = github.is_maintainer if github else lambda _login: False
    pull_request = _mapping(event.get("pull_request"))
    author = _mapping(pull_request.get("user"))

    if _metadata_exempt(event, mode, solo_maintainer_id, is_maintainer):
        print(f"{mode.capitalize()}-mode metadata exemption accepted for the trusted maintainer.")
        return 0

    if _repository_bot(event):
        if author.get("login") == DEPENDABOT_LOGIN:
            refusals = _dependabot_refusals(event)
            if refusals:
                print("Dependabot contribution intake refused:")
                for refusal in refusals:
                    print(f"{refusal.code}: {refusal.message}")
                return 1
        print("Bot on a branch of this repository: authorized by its write access. Human review is still required.")
        return 0

    if author.get("type") == "Bot":
        print(
            "REFUSE_BOT_UNAUTHORIZED: bots open pull requests only from a branch of this repository, which needs "
            "write access an administrator grants. Agents submit through a person's account; that person signs off."
        )
        return 1

    refusals = validate_body(pull_request.get("body") or "")
    if args.commits:
        try:
            commits = json.loads(args.commits.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as error:
            print(f"REFUSE_EVENT_INVALID: commits: {error}")
            return 2
        refusals += validate_signoff(commits)
    if not is_maintainer((pull_request.get("user") or {}).get("login", "")):
        refusals += validate_open_prs(args.open_prs, _limit(args.open_pr_limit, 2))
        default_branch = (event.get("repository") or {}).get("default_branch") or ""
        refusals += validate_shape(
            pull_request, default_branch, _limit(args.max_changed_lines, 5000), _limit(args.max_changed_files, 300)
        )
    if refusals:
        print("Contribution intake refused:")
        for refusal in refusals:
            print(f"{refusal.code}: {refusal.message}")
        return 1

    print("Contribution metadata accepted. Human review is still required.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
