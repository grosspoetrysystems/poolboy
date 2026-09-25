import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const marker = "<!-- pr-contract:v2 -->";
const headings = [
  "Outcome",
  "Issue or spec",
  "Acceptance criteria",
  "Evidence",
  "Scope",
  "Risk and rollback",
  "Accountability and agent provenance",
  "Human attestation",
];

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function section(body, heading) {
  const escaped = escapeRegExp(heading);
  const match = body.match(
    new RegExp(`^## ${escaped}\\s*$([\\s\\S]*?)(?=^## |$(?![\\s\\S]))`, "mi"),
  );
  return match?.[1].replace(/<!--[\s\S]*?-->/g, "").trim() ?? "";
}

export function validate(body) {
  const errors = [];
  const text = body ?? "";

  if (text.split(marker).length !== 2) {
    errors.push(`PR body must contain exactly one ${marker} marker.`);
  }

  const values = Object.fromEntries(headings.map((heading) => [heading, section(text, heading)]));
  for (const [heading, value] of Object.entries(values)) {
    if (!value) errors.push(`${heading} is required.`);
    if (/<[^>]+>|\b(?:TBD|TODO)\b/i.test(value)) {
      errors.push(`${heading} still contains a template placeholder.`);
    }
  }

  if (!/(?:#\d+|https:\/\/\S+|\binline\b)/i.test(values["Issue or spec"])) {
    errors.push("Issue or spec must contain an issue reference, HTTPS URL, or `inline`.");
  }

  const acceptanceRows = [
    ...values["Acceptance criteria"].matchAll(/^- (AC-\d+):\s*(.+)$/gim),
  ];
  const acceptance = new Map(
    acceptanceRows.map((match) => [match[1].toUpperCase(), match[2]]),
  );
  if (acceptance.size !== acceptanceRows.length) {
    errors.push("Acceptance criterion IDs must be unique.");
  }
  if (acceptance.size === 0) errors.push("Acceptance criteria must contain at least one `- AC-N:` row.");

  const evidenceRows = [
    ...values.Evidence.matchAll(/^- (AC-\d+)\s+[—-]\s+(.+)$/gim),
  ];
  const evidence = new Map(
    evidenceRows.map((match) => [match[1].toUpperCase(), match[2]]),
  );
  if (evidence.size !== evidenceRows.length) errors.push("Evidence IDs must be unique.");
  for (const id of acceptance.keys()) {
    const value = evidence.get(id) ?? "";
    if (!/command\/scenario:\s*\S/i.test(value) || !/observed:\s*\S/i.test(value)) {
      errors.push(`${id} needs evidence with command/scenario and observed result.`);
    }
  }
  for (const id of evidence.keys()) {
    if (!acceptance.has(id)) errors.push(`${id} has evidence but no acceptance criterion.`);
  }

  if (!/^- In:\s*\S/im.test(values.Scope) || !/^- Out:\s*\S/im.test(values.Scope)) {
    errors.push("Scope must define both `In` and `Out`.");
  }
  if (!/^- Level:\s*(?:low|medium|high)\s*$/im.test(values["Risk and rollback"])) {
    errors.push("Risk level must be low, medium, or high.");
  }
  for (const field of ["Failure mode", "Rollback/mitigation", "Reviewer focus"]) {
    if (!new RegExp(`^- ${field}:\\s*\\S`, "im").test(values["Risk and rollback"])) {
      errors.push(`Risk and rollback must define ${field}.`);
    }
  }
  const provenance = values["Accountability and agent provenance"];
  const field = (name) =>
    provenance.match(new RegExp(`^- ${escapeRegExp(name)}:\\s*(.+)$`, "im"))?.[1].trim() ??
    "";
  if (!/^@[A-Za-z0-9-]+$/.test(field("Accountable human"))) {
    errors.push("Accountable human must be one GitHub handle.");
  }
  const origin = field("Origin").toLowerCase();
  if (!["human", "assisted", "agent-authored", "authorized-automation"].includes(origin)) {
    errors.push("Origin must be human, assisted, agent-authored, or authorized-automation.");
  }
  for (const name of [
    "Agent identity",
    "Provider/model",
    "Run/trace",
    "Base SHA",
    "Capabilities",
    "Human verification",
  ]) {
    if (!field(name)) errors.push(`Accountability and agent provenance must define ${name}.`);
  }
  if (origin !== "human") {
    for (const name of ["Agent identity", "Provider/model"]) {
      if (/^none$/i.test(field(name))) errors.push(`${name} is required for AI/agent work.`);
    }
  }
  if (["agent-authored", "authorized-automation"].includes(origin)) {
    if (/^none$/i.test(field("Run/trace"))) {
      errors.push("Materially agent-authored work requires an immutable run ID or trace.");
    }
    if (!/^[0-9a-f]{40}$/i.test(field("Base SHA"))) {
      errors.push("Materially agent-authored work requires a 40-character base SHA.");
    }
    const capabilities = field("Capabilities");
    if (
      /^none$/i.test(capabilities) ||
      !["commands", "network", "secrets", "mcp"].every((name) =>
        new RegExp(`\\b${name}=\\S+`, "i").test(capabilities),
      )
    ) {
      errors.push(
        "Materially agent-authored work must declare commands, network, secrets, and MCP capabilities.",
      );
    }
  }

  const attestations = [
    "I am or represent the accountable human named above.",
    "I reviewed every changed line and can explain why it is needed.",
    "I personally verified the behavior and evidence described above.",
    "I have the right to submit this contribution under the project's license.",
    "This is not an unattended agent submission; I remain responsible for the contribution.",
  ];
  for (const attestation of attestations) {
    const checked = new RegExp(
      `^- \\[[xX]\\] ${escapeRegExp(attestation)}\\s*$`,
      "m",
    );
    if (!checked.test(values["Human attestation"])) {
      errors.push(`Check the attestation: ${attestation}`);
    }
  }

  return errors;
}

const validFixture = `${marker}
## Outcome
Users receive a deterministic result.
## Issue or spec
inline
## Acceptance criteria
- AC-1: The command exits successfully.
## Evidence
- AC-1 — command/scenario: \`make verify\`; observed: exit 0
## Scope
- In: verification
- Out: deployment
## Risk and rollback
- Level: low
- Failure mode: validation rejects a valid body
- Rollback/mitigation: revert
- Reviewer focus: parser behavior
## Accountability and agent provenance
- Accountable human: @thekidnamedkd
- Origin: agent-authored
- Agent identity: omp
- Provider/model: OpenAI GPT
- Run/trace: run-123
- Base SHA: 0000000000000000000000000000000000000000
- Capabilities: commands=unprivileged; network=restricted; secrets=none; mcp=none
- Human verification: reviewed the diff and ran the self-test
## Human attestation
- [x] I am or represent the accountable human named above.
- [x] I reviewed every changed line and can explain why it is needed.
- [x] I personally verified the behavior and evidence described above.
- [x] I have the right to submit this contribution under the project's license.
- [x] This is not an unattended agent submission; I remain responsible for the contribution.`;
if (process.argv[1] === fileURLToPath(import.meta.url)) {
  if (process.argv[2] === "--self-test") {
    assert.deepEqual(validate(validFixture), []);
    const unchecked = validFixture.replace("- [x]", "- [ ]\n> - [x]");
    assert(
      validate(unchecked).some((error) => error.startsWith("Check the attestation:")),
    );
    const duplicate = validFixture.replace(
      "- AC-1: The command exits successfully.",
      "- AC-1: The command exits successfully.\n- AC-1: The output is deterministic.",
    );
    assert(validate(duplicate).includes("Acceptance criterion IDs must be unique."));
    console.log("PR contract self-test passed.");
  } else {
    const event = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH, "utf8"));
    const errors = validate(event.pull_request?.body);
    if (errors.length > 0) {
      console.error(errors.map((error) => `- ${error}`).join("\n"));
      process.exitCode = 1;
    }
  }
}
