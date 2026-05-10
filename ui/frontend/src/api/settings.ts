import { fetchJSON, fetchPostgrest } from "./http";

const settingsLimit = 500;

export type ConfigScraper = {
  id: string;
  agent_id?: string | null;
  name: string;
  namespace?: string | null;
  description?: string | null;
  spec?: string | null;
  source?: string | null;
  application_id?: string | null;
  created_by?: string | null;
  created_at?: string;
  updated_at?: string | null;
  deleted_at?: string | null;
};

export type ScrapePlugin = {
  id: string;
  name: string;
  namespace?: string | null;
  spec?: unknown;
  source?: string | null;
  created_by?: string | null;
  created_at?: string;
  updated_at?: string | null;
  deleted_at?: string | null;
};

export type ScraperConfigItemSummary = {
  id: string;
  type?: string | null;
  config_class?: string | null;
  health?: string | null;
  status?: string | null;
  created_at?: string;
  updated_at?: string | null;
  deleted_at?: string | null;
};

export type ScraperJobHistory = {
  id: string;
  agent_id?: string | null;
  name?: string | null;
  success_count?: number | null;
  error_count?: number | null;
  hostname?: string | null;
  duration_millis?: number | null;
  resource_type?: string | null;
  resource_id?: string | null;
  details?: unknown;
  status?: string | null;
  time_start?: string | null;
  time_end?: string | null;
};

export type ScraperStats = {
  activeConfigs: number;
  deletedConfigs: number;
  typeCount: number;
  healthyConfigs: number;
  warningConfigs: number;
  unhealthyConfigs: number;
  latestRun?: ScraperJobHistory;
  recentHistory: ScraperJobHistory[];
  typeBreakdown: Array<{ type: string; count: number }>;
};

export type BulkDeleteConfigItemsResponse = {
  deleted: number;
  ids: string[];
};

export async function getConfigScrapers(): Promise<ConfigScraper[]> {
  const params = new URLSearchParams({
    select: "id,agent_id,name,namespace,description,spec,source,application_id,created_by,created_at,updated_at,deleted_at",
    deleted_at: "is.null",
    order: "name.asc",
    limit: String(settingsLimit),
  });
  const result = await fetchPostgrest<ConfigScraper[]>(`/db/config_scrapers?${params.toString()}`);
  return result.data ?? [];
}

export async function getConfigScraper(id: string): Promise<ConfigScraper | null> {
  const params = new URLSearchParams({
    select: "id,agent_id,name,namespace,description,spec,source,application_id,created_by,created_at,updated_at,deleted_at",
    id: `eq.${id}`,
    deleted_at: "is.null",
    limit: "1",
  });
  const result = await fetchPostgrest<ConfigScraper[]>(`/db/config_scrapers?${params.toString()}`);
  return result.data[0] ?? null;
}

export async function getScrapePlugins(): Promise<ScrapePlugin[]> {
  const params = new URLSearchParams({
    select: "id,name,namespace,spec,source,created_by,created_at,updated_at,deleted_at",
    deleted_at: "is.null",
    order: "name.asc",
    limit: String(settingsLimit),
  });
  const result = await fetchPostgrest<ScrapePlugin[]>(`/db/scrape_plugins?${params.toString()}`);
  return result.data ?? [];
}

export async function getScraperStats(scraperId: string): Promise<ScraperStats> {
  const [activeConfigs, deletedConfigs, recentHistory] = await Promise.all([
    getScraperConfigItems(scraperId, false),
    getScraperConfigItems(scraperId, true),
    getScraperHistory(scraperId),
  ]);
  const typeCounts = new Map<string, number>();
  let healthyConfigs = 0;
  let warningConfigs = 0;
  let unhealthyConfigs = 0;

  for (const config of activeConfigs) {
    const type = config.type || config.config_class || "unknown";
    typeCounts.set(type, (typeCounts.get(type) ?? 0) + 1);
    const health = (config.health || "").toLowerCase();
    if (health === "healthy") healthyConfigs += 1;
    if (health === "warning") warningConfigs += 1;
    if (health === "unhealthy") unhealthyConfigs += 1;
  }

  return {
    activeConfigs: activeConfigs.length,
    deletedConfigs: deletedConfigs.length,
    typeCount: typeCounts.size,
    healthyConfigs,
    warningConfigs,
    unhealthyConfigs,
    latestRun: recentHistory[0],
    recentHistory,
    typeBreakdown: Array.from(typeCounts.entries())
      .map(([type, count]) => ({ type, count }))
      .sort((a, b) => b.count - a.count || a.type.localeCompare(b.type)),
  };
}

async function getScraperConfigItems(scraperId: string, deleted: boolean): Promise<ScraperConfigItemSummary[]> {
  const params = new URLSearchParams({
    select: "id,type,config_class,health,status,created_at,updated_at,deleted_at",
    scraper_id: `eq.${scraperId}`,
    deleted_at: deleted ? "not.is.null" : "is.null",
    order: deleted ? "deleted_at.desc" : "updated_at.desc.nullslast",
    limit: "1000",
  });
  const result = await fetchPostgrest<ScraperConfigItemSummary[]>(`/db/config_items?${params.toString()}`);
  return result.data ?? [];
}

async function getScraperHistory(scraperId: string): Promise<ScraperJobHistory[]> {
  const params = new URLSearchParams({
    select: "id,agent_id,name,success_count,error_count,hostname,duration_millis,resource_type,resource_id,details,status,time_start,time_end",
    resource_id: `eq.${scraperId}`,
    order: "time_start.desc",
    limit: "25",
  });
  const result = await fetchPostgrest<ScraperJobHistory[]>(`/db/job_history?${params.toString()}`);
  return result.data ?? [];
}

export async function bulkDeleteConfigItems(ids: string[]): Promise<BulkDeleteConfigItemsResponse> {
  return fetchJSON<BulkDeleteConfigItemsResponse>("/catalog/config-items/bulk-delete", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ids }),
  });
}
