import type { KnipConfig } from "knip";

const config: KnipConfig = {
  entry: ["src/bin/cli.ts"],
  project: ["src/**/*.ts"],
};

export default config;
