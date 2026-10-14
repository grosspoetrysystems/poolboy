## Outcome

<!-- State the user-visible or operator-visible result in one sentence, and why it matters. -->
<what changes for users or operators and why>

## Issue or spec

<!-- Use `Fixes #123`, an HTTPS URL, or `inline` when the acceptance criteria are the complete spec. -->
#<issue-number-or-inline>

## Acceptance criteria

<!-- Every criterion must be an observable behavior or invariant. IDs are unique and each has matching evidence below. -->
- AC-1: <observable behavior or invariant>

## Evidence

<!-- Cover every AC-N with the exact command or scenario and its observed result. IDs are unique. -->
- AC-1 — command/scenario: <exact command or user path>; observed: <result>

## Scope

- In: <behavior, files, or subsystems included>
- Out: <explicit non-goals, or none>

## Human accountability

Accountable contributor: @YOUR_GITHUB_USERNAME

- [ ] I am or represent the accountable human named above.
- [ ] I reviewed the complete diff.
- [ ] I understand and can explain the change.
- [ ] I verified the evidence below.
- [ ] I have the right to submit this contribution.
- [ ] This is not an unattended agent submission; I remain responsible for the contribution.

## Verification

<!-- Describe the checks a human actually reviewed and exercised. CI verifies independently. -->
<commands and concise observed results>

## Risk

<!-- Keep the canonical Risk heading; complete every field. -->
- Level: <low|medium|high>
- Failure mode: <what could go wrong>
- Rollback/mitigation: <revert, migration, containment, or not applicable>
- Reviewer focus: <specific assumptions, files, or questions>

## AI / automation

Select exactly one origin:

- [ ] Human-authored
- [ ] AI-assisted
- [ ] Agent-authored
- [ ] Authorized automation

For **AI-assisted**, **Agent-authored**, or **Authorized automation** work, provide the identity and exact provider/model. For **Agent-authored** or **Authorized automation** work, provide immutable run provenance, base SHA, capabilities, and separate MCP disclosure. Human-authored work may leave the defaults below.

Agent identity: `none`
Provider/model: `none`
Run ID: `none`
Base SHA: `none`
MCP: `none`

Capabilities:

```json
{
  "repository_write": false,
  "command_execution": false,
  "network": "none",
  "secrets": "none",
  "approve": false,
  "merge": false
}
```
