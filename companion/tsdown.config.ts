import { defineConfig } from "tsdown";

export default defineConfig({
  banner: { js: "#!/usr/bin/env node" },
  clean: true,
  deps: {
    alwaysBundle: ["knap"],
    onlyBundle: ["knap", "dayjs"],
    onlyImport: [],
  },
  dts: false,
  entry: { cli: "src/bin/cli.ts" },
  format: "esm",
  outDir: "dist",
  // Go invokes `node companion/dist/cli.mjs`; force the .mjs entry filename.
  outputOptions: { entryFileNames: "[name].mjs" },
  sourcemap: true,
  target: "node22",
});
