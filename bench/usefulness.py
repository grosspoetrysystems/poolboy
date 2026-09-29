#!/usr/bin/env python3
"""Score a usefulness-benchmark run against the frozen question bank.

Graded tiers pass on `correct`. Refusal tiers invert: they pass when the reader
declined, and a confident substantive answer is the failure.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
QUESTIONS = ROOT / "questions.json"

GRADED_PASS = {"correct"}
REFUSAL_PASS = {"unsupported", "false-premise", "out-of-scope", "refused"}


def score(bank: dict, grades: dict[str, str]) -> dict:
    graded = set(bank["graded_tiers"])
    missing = [q["id"] for q in bank["questions"] if q["id"] not in grades]
    unknown = sorted(set(grades) - {q["id"] for q in bank["questions"]})

    tiers: dict[str, dict[str, int]] = {}
    failures: list[dict[str, str]] = []
    for question in bank["questions"]:
        grade = grades.get(question["id"])
        if grade is None:
            continue
        tier = question["tier"]
        passed = grade in (GRADED_PASS if tier in graded else REFUSAL_PASS)
        bucket = tiers.setdefault(tier, {"pass": 0, "total": 0})
        bucket["total"] += 1
        bucket["pass"] += int(passed)
        if not passed:
            failures.append({"id": question["id"], "tier": tier, "grade": grade})

    return {
        "tiers": tiers,
        "failures": failures,
        "missing": missing,
        "unknown": unknown,
        # An ungraded question is not a pass. A gate that ignores gaps is not a gate.
        "gate": not failures and not missing and not unknown,
    }


def render(bank: dict, result: dict) -> str:
    names = bank["tiers"]
    lines = ["| Tier | Questions | Pass | Rate |", "| --- | --- | --- | --- |"]
    for tier in bank["graded_tiers"] + bank["refusal_tiers"]:
        bucket = result["tiers"].get(tier)
        if not bucket:
            continue
        rate = 100 * bucket["pass"] // bucket["total"]
        lines.append(
            f"| `{tier}` {names[tier]} | {bucket['total']} | "
            f"{bucket['pass']}/{bucket['total']} | {rate}% |"
        )
    for label, key in (("Ungraded", "missing"), ("Unknown ids", "unknown")):
        if result[key]:
            lines.append(f"\n{label}: {', '.join(result[key])}")
    if result["failures"]:
        lines.append("\nFailures:")
        lines += [f"- `{f['id']}` ({f['tier']}): {f['grade']}" for f in result["failures"]]
    lines.append(f"\n**Usefulness gate: {'pass' if result['gate'] else 'fail'}.**")
    return "\n".join(lines)


def demo() -> None:
    bank = json.loads(QUESTIONS.read_text(encoding="utf-8"))
    ids = [q["id"] for q in bank["questions"]]

    perfect = {
        q["id"]: ("correct" if q["tier"] in bank["graded_tiers"] else "unsupported")
        for q in bank["questions"]
    }
    assert score(bank, perfect)["gate"], "a fully correct run must pass the gate"

    partial = dict(perfect, H3="partial")
    result = score(bank, partial)
    assert not result["gate"], "one partial must fail the gate"
    assert result["tiers"]["H"]["pass"] == result["tiers"]["H"]["total"] - 1

    # A refusal tier inverts: answering confidently is the failure.
    eager = dict(perfect, F1="correct")
    assert not score(bank, eager)["gate"], "answering a false premise must fail"

    assert score(bank, {k: v for k, v in perfect.items() if k != "X1"})["missing"] == ["X1"]
    assert score(bank, dict(perfect, Z9="correct"))["unknown"] == ["Z9"]
    assert len(ids) == len(set(ids)), "question ids must be unique"
    print("usefulness self-test passed")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("grades", type=Path, nargs="?", help="JSON object of question id to grade")
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()

    if args.self_test:
        demo()
        return 0
    if args.grades is None:
        parser.error("pass a grades file or --self-test")

    bank = json.loads(QUESTIONS.read_text(encoding="utf-8"))
    grades = json.loads(args.grades.read_text(encoding="utf-8"))
    result = score(bank, grades)
    print(render(bank, result))
    return 0 if result["gate"] else 1


if __name__ == "__main__":
    sys.exit(main())
