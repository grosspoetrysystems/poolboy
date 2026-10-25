#!/usr/bin/env python3
"""Keep every open pull request in a visible queue state with an honest clock.

Stateless: each run derives whose turn it is, and since when, from the pull
request timeline. Labels and notices are outputs, never inputs.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from urllib.parse import quote

API = os.environ.get("GITHUB_API_URL", "https://api.github.com")
BOT = "github-actions[bot]"
MAINTAINER = {"OWNER", "MEMBER", "COLLABORATOR"}
ADMISSION_CHECK = "contract"
LABELS = {
    "author": "queue:awaiting-author",
    "reviewer": "queue:awaiting-reviewer",
    "paused": "queue:no-capacity",
}
NOTICE_MARKER = "<!-- helicopter:queue {kind} since={since} -->"
HELICOPTER = "grosspoetrysystems/helicopter"
UPDATE_MARKER = "<!-- helicopter:update to={version} -->"
# Written by the helicopter skill; the queue checks it out alongside scripts/.
VERSION_FILE = Path(__file__).resolve().parent.parent / ".github/helicopter/version.json"


@dataclass(frozen=True)
class Turn:
    whose: str  # "author" or "reviewer"
    since: datetime
    reason: str


def _time(value: str) -> datetime:
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def turn(
    pr: dict[str, Any],
    events: list[dict[str, Any]],
    now: datetime,
    admitted: bool | None = None,
    ignored: frozenset[str] = frozenset(),
) -> Turn | None:
    """Return whose turn it is and since when, or None if the PR is not queued."""
    if pr.get("draft"):
        return None
    author = pr["user"]["login"]
    whose, since, reason = "reviewer", _time(pr["created_at"]), "opened"

    items = []
    for event in events:
        actor = event.get("user") or event.get("actor") or {}
        at = event.get("submitted_at") or event.get("created_at") or (event.get("committer") or {}).get("date")
        if at:
            items.append((_time(at), event, actor))
    items.sort(key=lambda item: item[0])

    for at, event, actor in items:
        kind = event.get("event")
        login = actor.get("login")
        if kind in {"ready_for_review", "reopened"}:
            whose, since, reason = "reviewer", at, kind
        elif kind == "committed":
            if whose == "author":
                whose, since, reason = "reviewer", at, "author pushed"
        elif kind == "review_requested":
            if whose == "author" and login == author:
                whose, since, reason = "reviewer", at, "author re-requested review"
        elif kind not in {"commented", "reviewed"}:
            continue
        elif actor.get("type") == "Bot" or login in ignored:
            continue
        elif login == author:
            if whose == "author":
                whose, since, reason = "reviewer", at, "author replied"
        elif event.get("author_association") in MAINTAINER:
            if event.get("state") == "changes_requested":
                whose, since, reason = "author", at, "maintainer requested changes"
            elif whose == "reviewer" or event.get("state") == "approved":
                whose, since, reason = "reviewer", at, "maintainer responded"

    if admitted is False:
        return Turn("author", since, "admission refused")
    return Turn(whose, since, reason)


def _hours(current: Turn, now: datetime) -> float:
    return (now - current.since).total_seconds() / 3600


def _waited(current: Turn, now: datetime) -> str:
    hours = int(_hours(current, now))
    return f"{hours} hours" if hours < 48 else f"{hours // 24} days"


def plan(current: Turn, now: datetime, nudge_hours: int, pause_hours: int) -> tuple[str, str | None]:
    """Return the queue label and the notice due (nudge, pause, or None)."""
    if current.whose == "author":
        return LABELS["author"], None
    waited = _hours(current, now)
    if waited >= pause_hours:
        return LABELS["paused"], "pause"
    if waited >= nudge_hours:
        return LABELS["reviewer"], "nudge"
    return LABELS["reviewer"], None


def notice(kind: str, current: Turn, now: datetime, author: str, owners: list[str]) -> str:
    waited = _waited(current, now)
    marker = NOTICE_MARKER.format(kind=kind, since=current.since.isoformat())
    if kind == "nudge":
        who = " ".join(f"@{owner}" for owner in owners) or "Maintainers"
        return f"{marker}\n{who}: this pull request has waited {waited} for maintainer review."
    return (
        f"{marker}\n@{author}: no maintainer has had capacity to review this pull request in "
        f"{waited}. It stays open and keeps its place; any maintainer response moves it back "
        "into review. This is not a judgment of the change."
    )


class GitHub:
    def __init__(self, repository: str, token: str, dry_run: bool = False) -> None:
        self.repository, self.token, self.dry_run = repository, token, dry_run

    def request(self, method: str, path: str, body: Any = None, accept: str = "application/vnd.github+json") -> Any:
        url = path if path.startswith("http") else f"{API}/repos/{self.repository}/{path}"
        if self.dry_run and method != "GET":
            print(f"DRY RUN {method} {url} {json.dumps(body) if body else ''}")
            return None
        request = urllib.request.Request(
            url,
            method=method,
            data=json.dumps(body).encode() if body is not None else None,
            headers={
                "Authorization": f"Bearer {self.token}",
                "Accept": accept,
                "X-GitHub-Api-Version": "2022-11-28",
            },
        )
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                raw = response.read()
                link = response.headers.get("Link") or ""
        except urllib.error.HTTPError as error:
            if error.code == 403:
                raise SystemExit(f"GitHub refused {method} {path} (403): check the workflow permissions.")
            raise
        if accept != "application/vnd.github+json":
            return raw.decode()
        data = json.loads(raw) if raw else None
        following = re.search(r'<([^>]+)>;\s*rel="next"', link)
        if isinstance(data, list) and following:
            data += self.request("GET", following.group(1))
        return data


def admitted(github: GitHub, sha: str) -> bool | None:
    runs = github.request("GET", f"commits/{sha}/check-runs?check_name={ADMISSION_CHECK}")["check_runs"]
    finished = [run for run in runs if run["status"] == "completed"]
    if not finished:
        return None
    return finished[0]["conclusion"] == "success"


def reconcile(
    github: GitHub,
    pr: dict[str, Any],
    now: datetime,
    nudge_hours: int,
    pause_hours: int,
    ignored: frozenset[str],
) -> None:
    number = pr["number"]
    events = github.request("GET", f"issues/{number}/timeline?per_page=100")
    current = turn(pr, events, now, admitted(github, pr["head"]["sha"]), ignored)
    have = {label["name"] for label in pr["labels"]} & set(LABELS.values())
    want, due = plan(current, now, nudge_hours, pause_hours) if current else (None, None)

    for label in have - {want}:
        github.request("DELETE", f"issues/{number}/labels/{quote(label, safe='')}")
    if want and want not in have:
        github.request("POST", f"issues/{number}/labels", {"labels": [want]})

    if due:
        marker = NOTICE_MARKER.format(kind=due, since=current.since.isoformat())
        posted = any(
            (event.get("actor") or event.get("user") or {}).get("login") == BOT
            and (event.get("body") or "").startswith(marker)
            for event in events
        )
        if not posted:
            owners = [user["login"] for user in pr.get("requested_reviewers") or []]
            owners += [
                f"{pr['base']['repo']['owner']['login']}/{team['slug']}"
                for team in pr.get("requested_teams") or []
            ]
            owners += sorted(
                {
                    event["user"]["login"]
                    for event in events
                    if event.get("event") == "reviewed"
                    and event.get("author_association") in MAINTAINER
                    and event["user"]["login"] not in ignored | {pr["user"]["login"]}
                }
                - set(owners)
            )
            github.request(
                "POST",
                f"issues/{number}/comments",
                {"body": notice(due, current, now, pr["user"]["login"], owners)},
            )

    state = f"{current.whose} since {current.since:%Y-%m-%d} ({current.reason})" if current else "not queued (draft)"
    print(f"#{number}: {want or 'no label'}; {state}{'; ' + due if due else ''}")


def numbers(version: str) -> tuple[int, ...]:
    return tuple(int(n) for n in re.findall(r"[0-9]+", version))


def update_notice(github: GitHub, installed: str, latest: dict[str, Any]) -> str | None:
    """Open one issue per newer Helicopter release. Closing it skips that release; nothing reopens it."""
    version = latest["version"]
    if numbers(version) <= numbers(installed):
        return None
    title = f"Helicopter {version} is available"
    # ponytail: search lags a minute or so, far inside the hourly sweep; it finds closed issues too.
    query = f'repo:{github.repository} is:issue author:app/github-actions in:title "{title}"'
    found = github.request("GET", f"{API}/search/issues?q={quote(query)}")
    if any(item["title"] == title for item in (found or {}).get("items", [])):
        return None
    steps = latest.get("action_required") or 0
    body = (
        f"{UPDATE_MARKER.format(version=version)}\n"
        f"This repository runs Helicopter {installed}. [{version}](https://github.com/{HELICOPTER}/releases/tag/{version}) is out"
        + (f", with {steps} Action required step{'s' if steps != 1 else ''} in its release notes.\n\n" if steps else ".\n\n")
        + "To update, ask your agent to update Helicopter with the `helicopter` skill "
        f"(`npx skills add {HELICOPTER}`). It opens one pull request for you to review. Close this issue to skip {version}."
    )
    github.request("POST", "issues", {"title": title, "body": body})
    return version


def check_for_update(github: GitHub) -> None:
    """Never fails the queue: an unreachable release or a missing marker just skips the check."""
    if not VERSION_FILE.is_file():
        return
    try:
        installed = json.loads(VERSION_FILE.read_text(encoding="utf-8"))["version"]
        url = f"https://github.com/{HELICOPTER}/releases/latest/download/helicopter.json"
        with urllib.request.urlopen(url, timeout=30) as response:  # noqa: S310 (fixed https URL)
            latest = json.loads(response.read())
    except (OSError, ValueError, KeyError) as error:
        print(f"Update check skipped: {error}")
        return
    opened = update_notice(github, installed, latest)
    print(f"Helicopter {installed}: " + (f"opened an issue for {opened}" if opened else "up to date or already notified"))


def queued(pr: dict[str, Any], team: bool, automation: frozenset[str] = frozenset()) -> bool:
    """Open pull requests from outside contributors are queued; in team mode, everyone's.

    Authorized bots (HELICOPTER_AUTOMATION_LOGINS) count as the maintainer's own delegated work.
    """
    outside = pr["author_association"] not in MAINTAINER and pr["user"]["login"] not in automation
    return pr["state"] == "open" and (team or outside)


def require_labels(github: GitHub, wanted: set[str]) -> None:
    names = {label["name"] for label in github.request("GET", "labels?per_page=100")}
    missing = sorted(wanted - names)
    if missing:
        message = f"Missing labels {', '.join(missing)}: run the documented admin activation steps first."
        if not github.dry_run:
            raise SystemExit(message)
        print(f"DRY RUN warning: {message}")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--pr", type=int, help="reconcile one pull request; default: every open one")
    parser.add_argument("--dry-run", action="store_true", help="print writes instead of making them")
    args = parser.parse_args()

    github = GitHub(os.environ["GITHUB_REPOSITORY"], os.environ["GITHUB_TOKEN"], args.dry_run)
    nudge_hours = int(os.environ.get("HELICOPTER_QUEUE_NUDGE_HOURS") or 168)
    pause_hours = int(os.environ.get("HELICOPTER_QUEUE_PAUSE_HOURS") or 504)
    team = os.environ.get("HELICOPTER_MODE") == "team"
    ignored = frozenset(filter(None, re.split(r"[\s,]+", os.environ.get("HELICOPTER_AUTOMATION_LOGINS", ""))))
    now = datetime.now(timezone.utc)

    # ponytail: one timeline call per open PR; fine to ~1,000 open PRs per sweep
    # (GITHUB_TOKEN allows 1,000 requests/hour). Batch through GraphQL beyond that.
    pulls = [github.request("GET", f"pulls/{args.pr}")] if args.pr else github.request("GET", "pulls?state=open&per_page=100")
    work = []
    for pr in pulls:
        if queued(pr, team, ignored):
            work.append(pr)
        else:
            print(f"#{pr['number']}: skipped ({pr['state'] if pr['state'] != 'open' else 'maintainer or authorized automation'})")
    if work:
        # Only when something needs a label: an idle repository without the labels stays green.
        require_labels(github, set(LABELS.values()))
    for pr in work:
        reconcile(github, pr, now, nudge_hours, pause_hours, ignored)
    if not args.pr:
        check_for_update(github)
    return 0


if __name__ == "__main__":
    sys.exit(main())
