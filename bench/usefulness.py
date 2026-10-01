#!/usr/bin/env python3
"""Score a usefulness-benchmark run against the frozen question bank.

Graded tiers pass on `correct`. Refusal tiers invert: they pass when the reader
declined, and a confident substantive answer is the failure.
"""

from __future__ import annotations

import argparse
import json
import math
import statistics
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


def combine(rubric_mean: float, graded_mean: float, rubric_scale: float = 4.0) -> dict:
    """Combine the two axes without letting either hide the other.

    The headline is the **minimum**, not the mean. A mean lets a cheap axis carry
    an expensive one: three iterations of document-structure work raised the
    rubric while readers gained nothing, and an average would have reported that
    as progress. Taking the minimum means a gain counts only when the weaker axis
    moves, which is the only kind of gain observed to be real.

    The **span** between the axes is reported alongside, because a widening span
    is the signature of optimising the cheaper measurement. A headline without
    its span is not interpretable.

    `limiter` names the axis currently holding the score down, which is where the
    next repair belongs.
    """
    r = rubric_mean / rubric_scale
    q = graded_mean
    return {
        "rubric": round(r, 3),
        "questions": round(q, 3),
        "score": round(min(r, q), 3),
        "span": round(abs(r - q), 3),
        "limiter": "questions" if q <= r else "rubric",
    }


def direction(before: tuple[float, float], after: tuple[float, float]) -> dict:
    """The movement between two runs as a vector, not two unrelated deltas.

    Each run is a point `(rubric, questions)`. What a run is worth is where it
    travelled, and the bearing says which kind of work it was: `90` is pure
    reader gain, `0` is pure rubric gain with readers unmoved, and `45` is
    balanced. A bearing near `0` is the gaming signature.
    """
    dr, dq = after[0] - before[0], after[1] - before[1]
    return {
        "d_rubric": round(dr, 3),
        "d_questions": round(dq, 3),
        "distance": round(math.hypot(dr, dq), 3),
        "bearing": round(math.degrees(math.atan2(dq, dr)), 1),
    }


def trajectory(runs: list[dict]) -> dict:
    """Correlation between the two axes across the logged run history.

    This is the claim the rubric rests on — that stating something makes it
    usable — expressed as a number that accrues instead of an assertion. It is
    deliberately refused below three runs: a slope through two points is a line
    by construction and carries no evidence.
    """
    pts = [(r["rubric"], r["questions"]) for r in runs]
    out = {"runs": len(pts), "points": pts}
    out["legs"] = [direction(a, b) for a, b in zip(pts, pts[1:])]
    if len(pts) < 3:
        out["slope"] = None
        out["note"] = "fewer than three runs; rubric-to-reader coupling is not yet measurable"
        return out
    xs, ys = [p[0] for p in pts], [p[1] for p in pts]
    if len(set(xs)) < 2:
        out["slope"] = None
        out["note"] = "rubric did not vary; coupling undefined"
        return out
    out["slope"] = round(statistics.linear_regression(xs, ys).slope, 3)
    out["correlation"] = round(statistics.correlation(xs, ys), 3)
    out["note"] = "reader points gained per rubric point, measured over logged runs"
    return out


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

    # The headline cannot be raised by the cheap axis alone. This is the whole
    # point: structural work that readers do not benefit from must not score.
    before = combine(3.30, 0.666)
    structural_only = combine(3.80, 0.666)
    assert structural_only["score"] == before["score"], "one axis must not move the headline"
    assert structural_only["span"] > before["span"], "a one-sided gain must widen the span"

    # A gain on the limiting axis does move it, and narrows the span.
    real = combine(3.30, 0.766)
    assert real["score"] > before["score"]
    assert real["span"] < before["span"]

    # The limiter names where the next repair belongs.
    assert combine(3.30, 0.666)["limiter"] == "questions"
    assert combine(2.00, 0.900)["limiter"] == "rubric"

    # Movement is a bearing, and the gaming signature has a distinct one.
    assert direction((0.77, 0.56), (0.83, 0.67))["bearing"] > 45, "balanced gain leans to readers"
    assert direction((0.77, 0.56), (0.95, 0.56))["bearing"] == 0, "rubric-only gain bears 0"

    # Coupling is refused until three runs exist; two points are a line by
    # construction and would manufacture the correlation they claim to measure.
    two = trajectory([{"rubric": 0.77, "questions": 0.56}, {"rubric": 0.83, "questions": 0.67}])
    assert two["slope"] is None and len(two["legs"]) == 1
    three = trajectory(
        [
            {"rubric": 0.70, "questions": 0.50},
            {"rubric": 0.80, "questions": 0.60},
            {"rubric": 0.90, "questions": 0.70},
        ]
    )
    assert three["slope"] == 1.0, "one reader point per rubric point"
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
