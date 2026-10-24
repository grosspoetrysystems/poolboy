# Poolboy security policy

This response pattern follows Helicopter v0.1.0 at exact commit (`cf83ae51567cd0801484cf6ead72cf599b58423a`), adapted to Poolboy's private GitHub reporting channel.

## Report privately

Do not open a public issue for a suspected vulnerability, exposed credential, malicious dependency, compromised automation identity, or workflow/runner compromise.

Use the repository's [private vulnerability reporting form](https://github.com/grosspoetrysystems/poolboy/security/advisories/new). GitHub Issues are disabled; never use a public pull request to request a private reporting channel or disclose vulnerability details.

Include what you can safely provide:

- affected version, commit, release, or dependency;
- impact and reproduction steps;
- proof of concept or sanitized logs;
- relevant pull request, workflow run, job, agent run, base, and head identifiers; and
- whether any secret, runner, bot identity, release artifact, or downstream user may be affected.

Remove credentials and personal data from attachments. Do not rerun a suspicious contribution merely to collect more evidence.

## Response

Poolboy maintainers will use the private GitHub thread when available to acknowledge receipt, contain active risk, coordinate fixes, and plan disclosure. Response time depends on maintainer capacity and severity; Poolboy does not promise a fixed service level.
