#!/usr/bin/env python3
"""Score a usefulness-benchmark run against the frozen question bank.

Graded tiers pass on `correct`. Refusal tiers invert: they pass when the reader
declined, and a confident substantive answer is the failure.
"""

from __future__ import annotations

import argparse
import collections
import json
import math
import statistics
import sys
from pathlib import Path
from typing import TypedDict

ROOT = Path(__file__).resolve().parent
QUESTIONS = ROOT / "questions.json"


# The run log's shape, declared once. These are the schema of record for an
# archive that outlives any single run, and they are what `features()` and
# `record()` construct. Rows are generated, never parsed from untrusted input,
# so construction is the gate and there is no runtime validator to drift from it.


class Corpus(TypedDict):
    documents: int
    bytes: int
    bytes_mean: float
    bytes_stdev: float
    links: int
    links_per_document: float
    unreferenced: int
    types: dict[str, int]
    sources_cited: int


class Run(TypedDict):
    run: str
    commit: str
    poolboy: str
    publication_sha256: str
    rubric: float
    questions: float
    refusal: float
    readers: int
    comparable: bool
    note: str
    corpus: Corpus


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


def improved(before: dict, after: dict, reader_spread: float) -> dict:
    """Did the corpus get more useful between two runs?

    Only the graded axis can answer that, so only the graded axis gates. The
    rubric is reported beside it, never folded into it: a 0-4 grader scaled to
    0-1 and a 0-1 continuous score are not calibrated against each other, so any
    arithmetic mixing them measures the two graders' relative harshness as much
    as the corpus.

    `reader_spread` is the mean disagreement between readers on the same
    publication. A graded move smaller than that is reader luck, not corpus
    improvement, and the verdict says so.
    """
    dq = after["questions"] - before["questions"]
    dr = after["rubric"] - before["rubric"]
    return {
        "d_questions": round(dq, 3),
        "d_rubric": round(dr, 3),
        "reader_spread": round(reader_spread, 3),
        "improved": dq > reader_spread,
        "verdict": (
            "improved" if dq > reader_spread
            else "regressed" if dq < -reader_spread
            else "inside reader spread; no corpus movement demonstrated"
        ),
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
    """Coupling between the two axes across the logged run history.

    This is the claim the rubric rests on — that stating something makes it
    usable — expressed as a number that accrues instead of an assertion.

    Two refusals are deliberate. Below three runs there is no slope: a line
    through two points is a line by construction. And runs marked
    `comparable: false` are excluded, because a point measured under a different
    protocol — a different reader count, bank, or grader — moves for reasons that
    have nothing to do with the corpus, and regressing across that change
    attributes an instrument difference to the documents.
    """
    usable = [r for r in runs if r.get("comparable", True)]
    pts = [(r["rubric"], r["questions"]) for r in usable]
    out: dict = {"runs": len(pts), "excluded": len(runs) - len(pts), "points": pts}
    out["legs"] = [direction(a, b) for a, b in zip(pts, pts[1:])]
    if len(pts) < 3:
        out["slope"] = None
        out["note"] = "fewer than three comparable runs; coupling is not yet measurable"
        return out
    xs, ys = [p[0] for p in pts], [p[1] for p in pts]
    if len(set(ys)) < 2:
        out["slope"] = 0.0
        out["correlation"] = None
        out["note"] = (
            "readers did not move across runs; slope is zero and correlation undefined. "
            "This is the rubric-gaming signature, not a missing measurement."
        )
        return out
    if len(set(xs)) < 2:
        out["slope"] = None
        out["correlation"] = None
        out["note"] = "rubric did not vary; coupling undefined"
        return out
    out["slope"] = round(statistics.linear_regression(xs, ys).slope, 3)
    out["correlation"] = round(statistics.correlation(xs, ys), 3)
    out["note"] = "reader points gained per rubric point, measured over comparable runs"
    return out


def features(graph: dict) -> Corpus:
    """Describe the corpus a run scored, derived from its published manifest.

    Every field is mechanical. Nothing here is typed in by hand, because a
    hand-maintained feature drifts from the artifact it claims to describe and
    then quietly corrupts the comparison it exists to support.

    These are recorded because they cannot be recovered later: the publication a
    run scored is rebuilt and gone. They are not yet evidence of anything.
    """
    files = graph["files"]
    sizes = [f["bytes"] for f in files.values()]
    out_links = [len(f.get("links", [])) for f in files.values()]
    linked_to = {t for f in files.values() for t in f.get("links", [])}
    return Corpus(
        documents=len(files),
        bytes=sum(sizes),
        bytes_mean=round(statistics.mean(sizes), 1),
        bytes_stdev=round(statistics.stdev(sizes), 1) if len(sizes) > 1 else 0.0,
        links=sum(out_links),
        links_per_document=round(statistics.mean(out_links), 2),
        unreferenced=sum(1 for k in files if k not in linked_to and k != graph.get("root")),
        types=dict(sorted(collections.Counter(f.get("type", "") for f in files.values()).items())),
        sources_cited=sum(len(f.get("sources", [])) for f in files.values()),
    )


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

    # Only the graded axis gates, and only beyond reader disagreement. A rubric
    # gain with readers unmoved is the rubric-gaming case and must not pass.
    base = {"rubric": 0.825, "questions": 0.666}
    spread = 0.048
    gamed = improved(base, {"rubric": 0.950, "questions": 0.666}, spread)
    assert not gamed["improved"] and gamed["d_rubric"] > 0
    assert "inside reader spread" in gamed["verdict"]

    # A graded move smaller than reader disagreement is luck, not improvement.
    assert not improved(base, {"rubric": 0.825, "questions": 0.700}, spread)["improved"]
    assert improved(base, {"rubric": 0.825, "questions": 0.766}, spread)["improved"]
    assert improved(base, {"rubric": 0.900, "questions": 0.600}, spread)["verdict"] == "regressed"

    # Movement is a bearing, and the gaming signature has a distinct one.
    assert direction((0.77, 0.56), (0.83, 0.67))["bearing"] > 45, "balanced gain leans to readers"
    assert direction((0.77, 0.56), (0.95, 0.56))["bearing"] == 0, "rubric-only gain bears 0"

    # A run measured under a different protocol is excluded, not averaged in.
    mixed = [
        {"rubric": 0.70, "questions": 0.50, "comparable": False},
        {"rubric": 0.80, "questions": 0.60},
        {"rubric": 0.90, "questions": 0.70},
        {"rubric": 1.00, "questions": 0.80},
    ]
    assert trajectory(mixed) == trajectory(mixed[1:]) | {"excluded": 1}

    # Flat readers across runs is the gaming case the tool exists to catch, so it
    # must report, not raise. `statistics.correlation` dies on a constant series.
    flat = trajectory([{"rubric": r, "questions": 0.666} for r in (0.70, 0.80, 0.90)])
    assert flat["slope"] == 0.0 and flat["correlation"] is None
    assert "gaming signature" in flat["note"]

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

    # Corpus features are derived, never asserted, so a malformed manifest is a
    # loud failure rather than a quietly wrong row in the run log.
    graph = {
        "root": "/index.md",
        "files": {
            "/index.md": {"bytes": 100, "links": ["/a.md"], "type": "concept", "sources": [{}]},
            "/a.md": {"bytes": 300, "links": [], "type": "reference", "sources": [{}, {}]},
            "/orphan.md": {"bytes": 200, "links": [], "type": "concept", "sources": []},
        },
    }
    f = features(graph)
    assert f == {
        "documents": 3,
        "bytes": 600,
        "bytes_mean": 200.0,
        "bytes_stdev": 100.0,
        "links": 1,
        "links_per_document": 0.33,
        "unreferenced": 1,
        "types": {"concept": 2, "reference": 1},
        "sources_cited": 3,
    }, f
    # The root is not counted as unreferenced: nothing is expected to link to it.
    assert features({"root": "/index.md", "files": {"/index.md": {"bytes": 1}}})["unreferenced"] == 0

    print("usefulness self-test passed")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("grades", type=Path, nargs="?", help="JSON object of question id to grade")
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument(
        "--trajectory",
        type=Path,
        nargs="?",
        const=ROOT / "usefulness/trajectory.json",
        help="derive legs and coupling from a run log instead of scoring grades",
    )
    args = parser.parse_args()

    if args.self_test:
        demo()
        return 0
    if args.trajectory is not None:
        log = json.loads(args.trajectory.read_text(encoding="utf-8"))
        print(json.dumps(trajectory(log["runs"]), indent=2))
        return 0
    if args.grades is None:
        parser.error("pass a grades file, --trajectory, or --self-test")

    bank = json.loads(QUESTIONS.read_text(encoding="utf-8"))
    grades = json.loads(args.grades.read_text(encoding="utf-8"))
    result = score(bank, grades)
    print(render(bank, result))
    return 0 if result["gate"] else 1


if __name__ == "__main__":
    sys.exit(main())
