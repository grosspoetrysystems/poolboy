#!/usr/bin/env python3
"""Validate the metadata contract in a GitHub pull request event.

Adapted from Helicopter `scripts/validate_pr.py` at revision
57db0a75bd2521d9f0a728932f0ed05a31cc3ec9. Poolboy extends its generic trust,
mode, and exact capability-JSON checks with outcome/spec, AC/evidence,
scope/risk, provenance, attestation, placeholder, and Dependabot rules.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any

# Canonical Helicopter section headings plus the Poolboy additions.
REQUIRED_SECTIONS = (
    "## Outcome",
    "## Issue or spec",
    "## Acceptance criteria",
    "## Evidence",
    "## Scope",
    "## Human accountability",
    "## Verification",
    "## Risk",
    "## AI / automation",
)
# Sections whose prose must be filled in (no template placeholders, not empty).
CONTENT_SECTIONS = (
    "Outcome",
    "Issue or spec",
    "Acceptance criteria",
    "Evidence",
    "Scope",
    "Verification",
    "Risk",
)
ORIGINS = (
    "Human-authored",
    "AI-assisted",
    "Agent-authored",
    "Authorized automation",
)
AGENT_ORIGINS = {"Agent-authored", "Authorized automation"}
# Canonical Helicopter attestations (first four) plus the Poolboy additions:
# accountable-human representation and no-unattended responsibility.
ATTESTATIONS = (
    "I am or represent the accountable human named above.",
    "I reviewed the complete diff.",
    "I understand and can explain the change.",
    "I verified the evidence below.",
    "I have the right to submit this contribution.",
    "This is not an unattended agent submission; I remain responsible for the contribution.",
)
# Exact Helicopter capability JSON schema. MCP is disclosed separately and is
# intentionally NOT part of this block.
CAPABILITY_KEYS = {
    "repository_write": bool,
    "command_execution": bool,
    "network": str,
    "secrets": str,
    "approve": bool,
    "merge": bool,
}
USERNAME = r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?"
DEPENDABOT_LOGIN = "dependabot[bot]"
DEPENDABOT_BRANCH = re.compile(r"dependabot/(?:github_actions|go_modules|npm_and_yarn)/\S+")
DEPENDABOT_TITLE = re.compile(r"(?:Bump|chore\(deps(?:-dev)?\): bump)\s+\S", re.IGNORECASE)


@dataclass(frozen=True)
class Refusal:
    code: str
    message: str


def _mapping(value: Any) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def _strip_comments(body: str) -> str:
    return re.sub(r"<!--.*?-->", "", body, flags=re.DOTALL)


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


def _repo_id(value: Any) -> int | None:
    return value if type(value) is int and value > 0 else None


def _metadata_exempt(event: dict[str, Any], mode: str, solo_maintainer_id: int) -> bool:
    pull_request = _mapping(event.get("pull_request"))
    author = _mapping(pull_request.get("user"))
    base_repository = _mapping(_mapping(pull_request.get("base")).get("repo"))
    head_repository = _mapping(_mapping(pull_request.get("head")).get("repo"))
    repository = _mapping(event.get("repository"))
    owner = _mapping(base_repository.get("owner"))
    sender = _mapping(event.get("sender"))
    repository_id = _repo_id(repository.get("id"))

    trusted_same_repository_author = (
        repository_id is not None
        and owner.get("type") in {"User", "Organization"}
        and author.get("type") == "User"
        and sender.get("type") == "User"
        and type(author.get("id")) is int
        and author.get("id") > 0
        and sender.get("id") == author.get("id")
        and _repo_id(base_repository.get("id")) == repository_id
        and _repo_id(head_repository.get("id")) == repository_id
    )
    if not trusted_same_repository_author:
        return False

    if mode == "team":
        return pull_request.get("author_association") in {"OWNER", "MEMBER", "COLLABORATOR"}

    if mode != "solo" or author.get("id") != solo_maintainer_id:
        return False

    return owner.get("type") == "Organization" or owner.get("id") == solo_maintainer_id


def _dependabot_refusals(event: dict[str, Any]) -> list[Refusal]:
    """Narrow authorization for Dependabot pull requests.

    Strengthens the original login/full-name heuristic by pinning the base and
    head repositories to the immutable repository ID of the receiving
    repository. The exemption is deliberately narrow: a real same-repository
    Dependabot event targeting main on a configured ecosystem branch.
    """
    pull_request = _mapping(event.get("pull_request"))
    repository = _mapping(event.get("repository"))
    repository_id = _repo_id(repository.get("id"))
    base_repository = _mapping(_mapping(pull_request.get("base")).get("repo"))
    head_repository = _mapping(_mapping(pull_request.get("head")).get("repo"))
    author = _mapping(pull_request.get("user"))
    sender = _mapping(event.get("sender"))
    author_id = _repo_id(author.get("id"))
    sender_id = _repo_id(sender.get("id"))
    head = _mapping(pull_request.get("head"))
    head_ref = head.get("ref") if isinstance(head.get("ref"), str) else ""
    base_ref = pull_request.get("base", {}).get("ref") if isinstance(pull_request.get("base"), dict) else None
    title = pull_request.get("title") if isinstance(pull_request.get("title"), str) else ""

    refusals: list[Refusal] = []
    if (
        author.get("type") != "Bot"
        or author.get("login") != DEPENDABOT_LOGIN
        or author_id is None
        or sender.get("type") != "Bot"
        or sender.get("login") != DEPENDABOT_LOGIN
        or sender_id is None
        or sender_id != author_id
    ):
        refusals.append(
            Refusal(
                "REFUSE_DEPENDABOT_UNAUTHORIZED",
                "Dependabot PR event must be authored and sent by the same dependabot[bot] GitHub App (Bot) account with a positive immutable user ID.",
            )
        )
    same_repository = (
        repository_id is not None
        and _repo_id(base_repository.get("id")) == repository_id
        and _repo_id(head_repository.get("id")) == repository_id
    )
    if not same_repository:
        refusals.append(
            Refusal(
                "REFUSE_DEPENDABOT_UNAUTHORIZED",
                "Dependabot PR must come from a branch in this repository (immutable repository IDs must match).",
            )
        )
    if base_ref != "main":
        refusals.append(
            Refusal("REFUSE_DEPENDABOT_UNAUTHORIZED", "Dependabot PR must target main.")
        )
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


def _section(body: str, heading: str) -> str:
    match = re.search(
        rf"^## {re.escape(heading)}\s*$(?P<value>.*?)(?=^## |\Z)",
        body,
        re.MULTILINE | re.DOTALL,
    )
    if not match:
        return ""
    text = re.sub(r"<!--.*?-->", "", match.group("value"), flags=re.DOTALL)
    return text.strip()


def _has_placeholder(text: str) -> bool:
    return re.search(r"<[^>]+>|\b(?:TBD|TODO)\b", text, re.IGNORECASE) is not None


def _checked(body: str, label: str) -> bool:
    return re.search(rf"^- \[[xX]\] {re.escape(label)}[ \t]*$", body, re.MULTILINE) is not None


def _backtick_field(body: str, label: str) -> str | None:
    match = re.search(
        rf"^{re.escape(label)}:[ \t]*`([^`\r\n]+)`[ \t]*$",
        body,
        re.IGNORECASE | re.MULTILINE,
    )
    return match.group(1).strip() if match else None


def _capabilities(body: str) -> tuple[dict[str, Any] | None, Refusal | None]:
    match = re.search(
        r"^Capabilities:\s*\n+```json\s*\n(?P<value>\{.*?\})\s*\n```",
        body,
        re.IGNORECASE | re.MULTILINE | re.DOTALL,
    )
    if not match:
        return None, Refusal(
            "REFUSE_CAPABILITIES_INVALID",
            "Agent-authored work requires the JSON capability block from the pull request template.",
        )

    try:
        value = json.loads(match.group("value"))
    except json.JSONDecodeError as error:
        return None, Refusal(
            "REFUSE_CAPABILITIES_INVALID",
            f"Capability JSON is invalid: {error.msg}.",
        )

    if not isinstance(value, dict):
        return None, Refusal("REFUSE_CAPABILITIES_INVALID", "Capabilities must be a JSON object.")

    invalid = [key for key, kind in CAPABILITY_KEYS.items() if type(value.get(key)) is not kind]
    unknown = sorted(set(value) - set(CAPABILITY_KEYS))
    if invalid or unknown:
        details = []
        if invalid:
            details.append(f"missing or wrong type: {', '.join(invalid)}")
        if unknown:
            details.append(f"unknown: {', '.join(unknown)}")
        return None, Refusal(
            "REFUSE_CAPABILITIES_INVALID",
            "Use exactly the capability fields from the template (" + "; ".join(details) + ").",
        )

    if value["network"] not in {"none", "restricted", "unrestricted"}:
        return None, Refusal(
            "REFUSE_CAPABILITIES_INVALID",
            "Capability `network` must be `none`, `restricted`, or `unrestricted`.",
        )

    return value, None


def _validate_acceptance_and_evidence(body: str) -> list[Refusal]:
    refusals: list[Refusal] = []
    acceptance_text = _section(body, "Acceptance criteria")
    evidence_text = _section(body, "Evidence")

    acceptance_rows = re.findall(
        r"^- (AC-\d+):[ \t]*(.*)$", acceptance_text, re.MULTILINE | re.IGNORECASE
    )
    acceptance = {
        identifier.upper(): text for identifier, text in acceptance_rows
    }
    if not acceptance_rows or any(not text.strip() for _, text in acceptance_rows):
        refusals.append(
            Refusal(
                "REFUSE_ACCEPTANCE_CRITERIA_INVALID",
                "Acceptance criteria need at least one `- AC-N:` row, with text for every criterion.",
            )
        )
    if len(acceptance) != len(acceptance_rows):
        refusals.append(
            Refusal("REFUSE_ACCEPTANCE_CRITERIA_INVALID", "Acceptance criterion IDs must be unique.")
        )

    evidence_rows = re.findall(
        r"^- (AC-\d+)[ \t]+[\u2014-][ \t]*(.*)$",
        evidence_text,
        re.MULTILINE | re.IGNORECASE,
    )
    evidence = {
        identifier.upper(): text for identifier, text in evidence_rows
    }
    if len(evidence) != len(evidence_rows):
        refusals.append(Refusal("REFUSE_EVIDENCE_INCOMPLETE", "Evidence IDs must be unique."))

    for identifier in acceptance:
        value = evidence.get(identifier, "")
        has_command = re.search(r"command/scenario:[ \t]*[^;\s]", value, re.IGNORECASE)
        has_observed = re.search(r"observed:[ \t]*[^;\s]", value, re.IGNORECASE)
        if not has_command or not has_observed:
            refusals.append(
                Refusal(
                    "REFUSE_EVIDENCE_INCOMPLETE",
                    f"{identifier} needs evidence with a command/scenario and observed result.",
                )
            )
    for identifier in evidence:
        if identifier not in acceptance:
            refusals.append(
                Refusal(
                    "REFUSE_EVIDENCE_INCOMPLETE",
                    f"{identifier} has evidence but no matching acceptance criterion.",
                )
            )
    return refusals


def validate_body(body: str) -> list[Refusal]:
    if not isinstance(body, str):
        return [Refusal("REFUSE_EVENT_INVALID", "pull request body must be text.")]
    body = _strip_comments(body)
    refusals: list[Refusal] = []
    missing_sections = [section for section in REQUIRED_SECTIONS if section not in body]
    if missing_sections:
        refusals.append(
            Refusal(
                "REFUSE_TEMPLATE_INCOMPLETE",
                "Missing required sections: " + ", ".join(missing_sections) + ".",
            )
        )

    empty_sections = [heading for heading in CONTENT_SECTIONS if not _section(body, heading)]
    if empty_sections:
        refusals.append(
            Refusal(
                "REFUSE_TEMPLATE_INCOMPLETE",
                "Fill in required sections: " + ", ".join(empty_sections) + ".",
            )
        )

    placeholder_sections = [
        heading
        for heading in (section.removeprefix("## ") for section in REQUIRED_SECTIONS)
        if _has_placeholder(_section(body, heading))
    ]
    if placeholder_sections:
        refusals.append(
            Refusal(
                "REFUSE_PLACEHOLDER_UNRESOLVED",
                "Resolve template placeholders in: " + ", ".join(placeholder_sections) + ".",
            )
        )

    issue_spec = _section(body, "Issue or spec")
    if issue_spec and re.search(r"#\d+|https://\S+|\binline\b", issue_spec, re.IGNORECASE) is None:
        refusals.append(
            Refusal(
                "REFUSE_ISSUE_SPEC_MISSING",
                "Issue or spec must reference an issue (`#123`), an HTTPS URL, or `inline`.",
            )
        )

    refusals.extend(_validate_acceptance_and_evidence(body))

    scope = _section(body, "Scope")
    if re.search(r"^- In:[ \t]*\S", scope, re.MULTILINE) is None or re.search(
        r"^- Out:[ \t]*\S", scope, re.MULTILINE
    ) is None:
        refusals.append(
            Refusal("REFUSE_SCOPE_INCOMPLETE", "Scope must define both `In` and `Out`.")
        )

    risk = _section(body, "Risk")
    if re.search(
        r"^- Level:[ \t]*(?:low|medium|high)[ \t]*$",
        risk,
        re.IGNORECASE | re.MULTILINE,
    ) is None:
        refusals.append(
            Refusal("REFUSE_RISK_INCOMPLETE", "Risk `Level` must be `low`, `medium`, or `high`.")
        )
    for field in ("Failure mode", "Rollback/mitigation", "Reviewer focus"):
        if re.search(
            rf"^- {re.escape(field)}:[ \t]*\S",
            risk,
            re.IGNORECASE | re.MULTILINE,
        ) is None:
            refusals.append(
                Refusal("REFUSE_RISK_INCOMPLETE", f"Risk section must define `{field}`.")
            )
    human_section = _section(body, "Human accountability")
    human = re.search(
        rf"^Accountable contributor:[ \t]*@(?P<user>{USERNAME})[ \t]*$",
        human_section,
        re.IGNORECASE | re.MULTILINE,
    )
    if not human or human.group("user").upper() in {"YOUR_GITHUB_USERNAME", "USERNAME"}:
        refusals.append(
            Refusal(
                "REFUSE_HUMAN_ACCOUNTABILITY_MISSING",
                "Declare `Accountable contributor: @github-user` with a real username.",
            )
        )

    unchecked = [label for label in ATTESTATIONS if not _checked(human_section, label)]
    if unchecked:
        refusals.append(
            Refusal(
                "REFUSE_ACCOUNTABILITY_UNCONFIRMED",
                "Confirm every human-accountability checkbox.",
            )
        )

    ai_section = _section(body, "AI / automation")
    selected_origins = [origin for origin in ORIGINS if _checked(ai_section, origin)]

    if len(selected_origins) != 1:
        refusals.append(
            Refusal(
                "REFUSE_AGENT_ORIGIN_AMBIGUOUS",
                "Select exactly one AI / automation origin.",
            )
        )
        return refusals

    origin = selected_origins[0]
    if origin == "Human-authored":
        return refusals

    identity = _backtick_field(ai_section, "Agent identity")
    if not identity or identity.lower() == "none":
        refusals.append(
            Refusal(
                "REFUSE_AGENT_IDENTITY_UNKNOWN",
                "AI-assisted or agent-authored work requires a non-empty agent or bot identity.",
            )
        )

    provider = _backtick_field(ai_section, "Provider/model")
    if not provider or provider.lower() == "none":
        refusals.append(
            Refusal(
                "REFUSE_PROVIDER_MODEL_UNKNOWN",
                "AI-assisted or agent-authored work requires the provider and exact model.",
            )
        )

    if origin not in AGENT_ORIGINS:
        return refusals

    run_id = _backtick_field(ai_section, "Run ID")
    base_sha = _backtick_field(ai_section, "Base SHA")
    if not run_id or run_id.lower() == "none":
        refusals.append(Refusal("REFUSE_PROVENANCE_INCOMPLETE", "Provide a non-empty immutable Run ID."))
    if not base_sha or re.fullmatch(r"[0-9a-fA-F]{40}", base_sha) is None:
        refusals.append(
            Refusal("REFUSE_PROVENANCE_INCOMPLETE", "Provide the full 40-character base commit SHA.")
        )

    mcp = _backtick_field(ai_section, "MCP")
    if not mcp:
        refusals.append(
            Refusal(
                "REFUSE_MCP_DISCLOSURE_MISSING",
                "Disclose MCP servers separately with `MCP: `none`` or the servers used.",
            )
        )

    capabilities, capability_refusal = _capabilities(ai_section)
    if capability_refusal:
        refusals.append(capability_refusal)
    elif capabilities:
        if capabilities["secrets"].lower() != "none":
            refusals.append(
                Refusal(
                    "REFUSE_SECRET_ACCESS",
                    "Untrusted contribution runs may not use repository or deployment secrets.",
                )
            )
        if capabilities["approve"] or capabilities["merge"]:
            refusals.append(
                Refusal(
                    "REFUSE_AGENT_SELF_APPROVAL",
                    "An authoring agent may not approve or merge its own contribution.",
                )
            )

    return refusals


def main() -> int:
    parser = argparse.ArgumentParser(description="Validate a GitHub pull request event contract.")
    parser.add_argument("event", type=Path, help="GitHub pull request event JSON")
    parser.add_argument(
        "--mode",
        default="contributor",
        help="repository mode: solo, contributor, or team",
    )
    parser.add_argument(
        "--solo-maintainer-id",
        default="",
        help="immutable numeric GitHub user ID",
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

    pull_request = _mapping(event.get("pull_request"))
    author = _mapping(pull_request.get("user"))
    if author.get("login") == DEPENDABOT_LOGIN:
        refusals = _dependabot_refusals(event)
        if refusals:
            print("Dependabot contribution intake refused:")
            for refusal in refusals:
                print(f"{refusal.code}: {refusal.message}")
            return 1
        print("Authorized Dependabot update accepted.")
        return 0

    mode = args.mode
    if mode not in {"solo", "contributor", "team"}:
        mode = "contributor"
    solo_maintainer_id = int(args.solo_maintainer_id) if re.fullmatch(
        r"[1-9][0-9]*", args.solo_maintainer_id
    ) else 0

    if _metadata_exempt(event, mode, solo_maintainer_id):
        print(f"{mode.capitalize()}-mode metadata exemption accepted for the trusted maintainer.")
        return 0

    body = pull_request.get("body") or ""
    refusals = validate_body(body)
    if refusals:
        print("Contribution intake refused:")
        for refusal in refusals:
            print(f"{refusal.code}: {refusal.message}")
        print("\nUpdate the pull request description from the repository template.")
        return 1

    print("Contribution metadata accepted. Human review is still required.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
