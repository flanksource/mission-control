import { useQuery } from "@tanstack/react-query";
import { fetchPostgrest } from "../api/http";
import { StatCard } from "../config-detail/config-changes/facet-components";

type HealthBuckets = {
  total: number;
  healthy: number;
  warning: number;
  unhealthy: number;
  unknown: number;
};

async function fetchHealthCounts(): Promise<HealthBuckets> {
  const buckets: HealthBuckets = { total: 0, healthy: 0, warning: 0, unhealthy: 0, unknown: 0 };
  const queries: Array<{ key: keyof HealthBuckets; filter: string }> = [
    { key: "healthy", filter: "health=eq.healthy" },
    { key: "warning", filter: "health=eq.warning" },
    { key: "unhealthy", filter: "health=eq.unhealthy" },
  ];
  const results = await Promise.all(
    queries.map(async ({ key, filter }) => {
      const path = `/db/config_items?${filter}&deleted_at=is.null&select=id&limit=1`;
      const result = await fetchPostgrest<unknown[]>(path);
      return { key, total: result.total ?? 0 };
    }),
  );
  for (const { key, total } of results) {
    buckets[key] = total;
  }
  const totalResult = await fetchPostgrest<unknown[]>(`/db/config_items?deleted_at=is.null&select=id&limit=1`);
  buckets.total = totalResult.total ?? 0;
  buckets.unknown = Math.max(0, buckets.total - buckets.healthy - buckets.warning - buckets.unhealthy);
  return buckets;
}

async function fetchActiveRunCount(): Promise<number> {
  const result = await fetchPostgrest<unknown[]>(`/db/playbook_runs?status=in.(running,pending_approval,scheduled,waiting,sleeping,retrying)&select=id&limit=1`);
  return result.total ?? 0;
}

async function fetchRecentChangeCount(): Promise<number> {
  const since = new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString();
  const result = await fetchPostgrest<unknown[]>(
    `/db/config_changes?created_at=gte.${encodeURIComponent(since)}&select=id&limit=1`,
  );
  return result.total ?? 0;
}

export function QuickStatsRow() {
  const healthQuery = useQuery({ queryKey: ["landing", "stats", "health"], queryFn: fetchHealthCounts });
  const runsQuery = useQuery({ queryKey: ["landing", "stats", "active-runs"], queryFn: fetchActiveRunCount });
  const changesQuery = useQuery({ queryKey: ["landing", "stats", "changes-24h"], queryFn: fetchRecentChangeCount });

  const buckets = healthQuery.data;
  const total = buckets?.total;
  const healthSublabel = buckets
    ? formatHealthSublabel(buckets)
    : healthQuery.isLoading
    ? "Loading…"
    : "";

  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
      <StatCard
        label="Config items"
        value={formatCount(total, healthQuery.isLoading)}
        sublabel={healthSublabel}
        color={pickHealthColor(buckets)}
      />
      <StatCard
        label="Active playbook runs"
        value={formatCount(runsQuery.data, runsQuery.isLoading)}
        sublabel="running, pending or waiting"
        color={(runsQuery.data ?? 0) > 0 ? "blue" : "gray"}
      />
      <StatCard
        label="Changes in last 24h"
        value={formatCount(changesQuery.data, changesQuery.isLoading)}
        sublabel="across all config items"
        color="orange"
      />
    </div>
  );
}

function formatCount(value: number | undefined, loading: boolean): string {
  if (loading) return "…";
  if (value === undefined) return "–";
  return value.toLocaleString();
}

function formatHealthSublabel(buckets: HealthBuckets): string {
  const parts: string[] = [];
  if (buckets.healthy) parts.push(`${buckets.healthy.toLocaleString()} healthy`);
  if (buckets.warning) parts.push(`${buckets.warning.toLocaleString()} warning`);
  if (buckets.unhealthy) parts.push(`${buckets.unhealthy.toLocaleString()} unhealthy`);
  if (parts.length === 0) return "no health data";
  return parts.join(" · ");
}

function pickHealthColor(buckets: HealthBuckets | undefined): "red" | "orange" | "green" | "gray" {
  if (!buckets) return "gray";
  if (buckets.unhealthy > 0) return "red";
  if (buckets.warning > 0) return "orange";
  if (buckets.healthy > 0) return "green";
  return "gray";
}
