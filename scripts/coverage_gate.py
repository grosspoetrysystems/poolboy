#!/usr/bin/env python3
"""Fail unless a Go coverprofile meets an exact integer 90% floor.

Reads a profile written by `go test -coverpkg=./... -coverprofile=...` and
compares aggregate statement coverage with an integer cross-multiply
(`covered * 100 >= total * 90`) so a sub-floor ratio never rounds up into a
pass. Repeated blocks from multiple test binaries are merged by location;
inconsistent statement counts are malformed.
"""

from __future__ import annotations

import argparse
import re
import sys

# A profile block is `name:sLine.sCol,eLine.eCol numStmt count`.
LINE_FIELDS = 3
COVERAGE_SPAN = re.compile(r"^\d+\.\d+,\d+\.\d+$")
COVERAGE_MODES = {"set", "count", "atomic"}


def aggregate(profile_text: str) -> tuple[int, int]:
    """Return (covered_statements, total_statements) from a coverprofile."""
    lines = [line.strip() for line in profile_text.splitlines() if line.strip()]
    header = lines[0].split() if lines else []
    if (
        len(header) != 2
        or header[0] != "mode:"
        or header[1] not in COVERAGE_MODES
    ):
        raise ValueError("coverprofile missing `mode:` header")

    blocks: dict[str, tuple[int, bool]] = {}
    for line in lines[1:]:
        try:
            block, counts = line.rsplit(":", 1)
            fields = counts.split()
            if len(fields) != LINE_FIELDS:
                raise ValueError
            span, num_stmt, count = fields
            num = int(num_stmt)
            hit = int(count)
        except ValueError as exc:
            raise ValueError(f"malformed coverage line: {line!r}") from exc
        if not block or not COVERAGE_SPAN.fullmatch(span) or num < 0 or hit < 0:
            raise ValueError(f"malformed coverage line: {line!r}")

        location = f"{block}:{span}"
        previous = blocks.get(location)
        if previous is not None:
            if previous[0] != num:
                raise ValueError(f"inconsistent statement count for {location!r}")
            blocks[location] = (num, previous[1] or hit > 0)
        else:
            blocks[location] = (num, hit > 0)

    total = sum(num for num, _ in blocks.values())
    covered = sum(num for num, hit in blocks.values() if hit)
    if total == 0:
        raise ValueError("coverprofile reports zero statements")
    return covered, total


def passes(covered: int, total: int) -> bool:
    """Exact integer compare; never rounds a sub-floor ratio up to a pass."""
    return covered * 100 >= total * 90


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("profile", help="path to a Go coverprofile")
    args = parser.parse_args()

    try:
        with open(args.profile, encoding="utf-8") as fh:
            covered, total = aggregate(fh.read())
    except OSError as exc:
        print(f"coverage gate: cannot read profile: {exc}", file=sys.stderr)
        return 1
    except ValueError as exc:
        print(f"coverage gate: {exc}", file=sys.stderr)
        return 1

    ratio = covered * 100 / total
    if passes(covered, total):
        print(f"coverage gate: {covered}/{total} statements ({ratio:.2f}%) >= 90%")
        return 0
    print(
        f"coverage gate: {covered}/{total} statements ({ratio:.2f}%) < 90%",
        file=sys.stderr,
    )
    return 1


if __name__ == "__main__":
    sys.exit(main())
