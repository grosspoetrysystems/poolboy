#!/usr/bin/env python3

import io
import sys
import unittest
import urllib.error
from datetime import datetime, timedelta, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))

from pr_queue import GitHub, LABELS as QUEUE_LABELS, notice, plan, queued, turn, update_notice

NOW = datetime(2026, 10, 30, tzinfo=timezone.utc)


def at(day: int) -> str:
    return (NOW - timedelta(days=30 - day)).isoformat().replace("+00:00", "Z")


PR = {"user": {"login": "alice"}, "created_at": at(0), "draft": False}


def comment(day, login, association="NONE", body="", kind="User"):
    return {
        "event": "commented",
        "created_at": at(day),
        "actor": {"login": login, "type": kind},
        "author_association": association,
        "body": body,
    }


def review(day, login, state, association="COLLABORATOR"):
    return {
        "event": "reviewed",
        "submitted_at": at(day),
        "user": {"login": login, "type": "User"},
        "author_association": association,
        "state": state,
    }


def commit(day):
    return {"event": "committed", "committer": {"date": at(day)}}


class TurnTests(unittest.TestCase):
    def assertTurn(self, events, whose, day, **kwargs):
        current = turn(PR, events, NOW, lambda login: login == "max", **kwargs)
        self.assertEqual((current.whose, current.since), (whose, datetime.fromisoformat(at(day).replace("Z", "+00:00"))))

    def test_new_pull_request_waits_on_reviewer_from_opening(self):
        self.assertTurn([], "reviewer", 0)

    def test_changes_requested_hands_turn_to_author_until_push(self):
        self.assertTurn([review(3, "max", "changes_requested")], "author", 3)
        self.assertTurn([review(3, "max", "changes_requested"), commit(5)], "reviewer", 5)

    def test_author_reply_returns_turn_but_author_ping_does_not_reset_clock(self):
        self.assertTurn([review(3, "max", "changes_requested"), comment(6, "alice")], "reviewer", 6)
        self.assertTurn([comment(4, "alice"), commit(9)], "reviewer", 0)

    def test_author_re_request_hands_back_but_maintainer_request_does_not(self):
        def requested(day, login):
            return {"event": "review_requested", "created_at": at(day), "actor": {"login": login, "type": "User"}}

        asked = review(3, "max", "changes_requested")
        self.assertTurn([asked, requested(6, "alice")], "reviewer", 6)
        self.assertTurn([asked, requested(6, "max")], "author", 3)

    def test_maintainer_response_uses_permission_not_association(self):
        self.assertTurn([comment(4, "max", "CONTRIBUTOR")], "reviewer", 4)
        self.assertTurn([review(3, "max", "changes_requested"), comment(4, "max", "CONTRIBUTOR")], "author", 3)
        self.assertTurn([review(3, "max", "changes_requested"), review(4, "max", "approved", "CONTRIBUTOR")], "reviewer", 4)

    def test_review_bot_comments_do_not_move_the_turn(self):
        events = [
            review(3, "max", "changes_requested"),
            comment(4, "github-actions[bot]", "NONE", body="PR Reviewer Guide", kind="Bot"),
        ]
        self.assertTurn(events, "author", 3)

    def test_bots_and_others_do_not_count_as_maintainers(self):
        events = [comment(2, "dependabot[bot]", "COLLABORATOR", kind="Bot"), comment(3, "bob")]
        self.assertTurn(events, "reviewer", 0)

    def test_refused_admission_is_the_authors_turn(self):
        self.assertEqual(turn(PR, [], NOW, lambda login: False, admitted=False).whose, "author")

    def test_drafts_are_not_queued_and_ready_restarts_clock(self):
        self.assertIsNone(turn({**PR, "draft": True}, [], NOW, lambda login: False))
        self.assertTurn([{"event": "ready_for_review", "created_at": at(8)}], "reviewer", 8)


class PlanTests(unittest.TestCase):
    def since(self, hours):
        return turn(PR, [{"event": "ready_for_review", "created_at": (NOW - timedelta(hours=hours)).isoformat()}], NOW, lambda login: False)

    def test_thresholds_in_hours(self):
        self.assertEqual(plan(self.since(3), NOW, 4, 72), (QUEUE_LABELS["reviewer"], None))
        self.assertEqual(plan(self.since(4), NOW, 4, 72), (QUEUE_LABELS["reviewer"], "nudge"))
        self.assertEqual(plan(self.since(72), NOW, 4, 72), (QUEUE_LABELS["paused"], "pause"))

    def test_default_thresholds_keep_seven_and_twenty_one_days(self):
        self.assertEqual(plan(self.since(7 * 24 - 1), NOW, 168, 504), (QUEUE_LABELS["reviewer"], None))
        self.assertEqual(plan(self.since(7 * 24), NOW, 168, 504), (QUEUE_LABELS["reviewer"], "nudge"))
        self.assertEqual(plan(self.since(21 * 24), NOW, 168, 504), (QUEUE_LABELS["paused"], "pause"))

    def test_author_turn_never_nudges_or_pauses(self):
        current = turn(PR, [review(0, "max", "changes_requested")], NOW, lambda login: login == "max")
        self.assertEqual(plan(current, NOW, 4, 72), (QUEUE_LABELS["author"], None))

    def test_notice_reports_hours_then_days(self):
        self.assertIn("waited 5 hours", notice("nudge", self.since(5), NOW, "alice", ["max"]))
        self.assertIn("waited 3 days", notice("nudge", self.since(72), NOW, "alice", ["max"]))


class MaintainerLookupTests(unittest.TestCase):
    def test_private_member_reported_as_contributor_is_a_maintainer(self):
        github = GitHub("o/r", "token")
        calls = []

        def request(method, path):
            calls.append((method, path))
            return {"permission": "admin"}

        github.request = request
        self.assertTrue(github.is_maintainer("thekidnamedkd"))
        self.assertTrue(github.is_maintainer("thekidnamedkd"))
        self.assertEqual(calls, [("GET", "collaborators/thekidnamedkd/permission")])

    def test_only_write_level_permissions_are_maintainers(self):
        for permission in ("admin", "maintain", "write", "push", "triage", "read"):
            with self.subTest(permission=permission):
                github = GitHub("o/r", "token")
                github.request = lambda method, path, permission=permission: {"permission": permission}
                self.assertEqual(github.is_maintainer(permission), permission in {"admin", "maintain", "write"})

    def test_lookup_failure_falls_back_to_outsider(self):
        failures = (
            urllib.error.HTTPError("url", 404, "Not Found", {}, io.BytesIO(b"missing")),
            SystemExit("workflow permission denied"),
            RuntimeError("network down"),
        )
        for failure in failures:
            with self.subTest(type=type(failure).__name__):
                github = GitHub("o/r", "token")

                def request(method, path, failure=failure):
                    raise failure

                github.request = request
                self.assertFalse(github.is_maintainer("someone"))

    def test_lookup_failure_closes_http_error(self):
        github = GitHub("o/r", "token")
        with urllib.error.HTTPError("url", 404, "Not Found", {}, io.BytesIO(b"missing")) as error:
            def request(method, path):
                raise error

            github.request = request
            self.assertFalse(github.is_maintainer("someone"))
            self.assertTrue(error.closed)


class QueueScopeTests(unittest.TestCase):
    def test_team_mode_queues_members(self):
        for association in ("OWNER", "MEMBER", "COLLABORATOR", "CONTRIBUTOR"):
            pr = {**PR, "state": "open", "author_association": association}
            lookup = lambda login: login == "alice"
            self.assertTrue(queued(pr, team=True, is_maintainer=lookup), association)
            self.assertFalse(queued(pr, team=False, is_maintainer=lookup), association)

    def test_outside_contributions_are_queued_in_every_mode(self):
        pr = {**PR, "state": "open", "author_association": "CONTRIBUTOR"}
        self.assertTrue(queued(pr, team=False, is_maintainer=lambda _login: False))
        self.assertTrue(queued(pr, team=True, is_maintainer=lambda _login: False))

    def test_closed_pull_requests_are_never_queued(self):
        self.assertFalse(queued(
            {**PR, "state": "closed", "author_association": "MEMBER"},
            team=True,
            is_maintainer=lambda _login: True,
        ))

    def test_repository_bots_are_queued_like_maintainer_work_but_fork_bots_are_not(self):
        repo = {"repo": {"id": 100}}
        bot = {**PR, "state": "open", "author_association": "NONE", "user": {"login": "renovate[bot]", "type": "Bot"},
               "head": repo, "base": repo}
        lookup = lambda _login: False
        self.assertFalse(queued(bot, team=False, is_maintainer=lookup))
        self.assertTrue(queued(bot, team=True, is_maintainer=lookup))
        self.assertTrue(queued({**bot, "head": {"repo": {"id": 101}}}, team=False, is_maintainer=lookup))
        self.assertTrue(queued({**bot, "head": {"repo": None}}, team=False, is_maintainer=lookup))





class UpdateNoticeTests(unittest.TestCase):
    class Recorder:
        def __init__(self, titles=()):
            self.repository, self.titles, self.posted = "o/r", list(titles), []

        def request(self, method, path, body=None):
            if method == "GET":
                return {"items": [{"title": title} for title in self.titles]}
            self.posted.append(body)

    def test_issues_turned_off_skips_without_failing_the_sweep(self):
        class IssuesOff:
            repository = "o/r"

            def request(self, method, path, body=None):
                raise urllib.error.HTTPError(path, 410, "Gone", {}, None)
        self.assertIsNone(update_notice(IssuesOff(), "v0.2.0", {"version": "v0.3.0"}))

    def test_newer_release_opens_one_issue_naming_its_action_steps(self):
        github = self.Recorder()
        self.assertEqual(update_notice(github, "v0.2.0", {"version": "v0.10.0", "action_required": 2}), "v0.10.0")
        issue, = github.posted
        self.assertEqual(issue["title"], "Helicopter v0.10.0 is available")
        self.assertIn("2 Action required steps", issue["body"])

    def test_current_or_already_notified_does_nothing(self):
        for installed, titles in (("v0.3.0", ()), ("v0.4.0", ()), ("v0.2.0", ("Helicopter v0.3.0 is available",))):
            github = self.Recorder(titles)
            self.assertIsNone(update_notice(github, installed, {"version": "v0.3.0"}))
            self.assertEqual(github.posted, [])


if __name__ == "__main__":
    unittest.main(verbosity=2)
