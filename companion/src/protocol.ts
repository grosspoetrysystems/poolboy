import type { TemplateResult, TemplateVariables } from "knap";

export const MAX_REQUEST_BYTES = 2 * 1024 * 1024;

interface RenderRequest {
  template: string;
  variables: TemplateVariables;
}

type ProtocolErrorCode = "INVALID_JSON" | "INVALID_REQUEST" | "INTERNAL";

export interface ProtocolFailure {
  code: ProtocolErrorCode;
  kind: "protocol";
  message: string;
  ok: false;
}

interface TemplateFailure {
  errors: TemplateResult["errors"];
  kind: "template";
  ok: false;
  warnings: TemplateResult["warnings"];
}

interface RenderSuccess {
  ok: true;
  output: string;
  warnings: TemplateResult["warnings"];
}

type RenderResponse = RenderSuccess | TemplateFailure;
export type Response = RenderResponse | ProtocolFailure;

type ParsedRequest =
  | { ok: true; request: RenderRequest }
  | { ok: false; response: ProtocolFailure };

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function protocolFailure(
  code: ProtocolErrorCode,
  message: string
): ProtocolFailure {
  return { code, kind: "protocol", message, ok: false };
}

function invalidRequest(message: string): ParsedRequest {
  return { ok: false, response: protocolFailure("INVALID_REQUEST", message) };
}

export function parseRequest(input: string | Uint8Array): ParsedRequest {
  const byteLength =
    typeof input === "string"
      ? Buffer.byteLength(input, "utf8")
      : input.byteLength;
  if (byteLength > MAX_REQUEST_BYTES) {
    return invalidRequest(`request exceeds ${MAX_REQUEST_BYTES} bytes`);
  }

  let source: string;
  try {
    source =
      typeof input === "string"
        ? input
        : new TextDecoder("utf-8", { fatal: true }).decode(input);
  } catch {
    return {
      ok: false,
      response: protocolFailure("INVALID_JSON", "request is not valid UTF-8"),
    };
  }

  let value: unknown;
  try {
    value = JSON.parse(source);
  } catch {
    return {
      ok: false,
      response: protocolFailure("INVALID_JSON", "request is not valid JSON"),
    };
  }

  if (!isRecord(value)) {
    return invalidRequest("request must be a JSON object");
  }

  const keys = Object.keys(value);
  if (
    keys.length !== 2 ||
    !Object.hasOwn(value, "template") ||
    !Object.hasOwn(value, "variables")
  ) {
    return invalidRequest("request must contain only template and variables");
  }

  if (typeof value.template !== "string") {
    return invalidRequest("template must be a string");
  }
  if (!isRecord(value.variables)) {
    return invalidRequest("variables must be a JSON object");
  }

  return {
    ok: true,
    request: {
      template: value.template,
      variables: value.variables,
    },
  };
}

export function responseFromResult(result: TemplateResult): RenderResponse {
  if (result.errors.length > 0) {
    return {
      errors: result.errors,
      kind: "template",
      ok: false,
      warnings: result.warnings,
    };
  }
  return {
    ok: true,
    output: result.output,
    warnings: result.warnings,
  };
}

export function internalFailure(message = "renderer failed"): ProtocolFailure {
  return protocolFailure("INTERNAL", message);
}

export function exitCode(response: Response): 0 | 1 | 2 {
  if (response.ok) {
    return 0;
  }
  return response.kind === "template" ? 1 : 2;
}
