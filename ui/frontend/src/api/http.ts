export type PaginatedResult<T> = {
  data: T;
  total?: number;
};

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
    const text = await response.text();
    throw new Error(`${init?.method ?? "GET"} ${path} failed with ${response.status}: ${text || response.statusText}`);
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
    const text = await response.text();
    throw new Error(`GET ${path} failed with ${response.status}: ${text || response.statusText}`);
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
