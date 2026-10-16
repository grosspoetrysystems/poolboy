# Contributing to Poolboy

Poolboy's contribution governance is adopted from [Helicopter](https://github.com/grosspoetrysystems/helicopter) (revision `57db0a75`), the policy pack Poolboy shares with other adopters such as Vernacular. Poolboy keeps Helicopter's canonical accountability, verification, risk, and AI/automation contract and adds its own acceptance-criteria and evidence requirements. Poolboy ships under GPLv3 (see `LICENSE`); the governance tooling copied from Helicopter keeps its upstream MIT notice (see `NOTICE`) and does not relicense the product.

Poolboy welcomes human-authored, AI-assisted, and agent-authored work when it is useful, reviewable, and owned by an accountable human.

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
make check    # tests, lint, types, coverage, dead-code, and the PR contract validator tests
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

## Human accountability

Every pull request needs an accountable human. By submitting one you confirm that you reviewed the complete diff, understand and can explain the change, personally verified the evidence it reports, can revise it under review, and have the right to submit it under Poolboy's license. AI assistance does not transfer those responsibilities to a model, provider, agent, bot, or tool.

## AI and agent use

AI assistance is welcome; unattended contributor submissions are not. Trivial autocomplete needs no disclosure; disclose AI use that materially shaped the implementation, tests, issue, or pull request. Choose exactly one origin in the template: human-authored, AI-assisted, agent-authored, or authorized automation.

Materially agent-authored and authorized-automation pull requests record the agent identity, provider and exact model, an immutable run ID or sanitized trace, the protected base SHA, and the agent's capabilities as the exact JSON block in the template (`repository_write`, `command_execution`, `network`, `secrets`, `approve`, `merge`). Disclose MCP tools and servers separately from that JSON block rather than altering it. Do not publish raw transcripts that may contain secrets or restricted material. Evidence must report observed behavior, not merely that an agent or generated test claims success.

An agent must not impersonate a human, approve or merge its own work, bypass checks or reviews, weaken project policy to make its pull request pass, use repository or deployment secrets in untrusted contribution CI, or answer substantive review questions as the accountable human's judgment. Pull-request changes to this policy or its validator do not govern that same pull request; intake validation runs the version from the protected base branch.

Dependabot is authorized repository automation. Its generated pull requests are exempt from the human-authored body template, but not from required CI or maintainer review before merge. It is Poolboy's only pre-authorized unattended automation; Poolboy intentionally has no broad automation-delegation workflow or external-contributor pull-request cap.

## Pull request contract

Open unfinished work as a draft. Before requesting review, complete `.github/pull_request_template.md` with:

- `Outcome` and `Issue or spec`: state the user-visible result and link the issue/specification, or use `inline` for a small change;
- the canonical `Human accountability`, `Verification`, `Risk`, and `AI / automation` sections;
- `Acceptance criteria` and matching `Evidence`, with the exact command or scenario and observed result for every criterion;
- `Scope` plus the Poolboy failure mode, rollback/mitigation, and reviewer-focus fields inside canonical `Risk`, including explicit non-goals;
- a named accountable human with the agent provenance above when applicable; and
- the human attestations, including that this is not an unattended agent submission.

Discuss large features, migrations, dependencies, security-sensitive work, and architectural changes in an issue before implementation. A ready pull request must be focused enough to review as one change, pass required CI on its latest commit, resolve every review conversation, and receive `@thekidnamedkd`'s approval. The sole maintainer's own pull requests merge on passing required CI without a second approval, because GitHub does not allow a pull-request author to approve their own work; see `MAINTAINERS.md`.

The sole-maintainer metadata exemption is configured through the `HELICOPTER_MODE` and `HELICOPTER_SOLO_MAINTAINER_ID` repository variables, not a hardcoded bypass. Unset or invalid configuration fails closed to the full contributor contract, and the exemption covers only the body template, never required CI.

## Commits and sign-off

Commit subjects are one-line [Conventional Commits](https://www.conventionalcommits.org/) headers, for example `fix(preview): reject non-loopback hosts`. Every commit must include a `Signed-off-by` trailer; a commit body and other trailers are allowed beneath the subject.

Poolboy requires the [Developer Certificate of Origin 1.1](https://developercertificate.org/) sign-off on every commit. Use `git commit -s`, which adds:

```text
Signed-off-by: Your Name <your.email@example.com>
```

The accountable human makes the certification; an agent may mechanically add their trailer only with explicit authorization. The external DCO GitHub App (app `1861`) validates identity and certification, and its `DCO` status is required by branch protection. Local commitlint also requires the trailer; nothing here exempts a contribution from required CI.

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
