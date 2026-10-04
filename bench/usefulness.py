#!/usr/bin/env python3
"""Score a usefulness-benchmark run against the frozen question bank.

Graded tiers pass on `correct`. Refusal tiers invert: they pass when the reader
declined, and a confident substantive answer is the failure.
"""

from __future__ import annotations

import argparse
import collections
import json
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
    reader_spread: float
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


def movement(before: dict, after: dict, reader_spread: float) -> dict:
    """Classify the movement between two runs. One verdict, computed once.

    Only the graded axis can say whether the corpus got more useful, so only the
    graded axis decides. The rubric delta is reported beside it, never folded in:
    a `0-4` rubric scaled to `0-1` and a `0-1` graded mean are not calibrated
    against each other, so any arithmetic mixing them — a mean, a minimum, a
    distance, an angle — measures the two graders' relative harshness as much as
    the corpus. Only the signs of the two deltas survive that objection.

    `reader_spread` is required rather than defaulted. It is the mean
    disagreement between readers on one publication, it is remeasured every run,
    and a stale default silently reclassifies every verdict that depends on it.
    """
    dq = after["questions"] - before["questions"]
    dr = after["rubric"] - before["rubric"]
    if dq > reader_spread:
        kind = "improved"
    elif dq < -reader_spread:
        kind = "regressed"
    elif dr > reader_spread:
        kind = "rubric-only: documents say more, readers gained nothing"
    else:
        kind = "flat: inside reader spread, no corpus movement demonstrated"
    return {
        "d_questions": round(dq, 3),
        "d_rubric": round(dr, 3),
        "reader_spread": round(reader_spread, 3),
        "improved": kind == "improved",
        "kind": kind,
    }


def trajectory(runs: list[dict]) -> dict:
    """Coupling between the two axes across the logged run history.

    This is the claim the rubric rests on — that stating something makes it
    usable — accruing as a measurement instead of an assertion.

    Each leg uses the `reader_spread` measured on its own later run. Reader
    disagreement is a property of a grading pass, not a constant, so threading
    one global value would relocate the stale default rather than remove it.

    Only `correlation` is reported as evidence. The slope is kept for inspection
    but is not interpretable as "reader points per rubric point": its magnitude
    scales with the arbitrary `/4` normalisation of the rubric axis, so it is an
    artifact of a unit choice. Correlation is scale-invariant, so "near zero" —
    the condition under which the rubric is retired rather than defended — means
    the same thing however the rubric is normalised.

    Two refusals are deliberate. Below three runs there is no coupling: a line
    through two points is a line by construction. And runs marked
    `comparable: false` are excluded, because a point measured under a different
    protocol — a different reader count, bank, or grader — moves for reasons that
    have nothing to do with the corpus, and regressing across that change
    attributes an instrument difference to the documents.
    """
    usable = [r for r in runs if r.get("comparable", True)]
    out: dict = {
        "runs": len(usable),
        "excluded": len(runs) - len(usable),
        "points": [(r["rubric"], r["questions"]) for r in usable],
        "legs": [movement(a, b, b["reader_spread"]) for a, b in zip(usable, usable[1:])],
        "correlation": None,
        "slope": None,
    }
    if len(usable) < 3:
        out["note"] = "fewer than three comparable runs; coupling is not yet measurable"
        return out
    xs = [r["rubric"] for r in usable]
    ys = [r["questions"] for r in usable]
    if len(set(ys)) < 2:
        out["slope"] = 0.0
        out["note"] = (
            "readers did not move across runs; correlation is undefined and the slope is zero. "
            "This is the rubric-gaming signature, not a missing measurement."
        )
        return out
    if len(set(xs)) < 2:
        out["note"] = "rubric did not vary; coupling undefined"
        return out
    out["correlation"] = round(statistics.correlation(xs, ys), 3)
    out["slope"] = round(statistics.linear_regression(xs, ys).slope, 3)
    out["note"] = (
        "correlation between the axes over comparable runs; near zero retires the rubric. "
        "Slope is not an exchange rate: its magnitude depends on the rubric's normalisation."
    )
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
    spread = 0.048
    base = {"rubric": 0.825, "questions": 0.666}
    gamed = movement(base, {"rubric": 0.950, "questions": 0.666}, spread)
    assert not gamed["improved"] and gamed["d_rubric"] > 0
    assert gamed["kind"].startswith("rubric-only")

    # A graded move smaller than reader disagreement is luck, not improvement.
    assert not movement(base, {"rubric": 0.825, "questions": 0.700}, spread)["improved"]
    assert movement(base, {"rubric": 0.825, "questions": 0.766}, spread)["improved"]
    assert movement(base, {"rubric": 0.900, "questions": 0.600}, spread)["kind"] == "regressed"
    assert movement(base, {"rubric": 0.830, "questions": 0.670}, spread)["kind"].startswith("flat")

    # A run measured under a different protocol is excluded, not averaged in.
    mixed = [
        {"rubric": 0.70, "questions": 0.50, "reader_spread": spread, "comparable": False},
        {"rubric": 0.80, "questions": 0.60, "reader_spread": spread},
        {"rubric": 0.90, "questions": 0.70, "reader_spread": spread},
        {"rubric": 1.00, "questions": 0.80, "reader_spread": spread},
    ]
    assert trajectory(mixed) == trajectory(mixed[1:]) | {"excluded": 1}

    # Flat readers across runs is the gaming case the tool exists to catch, so it
    # must report, not raise. `statistics.correlation` dies on a constant series.
    flat = trajectory([{"rubric": r, "questions": 0.666, "reader_spread": spread} for r in (0.70, 0.80, 0.90)])
    assert flat["correlation"] is None and flat["slope"] == 0.0
    assert "gaming signature" in flat["note"]
    assert all(leg["kind"].startswith("rubric-only") for leg in flat["legs"])

    # Coupling is refused until three runs exist; two points are a line by
    # construction and would manufacture the correlation they claim to measure.
    two = trajectory([{"rubric": 0.77, "questions": 0.56, "reader_spread": spread},
                      {"rubric": 0.83, "questions": 0.67, "reader_spread": spread}])
    assert two["correlation"] is None and len(two["legs"]) == 1

    # Correlation is the reported evidence; it is invariant to how the rubric is
    # normalised, which is exactly what the retire-the-rubric rule needs.
    rising = [{"rubric": r, "questions": q, "reader_spread": spread}
              for r, q in ((0.70, 0.50), (0.80, 0.60), (0.90, 0.70))]
    rescaled = [{**r, "rubric": r["rubric"] * 2} for r in rising]
    assert trajectory(rising)["correlation"] == trajectory(rescaled)["correlation"] == 1.0
    assert trajectory(rising)["slope"] != trajectory(rescaled)["slope"]

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
