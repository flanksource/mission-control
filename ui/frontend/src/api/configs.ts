import { fetchAllPostgrest, fetchJSON, fetchPostgrest } from "./http";
import type {
  CatalogReportPreviewResponse,
  CatalogReportRequest,
  ConfigAccessLog,
  ConfigAccessSummary,
  ConfigAnalysis,
  ConfigChange,
  ConfigChildItem,
  ConfigItem,
  ConfigRelationshipsResponse,
  ExternalGroup,
} from "./types";

export type ResourceSelector = {
  agent?: string;
  cache?: string;
  includeDeleted?: boolean;
  limit?: number;
  scope?: string;
  id?: string;
  name?: string;
  namespace?: string;
  tagSelector?: string;
  types?: string[];
  statuses?: string[];
  labelSelector?: string;
  fieldSelector?: string;
  search?: string;
  [key: string]: unknown;
};

type SelectedResource = {
  id: string;
  agent?: string;
  icon?: string;
  name: string;
  namespace?: string;
  type?: string;
  tags?: Record<string, string>;
  health?: string;
  status?: string;
};

type SearchResourcesResponse = {
  configs?: SelectedResource[];
};

export type CatalogReportErrorBody = {
  context?: Record<string, unknown>;
  error?: string;
  stacktrace?: string;
  time?: string;
  trace?: string;
};

export type CatalogReportProgress = {
  stage: "rendering" | "downloading";
  loaded?: number;
  total?: number;
};

export class CatalogReportError extends Error {
  status: number;
  statusText: string;
  body?: CatalogReportErrorBody;
  rawBody: string;

  constructor(status: number, statusText: string, rawBody: string, body?: CatalogReportErrorBody) {
    super(body?.error || rawBody || statusText || "Catalog report failed");
    this.name = "CatalogReportError";
    this.status = status;
    this.statusText = statusText;
    this.body = body;
    this.rawBody = rawBody;
  }
}

export async function getConfig(id: string): Promise<ConfigItem | null> {
  const result = await fetchPostgrest<ConfigItem[]>(
    `/db/config_detail?id=eq.${encodeURIComponent(id)}&select=*`,
  );
  return result.data[0] ?? null;
}

export async function searchConfigItems(query: string, type?: string, limit = 12): Promise<ConfigItem[]> {
  const params = new URLSearchParams({
    select: "id,name,type,config_class,status,health,path,external_id,updated_at,deleted_at",
    order: "updated_at.desc.nullslast,name.asc",
    limit: String(limit),
  });
  const trimmed = query.trim();
  if (type?.trim()) {
    params.set("type", `ilike.${escapePostgrestLike(type.trim())}`);
  }
  if (trimmed) {
    const filter = orFilter([
      `name.ilike.${ilike(trimmed)}`,
      `type.ilike.${ilike(trimmed)}`,
      `path.ilike.${ilike(trimmed)}`,
      exactUUIDFilter(trimmed),
    ]);
    if (filter) params.set("or", filter);
  }

  const result = await fetchPostgrest<ConfigItem[]>(`/db/config_detail?${params.toString()}`);
  return result.data ?? [];
}

export async function searchConfigResources(query: string, selectors?: ResourceSelector[], limit = 20): Promise<ConfigItem[]> {
  const normalizedSelectors = normalizeResourceSelectors(query, selectors);
  if (normalizedSelectors.length === 0) {
    return searchConfigItems(query, undefined, limit);
  }

  const response = await fetch("/resources/search", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: JSON.stringify({
      limit,
      configs: normalizedSelectors,
    }),
  });

  if (!response.ok) {
    throw new Error(
      `POST /resources/search failed with ${response.status}: ${await response.text()}`,
    );
  }

  const data = await response.json() as SearchResourcesResponse;
  return (data.configs ?? []).map(selectedResourceToConfigItem);
}

export async function getConfigParentsByLocation(id: string): Promise<ConfigChildItem[]> {
  const result = await fetchPostgrest<ConfigChildItem[]>(
    `/db/rpc/get_parents_by_location?config_id=${encodeURIComponent(id)}`,
  );
  return result.data ?? [];
}

export async function getConfigRelationshipTrees(id: string): Promise<ConfigRelationshipsResponse> {
  return fetchJSON<ConfigRelationshipsResponse>(`/catalog/${encodeURIComponent(id)}/relationships`);
}

export async function getConfigChanges(id: string, limit = 50): Promise<ConfigChange[]> {
  const result = await fetchPostgrest<ConfigChange[]>(
    `/db/config_changes?config_id=eq.${encodeURIComponent(id)}&order=created_at.desc&limit=${limit}`,
  );
  return result.data ?? [];
}

export async function getConfigInsights(id: string, limit = 50): Promise<ConfigAnalysis[]> {
  const result = await fetchPostgrest<ConfigAnalysis[]>(
    [
      "/db/config_analysis",
      "?select=id,config_id,source,analyzer,analysis_type,summary,message,severity,status,analysis,properties,first_observed,last_observed",
      `&config_id=eq.${encodeURIComponent(id)}`,
      "&order=last_observed.desc.nullslast,first_observed.desc.nullslast",
      `&limit=${limit}`,
    ].join(""),
  );
  return result.data ?? [];
}

export async function getConfigAccessSummary(id: string): Promise<ConfigAccessSummary[]> {
  const params = new URLSearchParams({
    select: "config_id,config_name,config_type,external_group_id,external_user_id,user,email,role,user_type,created_at,last_signed_in_at,last_reviewed_at",
    config_id: `eq.${id}`,
    order: "user.asc",
  });
  const result = await fetchAllPostgrest<ConfigAccessSummary>(
    `/db/config_access_summary?${params.toString()}`,
  );
  const rows = result.data ?? [];
  return hydrateConfigAccessGroupNames(rows);
}

async function hydrateConfigAccessGroupNames(rows: ConfigAccessSummary[]): Promise<ConfigAccessSummary[]> {
  const groupIDs = Array.from(new Set(rows.map((row) => row.external_group_id).filter(Boolean))) as string[];
  if (groupIDs.length === 0) return rows;

  const params = new URLSearchParams({
    select: "id,name",
    id: `in.(${groupIDs.join(",")})`,
  });
  const result = await fetchAllPostgrest<ExternalGroup>(`/db/external_groups?${params.toString()}`);
  const groupNames = new Map((result.data ?? []).map((group) => [group.id, group.name]));

  return rows.map((row) => ({
    ...row,
    group_name: row.group_name ?? (row.external_group_id ? groupNames.get(row.external_group_id) ?? null : null),
  }));
}

export async function getConfigAccessLogs(id: string, limit = 50): Promise<ConfigAccessLog[]> {
  const result = await fetchPostgrest<ConfigAccessLog[]>(
    [
      "/db/config_access_logs",
      "?select=*,external_users(name,user_email:email)",
      `&config_id=eq.${encodeURIComponent(id)}`,
      "&order=created_at.desc",
      `&limit=${limit}`,
    ].join(""),
  );
  return result.data ?? [];
}

export async function previewCatalogReport(request: CatalogReportRequest): Promise<CatalogReportPreviewResponse> {
  return fetchJSON<CatalogReportPreviewResponse>("/catalog/report/preview", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });
}

export async function generateCatalogReport(
  request: CatalogReportRequest,
  onProgress?: (progress: CatalogReportProgress) => void,
): Promise<{ blob: Blob; filename: string }> {
  onProgress?.({ stage: "rendering" });
  const response = await fetch("/catalog/report", {
    method: "POST",
    headers: {
      Accept: request.format === "json" ? "application/json" : "*/*",
      "Content-Type": "application/json",
    },
    body: JSON.stringify(request),
  });

  if (!response.ok) {
    const text = await response.text();
    throw new CatalogReportError(response.status, response.statusText, text, parseCatalogReportErrorBody(text));
  }

  onProgress?.({ stage: "downloading", loaded: 0, total: responseContentLength(response) });
  const blob = await readReportBlob(response, onProgress);

  return {
    blob,
    filename: filenameFromDisposition(response.headers.get("Content-Disposition")) ?? defaultReportFilename(request),
  };
}

function filenameFromDisposition(value: string | null): string | undefined {
  if (!value) return undefined;
  const match = value.match(/filename="?([^";]+)"?/i);
  return match?.[1];
}

function defaultReportFilename(request: CatalogReportRequest): string {
  const ext = request.format === "json" ? "json" : request.format === "facet-html" ? "html" : "pdf";
  return `catalog-report.${ext}`;
}

async function readReportBlob(response: Response, onProgress?: (progress: CatalogReportProgress) => void): Promise<Blob> {
  const total = responseContentLength(response);
  const contentType = response.headers.get("Content-Type") || "application/octet-stream";
  if (!response.body) {
    return response.blob();
  }

  const reader = response.body.getReader();
  const chunks: BlobPart[] = [];
  let loaded = 0;

  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    const chunk = new Uint8Array(value.byteLength);
    chunk.set(value);
    chunks.push(chunk);
    loaded += value.byteLength;
    onProgress?.({ stage: "downloading", loaded, total });
  }

  return new Blob(chunks, { type: contentType });
}

function responseContentLength(response: Response): number | undefined {
  const value = response.headers.get("Content-Length");
  if (!value) return undefined;
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
}

function parseCatalogReportErrorBody(text: string): CatalogReportErrorBody | undefined {
  if (!text.trim().startsWith("{")) return undefined;
  try {
    const parsed = JSON.parse(text) as unknown;
    if (!parsed || typeof parsed !== "object") return undefined;
    return parsed as CatalogReportErrorBody;
  } catch {
    return undefined;
  }
}

function ilike(value: string) {
  return `*${escapePostgrestLike(value)}*`;
}

function normalizeResourceSelectors(query: string, selectors?: ResourceSelector[]) {
  const trimmed = query.trim();
  return (selectors ?? [])
    .filter((selector) => selector && typeof selector === "object")
    .map((selector) => {
      const search = [trimmed, typeof selector.search === "string" ? selector.search.trim() : ""]
        .filter(Boolean)
        .join(" ");
      return {
        ...selector,
        ...(selector.agent ? {} : { agent: "all" }),
        ...(search ? { search } : {}),
      };
    });
}

function selectedResourceToConfigItem(resource: SelectedResource): ConfigItem {
  return {
    id: resource.id,
    name: resource.name || resource.id,
    type: resource.type,
    config_class: resource.icon,
    status: resource.status ?? null,
    health: resource.health as ConfigItem["health"],
    tags: resource.tags ?? null,
    agent_id: resource.agent,
  };
}

function escapePostgrestLike(value: string) {
  return value.replaceAll("*", "\\*").replaceAll(",", "\\,").replaceAll("(", "\\(").replaceAll(")", "\\)");
}

function exactUUIDFilter(value: string) {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value)
    ? `id.eq.${value}`
    : "";
}

function orFilter(filters: string[]) {
  const active = filters.filter(Boolean);
  return active.length ? `(${active.join(",")})` : "";
}
