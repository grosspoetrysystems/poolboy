# Maintaining Poolboy

Poolboy's files express policy; GitHub settings enforce required CI and automation boundaries. Keep the written policy, CODEOWNERS, branch protection, and workflow permissions in sync. Poolboy's governance implementation is rooted in [Helicopter](https://github.com/grosspoetrysystems/helicopter) v0.2.0 (exact commit `c16bcf26e44de77a4ebb532aabbd52c55b6679a7`); Vernacular is an existing adopter reference, not a source of Poolboy's product checks.

## Current protection state

The live default-branch protection requires `check`, `windows`, and `contract` from app `15368`, plus `DCO` from the external DCO GitHub App (app `1861`), with strict freshness. Repository variables set `HELICOPTER_MODE=solo` and `HELICOPTER_SOLO_MAINTAINER_ID=65736142` for `@thekidnamedkd`. Resolved conversations are required; force pushes and branch deletion are disabled. Administrator enforcement remains disabled.

Do not run Helicopter's upstream `scripts/configure_repository.py` against Poolboy. Its required contexts are not Poolboy's and would clobber `check`, `windows`, or `contract`. Apply the activation sequence below explicitly, preserving Poolboy's separate check, Windows, release, and deploy workflows.

## Ownership and review

`.github/CODEOWNERS` documents review ownership. It assigns the repository and governance plane to `@thekidnamedkd`; branch protection does not enforce CODEOWNERS while Poolboy has one maintainer.

Contributor changes to governance files, workflows, dependency manifests, lockfiles, release logic, signing, security-sensitive code, generated artifacts, and agent instructions should receive maintainer review before merge. A ready contributor pull request must pass required CI on its latest commit, resolve every review conversation, and receive `@thekidnamedkd`'s approval. Branch protection enforces required checks; the approval is maintainer practice rather than an enforced gate while the only reviewer is also the only author. Re-enable required reviews when a second maintainer joins.

Repository administrators retain emergency branch-protection bypass. Use it deliberately for recovery or a reviewed urgent change; it is not evidence that checks ran or that a human approved a contribution. Never fabricate an approval, DCO certification, or review on behalf of a human. The sole maintainer may merge a same-repository pull request on passing required CI without a second approval because GitHub does not allow its author to approve it. This authority is not delegated to bots or agents, and contributor, bot, and fork pull requests keep their respective contracts.

## Activation sequence

An administrator adopting the policy must:

1. Keep `check`, `windows`, and `contract` as required status contexts. Confirm each has run successfully from the protected default branch; do not replace them with Helicopter's `test-validator` or `validate-metadata` names.
2. Confirm the protected-base contract workflow reads `HELICOPTER_MODE`, `HELICOPTER_SOLO_MAINTAINER_ID`, and `HELICOPTER_AUTOMATION_LOGINS`, checks immutable repository and sender/author identity (including Bot type and positive equal IDs for authorized automation), and runs the trusted validator without checking out contributor code. Exercise its accepted and refused contributor paths with `HELICOPTER_MODE=contributor` (or an external/fork event); a same-repository solo metadata exemption intentionally returns before parsing an invalid body, so it cannot demonstrate refusal behavior.
3. Set repository-scoped variables deliberately. For the current sole-maintainer mode, use `HELICOPTER_MODE=solo` and configure `HELICOPTER_SOLO_MAINTAINER_ID=65736142` only after verifying that immutable GitHub user ID. If variables are unset, malformed, or the repository/head/author checks do not match, validation fails closed to contributor mode. The exemption is metadata-only: it never bypasses `check`, `windows`, `contract`, DCO, review conversations, or security controls.
4. Confirm the active external DCO GitHub App (app `1861`) reports both paths: a signed pull request must report a passing `DCO` status, and an unsigned pull request must report a refusal. Record the observed result; do not add `DCO` to required contexts based only on documentation or an app listing.
5. Only after those observations, add `DCO` bound to app `1861` **in addition to** `check`, `windows`, and `contract`, retaining their existing app `15368` bindings. DCO remains an independent status and does not replace the Poolboy contract validator or create automatic human sign-offs.
6. Verify the effective branch protection: strict required checks, resolved conversations, force-push and deletion restrictions, deliberate administrator bypass, and review/CODEOWNER settings appropriate to the current maintainer count. Keep production deploy and release triggers explicit.

No configurator command is part of this sequence. If the maintainer mode changes, update the repository variables and branch protection together, then exercise both accepted and refused intake paths before relying on the new mode.

## Optional review-ramp activation

The review ramp and queue are present locally; no hosted activation has occurred in this checkout. An administrator must complete these external gates deliberately:

- create `queue:awaiting-author`, `queue:awaiting-reviewer`, and `queue:no-capacity` labels;
- choose `HELICOPTER_QUEUE_NUDGE_HOURS` (default `168`) and `HELICOPTER_QUEUE_PAUSE_HOURS` (default `504`), or leave the workflow defaults;
- optionally add `HELICOPTER_REVIEW_KEY`; when absent, the advisory first-review step is skipped, with no fake key or fallback;
- keep `HELICOPTER_AUTOMATION_LOGINS` unset to use the workflow's `dependabot[bot]` default, or explicitly list additional bot logins;
- exercise accepted, refused, unsafe-sender, and fork intake paths before enabling any hosted setting.

Adding `HELICOPTER_REVIEW_KEY` activates a privileged third-party integration: the digest-pinned PR-Agent image receives the provider secret and a GitHub token with `issues: write` and `pull-requests: write` while processing untrusted PR text and diffs. Review the pinned image, provider, permissions, and prompt-injection risk before enabling it. Its output is advisory, not a human approval or verified evidence.

Do not run `scripts/configure_repository.py` against Poolboy. Its upstream required contexts and settings are incompatible with Poolboy's existing branch protection; labels and security settings remain separate administrator gates.

## Intentional adoption differences

- Poolboy uses Linear-only intake, so the upstream GitHub issue forms, `Closes #` placeholder, and GitHub-Issue-linked `queue:possibly-superseded` behavior are not applicable. Poolboy keeps its explicit Linear references and existing whose-turn queue.
- Poolboy keeps the existing `check`, `windows`, `contract`, and `DCO` status names and app bindings rather than upstream `test-validator` or `validate-metadata`.
- Poolboy follows v0.1.0's voluntary `Assisted-by` attribution and human-only DCO certification; there is no workflow variable or template section for tool-origin reporting.
- The upstream repository configurator and its read-only checker (`scripts/check_install.py`), self-development checks, issue-form tests, and standalone development hooks are not imported; Poolboy's existing `make check` and 90% Lefthook standard remain authoritative.
- The queue and advisory review workflows are present locally but have no hosted activation; no hosted setting, secret, label, or branch-protection mutation was performed.

## Review path

1. Route contributions through pull requests.
2. Let the contract workflow validate metadata from the protected base repository.
3. Run builds and tests in normal `pull_request` checks without repository secrets.
4. Have `@thekidnamedkd` review contributor changes to CODEOWNERS-owned or security-sensitive paths. This is maintainer practice, not a fabricated branch-protection approval.
5. Merge contributor changes through the protected branch path after required checks and conversations are resolved. Administrators may use emergency bypass only under the authority above.

Use one actionable response for policy failures: one reason, one policy link, and one repair path.

## Automation boundaries

Dependabot is authorized repository automation. The workflow defaults `HELICOPTER_AUTOMATION_LOGINS` to `dependabot[bot]`; additional bot logins require explicit maintainer configuration. Admission requires Bot author and sender types, positive equal immutable IDs, and equal receiving, base, and head repository IDs, plus `main` as the Dependabot target, a supported dependency branch, and a dependency-bump title. A human-triggered branch update is refused because the event sender is not the bot author. Exempt updates still require all required CI and maintainer review before merge; DCO validation is an independent required branch-protection check. Dependabot may open weekly dependency updates for GitHub Actions, Go modules, and npm packages under `companion`; development dependencies are grouped as development tooling. npm version updates have a seven-day cooldown aligned with the companion's strict pnpm release-age gate; pnpm still rejects immature transitive dependencies. Review the diff, release notes, lockfiles, and CI before merging.

GitHub Actions may validate pull requests, build, deploy, release, and attest artifacts only through the workflows in `.github/workflows/` and their declared permissions. The `contract` job runs on `pull_request_target` with read-only permissions and no checkout or execution of contributor code. Separate first-review and queue jobs grant `issues: write` and `pull-requests: write`; the optional first-review integration also receives the provider secret described above. Queue jobs run protected-base scripts against GitHub metadata, never contributor code. Build and test jobs remain separate `pull_request` jobs.

Only maintainer-authorized automation may submit without a human operating each run. Agents must not approve or merge their own work, bypass checks or reviews, impersonate a human, or answer substantive review questions as though they represented the accountable human's judgment. Automation may triage or provide evidence; it must not approve, merge, bypass CODEOWNERS, expand its own permissions, or become the only reviewer of consequential changes.

## Security response

Handle suspected vulnerabilities, exposed secrets, malicious dependencies, compromised automation, and workflow escalation through `SECURITY.md`. Preserve URLs, actor IDs, SHAs, workflow run IDs, job logs, and release artifact identifiers before rotating credentials or disabling affected automation. Do not rerun a suspicious contribution merely to collect evidence.
