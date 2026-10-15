# Maintaining Poolboy

Poolboy's files express policy; GitHub settings enforce required CI and automation boundaries. Keep the written policy, CODEOWNERS, branch protection, and workflow permissions in sync. Poolboy's governance implementation is rooted in [Helicopter](https://github.com/grosspoetrysystems/helicopter) revision `57db0a75`; Vernacular is an existing adopter reference, not a source of Poolboy's product checks.

## Current protection state

The live default-branch protection requires `check`, `windows`, and `contract` from app `15368`, plus `DCO` from the external DCO GitHub App (app `1861`), with strict freshness. Repository variables set `HELICOPTER_MODE=solo` and `HELICOPTER_SOLO_MAINTAINER_ID=65736142` for `@thekidnamedkd`. Resolved conversations are required; force pushes and branch deletion are disabled. Administrator enforcement remains disabled.

Do not run Helicopter's upstream `scripts/configure_repository.py` against Poolboy. Its required contexts are not Poolboy's and would clobber `check`, `windows`, or `contract`. Apply the activation sequence below explicitly, preserving Poolboy's separate check, Windows, release, and deploy workflows.

## Ownership and review

`.github/CODEOWNERS` documents review ownership. It assigns the repository and governance plane to `@thekidnamedkd`; branch protection does not enforce CODEOWNERS while Poolboy has one maintainer.

Contributor changes to governance files, workflows, dependency manifests, lockfiles, release logic, signing, security-sensitive code, generated artifacts, and agent instructions should receive maintainer review before merge. A ready contributor pull request must pass required CI on its latest commit, resolve every review conversation, and receive `@thekidnamedkd`'s approval. Branch protection enforces required checks; the approval is maintainer practice rather than an enforced gate while the only reviewer is also the only author. Re-enable required reviews when a second maintainer joins.

Repository administrators retain emergency branch-protection bypass. Use it deliberately for recovery or a reviewed urgent change; it is not evidence that checks ran or that a human approved a contribution. Never fabricate an approval, attestation, DCO certification, or review on behalf of a human. The sole maintainer may merge a same-repository pull request on passing required CI without a second approval because GitHub does not allow its author to approve it. This authority is not delegated to bots or agents, and contributor, bot, and fork pull requests keep their respective contracts.

## Activation sequence

An administrator adopting the policy must:

1. Keep `check`, `windows`, and `contract` as required status contexts. Confirm each has run successfully from the protected default branch; do not replace them with Helicopter's `test-validator` or `validate-metadata` names.
2. Confirm the protected-base contract workflow reads `HELICOPTER_MODE` and `HELICOPTER_SOLO_MAINTAINER_ID`, checks immutable repository and sender/author identity, and runs the trusted validator without checking out contributor code. Exercise its accepted and refused contributor paths with `HELICOPTER_MODE=contributor` (or an external/fork event); a same-repository solo metadata exemption intentionally returns before parsing an invalid body, so it cannot demonstrate refusal behavior.
3. Set repository-scoped variables deliberately. For the current sole-maintainer mode, use `HELICOPTER_MODE=solo` and configure `HELICOPTER_SOLO_MAINTAINER_ID=65736142` only after verifying that immutable GitHub user ID. If variables are unset, malformed, or the repository/head/author checks do not match, validation fails closed to contributor mode. The exemption is metadata-only: it never bypasses `check`, `windows`, `contract`, DCO, review conversations, or security controls.
4. Confirm the active external DCO GitHub App (app `1861`) reports both paths: a signed pull request must report a passing `DCO` status, and an unsigned pull request must report a refusal. Record the observed result; do not add `DCO` to required contexts based only on documentation or an app listing.
5. Only after those observations, add `DCO` bound to app `1861` **in addition to** `check`, `windows`, and `contract`, retaining their existing app `15368` bindings. DCO remains an independent status and does not replace the Poolboy contract validator or create automatic human sign-offs.
6. Verify the effective branch protection: strict required checks, resolved conversations, force-push and deletion restrictions, deliberate administrator bypass, and review/CODEOWNER settings appropriate to the current maintainer count. Keep production deploy and release triggers explicit.

No configurator command is part of this sequence. If the maintainer mode changes, update the repository variables and branch protection together, then exercise both accepted and refused intake paths before relying on the new mode.

## Review path

1. Route contributions through pull requests.
2. Let the contract workflow validate metadata from the protected base repository.
3. Run builds and tests in normal `pull_request` checks without repository secrets.
4. Have `@thekidnamedkd` review contributor changes to CODEOWNERS-owned or security-sensitive paths. This is maintainer practice, not a fabricated branch-protection approval.
5. Merge contributor changes through the protected branch path after required checks and conversations are resolved. Administrators may use emergency bypass only under the authority above.

Use one actionable response for policy failures: one reason, one policy link, and one repair path.

## Automation boundaries

Dependabot is authorized repository automation. Its narrow metadata exemption requires the PR author and event sender to be the same `dependabot[bot]` Bot account with matching positive immutable IDs; updates sent by a human or another bot are refused. Exempt updates still require all required CI and maintainer review before merge; DCO validation is an independent required branch-protection check. Dependabot may open weekly dependency updates for GitHub Actions, Go modules, and npm packages under `companion`; development dependencies are grouped as development tooling. Review the diff, release notes, lockfiles, and CI before merging.

GitHub Actions may validate pull requests, build, deploy, release, and attest artifacts only through the workflows in `.github/workflows/` and their declared permissions. The contract workflow runs on `pull_request_target`; keep it limited to protected-base files and event metadata, with read-only contents permission and no checkout or execution of contributor code. Build and test jobs remain separate `pull_request` jobs.

Only maintainer-authorized automation may submit without a human operating each run. Agents must not approve or merge their own work, bypass checks or reviews, impersonate a human, or answer substantive review questions as though they represented the accountable human's judgment. Automation may triage or provide evidence; it must not approve, merge, bypass CODEOWNERS, expand its own permissions, or become the only reviewer of consequential changes.

## Security response

Handle suspected vulnerabilities, exposed secrets, malicious dependencies, compromised automation, and workflow escalation through `SECURITY.md`. Preserve URLs, actor IDs, SHAs, workflow run IDs, job logs, and release artifact identifiers before rotating credentials or disabling affected automation. Do not rerun a suspicious contribution merely to collect evidence.
