#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { chmodSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

const packageDir = resolve(process.argv[2] ?? ".release/npm");
const manifest = JSON.parse(readFileSync(`${packageDir}/manifest.json`, "utf8"));

for (const entry of manifest) {
  const spec = `${entry.name}@${entry.version}`;
  const existing = spawnSync("npm", ["view", spec, "version"], { stdio: "ignore" });
  if (existing.status === 0) {
    console.log(`${spec} already published; skipping`);
    continue;
  }

  const source = `${packageDir}/${entry.directory}`;
  if (entry.binary) chmodSync(`${source}/${entry.binary}`, 0o755);
  const publishArgs = ["publish", source, "--provenance", "--access", "public", "--ignore-scripts"];
  publishArgs.push("--tag", entry.version.includes("-") ? "next" : "latest");

  console.log(`Publishing ${spec}`);
  const published = spawnSync("npm", publishArgs, { stdio: "inherit" });
  if (published.status !== 0) process.exit(published.status ?? 1);
}
