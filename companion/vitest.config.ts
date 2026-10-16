import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    coverage: {
      // The stdin/stdout CLI is exercised at the process boundary, not here.
      exclude: ["src/bin/**"],
      include: ["src/**/*.ts"],
      provider: "v8",
      thresholds: {
        branches: 90,
        functions: 90,
        lines: 90,
        statements: 90,
      },
    },
  },
});
