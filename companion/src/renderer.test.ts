import { describe, expect, it } from "vitest";

import { RENDER_LIMITS, render } from "./renderer";

describe("renderer", () => {
  it("renders deterministic Markdown from the pinned filter set", async () => {
    const result = await render("# {{ title | trim | upper }}\n", {
      title: "  Poolboy  ",
    });

    expect(result.errors).toEqual([]);
    expect(result.output).toBe("# POOLBOY\n");
  });

  it("rejects filters outside the allowlist", async () => {
    const result = await render("{{ title | date }}", { title: "2026-09-24" });

    expect(result.errors).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ code: "UNKNOWN_FILTER" }),
      ])
    );
  });

  it("reports a template limit error", async () => {
    const result = await render(
      "x".repeat(RENDER_LIMITS.maxTemplateLength + 1),
      {}
    );

    expect(result.errors).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ code: "LIMIT_EXCEEDED" }),
      ])
    );
  });
});
