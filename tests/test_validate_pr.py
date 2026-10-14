#!/usr/bin/env python3

from __future__ import annotations

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))

from validate_pr import validate_body

SCRIPT = ROOT / "scripts" / "validate_pr.py"

VALID_HUMAN = """## Outcome

Users receive a deterministic result.

## Issue or spec

inline

## Acceptance criteria

- AC-1: The validator reports the contribution metadata.

## Evidence

- AC-1 — command/scenario: `python3 tests/test_validate_pr.py`; observed: all assertions pass

## Scope

- In: pull-request contribution metadata
- Out: deployment and branch protection settings

## Human accountability

Accountable contributor: @alice

- [x] I am or represent the accountable human named above.
- [x] I reviewed the complete diff.
- [x] I understand and can explain the change.
- [x] I verified the evidence below.
- [x] I have the right to submit this contribution.
- [x] This is not an unattended agent submission; I remain responsible for the contribution.

## Verification

Ran the validator and inspected the resulting output.

## Risk

- Level: low
- Failure mode: a valid description is rejected
- Rollback/mitigation: revert the validator change
- Reviewer focus: section parsing and trust checks

## AI / automation

- [x] Human-authored
- [ ] AI-assisted
- [ ] Agent-authored
- [ ] Authorized automation

Agent identity: `none`
Provider/model: `none`
Run ID: `none`
Base SHA: `none`
MCP: `none`

Capabilities:

```json
{
  "repository_write": false,
  "command_execution": false,
  "network": "none",
  "secrets": "none",
  "approve": false,
  "merge": false
}
```
"""


def codes(body: str) -> set[str]:
    return {refusal.code for refusal in validate_body(body)}


def valid_agent() -> str:
    return (
        VALID_HUMAN.replace("- [x] Human-authored", "- [ ] Human-authored")
        .replace("- [ ] Agent-authored", "- [x] Agent-authored")
        .replace("Agent identity: `none`", "Agent identity: `poolboy-agent`")
        .replace("Provider/model: `none`", "Provider/model: `anthropic/claude-opus-4-8`")
        .replace("Run ID: `none`", "Run ID: `run-123`")
        .replace("Base SHA: `none`", "Base SHA: `" + "a" * 40 + "`")
    )


def run_cli(event: Any, *arguments: str) -> subprocess.CompletedProcess[str]:
    return run_raw_cli(json.dumps(event), *arguments)


def run_raw_cli(event: str, *arguments: str) -> subprocess.CompletedProcess[str]:
    with tempfile.TemporaryDirectory() as directory:
        event_path = Path(directory) / "event.json"
        event_path.write_text(event, encoding="utf-8")
        return subprocess.run(
            [sys.executable, str(SCRIPT), str(event_path), *arguments],
            check=False,
            capture_output=True,
            text=True,
        )


def owner_event(
    *,
    author_id: int = 1,
    author_type: str = "User",
    owner_type: str = "User",
    owner_id: int = 1,
    sender_id: int = 1,
    sender_type: str = "User",
    event_repository_id: int = 100,
    base_repository_id: int = 100,
    head_repository_id: int = 100,
    author_association: str = "OWNER",
) -> dict[str, Any]:
    return {
        "repository": {"id": event_repository_id},
        "sender": {"id": sender_id, "type": sender_type},
        "pull_request": {
            "author_association": author_association,
            "body": "",
            "user": {"id": author_id, "type": author_type},
            "base": {
                "ref": "main",
                "repo": {
                    "id": base_repository_id,
                    "owner": {"id": owner_id, "type": owner_type},
                },
            },
            "head": {"ref": "feature/change", "repo": {"id": head_repository_id}},
        },
    }


def dependabot_event(**overrides: Any) -> dict[str, Any]:
    event: dict[str, Any] = {
        "action": "synchronize",
        "repository": {"id": 100},
        "sender": {
            "id": 49699333,
            "login": "dependabot[bot]",
            "type": "Bot",
        },
        "pull_request": {
            "title": "chore(deps): bump actions/checkout from 4.1.0 to 4.2.0",
            "user": {
                "id": 49699333,
                "login": "dependabot[bot]",
                "type": "Bot",
            },
            "base": {"ref": "main", "repo": {"id": 100}},
            "head": {
                "ref": "dependabot/github_actions/actions/checkout-4.2.0",
                "repo": {"id": 100},
            },
            "body": "",
        },
    }
    pull_request_overrides = overrides.pop("pull_request", {})
    event["pull_request"].update(pull_request_overrides)
    event.update(overrides)
    return event


class ValidateBodyTests(unittest.TestCase):
    def test_valid_human_submission_passes(self) -> None:
        self.assertEqual(codes(VALID_HUMAN), set())

    def test_valid_agent_submission_passes(self) -> None:
        self.assertEqual(codes(valid_agent()), set())

    def test_unconfirmed_attestation_is_refused(self) -> None:
        body = VALID_HUMAN.replace(
            "- [x] I verified the evidence below.",
            "- [ ] I verified the evidence below.",
        )
        self.assertIn("REFUSE_ACCOUNTABILITY_UNCONFIRMED", codes(body))

    def test_incomplete_ai_provenance_is_refused(self) -> None:
        body = valid_agent().replace(
            "Agent identity: `poolboy-agent`", "Agent identity: `none`"
        ).replace(
            "Provider/model: `anthropic/claude-opus-4-8`", "Provider/model: `none`"
        ).replace("Run ID: `run-123`", "Run ID: `none`").replace(
            "Base SHA: `" + "a" * 40 + "`", "Base SHA: `none`"
        )
        refusal_codes = codes(body)
        self.assertIn("REFUSE_AGENT_IDENTITY_UNKNOWN", refusal_codes)
        self.assertIn("REFUSE_PROVIDER_MODEL_UNKNOWN", refusal_codes)
        self.assertIn("REFUSE_PROVENANCE_INCOMPLETE", refusal_codes)

    def test_invalid_capabilities_are_refused(self) -> None:
        body = valid_agent().replace('"network": "none"', '"network": true')
        self.assertIn("REFUSE_CAPABILITIES_INVALID", codes(body))

    def test_unsafe_or_incomplete_agent_declarations_are_refused(self) -> None:
        for original, replacement, refusal in (
            ('"secrets": "none"', '"secrets": "write"', "REFUSE_SECRET_ACCESS"),
            ('"approve": false', '"approve": true', "REFUSE_AGENT_SELF_APPROVAL"),
            ('"merge": false', '"merge": true', "REFUSE_AGENT_SELF_APPROVAL"),
            ("- [ ] AI-assisted", "- [x] AI-assisted", "REFUSE_AGENT_ORIGIN_AMBIGUOUS"),
            ("- [x] Agent-authored", "- [ ] Agent-authored", "REFUSE_AGENT_ORIGIN_AMBIGUOUS"),
            ("MCP: `none`", "", "REFUSE_MCP_DISCLOSURE_MISSING"),
        ):
            with self.subTest(replacement=replacement, refusal=refusal):
                self.assertIn(refusal, codes(valid_agent().replace(original, replacement)))

    def test_acceptance_criteria_need_unique_ids_and_covered_evidence(self) -> None:
        duplicate = VALID_HUMAN.replace(
            "- AC-1: The validator reports the contribution metadata.",
            "- AC-1: The validator reports the contribution metadata.\n- AC-1: The output is deterministic.",
        )
        self.assertIn("REFUSE_ACCEPTANCE_CRITERIA_INVALID", codes(duplicate))

        missing = VALID_HUMAN.replace(
            "- AC-1 — command/scenario: `python3 tests/test_validate_pr.py`; observed: all assertions pass\n",
            "",
        )
        self.assertIn("REFUSE_EVIDENCE_INCOMPLETE", codes(missing))

        orphan = VALID_HUMAN.replace("- AC-1 —", "- AC-2 —")
        self.assertIn("REFUSE_EVIDENCE_INCOMPLETE", codes(orphan))

        blank_command = VALID_HUMAN.replace(
            "- AC-1 — command/scenario: `python3 tests/test_validate_pr.py`; observed: all assertions pass",
            "- AC-1 — command/scenario: ; observed: all assertions pass",
        )
        self.assertIn("REFUSE_EVIDENCE_INCOMPLETE", codes(blank_command))

    def test_incomplete_rows_are_not_silently_ignored(self) -> None:
        incomplete = VALID_HUMAN.replace(
            "- AC-1: The validator reports the contribution metadata.",
            "- AC-1: The validator reports the contribution metadata.\n- AC-2:",
        )
        self.assertIn("REFUSE_ACCEPTANCE_CRITERIA_INVALID", codes(incomplete))
        self.assertIn("REFUSE_EVIDENCE_INCOMPLETE", codes(incomplete))
        orphan = VALID_HUMAN.replace("## Scope", "- AC-2 —\n\n## Scope")
        self.assertIn("REFUSE_EVIDENCE_INCOMPLETE", codes(orphan))

    def test_placeholders_are_refused(self) -> None:
        body = VALID_HUMAN.replace("Users receive a deterministic result.", "<pending outcome>")
        self.assertIn("REFUSE_PLACEHOLDER_UNRESOLVED", codes(body))
        agent_placeholder = valid_agent().replace(
            "Agent identity: `poolboy-agent`", "Agent identity: `<agent>`"
        )
        self.assertIn("REFUSE_PLACEHOLDER_UNRESOLVED", codes(agent_placeholder))

    def test_html_comments_cannot_satisfy_contract(self) -> None:
        body = VALID_HUMAN.replace(
            "- AC-1: The validator reports the contribution metadata.",
            "<!-- - AC-1: The validator reports the contribution metadata. -->",
        )
        self.assertIn("REFUSE_ACCEPTANCE_CRITERIA_INVALID", codes(body))
        self.assertIn("REFUSE_EVIDENCE_INCOMPLETE", codes(body))


class ValidateCliTests(unittest.TestCase):
    def test_cli_accepts_valid_human_event(self) -> None:
        result = run_cli({"pull_request": {"body": VALID_HUMAN}})
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_supported_modes_and_fail_closed_configuration(self) -> None:
        solo = run_cli(
            owner_event(), "--mode", "solo", "--solo-maintainer-id", "1"
        )
        self.assertEqual(solo.returncode, 0, solo.stdout + solo.stderr)

        invalid_flag_id = run_cli(
            owner_event(), "--mode", "solo", "--solo-maintainer-id", "01"
        )
        self.assertEqual(
            invalid_flag_id.returncode, 1, invalid_flag_id.stdout + invalid_flag_id.stderr
        )
        self.assertIn("REFUSE_HUMAN_ACCOUNTABILITY_MISSING", invalid_flag_id.stdout)
        team = run_cli(
            owner_event(owner_type="Organization", owner_id=200, author_association="MEMBER"),
            "--mode",
            "team",
        )
        self.assertEqual(team.returncode, 0, team.stdout + team.stderr)

        for arguments in (
            ("--mode", "contributor", "--solo-maintainer-id", "1"),
            ("--mode", "solo"),
            ("--mode", "unsupported", "--solo-maintainer-id", "1"),
        ):
            with self.subTest(arguments=arguments):
                refused = run_cli(owner_event(), *arguments)
                self.assertEqual(refused.returncode, 1, refused.stdout + refused.stderr)
                self.assertIn("REFUSE_HUMAN_ACCOUNTABILITY_MISSING", refused.stdout)

    def test_sender_fork_and_bot_do_not_get_solo_exemption(self) -> None:
        cases = (
            owner_event(sender_id=2),
            owner_event(head_repository_id=101),
            owner_event(author_type="Bot"),
            owner_event(sender_type="Bot"),
        )
        for event in cases:
            with self.subTest(event=event):
                result = run_cli(
                    event, "--mode", "solo", "--solo-maintainer-id", "1"
                )
                self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                self.assertIn("REFUSE_HUMAN_ACCOUNTABILITY_MISSING", result.stdout)

    def test_dependabot_authorization_is_narrow(self) -> None:
        accepted = run_cli(dependabot_event())
        self.assertEqual(accepted.returncode, 0, accepted.stdout + accepted.stderr)

        refused_events = (
            dependabot_event(
                sender={"id": 49699333, "login": "alice", "type": "User"}
            ),
            dependabot_event(
                sender={"id": 49699333, "login": "other[bot]", "type": "Bot"}
            ),
            dependabot_event(
                sender={"id": 42, "login": "dependabot[bot]", "type": "Bot"}
            ),
            dependabot_event(
                sender={"id": "49699333", "login": "dependabot[bot]", "type": "Bot"}
            ),
            dependabot_event(
                pull_request={
                    "user": {
                        "id": "49699333",
                        "login": "dependabot[bot]",
                        "type": "Bot",
                    }
                }
            ),
            dependabot_event(
                pull_request={"user": {"login": "dependabot[bot]", "type": "User"}}
            ),
            dependabot_event(
                pull_request={
                    "head": {
                        "ref": "dependabot/github_actions/actions/checkout-4.2.0",
                        "repo": {"id": 101},
                    }
                }
            ),
            dependabot_event(
                pull_request={
                    "head": {"ref": "bot/refresh", "repo": {"id": 100}}
                }
            ),
        )
        for event in refused_events:
            with self.subTest(event=event):
                result = run_cli(event)
                self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                self.assertIn("REFUSE_DEPENDABOT_UNAUTHORIZED", result.stdout)

    def test_cli_malformed_input_and_event_shapes_are_refused(self) -> None:
        malformed_json = run_raw_cli("{")
        self.assertEqual(malformed_json.returncode, 2, malformed_json.stdout + malformed_json.stderr)
        self.assertIn("REFUSE_EVENT_INVALID", malformed_json.stdout)

        for raw_event in ("[]", json.dumps({"pull_request": {"body": []}})):
            with self.subTest(raw_event=raw_event):
                result = run_raw_cli(raw_event)
                self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
                self.assertIn("REFUSE_EVENT_INVALID", result.stdout)


if __name__ == "__main__":
    unittest.main(verbosity=2)
