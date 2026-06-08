import { fetchJSON, fetchPostgrest } from "./api/http";
import type { ConfigItem } from "./api/types";

export type TriStateMode = "include" | "exclude";
export type TriStateFilterValue = Record<string, TriStateMode>;

export type ConfigListFilterState = {
  configType: string;
  search: string;
  labels: TriStateFilterValue;
  status: TriStateFilterValue;
  health: TriStateFilterValue;
  groupBy: string[];
  showDeleted: boolean;
  limit: number;
};

export type ConfigTypeOption = {
  type: string;
};

export type ConfigStatusOption = {
  status: string;
};

export type ConfigLabelOption = {
  key: string;
  value: string | number | boolean | null;
};

type SearchResourcesResponse = {
  configs?: Array<{
    id: string;
    agent?: string;
    name: string;
    namespace?: string;
    type?: string;
    tags?: Record<string, string>;
    health?: string;
    status?: string;
  }>;
};

export type ConfigListGroup = {
  key: string;
  label: string;
  rows: ConfigItem[];
};

const DEFAULT_LIMIT = 500;
const HEALTH_VALUES = ["healthy", "unhealthy", "warning", "unknown"];

export const BASE_GROUP_BY_OPTIONS = [
  { value: "name", label: "Name" },
  { value: "analysis", label: "Analysis" },
  { value: "changed", label: "Changed" },
  { value: "type", label: "Type" },
  { value: "config_class", label: "Provider" },
] as const;

export function parseConfigListFilters(
  params: URLSearchParams,
  configType: string,
): ConfigListFilterState {
  return {
    configType,
    search: params.get("search") ?? "",
    labels: parseTriStateParam(params.get("labels")),
    status: parseTriStateParam(params.get("status")),
    health: parseTriStateParam(params.get("health")),
    groupBy: splitListParam(params.get("groupBy")),
    showDeleted: params.get("showDeletedConfigs") === "true",
    limit: DEFAULT_LIMIT,
  };
}

export function splitListParam(value: string | null | undefined): string[] {
  return (value ?? "")
    .split(",")
    .map((part) => part.trim())
    .filter(Boolean);
}

export function parseTriStateParam(value: string | null | undefined): TriStateFilterValue {
  const out: TriStateFilterValue = {};
  for (const part of splitListParam(value)) {
    const [rawValue, rawMode] = part.split(":");
    const key = rawValue?.trim();
    if (!key) continue;
    out[key] = rawMode === "-1" || rawMode === "exclude" ? "exclude" : "include";
  }
  return out;
}

export function serializeTriStateParam(value: TriStateFilterValue): string | undefined {
  const parts = Object.entries(value)
    .filter(([key]) => key.trim() !== "")
    .map(([key, mode]) => `${key}:${mode === "exclude" ? "-1" : "1"}`);
  return parts.length > 0 ? parts.join(",") : undefined;
}

export function triStateToFilterExpression(value: TriStateFilterValue): string | undefined {
  const parts = Object.entries(value)
    .filter(([key]) => key.trim() !== "")
    .map(([key, mode]) => `${mode === "exclude" ? "!" : ""}${key}`);
  return parts.length > 0 ? parts.join(",") : undefined;
}

export function buildConfigListQuery(filters: ConfigListFilterState): string {
  const params = new URLSearchParams({
    select: "*",
    order: "name.asc",
    limit: String(filters.limit),
    type: `eq.${escapePostgrestValue(filters.configType)}`,
  });

  if (!filters.showDeleted) {
    params.set("deleted_at", "is.null");
  }

  const status = triStateToFilterExpression(filters.status);
  if (status) params.set("status.filter", status);

  const health = triStateToFilterExpression(filters.health);
  if (health) params.set("health.filter", health);

  const search = filters.search.trim();
  if (search) {
    const pattern = `*${escapePostgrestLike(search)}*`;
    params.set(
      "or",
      `(name.ilike.${pattern},type.ilike.${pattern},description.ilike.${pattern},namespace.ilike.${pattern})`,
    );
  }

  const labelClause = buildLabelAndTagClause(filters.labels);
  if (labelClause) {
    params.set("and", labelClause);
  }

  return params.toString();
}

export async function searchConfigItems(filters: ConfigListFilterState): Promise<ConfigItem[]> {
  const result = await fetchJSON<SearchResourcesResponse>("/resources/search", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      limit: filters.limit,
      configs: [{ types: [filters.configType] }],
    }),
  });

  return filterConfigItems((result.configs ?? []).map(searchResourceToConfigItem), filters);
}

export function filterConfigItems(rows: ConfigItem[], filters: ConfigListFilterState): ConfigItem[] {
  const search = filters.search.trim().toLowerCase();
  return rows.filter((row) => {
    if (search && !configSearchText(row).includes(search)) return false;
    if (!matchesTriState(row.status, filters.status)) return false;
    if (!matchesTriState(row.health, filters.health)) return false;
    if (!matchesLabels(row, filters.labels)) return false;
    if (!filters.showDeleted && row.deleted_at) return false;
    return true;
  });
}

export function buildLabelAndTagClause(value: TriStateFilterValue): string | undefined {
  const clauses: string[] = [];

  for (const [rawKeyValue, mode] of Object.entries(value)) {
    const [rawKey, rawValue] = rawKeyValue.split("____");
    const key = rawKey?.trim();
    const labelValue = rawValue?.trim();
    if (!key || !labelValue) continue;

    const safeKey = escapePostgrestJsonKey(key);
    const safeValue = escapePostgrestValue(labelValue);
    if (mode === "exclude") {
      clauses.push(`labels->>${safeKey}.neq.${safeValue}`);
      clauses.push(`tags->>${safeKey}.neq.${safeValue}`);
    } else {
      clauses.push(`or(labels->>${safeKey}.eq.${safeValue},tags->>${safeKey}.eq.${safeValue})`);
    }
  }

  return clauses.length > 0 ? `(${clauses.join(",")})` : undefined;
}

export function groupConfigItems(rows: ConfigItem[], groupBy: string[]): ConfigListGroup[] {
  if (groupBy.length === 0) {
    return [{ key: "all", label: "", rows }];
  }

  const groups = new Map<string, ConfigListGroup>();
  for (const row of rows) {
    const values = groupBy.map((field) => groupValue(row, field));
    const key = values.join("\u0000");
    const label = values.join(" / ");
    const group = groups.get(key);
    if (group) {
      group.rows.push(row);
    } else {
      groups.set(key, { key, label, rows: [row] });
    }
  }

  return Array.from(groups.values()).sort((a, b) => a.label.localeCompare(b.label));
}

export function groupValue(row: ConfigItem, field: string): string {
  if (field.endsWith("__tag")) {
    const tagKey = field.slice(0, -"__tag".length);
    return row.tags?.[tagKey] || `No ${tagKey}`;
  }

  switch (field) {
    case "analysis":
      return hasAnalysis(row.analysis) ? "Has analysis" : "No analysis";
    case "changed":
      return Number(row.changes ?? 0) > 0 ? "Changed" : "No changes";
    case "config_class":
      return row.config_class || row.type?.split("::")[0] || "No provider";
    case "health":
      return row.health || "No health";
    case "name":
      return row.name || "No name";
    case "status":
      return row.status || "No status";
    case "type":
      return row.type || "No type";
    default:
      return String((row as Record<string, unknown>)[field] ?? `No ${field}`);
  }
}

export function healthOptions() {
  return HEALTH_VALUES.map((value) => ({ value, label: titleCase(value) }));
}

export async function getConfigList(query: string): Promise<ConfigItem[]> {
  const result = await fetchPostgrest<ConfigItem[]>(`/db/configs?${query}`);
  return result.data ?? [];
}

export function configLabelOptionsFromRows(rows: ConfigItem[]): ConfigLabelOption[] {
  const out = new Map<string, ConfigLabelOption>();
  for (const row of rows) {
    for (const [key, value] of Object.entries(row.tags ?? {})) {
      if (key && value != null) out.set(`tag:${key}:${value}`, { key, value });
    }
    for (const [key, value] of Object.entries(row.labels ?? {})) {
      if (key && value != null) out.set(`label:${key}:${value}`, { key, value });
    }
  }
  return Array.from(out.values()).sort((a, b) => `${a.key}:${a.value}`.localeCompare(`${b.key}:${b.value}`));
}

export function statusOptionsFromRows(rows: ConfigItem[]): ConfigStatusOption[] {
  return Array.from(new Set(rows.map((row) => row.status).filter((value): value is string => Boolean(value))))
    .sort((a, b) => a.localeCompare(b))
    .map((status) => ({ status }));
}

export async function getConfigTypes(): Promise<ConfigTypeOption[]> {
  const result = await fetchPostgrest<ConfigTypeOption[]>(
    "/db/config_types?select=type&order=type.asc",
  );
  return result.data ?? [];
}

export async function getConfigStatuses(): Promise<ConfigStatusOption[]> {
  const result = await fetchPostgrest<ConfigStatusOption[]>(
    "/db/config_statuses?select=status&order=status.asc",
  );
  return result.data ?? [];
}

export async function getConfigTags(): Promise<ConfigLabelOption[]> {
  const result = await fetchPostgrest<ConfigLabelOption[]>(
    "/db/config_tags?select=key,value&order=key.asc,value.asc",
  );
  return result.data ?? [];
}

export async function getConfigLabels(): Promise<ConfigLabelOption[]> {
  const result = await fetchPostgrest<ConfigLabelOption[]>(
    "/db/config_labels?select=key,value&order=key.asc,value.asc",
  );
  return result.data ?? [];
}

function hasAnalysis(value: unknown): boolean {
  if (!value || typeof value !== "object") return false;
  return Object.values(value as Record<string, unknown>).some((entry) => Number(entry ?? 0) > 0);
}

function searchResourceToConfigItem(item: NonNullable<SearchResourcesResponse["configs"]>[number]): ConfigItem {
  return {
    id: item.id,
    name: item.name,
    namespace: item.namespace,
    type: item.type,
    tags: item.tags ?? {},
    health: item.health,
    status: item.status,
    agent_id: item.agent,
  };
}

function configSearchText(row: ConfigItem): string {
  return [row.name, row.type, row.description, row.namespace, row.status, row.health, ...tagEntriesForSearch(row.tags), ...tagEntriesForSearch(row.labels)]
    .filter((value): value is string => Boolean(value))
    .join(" ")
    .toLowerCase();
}

function tagEntriesForSearch(values: Record<string, string> | null | undefined): string[] {
  return Object.entries(values ?? {}).map(([key, value]) => `${key}=${value}`);
}

function matchesTriState(value: unknown, filter: TriStateFilterValue): boolean {
  const entries = Object.entries(filter);
  if (entries.length === 0) return true;
  const text = String(value ?? "");
  let included = false;
  for (const [key, mode] of entries) {
    if (mode === "exclude" && text === key) return false;
    if (mode === "include" && text === key) included = true;
  }
  return entries.some(([, mode]) => mode === "include") ? included : true;
}

function matchesLabels(row: ConfigItem, filter: TriStateFilterValue): boolean {
  const entries = Object.entries(filter);
  if (entries.length === 0) return true;
  let included = false;
  for (const [rawKeyValue, mode] of entries) {
    const [key, value] = rawKeyValue.split("____");
    const matches = row.labels?.[key] === value || row.tags?.[key] === value;
    if (mode === "exclude" && matches) return false;
    if (mode === "include" && matches) included = true;
  }
  return entries.some(([, mode]) => mode === "include") ? included : true;
}

function titleCase(value: string): string {
  return value.slice(0, 1).toUpperCase() + value.slice(1);
}

function escapePostgrestLike(value: string): string {
  return escapePostgrestValue(value).replaceAll("*", "\\*");
}

function escapePostgrestJsonKey(value: string): string {
  return value.replaceAll(",", "\\,").replaceAll(")", "\\)");
}

function escapePostgrestValue(value: string): string {
  return value.replaceAll("\\", "\\\\").replaceAll(",", "\\,").replaceAll("(", "\\(").replaceAll(")", "\\)");
}
