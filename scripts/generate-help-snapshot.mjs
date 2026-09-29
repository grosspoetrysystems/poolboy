#!/usr/bin/env node
// Generate version-targeted structured CLI help from the canonical catalog.
//
//   node scripts/generate-help-snapshot.mjs snapshot <version> <helpRoot>
//     -> <helpRoot>/v<version>/index.json           (catalog + schema + cli_version)
//        <helpRoot>/v<version>/commands/<name>.json (one per canonical command)
//
//   node scripts/generate-help-snapshot.mjs manifest <helpRoot>
//     -> <helpRoot>/index.json  {schema, latest, minimum_supported, versions}
//        built from the v<version>/ snapshots present under <helpRoot>.
//
//   node scripts/generate-help-snapshot.mjs --self-test
//
// Pure Node stdlib: no dependencies, no network.

import { readFileSync, writeFileSync, mkdirSync, rmSync, readdirSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const SCHEMA = 1;
const REPO_ROOT = join(dirname(fileURLToPath(import.meta.url)), "..");

function loadCatalog(catalogPath) {
  const catalog = JSON.parse(readFileSync(catalogPath, "utf8"));
  if (catalog.schema !== SCHEMA || !Array.isArray(catalog.commands)) {
    throw new Error(`${catalogPath}: expected schema ${SCHEMA} and a "commands" array`);
  }
  return catalog;
}

function commandName(name) {
  if (typeof name !== "string" || name === "" || name === "." || name === ".." || name.includes("/") || name.includes("\\")) {
    throw new Error(`snapshot: invalid command name ${JSON.stringify(name)}`);
  }
  return name;
}

function validateCommands(commands) {
  const names = new Set();
  for (const command of commands) {
    const name = commandName(command?.name);
    if (names.has(name)) throw new Error(`snapshot: duplicate command ${name}`);
    names.add(name);
  }
}

function writeJson(path, value) {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, JSON.stringify(value, null, 2) + "\n");
}

function snapshot(version, helpRoot, catalogPath = join(REPO_ROOT, "data/commands.json")) {
  if (!version) throw new Error("snapshot: version is required");
  const catalog = loadCatalog(catalogPath);
  validateCommands(catalog.commands);
  const versionDir = join(helpRoot, `v${version}`);
  rmSync(versionDir, { recursive: true, force: true });

  writeJson(join(versionDir, "index.json"), { ...catalog, schema: SCHEMA, cli_version: version });

  for (const command of catalog.commands) {
    writeJson(join(versionDir, "commands", `${commandName(command.name)}.json`), { ...command, schema: SCHEMA, cli_version: version });
  }
  return versionDir;
}


// Semver compare (stdlib): numeric release parts, then prerelease (absent > present).
function parseVersion(v) {
  if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(v)) {
    throw new Error(`unsupported version: ${v}`);
  }
  const [core, pre = ""] = v.split("-", 2);
  const release = core.split(".").map(Number);
  return { release, pre, stable: pre === "" };
}

function compareVersions(a, b) {
  const pa = parseVersion(a);
  const pb = parseVersion(b);
  for (let i = 0; i < 3; i++) {
    if (pa.release[i] !== pb.release[i]) return pa.release[i] - pb.release[i];
  }
  if (pa.pre === pb.pre) return 0;
  if (pa.pre === "") return 1; // release > prerelease
  if (pb.pre === "") return -1;
  return pa.pre < pb.pre ? -1 : 1;
}

function readJson(path, context) {
  try {
    return JSON.parse(readFileSync(path, "utf8"));
  } catch (err) {
    throw new Error(`manifest: ${context} is missing or malformed: ${err.message}`);
  }
}


function validateSnapshot(helpRoot, version) {
  const versionDir = join(helpRoot, `v${version}`);
  const indexPath = join(versionDir, "index.json");
  const doc = readJson(indexPath, indexPath);
  if (doc.schema !== SCHEMA || doc.cli_version !== version || !Array.isArray(doc.commands)) {
    throw new Error(`manifest: ${indexPath} has invalid schema, cli_version, or commands`);
  }

  const names = new Set();
  for (const command of doc.commands) {
    const name = commandName(command?.name);
    if (names.has(name)) throw new Error(`manifest: ${indexPath} repeats command ${name}`);
    names.add(name);

    const commandPath = join(versionDir, "commands", `${name}.json`);
    const commandDoc = readJson(commandPath, commandPath);
    if (commandDoc.schema !== SCHEMA || commandDoc.cli_version !== version || commandDoc.name !== name) {
      throw new Error(`manifest: ${commandPath} has invalid schema, cli_version, or name`);
    }
  }
  return doc;
}

function manifest(helpRoot) {
  const entries = readdirSync(helpRoot, { withFileTypes: true })
    .filter((e) => e.isDirectory() && /^v.+/.test(e.name))
    .map((e) => e.name.slice(1));
  if (entries.length === 0) {
    throw new Error(`manifest: no v<version> snapshots under ${helpRoot}`);
  }

  const stable = [];
  for (const version of entries) {
    const parsed = parseVersion(version);
    validateSnapshot(helpRoot, version);
    if (parsed.stable) stable.push(version);
  }
  // Deploy only feeds stable releases; drafts/prereleases never set minimum_supported.
  if (stable.length === 0) {
    throw new Error(`manifest: no stable snapshots under ${helpRoot}`);
  }
  const sorted = [...stable].sort(compareVersions);
  const value = {
    schema: SCHEMA,
    latest: sorted[sorted.length - 1],
    minimum_supported: sorted[0],
    versions: [...sorted].reverse(),
  };
  writeJson(join(helpRoot, "index.json"), value);
  return value;
}

function selfTest() {
  const assert = (cond, msg) => {
    if (!cond) throw new Error(`self-test: ${msg}`);
  };
  const tmp = join(REPO_ROOT, ".help-selftest");
  rmSync(tmp, { recursive: true, force: true });
  const catalog = join(tmp, "commands.json");
  mkdirSync(tmp, { recursive: true });
  writeFileSync(catalog, JSON.stringify({
    schema: 1,
    title: "T",
    commands: [
      { name: "list", summary: "s", mutates: false, aliases: "ls", source: "x", flags: [] },
      { name: "move", summary: "s", mutates: true, aliases: "mv", source: "x", flags: [] },
    ],
  }));

  const root = join(tmp, "help");
  for (const v of ["1.2.3", "1.2.4", "1.3.0-rc.1"]) snapshot(v, root, catalog);

  const idx = JSON.parse(readFileSync(join(root, "v1.2.3", "index.json"), "utf8"));
  assert(idx.schema === 1, "index schema must be 1");
  assert(idx.cli_version === "1.2.3", "index cli_version");
  assert(Array.isArray(idx.commands) && idx.commands.length === 2, "index commands passthrough");

  const cmd = JSON.parse(readFileSync(join(root, "v1.2.3", "commands", "list.json"), "utf8"));
  assert(cmd.schema === 1 && cmd.cli_version === "1.2.3" && cmd.aliases === "ls", "command doc fields");

  const m = manifest(root);
  assert(m.schema === 1, "manifest schema");
  assert(m.latest === "1.2.4", `latest should be 1.2.4, got ${m.latest}`);
  assert(m.minimum_supported === "1.2.3", `min should be 1.2.3, got ${m.minimum_supported}`);
  assert(JSON.stringify(m.versions) === JSON.stringify(["1.2.4", "1.2.3"]), `versions excludes prerelease, got ${m.versions}`);

  // Malformed retained artifact must fail the manifest.
  writeFileSync(join(root, "v1.2.3", "index.json"), "{ not json");
  let threw = false;
  try {
    manifest(root);
  } catch {
    threw = true;
  }
  assert(threw, "malformed snapshot must fail manifest");

  rmSync(tmp, { recursive: true, force: true });
  console.log("generate-help-snapshot self-test passed");
}

function main(argv) {
  const [cmd, ...rest] = argv;
  switch (cmd) {
    case "snapshot": {
      const [version, helpRoot] = rest;
      if (!helpRoot) throw new Error("usage: snapshot <version> <helpRoot>");
      console.log(`wrote ${snapshot(version, helpRoot)}`);
      return;
    }
    case "manifest": {
      const [helpRoot] = rest;
      if (!helpRoot) throw new Error("usage: manifest <helpRoot>");
      const m = manifest(helpRoot);
      console.log(`wrote ${join(helpRoot, "index.json")} (latest ${m.latest}, min ${m.minimum_supported}, ${m.versions.length} versions)`);
      return;
    }
    case "--self-test":
      selfTest();
      return;
    default:
      throw new Error("usage: generate-help-snapshot.mjs snapshot|manifest|--self-test ...");
  }
}

try {
  main(process.argv.slice(2));
} catch (err) {
  console.error(err.message);
  process.exit(1);
}
