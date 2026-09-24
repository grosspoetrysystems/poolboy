import { stdin, stdout } from "node:process";
import {
  exitCode,
  internalFailure,
  MAX_REQUEST_BYTES,
  type ProtocolFailure,
  parseRequest,
  protocolFailure,
  type Response,
  responseFromResult,
} from "../protocol";
import { render } from "../renderer";

type ReadResult =
  | { ok: true; input: Uint8Array }
  | { ok: false; response: ProtocolFailure };

async function readStdin(): Promise<ReadResult> {
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of stdin) {
    const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    if (size + bytes.length > MAX_REQUEST_BYTES) {
      return {
        ok: false,
        response: protocolFailure(
          "INVALID_REQUEST",
          `request exceeds ${MAX_REQUEST_BYTES} bytes`
        ),
      };
    }
    size += bytes.length;
    chunks.push(bytes);
  }
  return { input: Buffer.concat(chunks, size), ok: true };
}

function writeResponse(response: Response): void {
  stdout.write(`${JSON.stringify(response)}\n`);
  process.exitCode = exitCode(response);
}

async function main(): Promise<void> {
  let input: ReadResult;
  try {
    input = await readStdin();
  } catch {
    writeResponse(internalFailure("unable to read request"));
    return;
  }

  if (!input.ok) {
    writeResponse(input.response);
    return;
  }

  const parsed = parseRequest(input.input);
  if (!parsed.ok) {
    writeResponse(parsed.response);
    return;
  }

  try {
    const result = await render(
      parsed.request.template,
      parsed.request.variables
    );
    writeResponse(responseFromResult(result));
  } catch {
    writeResponse(internalFailure());
  }
}

await main().catch(() => {
  writeResponse(internalFailure());
});
