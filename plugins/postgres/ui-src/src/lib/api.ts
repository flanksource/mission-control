// API client for the postgres plugin's iframe.
//
// The iframe is served from `/api/plugins/postgres/ui/` (the host's
// reverse proxy). The plugin's operations live one level up at
// `/api/plugins/postgres/operations/<name>` — which is NOT under the iframe
// origin (`./`). To reach them we go up two segments.
//
// `config_id` is the catalog item the user is viewing. The host proxy reads
// it from the iframe URL and the
// operations endpoint accepts it as a query param.

export const PLUGIN_NAME = "postgres";

function operationURL(op: string, configID: string): string {
  // The iframe origin path is /api/plugins/postgres/ui/ — strip /ui/ and
  // append /operations/<op> to reach the host's operations endpoint.
  // We construct the URL relative to window.location to honour the
  // current host:port (works in dev with the vite proxy and in prod
  // under the iframe).
  const base = window.location.pathname.replace(/\/ui\/.*$/, "");
  const url = new URL(base + "/operations/" + op, window.location.origin);
  if (configID) url.searchParams.set("config_id", configID);
  return url.toString();
}

// OpError carries the parsed error body alongside the message so the UI's
// ErrorDetails component (via normalizeErrorDiagnostics) can lift trace IDs,
// stack traces, and oops context out of structured error responses.
export class OpError extends Error {
  readonly status: number;
  readonly operation: string;
  readonly body: unknown;

  constructor(operation: string, status: number, message: string, body: unknown) {
    super(message);
    this.name = "OpError";
    this.operation = operation;
    this.status = status;
    this.body = body;
  }
}

export async function callOp<T = unknown>(
  op: string,
  configID: string,
  params: Record<string, unknown> = {},
): Promise<T> {
  const res = await fetch(operationURL(op, configID), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "same-origin",
    body: JSON.stringify(params),
  });
  if (!res.ok) {
    const text = await res.text();
    let body: unknown = text;
    let message = text || res.statusText;
    try {
      const parsed = JSON.parse(text);
      body = parsed;
      if (parsed && typeof parsed === "object") {
        const record = parsed as Record<string, unknown>;
        const candidate = record.message ?? record.error ?? record.msg;
        if (typeof candidate === "string" && candidate) {
          message = candidate;
        }
      }
    } catch {
      // body is plain text — already captured above
    }
    throw new OpError(op, res.status, `${op} ${res.status}: ${message}`, body);
  }
  // The plugin SDK returns application/clicky+json — the payload is the
  // operation's `any` return wrapped in the clicky envelope. We parse as
  // JSON; pages pull out the data field they need.
  return (await res.json()) as T;
}

export function configIDFromURL(): string {
  return new URLSearchParams(window.location.search).get("config_id") ?? "";
}
