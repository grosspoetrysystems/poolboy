import { readFileSync } from "node:fs";

const catalog = JSON.parse(readFileSync("data/commands.json", "utf8"));
const errors = [];
const commands = new Map(catalog.commands.map((command) => [command.name, command]));
const aliases = new Map();

for (const command of catalog.commands) {
  for (const field of ["name", "summary", "usage", "source", "effects", "does_not"]) {
    if (typeof command[field] !== "string" || command[field].trim() === "") {
      errors.push(`${command.name ?? "<unnamed>"}: missing ${field}`);
    }
  }
  if (typeof command.aliases !== "string") errors.push(`${command.name}: aliases must be a string`);
  if (typeof command.mutates !== "boolean") errors.push(`${command.name}: mutates must be boolean`);
  for (const field of ["flags", "verify_with", "related"]) {
    if (!Array.isArray(command[field])) errors.push(`${command.name}: ${field} must be an array`);
  }
  for (const flag of command.flags ?? []) {
    if (typeof flag.name !== "string" || !flag.name || typeof flag.description !== "string" || !flag.description) {
      errors.push(`${command.name}: every flag needs a name and description`);
    }
  }
  for (const alias of splitAliases(command.aliases)) {
    if (commands.has(alias) || aliases.has(alias)) errors.push(`${command.name}: duplicate alias ${alias}`);
    aliases.set(alias, command.name);
  }
}

if (catalog.schema !== 1) errors.push("catalog schema must be 1");


for (const command of catalog.commands) {
  for (const related of command.related ?? []) {
    if (!commands.has(related) && !aliases.has(related)) errors.push(`${command.name}: unknown related command ${related}`);
  }
  for (const argv of command.verify_with ?? []) {
    if (!Array.isArray(argv) || argv.length === 0) {
      errors.push(`${command.name}: verify_with entries must be nonempty argv arrays`);
      continue;
    }
    const invoked = argv[0] === "poolboy" ? argv[1] : argv[0];
    if (!commands.has(invoked) && !aliases.has(invoked)) {
      errors.push(`${command.name}: verify_with invokes unknown command ${invoked}`);
    }
  }

  if (command.name === "version" || command.name === "help") continue;
  const handler = `cmd${command.name.split("-").map((part) => part[0].toUpperCase() + part.slice(1)).join("")}`;
  let source;
  try {
    source = readFileSync(command.source, "utf8");
  } catch {
    errors.push(`${command.name}: source does not exist: ${command.source}`);
    continue;
  }
  const body = functionBody(source, handler);
  if (body === null) {
    errors.push(`${command.name}: ${handler} not found in ${command.source}`);
    continue;
  }
  const actual = declaredFlags(body);
  if (body.includes("countFlags(")) for (const flag of ["format", "counts", "sort", "prefix", "where"]) actual.add(flag);
  if (body.includes("addTrustFlags(")) for (const flag of ["lock", "tofu", "identity", "channel"]) actual.add(flag);
  const documented = new Set(
    command.flags
      .map((flag) => flag.name.match(/^--([A-Za-z0-9-]+)/)?.[1])
      .filter(Boolean),
  );
  for (const flag of actual) if (!documented.has(flag)) errors.push(`${command.name}: undocumented --${flag}`);
  for (const flag of documented) if (!actual.has(flag)) errors.push(`${command.name}: documented --${flag} is not declared`);
}

if (errors.length > 0) {
  console.error(errors.map((error) => `command docs: ${error}`).join("\n"));
  process.exit(1);
}
console.log(`Command documentation covers ${commands.size} commands and ${commands.size + aliases.size} command names.`);

function splitAliases(value) {
  if (typeof value !== "string" || value === "none") return [];
  return value.split(",").map((alias) => alias.trim()).filter(Boolean);
}

function functionBody(source, name) {
  const start = source.search(new RegExp(`func\\s+${name}\\s*\\(`));
  if (start < 0) return null;
  const open = source.indexOf("{", start);
  if (open < 0) return null;
  let depth = 0;
  for (let index = open; index < source.length; index += 1) {
    if (source[index] === "{") depth += 1;
    if (source[index] === "}" && --depth === 0) return source.slice(open + 1, index);
  }
  return null;
}

function declaredFlags(body) {
  const result = new Set();
  for (const match of body.matchAll(/fs\.(?:String|Bool|Int|Duration)\(\s*"([^"]+)"/g)) result.add(match[1]);
  for (const match of body.matchAll(/fs\.Var\([^,]+,\s*"([^"]+)"/g)) result.add(match[1]);
  return result;
}
