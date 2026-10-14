# Contributing

## Prerequisites

- Go version declared in `go.mod`
- Node.js 24 or newer
- Corepack

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
make check    # tests, lint, types, coverage, and dead-code checks
make verify   # check, build the site, verify generated files, and smoke-test the CLI
```

Pre-commit checks formatting, Go lint, Biome, TypeScript, Knip, and Conventional Commit messages. Pre-push runs `make verify`. CI runs the same verification command on Linux, then compiles packages and tests and smoke-tests the CLI on Windows.

Commit messages must use a [Conventional Commits](https://www.conventionalcommits.org/) header, for example `fix(preview): reject non-loopback hosts`, and include a `Signed-off-by` trailer. Use `git commit -s` to certify your right to contribute under the [Developer Certificate of Origin](https://developercertificate.org/). Local commitlint requires the trailer; the GitHub DCO check validates each commit's sign-off. An agent must not certify this on a human's behalf without that human's authorization.

## Pull request contract

Open unfinished work as a draft. Before requesting review, complete
`.github/pull_request_template.md` with:

- a linked issue or specification, or an inline specification for a small change;
- observable acceptance criteria and evidence for each criterion;
- explicit scope, non-goals, risk, rollback, and reviewer focus;
- a named accountable human and, when applicable, agent provenance;
- the accountable human's verification and attestations.

Discuss large features, migrations, dependencies, security-sensitive work, and architectural changes in an issue before implementation. A ready pull request must be focused enough to review as one change. To be merge eligible, contributor pull requests must pass required CI on the latest commit, resolve every review conversation, and receive `@thekidnamedkd`'s approval. The sole maintainer's own pull requests merge on passing required CI without a second approval, because GitHub does not allow a pull request author to approve their own work; see `MAINTAINERS.md`.

## AI and agent contributions

AI assistance is welcome; unattended contributor submissions are not. A named human must review every contributed line, understand and be able to explain the design, personally exercise the result, and remain accountable for correctness, security, licensing, and review follow-up.

Contributors disclose material AI use in the pull request template. Assisted work identifies the tool and model. Materially agent-authored work also records an immutable run or sanitized trace, the protected base SHA, and the agent's command, network, secret, and MCP capabilities. The sole maintainer's same-repository pull requests are exempt from the PR contract; required CI still applies. Evidence must report observed behavior, not merely that an agent or generated test claims success.

Only maintainer-authorized automation may submit without a human operating each run. An agent may not approve or merge its own work, bypass checks or reviews, weaken project policy to make its pull request pass, impersonate a human, or answer substantive review questions as though it represented the accountable human's judgment. Pull-request changes to this policy or its validator do not govern that same pull request; intake validation runs the version from the protected base branch.

Dependabot is authorized repository automation. Its generated pull requests are exempt from the human-authored body template, but not from required CI or maintainer review before merge.

## Generated documentation

Edit templates and metadata rather than generated output. Run `make site`, review changes to `.poolboy/generated.json` and generated documents, and include them in the same commit as their inputs. `make verify` fails when generated output is stale.

## What lives in this repository

The repository carries the product and nothing else: Go and TypeScript source,
maintained documentation under `docs/`, the published site inputs, packaging,
and the shipped `skills/poolboy-discovery/` Skill that users install in their
own coding agent.

Evaluation material is local working evidence and is deliberately absent. The
benchmark harness and its retained results, dogfood records, trial narratives,
captured foreign corpora, and external-project notes live in each contributor's
private workspace outside the checkout. Personal agent tooling kept inside the
checkout is Git-ignored by a `.gitignore` containing `*` inside its own
directory. A fresh clone will not have any of it, and that is intended.

The test is whether someone using or building Poolboy needs the file. The
discovery Skill ships because the product tells users to run it. A benchmark
cohort, a question bank, a grader, or a record of how one documentation site
survived capture does not ship, because it describes our evaluation of the
product rather than the product.

Two consequences worth stating plainly. Benchmark runs are not reproducible
from a clone; replaying one needs the local harness. And a path into a private
workspace is a provenance pointer, not retrievable evidence — when evidence
has to reach a reader, put the measured result in the issue or pull request
rather than a link nobody else can open.

## Pull requests

Keep changes focused. Include the command or scenario that proves the behavior. Do not commit build output, local signing keys, environment files, or generated `.site/` contents.
