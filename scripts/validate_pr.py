#!/usr/bin/env python3
"""Validate Poolboy's pull request metadata contract."""

from __future__ import annotations

import argparse
import json
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any

REQUIRED_SECTIONS = ("Change", "Verification", "Risk")
PLACEHOLDERS = (
    "<problem, smallest solution, and user-visible effect>",
    "Linear: <Linear work item URL or `inline`>",
    "Commands and results:",
    "<commands and concise result; project CI verifies independently>",
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


def _metadata_exempt(event: dict[str, Any], mode: str, solo_maintainer_id: int) -> bool:
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
        return pull_request.get("author_association") in {"OWNER", "MEMBER", "COLLABORATOR"}

    if mode != "solo" or author_id != solo_maintainer_id:
        return False

    return owner.get("type") == "Organization" or _repo_id(owner.get("id")) == solo_maintainer_id


def _authorized_automation(event: dict[str, Any], logins: frozenset[str]) -> bool:
    """Admit a maintainer-listed bot on a branch of this repository."""
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
        and sender.get("type") == "Bot"
        and author.get("login") in logins
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
        "--automation-logins",
        default="",
        help="maintainer-authorized bot logins (HELICOPTER_AUTOMATION_LOGINS), comma or space separated",
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

    mode = args.mode if args.mode in {"solo", "contributor", "team"} else "contributor"
    solo_maintainer_id = int(args.solo_maintainer_id) if re.fullmatch(r"[1-9][0-9]*", args.solo_maintainer_id) else 0
    automation_logins = frozenset(filter(None, re.split(r"[\s,]+", args.automation_logins)))
    pull_request = _mapping(event.get("pull_request"))
    author = _mapping(pull_request.get("user"))

    if author.get("type") == "Bot" and author.get("login") in automation_logins:
        if not _authorized_automation(event, automation_logins):
            print(
                "REFUSE_AUTOMATION_UNAUTHORIZED: authorized automation must be the listed Bot author, "
                "the event sender, and a same-repository branch."
            )
            return 1
        if author.get("login") == DEPENDABOT_LOGIN:
            refusals = _dependabot_refusals(event)
            if refusals:
                print("Dependabot contribution intake refused:")
                for refusal in refusals:
                    print(f"{refusal.code}: {refusal.message}")
                return 1
        print("Authorized automation exemption accepted. Human review is still required.")
        return 0

    if _metadata_exempt(event, mode, solo_maintainer_id):
        print(f"{mode.capitalize()}-mode metadata exemption accepted for the trusted maintainer.")
        return 0

    if author.get("type") == "Bot":
        print(
            "REFUSE_BOT_UNAUTHORIZED: bots need maintainer authorization before opening pull requests. "
            "Their operator requests it with the Automation authorization issue form."
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
    if pull_request.get("author_association") not in {"OWNER", "MEMBER", "COLLABORATOR"}:
        limit = int(args.open_pr_limit) if re.fullmatch(r"[0-9]+", args.open_pr_limit) else 2
        refusals += validate_open_prs(args.open_prs, limit)
    if refusals:
        print("Contribution intake refused:")
        for refusal in refusals:
            print(f"{refusal.code}: {refusal.message}")
        return 1

    print("Contribution metadata accepted. Human review is still required.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
