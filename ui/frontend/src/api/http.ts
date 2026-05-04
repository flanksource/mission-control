import { normalizeErrorDiagnostics, type ErrorDiagnostics } from "@flanksource/clicky-ui";

export type PaginatedResult<T> = {
  data: T;
  total?: number;
};

// ApiError preserves the structured oops payload returned by the backend
// (`{error, code, time, trace, context, stacktrace, ...}`) alongside the
// plain message, so call sites can render `<ErrorDetails>` from any caught
// error without re-parsing the response body.
export class ApiError extends Error {
  readonly status: number;
  readonly diagnostics: ErrorDiagnostics;
  readonly body?: unknown;

  constructor(message: string, status: number, diagnostics: ErrorDiagnostics, body?: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.diagnostics = diagnostics;
    this.body = body;
  }
}

export function errorDiagnosticsFromUnknown(error: unknown): ErrorDiagnostics | null {
  if (!error) return null;
  if (error instanceof ApiError) return error.diagnostics;
  if (error instanceof Error) {
    return normalizeErrorDiagnostics(error.message) ?? { message: error.message, context: [] };
  }
  return normalizeErrorDiagnostics(error);
}

async function failResponse(method: string, path: string, response: Response): Promise<never> {
  const fallback = `${method} ${path} failed with ${response.status}: ${response.statusText || "request failed"}`;
  let body: unknown;
  let diagnostics: ErrorDiagnostics | null = null;
  try {
    body = await response.clone().json();
    diagnostics = normalizeErrorDiagnostics(body, fallback);
  } catch {
    const text = await response.text().catch(() => "");
    body = text;
    diagnostics = text.trim() ? { message: text.trim(), context: [] } : null;
  }
  const finalDiagnostics: ErrorDiagnostics = diagnostics ?? { message: fallback, context: [] };
  throw new ApiError(finalDiagnostics.message, response.status, finalDiagnostics, body);
}

export async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      Accept: "application/json",
      Prefer: "return=representation",
      ...(init?.headers ?? {}),
    },
  });

  if (!response.ok) {
    await failResponse(init?.method ?? "GET", path, response);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return (await response.json()) as T;
}

export async function fetchPostgrest<T>(path: string): Promise<PaginatedResult<T>> {
  const response = await fetch(path, {
    headers: {
      Accept: "application/json",
      Prefer: "count=exact,return=representation",
    },
  });

  if (!response.ok) {
    await failResponse("GET", path, response);
  }

  return {
    data: (await response.json()) as T,
    total: parseContentRangeTotal(response.headers.get("Content-Range")),
  };
}

export async function fetchAllPostgrest<T>(path: string, pageSize = 500): Promise<PaginatedResult<T[]>> {
  const rows: T[] = [];
  let offset = 0;
  let total: number | undefined;

  while (true) {
    const page = await fetchPostgrest<T[]>(withPostgrestPage(path, pageSize, offset));
    const pageRows = page.data ?? [];
    rows.push(...pageRows);
    total = page.total ?? total;

    if (pageRows.length === 0) break;
    offset += pageRows.length;
    if (total !== undefined && rows.length >= total) break;
    if (pageRows.length < pageSize) break;
  }

  return { data: rows, total: total ?? rows.length };
}

function withPostgrestPage(path: string, limit: number, offset: number) {
  const url = new URL(path, "http://incident-commander.local");
  url.searchParams.set("limit", String(limit));
  url.searchParams.set("offset", String(offset));
  return `${url.pathname}${url.search}`;
}

function parseContentRangeTotal(value: string | null): number | undefined {
  if (!value) return undefined;
  const total = value.split("/")[1];
  if (!total || total === "*") return undefined;
  const parsed = Number(total);
  return Number.isFinite(parsed) ? parsed : undefined;
}
