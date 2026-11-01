#!/usr/bin/env python3

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))

from validate_pr import _metadata_exempt, validate_body, validate_signoff

SCRIPT = ROOT / "scripts" / "validate_pr.py"


VALID = """## Change

Implement the requested behavior with the smallest reviewable change.

Linear: inline

## Verification

Ran the validator test module and inspected its output.

## Risk

- [x] GitHub Actions, build, release, policy, or agent instructions

Details and mitigations:

The trusted workflow still checks out only the protected base.
"""


def codes(body: str) -> set[str]:
    return {refusal.code for refusal in validate_body(body)}


def run_cli(event: Any, *arguments: str) -> subprocess.CompletedProcess[str]:
    return run_raw_cli(json.dumps(event), *arguments)


def run_raw_cli(event: str, *arguments: str) -> subprocess.CompletedProcess[str]:
    with tempfile.TemporaryDirectory() as directory:
        event_path = Path(directory) / "event.json"
        event_path.write_text(event, encoding="utf-8")
        env = os.environ.copy()
        env.pop("GITHUB_TOKEN", None)
        env.pop("GITHUB_REPOSITORY", None)
        return subprocess.run(
            [sys.executable, str(SCRIPT), str(event_path), *arguments],
            check=False,
            capture_output=True,
            text=True,
            env=env,
        )


def owner_event(
    *,
    author_id: int = 1,
    author_type: str = "User",
    author_login: str = "alice",
    owner_type: str = "User",
    owner_id: int = 1,
    sender_id: int = 1,
    sender_type: str = "User",
    event_repository_id: int = 100,
    base_repository_id: int = 100,
    head_repository_id: int = 100,
    author_association: str = "OWNER",
    body: str = "",
) -> dict[str, Any]:
    return {
        "repository": {"id": event_repository_id},
        "sender": {"id": sender_id, "type": sender_type},
        "pull_request": {
            "author_association": author_association,
            "body": body,
            "user": {"id": author_id, "login": author_login, "type": author_type},
            "base": {
                "ref": "main",
                "repo": {
                    "id": base_repository_id,
                    "owner": {"id": owner_id, "type": owner_type},
                },
            },
            "head": {"ref": "feature/change", "repo": {"id": head_repository_id}}
        },
    }


def dependabot_event(**overrides: Any) -> dict[str, Any]:
    event: dict[str, Any] = {
        "action": "synchronize",
        "repository": {"id": 100},
        "sender": {"id": 49699333, "login": "dependabot[bot]", "type": "Bot"},
        "pull_request": {
            "title": "chore(deps): bump actions/checkout from 4.1.0 to 4.2.0",
            "user": {"id": 49699333, "login": "dependabot[bot]", "type": "Bot"},
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
    def test_minimal_submission_passes(self) -> None:
        self.assertEqual(codes(VALID), set())

    def test_linear_reference_cannot_replace_change_description(self) -> None:
        body = (
            "## Change\nLinear: inline\n"
            "## Verification\nLocal CLI smoke.\n"
            "## Risk\n- [x] None of the above\n"
        )
        self.assertEqual(codes(body), {"REFUSE_CHANGE_EMPTY"})

    def test_real_template_requires_completion(self) -> None:
        body = (ROOT / ".github" / "pull_request_template.md").read_text(encoding="utf-8")
        self.assertEqual(
            codes(body),
            {"REFUSE_CHANGE_EMPTY", "REFUSE_VERIFICATION_EMPTY", "REFUSE_RISK_UNCHECKED"},
        )
        for section, content in (
            ("Change", "Reject a Linear reference without a change description.\nLinear: inline"),
            ("Verification", "Ran the validator CLI; a reference-only Change was refused."),
        ):
            body = re.sub(
                rf"(?ms)^## {section}[ \t]*\n.*?(?=^## |\Z)",
                lambda match: f"## {section}\n\n{content}\n\n",
                body,
            )
        body = body.replace("- [ ]", "- [x]", 1)
        self.assertEqual(codes(body), set())

    def test_each_missing_section_is_named(self) -> None:
        for section in ("Change", "Verification", "Risk"):
            with self.subTest(section=section):
                body = VALID.replace(f"## {section}", "## Other")
                refusals = validate_body(body)
                self.assertIn("REFUSE_TEMPLATE_INCOMPLETE", {r.code for r in refusals})
                self.assertIn(f"## {section}", " ".join(r.message for r in refusals))

    def test_empty_change_and_verification_are_refused(self) -> None:
        body = "## Change\n\n## Verification\n\n## Risk\n- [x] None of the above\n"
        self.assertEqual(
            codes(body),
            {"REFUSE_CHANGE_EMPTY", "REFUSE_VERIFICATION_EMPTY"},
        )

    def test_risk_requires_a_checked_area(self) -> None:
        self.assertIn("REFUSE_RISK_UNCHECKED", codes(VALID.replace("- [x]", "- [ ]")))


    def test_comments_cannot_satisfy_required_sections(self) -> None:
        body = (
            "## Change\n<!-- hidden -->\n"
            "## Verification\nLocal CLI smoke.\n"
            "## Risk\n- [x] None of the above\n"
        )
        self.assertIn("REFUSE_CHANGE_EMPTY", codes(body))

    def test_comments_cannot_hide_required_headings(self) -> None:
        for closing in ("-->", ""):
            with self.subTest(closing=closing):
                body = (
                    "## Change\n<!--\n"
                    "## Verification\nHidden steps.\n"
                    "## Risk\n- [x] None of the above\n" + closing
                )
                self.assertEqual(
                    codes(body),
                    {"REFUSE_CHANGE_EMPTY", "REFUSE_TEMPLATE_INCOMPLETE"},
                )


class ValidateCliTests(unittest.TestCase):
    def test_cli_accepts_valid_event(self) -> None:
        result = run_cli({"pull_request": {"body": VALID}})
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("Human review is still required", result.stdout)

    def test_contributor_valid_body_is_accepted_without_metadata_exemption(self) -> None:
        result = run_cli(owner_event(body=VALID), "--mode", "contributor")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("Contribution metadata accepted", result.stdout)


    def test_solo_mode_requires_configured_maintainer(self) -> None:
        owner = owner_event()
        enabled = ("--mode", "solo", "--solo-maintainer-id", "1")
        cases = {
            "personal owner": (owner, 0),
            "organization maintainer": (
                owner_event(owner_id=200, owner_type="Organization"),
                0,
            ),
            "wrong account": (owner_event(author_id=2), 1),
            "wrong repository": (owner_event(event_repository_id=101), 1),
            "different sender": (owner_event(sender_id=2), 1),
            "boolean author ID": (owner_event(author_id=True), 1),
            "boolean sender ID": (owner_event(sender_id=True), 1),
            "bot sender": (owner_event(sender_type="Bot"), 1),
            "fork": (owner_event(head_repository_id=101), 1),
        }
        for name, (event, expected_code) in cases.items():
            with self.subTest(name=name):
                result = run_cli(event, *enabled)
                self.assertEqual(expected_code, result.returncode, result.stdout + result.stderr)

        for invalid_id in ("", "0", "-1", "+1", "01", " 1 ", "owner"):
            with self.subTest(invalid_id=invalid_id):
                rejected = run_cli(owner, "--mode", "solo", "--solo-maintainer-id", invalid_id)
                self.assertEqual(rejected.returncode, 1, rejected.stdout + rejected.stderr)

    def test_contributor_mode_exempts_nobody(self) -> None:
        for mode in ("contributor", "invalid"):
            with self.subTest(mode=mode):
                result = run_cli(owner_event(), "--mode", mode, "--solo-maintainer-id", "1")
                self.assertEqual(result.returncode, 1, result.stdout + result.stderr)

    def test_team_mode_uses_permission_lookup_not_author_association(self) -> None:
        lookup = lambda login: login == "alice"
        for association in ("OWNER", "MEMBER", "COLLABORATOR", "CONTRIBUTOR"):
            with self.subTest(association=association):
                event = owner_event(
                    author_association=association,
                    owner_id=200,
                    owner_type="Organization",
                )
                self.assertTrue(_metadata_exempt(event, "team", 0, lookup))

        outsider = owner_event(
            author_association="CONTRIBUTOR",
            owner_id=200,
            owner_type="Organization",
            author_login="outsider",
        )
        self.assertFalse(_metadata_exempt(outsider, "team", 0, lookup))

        refused = (
            owner_event(author_association="MEMBER", head_repository_id=101),
            owner_event(author_association="MEMBER", sender_id=2),
            owner_event(author_association="MEMBER", author_type="Bot"),
        )
        for event in refused:
            self.assertFalse(_metadata_exempt(event, "team", 0, lookup))

    def test_malformed_input_and_event_shapes_are_refused(self) -> None:
        malformed_json = run_raw_cli("{")
        self.assertEqual(malformed_json.returncode, 2, malformed_json.stdout + malformed_json.stderr)
        self.assertIn("REFUSE_EVENT_INVALID", malformed_json.stdout)

        for raw_event in ("[]", json.dumps({"pull_request": {"body": []}})):
            with self.subTest(raw_event=raw_event):
                result = run_raw_cli(raw_event)
                self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
                self.assertIn("REFUSE_EVENT_INVALID", result.stdout)


class RepositoryBotTests(unittest.TestCase):
    def bot_event(self, login: str = "dependabot[bot]", head_repository_id: int = 100) -> dict[str, Any]:
        event = owner_event(author_type="Bot", sender_type="Bot", head_repository_id=head_repository_id)
        event["pull_request"]["user"]["login"] = login
        event["sender"]["login"] = login
        event["pull_request"]["head"]["repo"]["id"] = head_repository_id
        return event

    def test_any_bot_on_a_repository_branch_is_admitted_without_a_list(self) -> None:
        events = (dependabot_event(), self.bot_event(login="renovate[bot]"))
        for event in events:
            result = run_cli(event)
            self.assertEqual(result.returncode, 0, result.stdout)
            self.assertIn("authorized by its write access", result.stdout)

    def test_bot_on_a_fork_or_acting_for_another_sender_is_refused(self) -> None:
        fork = self.bot_event(head_repository_id=101)
        relayed = self.bot_event()
        relayed["sender"]["login"] = "someone-else[bot]"
        for event in (fork, relayed):
            event["pull_request"]["body"] = VALID
            result = run_cli(event)
            self.assertEqual(result.returncode, 1, result.stdout)
            self.assertIn("REFUSE_BOT_UNAUTHORIZED", result.stdout)

    def test_repository_bot_requires_positive_ids_and_same_event_repositories(self) -> None:
        cases = []
        invalid_author = self.bot_event()
        invalid_author["pull_request"]["user"]["id"] = 0
        cases.append(invalid_author)
        invalid_sender = self.bot_event()
        invalid_sender["sender"]["id"] = True
        cases.append(invalid_sender)
        mismatched_ids = self.bot_event()
        mismatched_ids["sender"]["id"] = 7
        cases.append(mismatched_ids)
        mismatched_event_repository = self.bot_event()
        mismatched_event_repository["repository"]["id"] = 101
        cases.append(mismatched_event_repository)
        mismatched_base_repository = self.bot_event()
        mismatched_base_repository["pull_request"]["base"]["repo"]["id"] = 101
        cases.append(mismatched_base_repository)
        for event in cases:
            with self.subTest(event=event):
                event["pull_request"]["body"] = VALID
                result = run_cli(event)
                self.assertEqual(result.returncode, 1, result.stdout)
                self.assertIn("REFUSE_BOT_UNAUTHORIZED", result.stdout)

    def test_dependabot_target_branch_and_title_are_preserved(self) -> None:
        for event in (
            dependabot_event(pull_request={"base": {"ref": "develop", "repo": {"id": 100}}}),
            dependabot_event(pull_request={"head": {"ref": "bot/refresh", "repo": {"id": 100}}}),
            dependabot_event(pull_request={"title": "refresh dependencies"}),
        ):
            result = run_cli(event)
            self.assertEqual(result.returncode, 1, result.stdout)
            self.assertIn("REFUSE_DEPENDABOT_UNAUTHORIZED", result.stdout)

    def test_a_person_named_like_a_bot_is_not_exempt(self) -> None:
        event = owner_event(author_association="NONE")
        event["pull_request"]["user"]["login"] = event["sender"]["login"] = "dependabot[bot]"
        self.assertEqual(run_cli(event).returncode, 1)

def commit(sha: str, email: str, message: str, parents: int = 1) -> dict[str, object]:
    return {"sha": sha, "parents": [{}] * parents, "commit": {"author": {"email": email}, "message": message}}


class SignoffTests(unittest.TestCase):
    def test_each_commit_needs_its_authors_signoff(self) -> None:
        signed = commit("aaaaaaa1", "Ann@Example.com", "fix\n\nSigned-off-by: Ann <ann@example.com>\n")
        other = commit("bbbbbbb2", "bob@example.com", "fix\n\nSigned-off-by: Ann <ann@example.com>")
        prose = commit("ccccccc3", "cat@example.com", "says Signed-off-by: Cat <cat@example.com> inline")
        merge = commit("ddddddd4", "web@github.com", "Merge branch main", parents=2)
        self.assertEqual(validate_signoff([signed, merge]), [])
        refusal, = validate_signoff([[signed, other], [prose]])  # --slurp pages
        self.assertEqual(refusal.code, "REFUSE_SIGNOFF_MISSING")
        self.assertIn("bbbbbbb, ccccccc", refusal.message)
        self.assertNotIn("aaaaaaa", refusal.message)

    def test_cli_checks_signoff_only_when_commits_are_given(self) -> None:
        event = owner_event(author_association="NONE", author_id=2, sender_id=2)
        event["pull_request"]["body"] = VALID
        self.assertEqual(run_cli(event).returncode, 0)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "commits.json"
            path.write_text(json.dumps([[commit("eeeeeee5", "x@example.com", "unsigned")]]), encoding="utf-8")
            result = run_cli(event, "--commits", str(path))
        self.assertEqual(result.returncode, 1)
        self.assertIn("REFUSE_SIGNOFF_MISSING", result.stdout)


class OpenPullRequestLimitTests(unittest.TestCase):
    def run_with(self, association: str, open_prs: int, limit: str = "2") -> subprocess.CompletedProcess[str]:
        event = owner_event(author_association=association, author_id=2, sender_id=2)
        event["pull_request"]["body"] = VALID
        return run_cli(event, "--open-prs", str(open_prs), "--open-pr-limit", limit)

    def test_outside_contributors_are_held_to_the_limit(self) -> None:
        self.assertEqual(self.run_with("NONE", 2).returncode, 0)
        result = self.run_with("FIRST_TIME_CONTRIBUTOR", 3)
        self.assertEqual(result.returncode, 1)
        self.assertIn("REFUSE_OPEN_PR_LIMIT", result.stdout)

    def test_collaborator_association_does_not_bypass_without_permission_lookup(self) -> None:
        result = self.run_with("COLLABORATOR", 9)
        self.assertEqual(result.returncode, 1)
        self.assertIn("REFUSE_OPEN_PR_LIMIT", result.stdout)
        self.assertEqual(self.run_with("NONE", 9, limit="0").returncode, 0)


class ShapeTests(unittest.TestCase):
    def run_with(self, association: str = "NONE", *arguments: str, **pull_request: object) -> subprocess.CompletedProcess[str]:
        event = owner_event(author_association=association, author_id=2, sender_id=2)
        event["repository"]["default_branch"] = "main"
        event["pull_request"]["base"]["ref"] = pull_request.pop("base_ref", "main")
        event["pull_request"].update({"body": VALID, **pull_request})
        return run_cli(event, *arguments)

    def test_drafts_skip_admission(self) -> None:
        event = owner_event(author_association="NONE", author_id=2, sender_id=2)
        event["pull_request"]["draft"] = True
        self.assertEqual(run_cli(event).returncode, 0)

    def test_outside_contributions_target_the_default_branch(self) -> None:
        self.assertEqual(self.run_with().returncode, 0)
        result = self.run_with(base_ref="release/1.x")
        self.assertEqual(result.returncode, 1)
        self.assertIn("REFUSE_BASE_BRANCH", result.stdout)

    def test_size_limits_count_lines_and_files_and_zero_turns_them_off(self) -> None:
        self.assertEqual(self.run_with(additions=4000, deletions=1000, changed_files=300).returncode, 0)
        for size in ({"additions": 4000, "deletions": 1001}, {"changed_files": 301}):
            result = self.run_with(**size)
            self.assertEqual(result.returncode, 1, size)
            self.assertIn("REFUSE_TOO_LARGE", result.stdout)
        self.assertEqual(self.run_with("NONE", "--max-changed-lines", "0", additions=9999).returncode, 0)
        result = self.run_with("NONE", additions=9999)
        self.assertEqual(result.returncode, 1)
        self.assertIn("REFUSE_TOO_LARGE", result.stdout)
if __name__ == "__main__":
    unittest.main(verbosity=2)
