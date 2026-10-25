# Contributing to Poolboy

Poolboy's contribution governance is adopted from [Helicopter](https://github.com/grosspoetrysystems/helicopter) v0.2.0 (exact commit `c16bcf26e44de77a4ebb532aabbd52c55b6679a7`), the policy pack Poolboy shares with other adopters such as Vernacular. Poolboy adapts Helicopter's Change, Verification, Risk, authorization, review-ramp, and queue controls to its Linear-only intake and existing protected checks. Poolboy ships under GPLv3 (see `LICENSE`); the governance tooling copied from Helicopter keeps its upstream MIT notice (see `NOTICE`) and does not relicense the product.

Poolboy welcomes useful, reviewable work however it was produced.

## What's checked

| Rule | Enforced by | Refusal code |
| --- | --- | --- |
| The description has **Change**, **Verification**, and **Risk** sections, each filled in | `validate-metadata` (required check) | `REFUSE_TEMPLATE_INCOMPLETE`, `REFUSE_CHANGE_EMPTY`, `REFUSE_VERIFICATION_EMPTY`, `REFUSE_RISK_UNCHECKED` |
| Every commit carries `Signed-off-by` with its author's email ([DCO](#legal)); merge commits are skipped | `validate-metadata` | `REFUSE_SIGNOFF_MISSING` |
| A bot can open pull requests only when a maintainer has listed it in `HELICOPTER_AUTOMATION_LOGINS` | `validate-metadata` | `REFUSE_BOT_UNAUTHORIZED` |
| Contributors without write access have at most two open non-draft pull requests (drafts don't count) | `validate-metadata` | `REFUSE_OPEN_PR_LIMIT` |
| Merging needs the required checks to pass and the reviews your mode requires; nobody can force-push or delete the default branch | Branch protection | — |
| Changes to workflows, security, dependencies, release machinery, policy, or agent instructions need the owners in `.github/CODEOWNERS` (contributor and team modes) | Branch protection (code-owner review) | — |
| Poolboy reruns its own checks; your report of what you ran is context, not evidence | Project CI | — |

Each refusal says how to fix it. After fixing, push or edit the description to re-run the check.

## Prerequisites

- Go 1.26 or newer. `go.mod` declares the source minimum; automated CI, release, and deploy builds select the latest patch of the supported Go 1.26 line.
- Node.js 24 or newer
- Corepack
- Python 3.10 or newer for the contribution validator and its tests. `make check` uses `python3` by default; select another interpreter with `make check PYTHON=python3.14`.

## Set up

```sh
git clone https://github.com/grosspoetrysystems/poolboy.git
cd poolboy
corepack enable
pnpm --dir companion install --frozen-lockfile
make hooks
make verify
```

`make hooks` installs the repository's Lefthook configuration; it does not modify commits or push branches.

## Work locally

```sh
make fmt      # apply Go and TypeScript formatting
make check    # tests, lint, types, coverage, dead-code, and the PR contract/queue tests
make verify   # check, build the site, verify generated files, and smoke-test the CLI
```

Pre-commit checks formatting, Go lint, Biome, TypeScript, Knip, and command documentation. The commit-msg hook checks Conventional Commit subjects and requires a `Signed-off-by` trailer. Pre-push runs `make verify`. CI runs the same verification on Linux; Windows CI compiles packages and test binaries and smoke-tests the CLI. A separate metadata-only workflow validates the pull-request contract, including drafts.

## Test coverage

The [coverage gates concept](docs/coverage.md) defines the enforced contract:
Go uses one aggregate statement profile across `./...` at 90%, while the
companion requires 90% branches, functions, lines, and statements. The
companion keeps its existing `src/bin/**` exclusion; the CLI smoke test is
separate.

Use `python3 tests/test_coverage_gate.py` for focused checker feedback,
`make check` for the full coverage gate, and `make verify` for the pre-push/CI
path that also builds and smoke-tests the CLI. Add meaningful
consumer-visible, boundary, and error tests; do not lower thresholds, add
exclusions, or write tests solely to inflate coverage. See the concept for
exact integer gate behavior, hook recovery, and the distinction between an
observed baseline and the 90% floor.

## Contributor responsibilities

Poolboy expects every contributor to read the complete diff, understand and explain the change, verify the evidence they report, and revise the work in response to review. These responsibilities stay with the person submitting the contribution; AI tools, providers, agents, and bots do not take them over.

## AI and agent use

AI assistance is allowed, but unattended contributor submissions are not. Contributors do not need to declare how a change was produced by default. If a tool helped, the commit trailer it adds is welcome, for example the Linux kernel's `Assisted-by: AGENT_NAME:MODEL_VERSION`. Only a person certifies the DCO; an agent may mechanically add that person's sign-off only with explicit authorization. An agent must not impersonate a person, approve or merge its own contribution, bypass required checks or reviews, weaken policy or security controls to make its work pass, use repository or deployment secrets in untrusted contribution CI, or answer substantive review questions as though it were the responsible human.

Never publish raw transcripts containing secrets, personal data, or restricted source material.

## Pull request contract

Open unfinished work as a draft. Before requesting review, complete `.github/pull_request_template.md`. HTML comments do not count as required headings or section content:

- `Change` states the problem, smallest solution, and user-visible effect. Include a Linear work item URL or write `inline`; the reference alone does not replace the change description. GitHub Issues are disabled.
- `Verification` lists the commands or steps run and what they showed, or explains why verification does not apply.
- `Risk` checks every area the change touches and records details and mitigations.
- `Reviewer focus` is optional and names uncertain behavior or assumptions.

The contract has no per-pull-request origin, capability, provenance, or human-attestation form. A human's DCO sign-off remains the legal contribution mechanism on every commit, and human review remains required even when metadata admission passes.

Each pull request should solve one coherent problem. Maintainers may ask for unrelated, generated, mechanical, or otherwise unreviewable changes to be split. Contributors without write access may have at most two non-draft pull requests open at once unless a maintainer grants an exception.

Poolboy tracks work in Linear, not GitHub Issues. Coordinate large features, migrations, dependencies, and architectural changes with `@thekidnamedkd` before implementation; maintainers track agreed work in Linear. Report security-sensitive concerns privately through [SECURITY.md](SECURITY.md), not a public pull request.

A ready pull request must be focused enough to review as one change, pass required CI on its latest commit, resolve every review conversation, and receive `@thekidnamedkd`'s approval. The sole maintainer's own pull requests merge on passing required CI without a second approval, because GitHub does not allow a pull-request author to approve their own work; see `MAINTAINERS.md`.

The sole-maintainer metadata exemption is configured through the `HELICOPTER_MODE` and `HELICOPTER_SOLO_MAINTAINER_ID` repository variables, not a hardcoded bypass. Unset or invalid configuration fails closed to the full contributor contract, and the exemption covers only the body template, never required CI, DCO, review conversations, or security controls.

## Review ramp

When the optional review ramp is activated, admission is followed by one advisory first review only when `HELICOPTER_REVIEW_KEY` exists; missing keys skip that step without a fallback. The queue then labels whose turn it is, nudges after `HELICOPTER_QUEUE_NUDGE_HOURS` (default 168), and marks no-capacity after `HELICOPTER_QUEUE_PAUSE_HOURS` (default 504). Drafts are not queued, and the queue never checks out or executes contributor code.
Dependabot is authorized repository automation. The trusted workflow defaults `HELICOPTER_AUTOMATION_LOGINS` to `dependabot[bot]`; other bot logins require explicit maintainer configuration. Admission requires Bot author and sender types, positive equal immutable IDs, and equal receiving, base, and head repository IDs. Dependabot updates must target `main`, use a supported dependency branch, and use a dependency-bump title; a human-triggered branch update remains refused. Authorized updates still require all required CI, DCO, and maintainer review before merge.

## Commits and sign-off

Commit subjects are one-line [Conventional Commits](https://www.conventionalcommits.org/) headers, for example `fix(preview): reject non-loopback hosts`. Every commit must include a `Signed-off-by` trailer; a commit body and other trailers are allowed beneath the subject.

Poolboy requires the [Developer Certificate of Origin 1.1](https://developercertificate.org/) sign-off on every commit. Use `git commit -s`, which adds:

```text
Signed-off-by: Your Name <your.email@example.com>
```

The accountable human makes each DCO certification; an agent may mechanically add their trailer only with explicit authorization. This deliberate Poolboy exception retains human certification without requiring manual command entry. The pull request form does not repeat the DCO certification or replace the external DCO GitHub App (app `1861`), whose `DCO` status is required by branch protection. Local commitlint also requires the trailer; nothing here exempts a contribution from required CI.

## Review

Poolboy independently reruns relevant checks; contributor and agent reports are context, not authoritative evidence. Changes to workflows, the validator, dependency manifests, lockfiles, release machinery, security-sensitive code, generated artifacts, or agent instructions require the owners named in `.github/CODEOWNERS`.

## Generated documentation

Edit templates and metadata rather than generated output. Run `make site`, review changes to `.poolboy/generated.json` and generated documents, and include them in the same commit as their inputs. `make verify` fails when generated output is stale.

## What lives in this repository

The repository carries the product and its maintained governance: Go and TypeScript source, Python contribution tooling, maintained documentation under `docs/`, the published site inputs, packaging, and the shipped `skills/poolboy-discovery/` Skill that users install in their own coding agent.

Evaluation material is local working evidence and is deliberately absent. The
benchmark harness and its retained results, dogfood records, trial narratives,
captured foreign corpora, and external-project notes live in each contributor's
private workspace outside the checkout. Personal agent tooling kept inside the
checkout is Git-ignored by a `.gitignore` containing `*` inside its own
directory. A fresh clone will not have any of it, and that is intended.

The test is whether someone using or building Poolboy needs the file. The discovery Skill ships because the product tells users to run it. A benchmark cohort, a question bank, a grader, or a record of how one documentation site survived capture does not ship, because it describes our evaluation of the product rather than the product.

Two consequences worth stating plainly. Benchmark runs are not reproducible
from a clone; replaying one needs the local harness. And a path into a private
workspace is a provenance pointer, not retrievable evidence — when evidence
has to reach a reader, put the measured result in the issue or pull request
rather than a link nobody else can open.

## Pull requests

Keep changes focused. Include the command or scenario that proves the behavior. Do not commit build output, local signing keys, environment files, or generated `.site/` contents.

## Security and conduct

Report vulnerabilities privately as described in `SECURITY.md`. Follow `CODE_OF_CONDUCT.md` in all project spaces.
