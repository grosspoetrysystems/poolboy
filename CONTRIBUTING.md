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

Commit messages must be one-line [Conventional Commits](https://www.conventionalcommits.org/) headers, for example `fix(preview): reject non-loopback hosts`.

## Pull request contract

Open unfinished work as a draft. Before requesting review, complete
`.github/pull_request_template.md` with:

- a linked issue or specification, or an inline specification for a small change;
- observable acceptance criteria and evidence for each criterion;
- explicit scope, non-goals, risk, rollback, and reviewer focus;
- a named accountable human and, when applicable, agent provenance;
- the accountable human's verification and attestations.

Discuss large features, migrations, dependencies, security-sensitive work, and architectural changes in an issue before implementation. A ready pull request must be focused enough to review as one change. To be merge eligible, all required CI checks must pass on the latest commit, every review conversation must be resolved, and `@thekidnamedkd` must approve.

## AI and agent contributions

AI assistance is welcome; unattended submissions are not. A named human must review every changed line, understand and be able to explain the design, personally exercise the result, and remain accountable for correctness, security, licensing, and review follow-up.

Disclose material AI use in the pull request template. Assisted work identifies the tool and model. Materially agent-authored work also records an immutable run or sanitized trace, the protected base SHA, and the agent's command, network, secret, and MCP capabilities. Evidence must report observed behavior, not merely that an agent or generated test claims success.

Only maintainer-authorized automation may submit without a human operating each run. An agent may not approve or merge its own work, bypass checks or reviews, weaken project policy to make its pull request pass, impersonate a human, or answer substantive review questions as though it represented the accountable human's judgment. Pull-request changes to this policy or its validator do not govern that same pull request; intake validation runs the version from the protected base branch.

## Generated documentation

Edit templates and metadata rather than generated output. Run `make site`, review changes to `.poolboy/generated.json` and generated documents, and include them in the same commit as their inputs. `make verify` fails when generated output is stale.

## Pull requests

Keep changes focused. Include the command or scenario that proves the behavior. Do not commit build output, local signing keys, environment files, or generated `.site/` contents.
