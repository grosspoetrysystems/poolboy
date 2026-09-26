# Maintaining Poolboy

Poolboy's files express policy; GitHub settings enforce required CI and automation boundaries. Keep the written policy, CODEOWNERS, branch protection, and automation permissions in sync, including which review rules are documented but not enforced.

## Ownership

`.github/CODEOWNERS` documents review ownership. It currently assigns the repository and `.github/` policy plane to `@thekidnamedkd`; branch protection does not enforce CODEOWNERS while Poolboy has one maintainer.

Contributor changes to governance files, workflows, dependency manifests, lockfiles, release logic, security-sensitive code, generated artifacts, and agent instructions should receive maintainer review before merge. A ready contributor pull request must pass required CI on its latest commit, resolve every review conversation, and receive `@thekidnamedkd`'s approval. Branch protection enforces the required checks; the approval is maintainer practice rather than an enforced gate, because GitHub's required-review setting cannot be satisfied while the only reviewer is also the only author. Re-enable required reviews when a second maintainer joins.

Repository administrators retain full branch-protection bypass. While Poolboy has one maintainer, `@thekidnamedkd` may merge their own same-repository pull requests without contributor review or PR-contract requirements; required CI still applies. The exemption is keyed to a human account opening a branch in this repository. Contributor, bot, and fork pull requests keep their respective contracts, and this authority is not delegated to bots or agents.

## Review path

1. Route contributions through pull requests.
2. Let the PR Contract workflow check metadata from the protected base repository.
3. Run builds and tests in the normal `pull_request` checks without repository secrets.
4. Have `@thekidnamedkd` review contributor changes to CODEOWNERS-owned or security-sensitive paths. This is maintainer practice, not a branch-protection gate.
5. Merge contributor changes through the protected branch path after required checks and conversations are resolved. Repository administrators may bypass these gates under their maintainer authority.

Use one actionable response for policy failures: one reason, one policy link, and one repair path.

## Automation boundaries

Dependabot is authorized repository automation. Its `dependabot[bot]` pull requests may omit the human-authored body template, but still require all required CI checks and maintainer review before merge. Dependabot may open weekly dependency updates for GitHub Actions, Go modules, and all npm packages under `companion`; development dependencies are grouped as development tooling. Treat Dependabot pull requests like any other dependency change: review the diff, release notes, lockfiles, and CI before merging.

GitHub Actions may validate pull requests, build, deploy, release, and attest artifacts only through the workflows in `.github/workflows/` and their declared permissions. The PR Contract workflow runs on `pull_request_target`; keep it limited to protected-base files and event metadata, with read-only contents permission and no checkout or execution of contributor code.

Only maintainer-authorized automation may submit without a human operating each run. Agents must not approve or merge their own work, bypass checks or reviews, impersonate a human, or answer substantive review questions as though they represented the accountable human's judgment.

Automation may triage or provide evidence. It must not approve, merge, bypass CODEOWNERS, expand its own permissions, or become the only reviewer of consequential changes.

## Security response

Handle suspected vulnerabilities, exposed secrets, malicious dependencies, compromised automation, and workflow escalation through `SECURITY.md`. Preserve URLs, actor IDs, SHAs, workflow run IDs, job logs, and release artifact identifiers before rotating credentials or disabling affected automation.
