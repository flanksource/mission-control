import type {
  ExecutionResponse,
  OpenAPISpec,
  OperationLookupResponse,
  OperationsApiClient,
} from "@flanksource/clicky-ui";

function substitutePath(
  path: string,
  params: Record<string, string>,
): { resolved: string; remaining: Record<string, string> } {
  const remaining = { ...params };
  let resolved = path;
  for (const [key, value] of Object.entries(params)) {
    if (resolved.includes(`{${key}}`)) {
      resolved = resolved.replace(`{${key}}`, encodeURIComponent(value));
      delete remaining[key];
      delete remaining.args;
    }
  }
  return { resolved, remaining };
}

async function request(
  path: string,
  method: string,
  body?: unknown,
  headers?: Record<string, string>,
  requestUrl?: string,
): Promise<ExecutionResponse> {
  const upper = method.toUpperCase();
  const init: RequestInit = {
    method: upper,
    headers: { Accept: "application/json+clicky", ...(headers ?? {}) },
  };
  if (upper !== "GET" && body !== undefined) {
    init.headers = { "Content-Type": "application/json", ...init.headers };
    init.body = JSON.stringify(body);
  }
  const response = await fetch(path, init);
  const text = await response.text();
  const contentType = response.headers.get("Content-Type") || undefined;
  if (!response.ok) {
    throw new Error(
      `${upper} ${path} failed with ${response.status}: ${text || response.statusText}`,
    );
  }
  return {
    success: true,
    exit_code: 0,
    stdout: text,
    contentType,
    requestUrl,
    parsed: maybeParseJson(text, contentType),
  };
}

function maybeParseJson(text: string, contentType?: string) {
  const trimmed = text.trim();
  if (!trimmed) return undefined;

  const looksJson =
    contentType?.toLowerCase().includes("json") ||
    trimmed.startsWith("{") ||
    trimmed.startsWith("[");
  if (!looksJson) return undefined;

  try {
    return JSON.parse(trimmed) as unknown;
  } catch {
    return undefined;
  }
}

export const apiClient: OperationsApiClient = {
  async getOpenAPISpec(): Promise<OpenAPISpec> {
    const response = await fetch("/ui/openapi.json", {
      headers: { Accept: "application/json" },
    });
    if (!response.ok) {
      throw new Error(
        `GET /ui/openapi.json failed with ${response.status}: ${response.statusText}`,
      );
    }
    return (await response.json()) as OpenAPISpec;
  },

  async executeCommand(path, method, params, headers) {
    const { resolved, remaining } = substitutePath(path, params);
    if (method.toUpperCase() === "GET") {
      const search = new URLSearchParams(remaining).toString();
      const url = search ? `${resolved}?${search}` : resolved;
      return request(url, method, undefined, headers, url);
    }
    return request(resolved, method, remaining, headers);
  },

  async lookupFilters(
    path,
    _method,
    params,
    headers,
  ): Promise<OperationLookupResponse> {
    const { resolved, remaining } = substitutePath(path, params);
    const searchParams = new URLSearchParams(remaining);
    searchParams.set("__lookup", "filters");
    const url = `${resolved}?${searchParams.toString()}`;
    const response = await fetch(url, {
      method: "GET",
      headers: { Accept: "application/json+clicky", ...(headers ?? {}) },
    });
    if (!response.ok) {
      throw new Error(
        `GET ${url} failed with ${response.status}: ${await response.text() || response.statusText}`,
      );
    }
    return (await response.json()) as OperationLookupResponse;
  },
};

export type CatalogSummaryEntry = {
  type: string;
  count: number;
  health?: Record<string, number>;
  analysis?: Record<string, number> | null;
};

export async function fetchCatalogSummary(): Promise<CatalogSummaryEntry[]> {
  const response = await fetch("/catalog/summary", {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify({}),
  });
  if (!response.ok) {
    throw new Error(
      `POST /catalog/summary failed with ${response.status}: ${response.statusText}`,
    );
  }
  const data = (await response.json()) as CatalogSummaryEntry[] | null;
  return data ?? [];
}
