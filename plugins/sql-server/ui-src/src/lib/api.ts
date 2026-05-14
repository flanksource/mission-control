// API client for the sql-server plugin's iframe.
//
// The iframe is served from `/api/plugins/sql-server/ui/` (the host's
// reverse proxy). Unary operations are exposed by the host at
// `/api/plugins/sql-server/invoke/<name>` and HTTP/SSE operations at
// `/api/plugins/sql-server/proxy/<name>`.
//
// `config_id` is the catalog item the user is viewing. The host reads it from
// the query string for selector checks and connection resolution.

export const PLUGIN_NAME = "sql-server";

function pluginBase(): string {
  return window.location.pathname.replace(/\/ui(?:\/.*)?$/, "");
}

function operationURL(op: string, configID: string): string {
  const url = new URL(pluginBase() + "/invoke/" + op, window.location.origin);
  if (configID) url.searchParams.set("config_id", configID);
  return url.toString();
}

function traceStreamURL(traceID: string, configID: string, since?: string): string {
  const url = new URL(pluginBase() + "/proxy/trace-stream", window.location.origin);
  url.searchParams.set("id", traceID);
  if (configID) url.searchParams.set("config_id", configID);
  if (since) url.searchParams.set("since", since);
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

export function openTraceStream(
  traceID: string,
  configID: string,
  onEvent: (e: unknown) => void,
  onDone?: () => void,
  since?: string,
): EventSource {
  const es = new EventSource(traceStreamURL(traceID, configID, since), {
    withCredentials: true,
  });
  es.onmessage = (ev) => {
    try {
      onEvent(JSON.parse(ev.data));
    } catch {
      // Skip malformed frames silently — the server only emits JSON.
    }
  };
  es.addEventListener("done", () => {
    onDone?.();
    es.close();
  });
  return es;
}

export function configIDFromURL(): string {
  return new URLSearchParams(window.location.search).get("config_id") ?? "";
}
