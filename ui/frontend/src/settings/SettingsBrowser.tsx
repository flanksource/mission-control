import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Badge,
  DetailEmptyState,
  Icon,
  JsonView,
  KeyValueList,
  Modal,
  Section,
  type KeyValueListItem,
} from "@flanksource/clicky-ui";
import { ConfigIcon } from "../ConfigIcon";
import {
  bulkDeleteConfigItems,
  getConfigScraper,
  getConfigScrapers,
  getScraperStats,
  getScrapePlugins,
  type ConfigScraper,
  type ScrapePlugin,
  type ScraperJobHistory,
  type ScraperStats,
} from "../api/settings";
import type { ResourceSelector } from "../api/configs";
import type { ConfigItem } from "../api/types";
import { ConfigItemSelector } from "../components/ConfigItemSelector";
import { formatDate, stringifyValue, timeAgo } from "../config-detail/utils";
import { DetailPageLayout, EntityHeader, HeaderPill, PageBreadcrumbs } from "../layout/DetailPageLayout";

export type SettingsMode = "scrapers" | "scrape-plugins";

export type SettingsBrowserProps = {
  mode: SettingsMode;
  scraperId?: string;
};

export function SettingsBrowser({ mode, scraperId }: SettingsBrowserProps) {
  if (mode === "scrape-plugins") return <ScrapePluginsPage />;
  if (scraperId) return <ScraperDetailPage id={scraperId} />;
  return <ScrapersPage />;
}

function ScrapersPage() {
  const query = useQuery({
    queryKey: ["settings", "scrapers"],
    queryFn: getConfigScrapers,
  });
  const [deleteOpen, setDeleteOpen] = useState(false);
  const scrapers = query.data ?? [];

  return (
    <SettingsShell
      title="Scrapers"
      description="Configured catalog scrapers"
      icon="lucide:radar"
      count={query.data?.length}
      actions={
        <button
          type="button"
          onClick={() => setDeleteOpen(true)}
          className="inline-flex h-9 items-center gap-2 rounded-md border border-destructive/40 px-3 text-sm font-medium text-destructive hover:bg-destructive/10"
        >
          <Icon name="lucide:trash-2" />
          <span>Delete catalog items</span>
        </button>
      }
    >
      <Section title="Scrapers" icon="lucide:radar" defaultOpen>
        <QueryState query={query} emptyIcon="lucide:radar" emptyLabel="No scrapers" />
        {scrapers.length > 0 && <ScraperTable rows={scrapers} />}
      </Section>
      <BulkConfigItemDeleteDialog open={deleteOpen} onClose={() => setDeleteOpen(false)} />
    </SettingsShell>
  );
}

function ScraperDetailPage({ id }: { id: string }) {
  const [deleteOpen, setDeleteOpen] = useState(false);
  const scraperQuery = useQuery({
    queryKey: ["settings", "scrapers", id],
    queryFn: () => getConfigScraper(id),
    enabled: !!id,
  });
  const statsQuery = useQuery({
    queryKey: ["settings", "scrapers", id, "stats"],
    queryFn: () => getScraperStats(id),
    enabled: !!id,
  });
  const scraper = scraperQuery.data;
  const stats = statsQuery.data;

  return (
    <DetailPageLayout
      breadcrumbs={
        <PageBreadcrumbs
          items={[
            { label: "Scrapers", href: "/ui/settings/scrapers", icon: "lucide:chevron-left" },
            { label: scraper?.name ?? id, title: scraper?.name ?? id, className: "max-w-[28rem]" },
          ]}
        />
      }
      header={
        <EntityHeader
          variant="card"
          titleSize="lg"
          icon="lucide:radar"
          title={scraper?.name ?? "Scraper"}
          description={scraper?.description || scraper?.namespace || id}
          meta={scraper ? <ScraperHeaderMeta scraper={scraper} stats={stats} /> : undefined}
        />
      }
      actions={
        scraper ? (
          <button
            type="button"
            onClick={() => setDeleteOpen(true)}
            className="inline-flex h-9 items-center gap-2 rounded-md border border-destructive/40 px-3 text-sm font-medium text-destructive hover:bg-destructive/10"
          >
            <Icon name="lucide:trash-2" />
            <span>Delete scraper items</span>
          </button>
        ) : undefined
      }
      main={
        <>
          <div className="grid min-w-0 grid-cols-1 gap-5 xl:grid-cols-[minmax(0,1fr)_24rem]">
            <div className="flex min-w-0 flex-col gap-5">
              <QueryState query={scraperQuery} emptyIcon="lucide:radar" emptyLabel="Scraper not found" />
              {scraper && (
                <>
                  <ScraperStatsSection query={statsQuery} />
                  <ScraperHistorySection history={stats?.recentHistory ?? []} loading={statsQuery.isLoading} error={statsQuery.error} />
                  <Section title="Spec" icon="lucide:braces" defaultOpen>
                    <JsonView data={parseJSONLike(scraper.spec)} defaultOpenDepth={2} />
                  </Section>
                </>
              )}
            </div>
            <div className="min-w-0">
              {scraper && (
                <div className="flex min-w-0 flex-col gap-5">
                  <Section title="Details" icon="lucide:info" defaultOpen>
                    <KeyValueList items={scraperDetailItems(scraper)} />
                  </Section>
                  <Section title="Bulk Delete" icon="lucide:trash-2" defaultOpen={false}>
                    <div className="grid gap-3 text-sm">
                      <p className="text-muted-foreground">
                        Select and permanently delete catalog items created by this scraper.
                      </p>
                      <button
                        type="button"
                        onClick={() => setDeleteOpen(true)}
                        className="inline-flex h-9 w-fit items-center gap-2 rounded-md border border-destructive/40 px-3 font-medium text-destructive hover:bg-destructive/10"
                      >
                        <Icon name="lucide:trash-2" />
                        <span>Delete scraper items</span>
                      </button>
                    </div>
                  </Section>
                </div>
              )}
            </div>
          </div>
          {scraper && (
            <BulkConfigItemDeleteDialog
              open={deleteOpen}
              onClose={() => setDeleteOpen(false)}
              title={`Delete ${scraper.name || "scraper"} config items`}
              selectors={[{ scope: scraper.id }]}
              emptyLabel="No scraper config items selected"
              placeholder="Search this scraper's config items..."
            />
          )}
        </>
      }
    />
  );
}

function ScrapePluginsPage() {
  const query = useQuery({
    queryKey: ["settings", "scrape-plugins"],
    queryFn: getScrapePlugins,
  });
  const plugins = query.data ?? [];
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const selected = plugins.find((plugin) => plugin.id === selectedId) ?? plugins[0] ?? null;

  useEffect(() => {
    if (!selectedId && plugins[0]) setSelectedId(plugins[0].id);
    if (selectedId && plugins.length > 0 && !plugins.some((plugin) => plugin.id === selectedId)) {
      setSelectedId(plugins[0].id);
    }
  }, [plugins, selectedId]);

  return (
    <SettingsShell
      title="Scrape Plugins"
      description="Installed scrape plugin definitions"
      icon="lucide:plug-zap"
      count={query.data?.length}
    >
      <div className="grid min-w-0 grid-cols-1 gap-5 xl:grid-cols-[minmax(0,1fr)_24rem]">
        <Section title="Scrape Plugins" icon="lucide:plug-zap" defaultOpen>
          <QueryState query={query} emptyIcon="lucide:plug-zap" emptyLabel="No scrape plugins" />
          {plugins.length > 0 && (
            <ScrapePluginTable rows={plugins} selectedId={selected?.id ?? null} onSelect={setSelectedId} />
          )}
        </Section>
        <div className="min-w-0">
          {selected ? (
            <ScrapePluginDetail plugin={selected} />
          ) : (
            <Section title="Details" icon="lucide:info" defaultOpen>
              <DetailEmptyState icon="lucide:plug-zap" label="Select a scrape plugin" />
            </Section>
          )}
        </div>
      </div>
    </SettingsShell>
  );
}

function SettingsShell({
  title,
  description,
  icon,
  count,
  actions,
  children,
}: {
  title: string;
  description: string;
  icon: string;
  count?: number;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <DetailPageLayout
      breadcrumbs={<PageBreadcrumbs items={[{ label: "Settings", icon: "lucide:settings" }, { label: title }]} />}
      actions={actions}
      header={
        <EntityHeader
          variant="card"
          titleSize="lg"
          icon={icon}
          title={title}
          description={description}
          meta={count === undefined ? undefined : <HeaderPill icon="lucide:hash" label={`${count} ${count === 1 ? "item" : "items"}`} />}
        />
      }
      main={children}
    />
  );
}

function BulkConfigItemDeleteDialog({
  open,
  onClose,
  title = "Delete catalog config items",
  selectors,
  placeholder = "Search config items to delete...",
  emptyLabel = "No config items selected",
}: {
  open: boolean;
  onClose: () => void;
  title?: string;
  selectors?: ResourceSelector[];
  placeholder?: string;
  emptyLabel?: string;
}) {
  const queryClient = useQueryClient();
  const [items, setItems] = useState<ConfigItem[]>([]);
  const [confirming, setConfirming] = useState(false);
  const [success, setSuccess] = useState<string | null>(null);
  const ids = useMemo(() => items.map((item) => item.id), [items]);
  const mutation = useMutation({
    mutationFn: () => bulkDeleteConfigItems(ids),
    onSuccess: (result) => {
      setItems([]);
      setConfirming(false);
      setSuccess(`${result.deleted} config ${result.deleted === 1 ? "item" : "items"} deleted`);
      queryClient.invalidateQueries({ queryKey: ["config-selector"] });
      queryClient.invalidateQueries({ queryKey: ["settings", "scrapers"] });
    },
  });

  useEffect(() => {
    if (!open) {
      setConfirming(false);
      setSuccess(null);
      mutation.reset();
    }
  }, [open]);

  const addItem = (config: ConfigItem | null) => {
    if (!config) return;
    setSuccess(null);
    mutation.reset();
    setItems((current) => current.some((item) => item.id === config.id) ? current : [...current, config]);
  };

  const removeItem = (id: string) => {
    setSuccess(null);
    mutation.reset();
    setItems((current) => current.filter((item) => item.id !== id));
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      size="lg"
      className="max-h-[88vh] overflow-hidden"
      headerSlot={
        <div className="flex min-w-0 items-center gap-2">
          <Icon name="lucide:trash-2" className="shrink-0 text-destructive" />
          <span className="truncate text-sm font-semibold">{title}</span>
        </div>
      }
    >
      <div className="flex max-h-[72vh] min-h-[22rem] flex-col gap-4 overflow-hidden">
        {confirming ? (
          <div className="grid gap-4 text-sm">
            <p className="text-muted-foreground">
              This will permanently delete {items.length} selected config {items.length === 1 ? "item" : "items"} and related catalog records.
            </p>
            <SelectedConfigItems items={items} onRemove={removeItem} compact />
          </div>
        ) : (
          <>
            <ConfigItemSelector placeholder={placeholder} selectors={selectors} onSelect={addItem} />
            <div className="min-h-0 flex-1 overflow-auto">
              {items.length === 0 ? (
                <DetailEmptyState icon="lucide:list-plus" label={emptyLabel} />
              ) : (
                <SelectedConfigItems items={items} onRemove={removeItem} />
              )}
            </div>
          </>
        )}
        {success && (
          <div className="rounded-md border border-emerald-300 bg-emerald-50 px-3 py-2 text-sm text-emerald-800">
            {success}
          </div>
        )}
        {mutation.error && (
          <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">
            {mutation.error instanceof Error ? mutation.error.message : String(mutation.error)}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={confirming ? () => setConfirming(false) : onClose}
            className="h-9 rounded-md border border-border px-3 text-sm hover:bg-accent/50"
          >
            {confirming ? "Back" : "Close"}
          </button>
          <button
            type="button"
            disabled={items.length === 0 || mutation.isPending}
            onClick={confirming ? () => mutation.mutate() : () => setConfirming(true)}
            className="inline-flex h-9 items-center gap-2 rounded-md bg-destructive px-3 text-sm font-medium text-destructive-foreground disabled:cursor-not-allowed disabled:opacity-50"
          >
            <Icon name={mutation.isPending ? "lucide:loader-2" : "lucide:trash-2"} className={mutation.isPending ? "animate-spin" : undefined} />
            <span>{confirming ? "Delete" : "Review delete"}</span>
          </button>
        </div>
      </div>
    </Modal>
  );
}

function SelectedConfigItems({
  items,
  onRemove,
  compact = false,
}: {
  items: ConfigItem[];
  onRemove: (id: string) => void;
  compact?: boolean;
}) {
  return (
    <div className="overflow-hidden rounded-md border border-border">
      {items.map((item) => (
        <div key={item.id} className="flex min-w-0 items-center gap-3 border-b border-border px-3 py-2 last:border-b-0">
          <ConfigIcon primary={item.type || item.config_class || "config"} className="h-4 max-w-4 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-medium">{item.name || item.id}</div>
            <div className="truncate text-xs text-muted-foreground">{item.type || item.config_class || item.path || item.id}</div>
          </div>
          {!compact && (
            <button
              type="button"
              onClick={() => onRemove(item.id)}
              className="inline-flex h-8 shrink-0 items-center gap-1 rounded-md border border-border px-2 text-xs hover:bg-accent/50"
            >
              <Icon name="lucide:x" />
              <span>Remove</span>
            </button>
          )}
        </div>
      ))}
    </div>
  );
}

function ScraperTable({ rows }: { rows: ConfigScraper[] }) {
  return (
    <div className="overflow-hidden rounded-md border border-border">
      <table className="w-full table-fixed text-left text-sm">
        <thead className="border-b border-border bg-muted/40 text-xs text-muted-foreground">
          <tr>
            <th className="w-[45%] px-3 py-2 font-medium">Name</th>
            <th className="w-[18%] px-3 py-2 font-medium">Namespace</th>
            <th className="w-[18%] px-3 py-2 font-medium">Source</th>
            <th className="w-[19%] px-3 py-2 font-medium">Updated</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.id} className="border-b border-border last:border-b-0 hover:bg-accent/40">
              <td className="px-3 py-2">
                <a href={`/ui/settings/scrapers/${encodeURIComponent(row.id)}`} className="flex min-w-0 items-center gap-2 hover:text-primary">
                  <Icon name="lucide:radar" className="shrink-0 text-muted-foreground" />
                  <span className="truncate font-medium">{row.name || row.id}</span>
                </a>
              </td>
              <td className="truncate px-3 py-2 text-muted-foreground">{stringifyValue(row.namespace)}</td>
              <td className="truncate px-3 py-2 text-muted-foreground">{stringifyValue(row.source)}</td>
              <td className="truncate px-3 py-2 text-muted-foreground">{timeAgo(row.updated_at ?? row.created_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ScraperStatsSection({ query }: { query: { isLoading: boolean; error: unknown; data?: ScraperStats } }) {
  if (query.isLoading) return <div className="text-sm text-muted-foreground">Loading stats...</div>;
  if (query.error) {
    return (
      <div className="rounded-md border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
        {query.error instanceof Error ? query.error.message : String(query.error)}
      </div>
    );
  }
  if (!query.data) return null;
  const stats = query.data;
  return (
    <Section title="Stats" icon="lucide:bar-chart-3" defaultOpen>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label="Active Configs" value={stats.activeConfigs} icon="lucide:boxes" />
        <StatCard label="Deleted Configs" value={stats.deletedConfigs} icon="lucide:archive-x" />
        <StatCard label="Types" value={stats.typeCount} icon="lucide:layers-3" />
        <StatCard label="Last Run" value={stats.latestRun?.time_start ? timeAgo(stats.latestRun.time_start) : "-"} icon="lucide:clock-3" />
      </div>
      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <HealthSummary stats={stats} />
        <TypeBreakdown rows={stats.typeBreakdown} />
      </div>
    </Section>
  );
}

function HealthSummary({ stats }: { stats: ScraperStats }) {
  return (
    <div className="rounded-md border border-border p-3">
      <div className="mb-2 text-xs font-medium uppercase text-muted-foreground">Health</div>
      <div className="flex flex-wrap gap-2">
        <Badge tone="success" size="xs">{stats.healthyConfigs} healthy</Badge>
        <Badge tone="warning" size="xs">{stats.warningConfigs} warning</Badge>
        <Badge tone="danger" size="xs">{stats.unhealthyConfigs} unhealthy</Badge>
      </div>
    </div>
  );
}

function TypeBreakdown({ rows }: { rows: Array<{ type: string; count: number }> }) {
  return (
    <div className="rounded-md border border-border p-3">
      <div className="mb-2 text-xs font-medium uppercase text-muted-foreground">Top Types</div>
      {rows.length === 0 ? (
        <div className="text-sm text-muted-foreground">No config types</div>
      ) : (
        <div className="grid gap-2">
          {rows.slice(0, 8).map((row) => (
            <div key={row.type} className="flex min-w-0 items-center justify-between gap-3 text-sm">
              <span className="truncate">{row.type}</span>
              <Badge size="xs">{row.count}</Badge>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function StatCard({ label, value, icon }: { label: string; value: ReactNode; icon: string }) {
  return (
    <div className="rounded-md border border-border bg-background p-3">
      <div className="mb-2 flex items-center gap-2 text-xs font-medium uppercase text-muted-foreground">
        <Icon name={icon} />
        <span>{label}</span>
      </div>
      <div className="truncate text-2xl font-semibold">{value}</div>
    </div>
  );
}

function ScraperHistorySection({
  history,
  loading,
  error,
}: {
  history: ScraperJobHistory[];
  loading: boolean;
  error: unknown;
}) {
  return (
    <Section title="History" icon="lucide:history" defaultOpen summary={history.length ? `${history.length} runs` : undefined}>
      {loading ? (
        <div className="text-sm text-muted-foreground">Loading history...</div>
      ) : error ? (
        <div className="rounded-md border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
          {error instanceof Error ? error.message : String(error)}
        </div>
      ) : history.length === 0 ? (
        <DetailEmptyState icon="lucide:history" label="No scraper history" />
      ) : (
        <div className="overflow-hidden rounded-md border border-border">
          {history.map((run) => (
            <div key={run.id} className="grid gap-2 border-b border-border px-3 py-3 last:border-b-0 md:grid-cols-[minmax(0,1fr)_auto]">
              <div className="min-w-0">
                <div className="flex min-w-0 flex-wrap items-center gap-2">
                  <Badge tone={historyStatusTone(run.status)} size="xs">{run.status || "unknown"}</Badge>
                  <span className="truncate text-sm font-medium">{run.name || run.resource_type || run.id}</span>
                </div>
                <div className="mt-1 truncate text-xs text-muted-foreground">
                  {run.time_start ? formatDate(run.time_start) : "-"} · {durationLabel(run.duration_millis)}
                </div>
              </div>
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <span>{Number(run.success_count ?? 0)} ok</span>
                <span>{Number(run.error_count ?? 0)} errors</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </Section>
  );
}

function ScrapePluginTable({
  rows,
  selectedId,
  onSelect,
}: {
  rows: ScrapePlugin[];
  selectedId: string | null;
  onSelect: (id: string) => void;
}) {
  return (
    <div className="overflow-hidden rounded-md border border-border">
      <table className="w-full table-fixed text-left text-sm">
        <thead className="border-b border-border bg-muted/40 text-xs text-muted-foreground">
          <tr>
            <th className="w-[45%] px-3 py-2 font-medium">Name</th>
            <th className="w-[20%] px-3 py-2 font-medium">Namespace</th>
            <th className="w-[18%] px-3 py-2 font-medium">Source</th>
            <th className="w-[17%] px-3 py-2 font-medium">Updated</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr
              key={row.id}
              className={[
                "cursor-pointer border-b border-border last:border-b-0 hover:bg-accent/40",
                row.id === selectedId ? "bg-accent/60" : "",
              ].join(" ")}
              onClick={() => onSelect(row.id)}
            >
              <td className="px-3 py-2">
                <div className="flex min-w-0 items-center gap-2">
                  <Icon name="lucide:plug-zap" className="shrink-0 text-muted-foreground" />
                  <span className="truncate font-medium">{row.name || row.id}</span>
                </div>
              </td>
              <td className="truncate px-3 py-2 text-muted-foreground">{stringifyValue(row.namespace)}</td>
              <td className="truncate px-3 py-2 text-muted-foreground">{stringifyValue(row.source)}</td>
              <td className="truncate px-3 py-2 text-muted-foreground">{timeAgo(row.updated_at ?? row.created_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ScrapePluginDetail({ plugin }: { plugin: ScrapePlugin }) {
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <Section title="Details" icon="lucide:info" defaultOpen>
        <KeyValueList items={scrapePluginDetailItems(plugin)} />
      </Section>
      <Section title="Spec" icon="lucide:braces" defaultOpen>
        <JsonView data={parseJSONLike(plugin.spec)} defaultOpenDepth={2} />
      </Section>
    </div>
  );
}

function ScraperHeaderMeta({ scraper, stats }: { scraper: ConfigScraper; stats?: ScraperStats }) {
  return (
    <>
      <HeaderPill icon="lucide:folder" label={scraper.namespace || "-"} />
      {scraper.source && <HeaderPill icon="lucide:git-branch" label={scraper.source} />}
      {stats && <HeaderPill icon="lucide:boxes" label={`${stats.activeConfigs} active configs`} />}
      <HeaderPill icon="lucide:fingerprint" label={scraper.id} mono />
    </>
  );
}

function scraperDetailItems(scraper: ConfigScraper): KeyValueListItem[] {
  return [
    kv("ID", <Mono value={scraper.id} />),
    kv("Name", scraper.name),
    kv("Namespace", scraper.namespace),
    kv("Description", scraper.description),
    kv("Source", sourceBadge(scraper.source)),
    kv("Agent", scraper.agent_id ? <Mono value={scraper.agent_id} /> : "-"),
    kv("Application", scraper.application_id ? <Mono value={scraper.application_id} /> : "-"),
    kv("Created", dateValue(scraper.created_at)),
    kv("Updated", dateValue(scraper.updated_at)),
  ];
}

function scrapePluginDetailItems(plugin: ScrapePlugin): KeyValueListItem[] {
  return [
    kv("ID", <Mono value={plugin.id} />),
    kv("Name", plugin.name),
    kv("Namespace", plugin.namespace),
    kv("Source", sourceBadge(plugin.source)),
    kv("Created", dateValue(plugin.created_at)),
    kv("Updated", dateValue(plugin.updated_at)),
  ];
}

function QueryState({
  query,
  emptyIcon,
  emptyLabel,
}: {
  query: { isLoading: boolean; error: unknown; data?: unknown };
  emptyIcon: string;
  emptyLabel: string;
}) {
  if (query.isLoading) return <div className="text-sm text-muted-foreground">Loading...</div>;
  if (query.error) {
    return (
      <div className="rounded-md border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
        {query.error instanceof Error ? query.error.message : String(query.error)}
      </div>
    );
  }
  if (Array.isArray(query.data) && query.data.length === 0) {
    return <DetailEmptyState icon={emptyIcon} label={emptyLabel} />;
  }
  if (query.data === null) {
    return <DetailEmptyState icon={emptyIcon} label={emptyLabel} />;
  }
  return null;
}

function kv(label: string, value: ReactNode): KeyValueListItem {
  return {
    key: label,
    label,
    value: value || "-",
  };
}

function sourceBadge(source?: string | null) {
  if (!source) return "-";
  return <Badge size="xs">{source}</Badge>;
}

function dateValue(value?: string | null) {
  if (!value) return "-";
  return (
    <span title={formatDate(value)}>
      {timeAgo(value)}
    </span>
  );
}

function durationLabel(value?: number | null) {
  const millis = Number(value ?? 0);
  if (!Number.isFinite(millis) || millis <= 0) return "-";
  if (millis < 1000) return `${millis}ms`;
  if (millis < 60_000) return `${Math.round(millis / 1000)}s`;
  return `${Math.round(millis / 60_000)}m`;
}

function historyStatusTone(status?: string | null) {
  switch ((status || "").toUpperCase()) {
    case "SUCCESS":
      return "success" as const;
    case "WARNING":
    case "STALE":
      return "warning" as const;
    case "FAILED":
      return "danger" as const;
    default:
      return "neutral" as const;
  }
}

function parseJSONLike(value: unknown) {
  if (typeof value !== "string") return value ?? {};
  const trimmed = value.trim();
  if (!trimmed) return {};
  try {
    return JSON.parse(trimmed);
  } catch {
    return trimmed;
  }
}

function Mono({ value }: { value: string }) {
  return <span className="break-all font-mono text-xs">{value}</span>;
}
