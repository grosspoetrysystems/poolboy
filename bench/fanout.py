#!/usr/bin/env python3
"""Run Poolboy against pinned, unmodified third-party documentation corpora.

The cohort manifest lives beside this file and is versioned. The clones and the
result directories are bulk scratch and live in a workspace outside the
repository; `--clone` recreates that workspace from the manifest.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import shutil
import subprocess
import sys
import time
import uuid
from pathlib import Path

ROOT = Path(__file__).resolve().parent
MANIFEST = ROOT / "cohort.json"
COMMANDS = ("check", "status", "scan", "build")


def run(command: list[str], cwd: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(command, cwd=cwd, text=True, capture_output=True, check=False)


def tree_stats(root: Path) -> dict[str, int | str]:
    count = 0
    size = 0
    digest = hashlib.sha256()
    if root.exists():
        for path in sorted(p for p in root.rglob("*") if p.is_file()):
            rel = path.relative_to(root).as_posix().encode()
            data = path.read_bytes()
            count += 1
            size += len(data)
            digest.update(len(rel).to_bytes(4, "big"))
            digest.update(rel)
            digest.update(len(data).to_bytes(8, "big"))
            digest.update(data)
    return {"files": count, "bytes": size, "sha256": digest.hexdigest()}

def file_sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def cases_sha256(manifest: dict[str, object]) -> str:
    """Digest the case definitions only.

    Clone URLs and prose live in the same file but do not change what is
    measured, so editing them must not look like a cohort change.
    """
    cases = [
        {key: case[key] for key in ("id", "repo", "revision", "corpus")}
        for case in manifest["cases"]
    ]
    payload = json.dumps(cases, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(payload).hexdigest()


def corpus_stats(root: Path) -> dict[str, int]:
    stats = {"markdown_files": 0, "markdown_bytes": 0, "mdx_files": 0, "mdx_bytes": 0}
    for path in root.rglob("*"):
        if not path.is_file():
            continue
        suffix = path.suffix.lower()
        if suffix == ".md":
            stats["markdown_files"] += 1
            stats["markdown_bytes"] += path.stat().st_size
        elif suffix == ".mdx":
            stats["mdx_files"] += 1
            stats["mdx_bytes"] += path.stat().st_size
    return stats


def write_config(worktree: Path, case: dict[str, object], ingest_only: bool) -> str:
    config = worktree / "poolboy.toml"
    if config.exists():
        raise RuntimeError("checkout already contains poolboy.toml; refusing to replace it")
    name = str(case["id"]).replace('"', "")
    corpus = "poolboy-docs" if ingest_only else str(case["corpus"]).replace('"', "")
    if ingest_only:
        (worktree / corpus).mkdir()
    config.write_text(
        f'spec = "0.2"\nname = "{name}"\ncorpus = "{corpus}"\noutput = "poolboy-dist"\n',
        encoding="utf-8",
    )
    return corpus


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", action="append", dest="cases", help="case id; repeatable")
    parser.add_argument(
        "--sandbox",
        type=Path,
        default=os.environ.get("POOLBOY_BENCH_SANDBOX"),
        help="scratch directory holding cohort clones and results; "
        "defaults to $POOLBOY_BENCH_SANDBOX",
    )
    parser.add_argument(
        "--poolboy",
        type=Path,
        required=True,
        help="path to the Poolboy binary under test; always name it explicitly",
    )
    parser.add_argument("--skip-build", action="store_true")
    parser.add_argument("--ingest-only", action="store_true")
    args = parser.parse_args()

    if args.sandbox is None:
        parser.error("pass --sandbox or set POOLBOY_BENCH_SANDBOX; "
                     "cohort clones and results must not land in the repository")
    sandbox = Path(args.sandbox).expanduser().resolve()
    if sandbox == ROOT or ROOT in sandbox.parents or sandbox in ROOT.parents:
        parser.error(f"sandbox must be outside the repository: {sandbox}")
    if not sandbox.is_dir():
        parser.error(f"sandbox not found: {sandbox}")

    manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
    selected = [case for case in manifest["cases"] if not args.cases or case["id"] in args.cases]
    unknown = set(args.cases or ()) - {case["id"] for case in selected}
    if unknown:
        parser.error("unknown case: " + ", ".join(sorted(unknown)))

    poolboy = args.poolboy.expanduser().resolve()
    if not poolboy.is_file():
        parser.error(f"Poolboy binary not found: {poolboy}")

    for case in selected:
        checkout = sandbox / case["repo"]
        if not checkout.is_dir():
            parser.error(f"{case['id']}: missing clone {checkout}")
        actual = run(["git", "rev-parse", "HEAD"], checkout)
        if actual.returncode != 0 or actual.stdout.strip() != case["revision"]:
            parser.error(
                f"{case['id']}: expected {case['revision']}, "
                f"found {actual.stdout.strip() or actual.stderr.strip()}"
            )
        dirty = run(["git", "status", "--porcelain", "--untracked-files=all"], checkout)
        if dirty.returncode != 0 or dirty.stdout:
            parser.error(f"{case['id']}: checkout must be clean before benchmarking")

    run_id = dt.datetime.now(dt.UTC).strftime("%Y%m%dT%H%M%S%fZ") + "-" + uuid.uuid4().hex[:8]
    result_root = sandbox / "results" / run_id
    work_root = sandbox / ".work" / run_id
    result_root.mkdir(parents=True)
    work_root.mkdir(parents=True)
    version = run([str(poolboy), "version"], sandbox)
    if version.returncode != 0 or not version.stdout.strip():
        parser.error(f"Poolboy version failed: {version.stderr.strip() or 'empty output'}")
    commands = ("scan",) if args.ingest_only else (COMMANDS[:-1] if args.skip_build else COMMANDS)
    environment = {
        "schema": 2,
        "run_id": run_id,
        "cohort_sha256": cases_sha256(manifest),
        "baseline": manifest["baseline"],
        "poolboy": str(poolboy),
        "poolboy_sha256": file_sha256(poolboy),
        "poolboy_version": version.stdout.strip(),
        "arguments": {
            "cases": list(args.cases or ()),
            "skip_build": args.skip_build,
            "ingest_only": args.ingest_only,
        },
        "started_at": dt.datetime.now(dt.UTC).isoformat(),
        "cases": [case["id"] for case in selected],
        "complete": False,
        "results": [],
        "effective_commands": list(commands),
    }
    (result_root / "environment.json").write_text(json.dumps(environment, indent=2) + "\n")
    (result_root / "summary.json").write_text(json.dumps(environment, indent=2) + "\n")

    summaries: list[dict[str, object]] = []
    for case in selected:
        case_id = case["id"]
        repo = sandbox / case["repo"]
        revision = case["revision"]
        worktree = work_root / case_id
        case_result = result_root / case_id
        case_result.mkdir()


        added = run(["git", "worktree", "add", "--detach", str(worktree), revision], repo)
        if added.returncode != 0:
            raise RuntimeError(f"{case_id}: could not create worktree: {added.stderr.strip()}")

        summary: dict[str, object] = {
            "id": case_id,
            "repo": case["repo"],
            "revision": revision,
            "corpus": case["corpus"],
            "tier": case["tier"],
            "role": case["role"],
            "axes": case["axes"],
            "commands": {},
        }
        try:
            configured_corpus = write_config(worktree, case, args.ingest_only)
            summary["configured_corpus"] = configured_corpus
            source_corpus = worktree / case["corpus"]
            summary["corpus_stats"] = corpus_stats(source_corpus)
            summary["configured_corpus_stats"] = corpus_stats(worktree / configured_corpus)
            for command in commands:
                started = time.perf_counter()
                completed = run([str(poolboy), "--root", str(worktree), command, "--format", "json"], worktree)
                elapsed = time.perf_counter() - started
                (case_result / f"{command}.stdout").write_text(completed.stdout, encoding="utf-8")
                (case_result / f"{command}.stderr").write_text(completed.stderr, encoding="utf-8")
                summary["commands"][command] = {
                    "exit_code": completed.returncode,
                    "seconds": round(elapsed, 6),
                }

            publication = worktree / "poolboy-dist"
            if publication.exists():
                shutil.move(str(publication), case_result / "publication")
                summary["publication"] = tree_stats(case_result / "publication")
            lock = worktree / ".poolboy" / "sources.lock.json"
            if lock.exists():
                shutil.copy2(lock, case_result / "sources.lock.json")
        finally:
            removed = run(["git", "worktree", "remove", "--force", str(worktree)], repo)
            if removed.returncode != 0:
                print(f"warning: {case_id}: worktree cleanup failed: {removed.stderr.strip()}", file=sys.stderr)

        (case_result / "summary.json").write_text(json.dumps(summary, indent=2) + "\n", encoding="utf-8")
        summaries.append(summary)
        environment["results"] = summaries
        (result_root / "summary.json").write_text(json.dumps(environment, indent=2) + "\n", encoding="utf-8")
        print(f"{case_id}: " + ", ".join(f"{name}={data['exit_code']}" for name, data in summary["commands"].items()))

    environment["finished_at"] = dt.datetime.now(dt.UTC).isoformat()
    environment["complete"] = True
    (result_root / "summary.json").write_text(json.dumps(environment, indent=2) + "\n", encoding="utf-8")
    shutil.rmtree(work_root, ignore_errors=True)
    print(result_root)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
