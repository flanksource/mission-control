export const PLUGIN_NAME = "arthas";

function operationURL(op: string, configID: string): string {
  const base = window.location.pathname.replace(/\/ui\/.*$/, "");
  const url = new URL(base + "/operations/" + op, window.location.origin);
  if (configID) url.searchParams.set("config_id", configID);
  return url.toString();
}

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
  params: Record<string, unknown> = {},
): Promise<T> {
  const configID = configIDFromURL();
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
        const candidate = (parsed as Record<string, unknown>).message ?? (parsed as Record<string, unknown>).error;
        if (typeof candidate === "string" && candidate) message = candidate;
      }
    } catch {
      // plain text
    }
    throw new OpError(op, res.status, `${op} ${res.status}: ${message}`, body);
  }
  return (await res.json()) as T;
}

export function configIDFromURL(): string {
  return new URLSearchParams(window.location.search).get("config_id") ?? "";
}

export function pluginURL(path: string): string {
  return new URL(path.replace(/^\//, ""), window.location.href).toString();
}
