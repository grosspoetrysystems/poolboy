# Poolboy security policy

## Report privately

Do not open a public issue for a suspected vulnerability, exposed credential, malicious dependency, compromised automation identity, or workflow/runner compromise.

Use the repository's GitHub **Security** tab to report privately through GitHub private vulnerability reporting or a draft security advisory when that option is available. If GitHub does not offer a private report button, open a public issue only to request a maintainer-owned private channel; do not include vulnerability details there.

Include what you can safely provide:

- affected version, commit, release, or dependency;
- impact and reproduction steps;
- proof of concept or sanitized logs;
- relevant pull request, workflow run, job, agent run, base, and head identifiers; and
- whether any secret, runner, bot identity, release artifact, or downstream user may be affected.

Remove credentials and personal data from attachments. Do not rerun a suspicious contribution merely to collect more evidence.

## Response

Poolboy maintainers will use the private GitHub thread when available to acknowledge receipt, contain active risk, coordinate fixes, and plan disclosure. Response time depends on maintainer capacity and severity; Poolboy does not promise a fixed service level.
