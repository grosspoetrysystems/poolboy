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


# Partial credit when a grade arrives as a probability distribution rather than a
# single label. A grade is only ever a pass on its argmax; these weights feed the
# continuous score, which tracks movement a four-bucket label cannot resolve.
WEIGHTS = {"correct": 1.0, "partial": 0.5, "incorrect": 0.0, "unsupported": 0.0}


def resolve(grade: str | dict[str, float], tier: str, graded: set[str]) -> tuple[str, float]:
    """Return the argmax label and a continuous 0..1 score for one grade.

    A grade is either a label or a probability map. The partial-credit weights
    apply only to the graded tiers; on a refusal tier the single thing being
    measured is whether the reader declined, so a pass is worth 1 and an answer 0.
    """
    if isinstance(grade, str):
        passed = grade in (GRADED_PASS if tier in graded else REFUSAL_PASS)
        if tier not in graded:
            return grade, float(passed)
        return grade, WEIGHTS.get(grade, float(passed))

    label = max(grade, key=grade.get)
    if tier in graded:
        return label, sum(p * WEIGHTS.get(k, 0.0) for k, p in grade.items())
    return label, sum(p for k, p in grade.items() if k in REFUSAL_PASS)


def score(bank: dict, grades: dict[str, str | dict[str, float]]) -> dict:
    graded = set(bank["graded_tiers"])
    missing = [q["id"] for q in bank["questions"] if q["id"] not in grades]
    unknown = sorted(set(grades) - {q["id"] for q in bank["questions"]})

    tiers: dict[str, dict[str, float]] = {}
    failures: list[dict[str, str]] = []
    for question in bank["questions"]:
        grade = grades.get(question["id"])
        if grade is None:
            continue
        tier = question["tier"]
        label, continuous = resolve(grade, tier, graded)
        passed = label in (GRADED_PASS if tier in graded else REFUSAL_PASS)
        bucket = tiers.setdefault(tier, {"pass": 0, "total": 0, "score": 0.0})
        bucket["total"] += 1
        bucket["pass"] += int(passed)
        bucket["score"] += continuous
        if not passed:
            failures.append({"id": question["id"], "tier": tier, "grade": label})

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
    lines = ["| Tier | Questions | Pass | Rate | Score |", "| --- | --- | --- | --- | --- |"]
    for tier in bank["graded_tiers"] + bank["refusal_tiers"]:
        bucket = result["tiers"].get(tier)
        if not bucket:
            continue
        rate = 100 * bucket["pass"] // bucket["total"]
        lines.append(
            f"| `{tier}` {names[tier]} | {bucket['total']} | "
            f"{bucket['pass']}/{bucket['total']} | {rate}% | "
            f"{bucket['score'] / bucket['total']:.2f} |"
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

    # Probability grades: the argmax decides pass/fail, the mass decides the score.
    probs = dict(perfect, H4={"partial": 0.6, "correct": 0.3, "incorrect": 0.1})
    result = score(bank, probs)
    assert not result["gate"], "an argmax of partial must still fail the gate"
    h4 = [q["id"] for q in bank["questions"] if q["tier"] == "H"]
    assert result["tiers"]["H"]["score"] == len(h4) - 1 + 0.6, "0.6*0.5 + 0.3*1.0 = 0.6"

    # The same argmax can carry very different scores; that is the point.
    weak = score(bank, dict(perfect, H4={"partial": 0.9, "incorrect": 0.1}))
    strong = score(bank, dict(perfect, H4={"partial": 0.5, "correct": 0.5}))
    assert weak["tiers"]["H"]["score"] < strong["tiers"]["H"]["score"]

    # A refusal tier scores on refusal mass, not on the graded weights.
    hedged = score(bank, dict(perfect, F1={"unsupported": 0.4, "correct": 0.6}))
    assert hedged["tiers"]["F"]["score"] == len(
        [q for q in bank["questions"] if q["tier"] == "F"]
    ) - 1 + 0.4
    assert not hedged["gate"], "an argmax of correct on a false premise must fail"
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
