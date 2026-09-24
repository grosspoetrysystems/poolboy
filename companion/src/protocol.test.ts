import { describe, expect, it } from "vitest";

import {
  exitCode,
  MAX_REQUEST_BYTES,
  parseRequest,
  responseFromResult,
} from "./protocol";
import { render } from "./renderer";

const validRequest = JSON.stringify({
  template: "# {{ title }}\n",
  variables: { title: "Poolboy" },
});

describe("renderer protocol", () => {
  it("rejects malformed JSON with the protocol exit code", () => {
    const parsed = parseRequest("{");

    expect(parsed.ok).toBe(false);
    if (!parsed.ok) {
      expect(parsed.response.code).toBe("INVALID_JSON");
      expect(exitCode(parsed.response)).toBe(2);
    }
  });

  it("rejects unknown fields and non-object variables", () => {
    const extra = parseRequest(
      JSON.stringify({ resolver: "not allowed", template: "", variables: {} })
    );
    const primitive = parseRequest(
      JSON.stringify({ template: "", variables: [] })
    );

    expect(extra.ok).toBe(false);
    expect(primitive.ok).toBe(false);
    if (!(extra.ok || primitive.ok)) {
      expect(extra.response.code).toBe("INVALID_REQUEST");
      expect(primitive.response.code).toBe("INVALID_REQUEST");
    }
  });

  it("renders an accepted request with the success exit code", async () => {
    const parsed = parseRequest(validRequest);
    expect(parsed.ok).toBe(true);
    if (!parsed.ok) {
      throw new Error("valid request rejected");
    }
    const response = responseFromResult(
      await render(parsed.request.template, parsed.request.variables)
    );
    expect(response).toMatchObject({ ok: true, output: "# Poolboy\n" });
    expect(exitCode(response)).toBe(0);
  });

  it("rejects requests over the byte limit", () => {
    const oversized = JSON.stringify({
      template: "x".repeat(MAX_REQUEST_BYTES),
      variables: {},
    });
    const parsed = parseRequest(oversized);

    expect(parsed.ok).toBe(false);
    if (!parsed.ok) {
      expect(parsed.response.code).toBe("INVALID_REQUEST");
      expect(parsed.response.message).toContain("bytes");
    }
  });

  it("discards real partial output after a template failure", async () => {
    const result = await render("partial output {{ value | date }}", {
      value: "2026-09-24",
    });
    const response = responseFromResult(result);
    expect(response).toMatchObject({
      errors: expect.arrayContaining([
        expect.objectContaining({ code: "UNKNOWN_FILTER" }),
      ]),
      kind: "template",
      ok: false,
    });
    expect(JSON.stringify(response)).not.toContain("partial output");
    expect(exitCode(response)).toBe(1);
  });

  it("rejects malformed UTF-8 rather than rendering replacement characters", () => {
    const parsed = parseRequest(new Uint8Array([0xff]));
    expect(parsed).toMatchObject({
      ok: false,
      response: { code: "INVALID_JSON", kind: "protocol" },
    });
  });

  it("rejects non-string templates before invoking the engine", () => {
    expect(
      parseRequest(JSON.stringify({ template: ["not", "text"], variables: {} }))
    ).toMatchObject({
      ok: false,
      response: { code: "INVALID_REQUEST", kind: "protocol" },
    });
  });
});
