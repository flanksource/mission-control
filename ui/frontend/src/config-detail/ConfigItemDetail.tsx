import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import {
  Badge,
  DataTable,
  DetailEmptyState,
  ErrorDetails,
  Icon,
  JsonView,
  KeyValueList,
  MatrixTable,
  Section,
  TabButton,
  type DataTableColumn,
  type KeyValueListItem,
  type MatrixTableRow,
} from "@flanksource/clicky-ui";
import { errorDiagnosticsFromUnknown } from "../api/http";
import { ConfigIcon } from "../ConfigIcon";
import {
  useConfigAccess,
  useConfigChanges,
  useConfigDetail,
  useConfigInsights,
  useConfigParents,
  useConfigRelationshipTrees,
} from "../api/hooks";
import type {
  ConfigAccessLog,
  ConfigAccessSummary,
  ConfigAnalysis,
  ConfigChange,
  ConfigChildItem,
  ConfigItem,
  ConfigRelationshipTreeNode,
  ConfigRelationshipsResponse,
} from "../api/types";
import {
  buildGroupedRBACMatrix,
  buildRBACResource,
  costItems,
  formatDate,
  groupInsightsByType,
  healthStatus,
  isIndirectAccess,
  isKnownHealth,
  isNilUUID,
  NIL_UUID,
  severityCounts,
  severityTone,
  stringifyValue,
  dedupeTagEntries,
  tagEntries,
  timeAgo,
} from "./utils";
import { TagList } from "./TagList";
import { CatalogReportDialog } from "./CatalogReportDialog";
import ConfigChangesSection from "./config-changes/ConfigChangesSection";
import { defaultConfigChangesExtensions } from "./config-changes/config-changes-builtin-extensions";
import { ConfigPlaybooksTab } from "../playbooks/PlaybookBrowser";
import { DetailPageLayout, EntityHeader, PageBreadcrumbs } from "../layout/DetailPageLayout";
import type {
  ConfigChange as UIConfigChange,
  ConfigSeverity as UIConfigSeverity,
  ConfigTypedChange,
} from "./config-changes/types";
import { PluginTab } from "./PluginTab";
import { pluginTabKey, usePluginTabs } from "./use-plugin-tabs";

type TabKey = "overview" | "relationships" | "changes" | "insights" | "access" | "playbooks" | "json" | string;

export type ConfigItemDetailProps = {
  id: string;
};

export function ConfigItemDetail({ id }: ConfigItemDetailProps) {
  const [activeTab, setActiveTab] = useState<TabKey>("overview");
  const [reportOpen, setReportOpen] = useState(false);
  const configQuery = useConfigDetail(id);
  const parentsQuery = useConfigParents(id);
  const relationshipsQuery = useConfigRelationshipTrees(id);
  const changesQuery = useConfigChanges(id);
  const insightsQuery = useConfigInsights(id);
  const accessQuery = useConfigAccess(id);

  const config = configQuery.data;
  const relationships = relationshipsQuery.data;
  const changes = changesQuery.data ?? [];
  const insights = insightsQuery.data ?? [];
  const access = accessQuery.data?.summary ?? [];
  const parents = parentsQuery.data ?? [];
  const { plugins: pluginListings } = usePluginTabs(id);

  if (configQuery.isLoading) {
    return <LoadingState label="Loading config item" />;
  }

  if (configQuery.error) {
    return <ErrorState error={configQuery.error} />;
  }

  if (!config) {
    return (
      <div className="p-6">
        <DetailEmptyState icon="codicon:warning" label="Config item not found" />
      </div>
    );
  }

  const tabs: Array<{ key: TabKey; label: string; icon: string; count?: number }> = [
    { key: "overview", label: "Overview", icon: "lucide:panel-top" },
    {
      key: "relationships",
      label: "Relationships",
      icon: "lucide:network",
      count: relationshipCount(relationships),
    },
    { key: "changes", label: "Changes", icon: "lucide:git-compare", count: changes.length },
    { key: "insights", label: "Insights", icon: "lucide:lightbulb", count: insights.length },
    { key: "access", label: "Access", icon: "lucide:shield-check", count: access.length },
    { key: "playbooks", label: "Playbooks", icon: "lucide:play-square" },
    { key: "json", label: "JSON", icon: "lucide:braces" },
  ];

  for (const plugin of pluginListings) {
    for (const t of plugin.tabs ?? []) {
      tabs.push({
        key: pluginTabKey(plugin.name, t.name),
        label: t.name,
        icon: t.icon || "lucide:puzzle",
      });
    }
  }

  const activePluginTab = (() => {
    if (typeof activeTab !== "string" || !activeTab.startsWith("plugin:")) {
      return null;
    }
    const [, pluginName, tabName] = activeTab.split(":");
    const plugin = pluginListings.find((p) => p.name === pluginName);
    const tab = plugin?.tabs?.find((t) => t.name === tabName);
    if (!plugin || !tab) return null;
    return { pluginName, tabPath: tab.path };
  })();

  const tabBar = (
    <div className="border-b border-border pb-2">
      <div className="flex flex-wrap items-center gap-2" role="tablist">
        {tabs.map((tab) => (
          <TabButton
            key={tab.key}
            active={activeTab === tab.key}
            onClick={() => setActiveTab(tab.key)}
            label={tab.label}
            icon={tab.icon}
            count={tab.count}
          />
        ))}
      </div>
    </div>
  );

  return (
    <>
      <DetailPageLayout
        breadcrumbs={<ConfigDetailBreadcrumbs config={config} />}
        actions={<ConfigHeaderActions config={config} onReport={() => setReportOpen(true)} />}
        header={<ConfigHeader config={config} />}
        main={
          <div className="flex min-w-0 flex-col gap-4">
            {tabBar}
            {activeTab === "overview" && (
              <OverviewTab config={config} parents={parents} parentsLoading={parentsQuery.isLoading} />
            )}
            {activeTab === "relationships" && (
              <RelationshipsTab
                config={config}
                relationships={relationships}
                isLoading={relationshipsQuery.isLoading}
                error={relationshipsQuery.error}
              />
            )}
            {activeTab === "changes" && (
              <ChangesTab config={config} changes={changes} isLoading={changesQuery.isLoading} error={changesQuery.error} />
            )}
            {activeTab === "insights" && (
              <InsightsTab insights={insights} isLoading={insightsQuery.isLoading} error={insightsQuery.error} />
            )}
            {activeTab === "access" && (
              <AccessTab
                config={config}
                rows={access}
                logs={accessQuery.data?.logs ?? []}
                isLoading={accessQuery.isLoading}
                error={accessQuery.error}
              />
            )}
            {activeTab === "playbooks" && <ConfigPlaybooksTab config={config} />}
            {activeTab === "json" && <JsonTab config={config} />}
            {activePluginTab && (
              <PluginTab
                pluginName={activePluginTab.pluginName}
                tabPath={activePluginTab.tabPath}
                configId={id}
              />
            )}
          </div>
        }
      />
      {reportOpen && (
        <CatalogReportDialog
          open={reportOpen}
          config={config}
          onClose={() => setReportOpen(false)}
        />
      )}
    </>
  );
}

function ConfigDetailBreadcrumbs({ config }: { config: ConfigItem }) {
  return (
    <PageBreadcrumbs
      items={[
        {
          label: config.type || "Resource type",
          href: config.type ? `/ui/type/${encodeURIComponent(config.type)}` : undefined,
          icon: "lucide:chevron-left",
          title: config.type ?? undefined,
          className: "max-w-[20rem]",
        },
        {
          label: config.name || config.id,
          title: config.name || config.id,
          className: "max-w-[24rem]",
        },
      ]}
    />
  );
}

function ConfigHeader({ config }: { config: ConfigItem }) {
  return (
    <EntityHeader
      variant="card"
      titleSize="lg"
      icon={<ConfigIcon primary={config.type} className="h-5 max-w-5 text-xl" />}
      title={config.name || config.id}
      tags={
        <>
          {config.deleted_at && (
            <Badge tone="danger" size="xs" icon="lucide:trash-2">
              Deleted
            </Badge>
          )}
          {isKnownHealth(config.health) && (
            <Badge variant="status" status={healthStatus(config.health)} label={config.health} value={config.status ?? undefined} size="xs" />
          )}
        </>
      }
      description={
        <>
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            {config.type && (
              <a className="font-mono text-xs text-sky-700 hover:underline" href={`/ui/type/${encodeURIComponent(config.type)}`}>
                {config.type}
              </a>
            )}
            {config.config_class && <span>{config.config_class}</span>}
            <span className="font-mono text-xs">{config.id}</span>
          </div>
          {config.description && (
            <p className="mt-2 max-w-4xl">{config.description}</p>
          )}
        </>
      }
    />
  );
}

function ConfigHeaderActions({ config, onReport }: { config: ConfigItem; onReport: () => void }) {
  return (
    <>
      {config.updated_at && <Badge variant="metric" label="Updated" value={timeAgo(config.updated_at)} size="xs" icon="lucide:clock-3" />}
      {config.last_scraped_time && <Badge variant="metric" label="Scraped" value={timeAgo(config.last_scraped_time)} size="xs" icon="lucide:refresh-cw" />}
      <button
        type="button"
        onClick={onReport}
        className="inline-flex h-9 items-center gap-2 rounded-md border border-border bg-background px-3 text-sm font-medium shadow-sm hover:bg-accent/50"
      >
        <Icon name="lucide:file-down" />
        <span>Report</span>
      </button>
    </>
  );
}

function OverviewTab({
  config,
  parents,
  parentsLoading,
}: {
  config: ConfigItem;
  parents: ConfigChildItem[];
  parentsLoading: boolean;
}) {
  const costs = costItems(config);
  const details = useMemo<KeyValueListItem[]>(() => buildDetailItems(config, parents), [config, parents]);
  const labelEntries = useMemo(() => tagEntries(config.labels), [config.labels]);
  const uniqueTagEntries = useMemo(() => dedupeTagEntries(tagEntries(config.tags), labelEntries), [config.tags, labelEntries]);

  return (
    <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
      <div className="flex min-w-0 flex-col gap-4">
        <Section title="Details" icon="lucide:list" defaultOpen>
          <KeyValueList items={details} />
        </Section>
        <Section title="Properties" icon="lucide:sliders-horizontal" defaultOpen summary={String(config.properties?.length ?? 0)}>
          <PropertiesList config={config} />
        </Section>
        <Section title="Labels" icon="lucide:tag" defaultOpen summary={String(labelEntries.length)}>
          <TagGrid entries={labelEntries} emptyLabel="No labels" />
        </Section>
        <Section title="Tags" icon="lucide:tags" defaultOpen summary={String(uniqueTagEntries.length)}>
          <TagGrid entries={uniqueTagEntries} emptyLabel="No unique tags" />
        </Section>
      </div>
      <div className="flex min-w-0 flex-col gap-4">
        <Section title="Locations" icon="lucide:map-pin" defaultOpen summary={parentsLoading ? "..." : String(parents.length)}>
          {parentsLoading ? (
            <div className="text-sm text-muted-foreground">Loading locations...</div>
          ) : parents.length === 0 ? (
            <DetailEmptyState label="No locations" />
          ) : (
            <div className="flex flex-col gap-2">
              {parents.map((parent) => (
                <a key={parent.id} href={`/ui/item/${encodeURIComponent(parent.id)}`} className="rounded-md border border-border p-2 hover:bg-accent/50">
                  <div className="flex items-center gap-2">
                    <ConfigIcon primary={parent.type} className="h-4 max-w-4 shrink-0 text-muted-foreground" />
                    <span className="truncate text-sm font-medium">{parent.name}</span>
                  </div>
                  <div className="mt-1 truncate font-mono text-xs text-muted-foreground">{parent.type}</div>
                </a>
              ))}
              <div className="rounded-md border border-primary/30 bg-primary/5 p-2">
                <div className="flex items-center gap-2">
                  <ConfigIcon primary={config.type} className="h-4 max-w-4 shrink-0 text-primary" />
                  <span className="truncate text-sm font-medium">{config.name || config.id}</span>
                  <Badge size="xxs">Current</Badge>
                </div>
                {config.type && <div className="mt-1 truncate font-mono text-xs text-muted-foreground">{config.type}</div>}
              </div>
            </div>
          )}
        </Section>
        {costs.length > 0 && (
          <Section title="Cost" icon="lucide:coins" defaultOpen>
            <KeyValueList
              items={costs.map(([label, value]) => ({
                key: label,
                label,
                value: currency(value),
              }))}
            />
          </Section>
        )}
      </div>
    </div>
  );
}

function RelationshipsTab({
  config,
  relationships,
  isLoading,
  error,
}: {
  config: ConfigItem;
  relationships?: ConfigRelationshipsResponse;
  isLoading: boolean;
  error: unknown;
}) {
  if (isLoading) return <LoadingState label="Loading relationships" />;
  if (error) return <ErrorState error={error} />;
  if (!relationships) return <DetailEmptyState label="No relationships" />;

  return (
    <RelationshipTree config={config} relationships={relationships} />
  );
}

function RelationshipTree({
  config,
  relationships,
}: {
  config: ConfigItem;
  relationships: ConfigRelationshipsResponse;
}) {
  const incoming = relationshipChildren(relationships.incoming);
  const outgoing = relationshipChildren(relationships.outgoing);
  const total = countRelationshipNodes(incoming) + countRelationshipNodes(outgoing);

  if (total === 0) {
    return <DetailEmptyState icon="lucide:network" label="No relationships" />;
  }

  return (
    <Section title="Relationship Tree" icon="lucide:network" defaultOpen summary={String(total)}>
      <div className="grid min-w-[60rem] grid-cols-[minmax(18rem,1fr)_minmax(20rem,26rem)_minmax(18rem,1fr)] gap-6 overflow-auto py-2">
        <RelationshipBranch
          title="Incoming"
          icon="lucide:arrow-left"
          emptyLabel="No incoming relationships"
          nodes={incoming}
          side="incoming"
        />
        <div className="flex min-w-0 items-center justify-center">
          <CurrentRelationshipCard config={config} />
        </div>
        <RelationshipBranch
          title="Outgoing"
          icon="lucide:arrow-right"
          emptyLabel="No outgoing relationships"
          nodes={outgoing}
          side="outgoing"
        />
      </div>
    </Section>
  );
}

function RelationshipBranch({
  title,
  icon,
  emptyLabel,
  nodes,
  side,
}: {
  title: string;
  icon: string;
  emptyLabel: string;
  nodes: ConfigRelationshipTreeNode[];
  side: "incoming" | "outgoing";
}) {
  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
        <Icon name={icon} />
        <span>{title}</span>
        <Badge size="xxs">{countRelationshipNodes(nodes)}</Badge>
      </div>
      {nodes.length === 0 ? (
        <div className="rounded-md border border-dashed border-border p-3 text-sm text-muted-foreground">{emptyLabel}</div>
      ) : (
        <RelationshipNodeList nodes={nodes} side={side} depth={0} />
      )}
    </div>
  );
}

function RelationshipNodeList({
  nodes,
  side,
  depth,
}: {
  nodes: ConfigRelationshipTreeNode[];
  side: "incoming" | "outgoing";
  depth: number;
}) {
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(() => new Set());
  const grouped = groupRelationshipNodesByType(nodes);
  const shouldGroup = nodes.length > 3;

  if (!shouldGroup) {
    return (
      <div className="flex min-w-0 flex-col gap-2">
        {nodes.map((node) => (
          <RelationshipNodeView key={node.id} node={node} side={side} depth={depth} />
        ))}
      </div>
    );
  }

  return (
    <div className="flex min-w-0 flex-col gap-3">
      {grouped.map((group) => {
        const groupKey = `${depth}:${group.type}`;
        const expanded = expandedGroups.has(groupKey);
        const visibleNodes = expanded ? group.nodes : group.nodes.slice(0, 3);
        const hiddenCount = group.nodes.length - visibleNodes.length;

        return (
          <div key={group.type} className="flex min-w-0 flex-col gap-2">
            <div className="flex min-w-0 items-center gap-2 text-xs font-medium text-muted-foreground">
              <ConfigIcon primary={group.type === UNKNOWN_TYPE ? undefined : group.type} className="h-3.5 max-w-3.5 shrink-0" />
              <span className="min-w-0 truncate font-mono">{group.type}</span>
              <Badge size="xxs">{group.nodes.length}</Badge>
            </div>
            <div className="flex min-w-0 flex-col gap-2">
              {visibleNodes.map((node) => (
                <RelationshipNodeView key={node.id} node={node} side={side} depth={depth} />
              ))}
              {hiddenCount > 0 && (
                <button
                  type="button"
                  className="inline-flex w-fit items-center gap-1 rounded-md border border-dashed border-border px-2 py-1 text-xs text-muted-foreground hover:bg-accent/50"
                  onClick={() => {
                    setExpandedGroups((current) => {
                      const next = new Set(current);
                      next.add(groupKey);
                      return next;
                    });
                  }}
                >
                  <Icon name="lucide:ellipsis" />
                  <span>... and {hiddenCount} more</span>
                </button>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}

function RelationshipNodeView({
  node,
  side,
  depth,
}: {
  node: ConfigRelationshipTreeNode;
  side: "incoming" | "outgoing";
  depth: number;
}) {
  const isRelated = node.edgeType === "related";
  const children = relationshipChildren(node);
  return (
    <div className="min-w-0">
      <div
        className={[
          "relative flex min-w-0 items-start gap-2",
          depth > 0 && "ml-5 border-l pl-4",
          isRelated ? "border-dashed border-violet-300" : "border-border",
        ].filter(Boolean).join(" ")}
      >
        {depth > 0 && (
          <span
            className={[
              "absolute left-0 top-5 h-px w-4 -translate-x-px",
              isRelated ? "border-t border-dashed border-violet-300" : "border-t border-border",
            ].join(" ")}
          />
        )}
        {side === "outgoing" && <RelationshipConnector related={isRelated} />}
        <RelationshipCard item={node} />
        {side === "incoming" && <RelationshipConnector related={isRelated} />}
      </div>
      {children.length > 0 && (
        <div className="mt-2">
          <RelationshipNodeList nodes={children} side={side} depth={depth + 1} />
        </div>
      )}
    </div>
  );
}

function CurrentRelationshipCard({ config }: { config: ConfigItem }) {
  return (
    <div className="w-full rounded-lg border border-border bg-background p-4 shadow-md ring-1 ring-primary/10">
      <div className="flex min-w-0 items-start gap-3">
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md border border-border bg-primary/5">
          <ConfigIcon primary={config.type} className="h-5 max-w-5 text-primary" />
        </div>
        <div className="min-w-0">
          <div className="truncate text-sm font-semibold">{config.name || config.id}</div>
          {config.type && <div className="mt-1 truncate font-mono text-xs text-muted-foreground">{config.type}</div>}
          {config.path && <div className="mt-1 truncate font-mono text-[11px] text-muted-foreground">{config.path}</div>}
          <div className="mt-2 flex flex-wrap items-center gap-1">
            {isKnownHealth(config.health) && (
              <Badge variant="status" status={healthStatus(config.health)} label={config.health} value={config.status ?? undefined} size="xs" />
            )}
            {config.deleted_at && <Badge tone="danger" size="xs" icon="lucide:trash-2">Deleted</Badge>}
          </div>
        </div>
      </div>
    </div>
  );
}

function RelationshipCard({ item }: { item: ConfigRelationshipTreeNode }) {
  const isRelated = item.edgeType === "related";
  const tags = tagEntries(item.tags);
  const tagValues = tags.map(([key, value]) => `${key}=${value}`);
  return (
    <a
      href={`/ui/item/${encodeURIComponent(item.id)}`}
      className={[
        "group flex min-w-0 flex-1 items-center gap-2 rounded-md border bg-background px-2.5 py-1.5 shadow-sm transition hover:bg-accent/50 hover:shadow",
        isRelated ? "border-dashed border-violet-300" : "border-border",
      ].join(" ")}
      title={[item.name || item.id, item.type, item.relation].filter(Boolean).join(" - ")}
    >
      <ConfigIcon
        primary={item.type}
        className={isRelated ? "h-4 max-w-4 shrink-0 text-violet-600" : "h-4 max-w-4 shrink-0 text-muted-foreground"}
      />
      <span className="min-w-0 flex-1 truncate text-left text-sm font-medium group-hover:underline">
        {item.name || item.id}
      </span>
      {tagValues.length > 0 && (
        <TagList
          values={tagValues}
          maxVisible={2}
          emptyLabel=""
          className="max-w-[45%] shrink text-xs"
        />
      )}
      {isKnownHealth(item.health) && (
        <Badge variant="status" status={healthStatus(item.health)} label={item.health} value={item.status ?? undefined} size="xxs" />
      )}
      {item.relation && <Badge size="xxs">{item.relation}</Badge>}
      {isRelated && <span className="h-2 w-2 shrink-0 rounded-full bg-violet-500" title="Related config" />}
    </a>
  );
}

function RelationshipConnector({ related }: { related: boolean }) {
  return (
    <span
      className={[
        "mt-5 h-px w-5 shrink-0",
        related ? "border-t border-dashed border-violet-300" : "border-t border-border",
      ].join(" ")}
    />
  );
}

function relationshipChildren(node?: ConfigRelationshipTreeNode | null): ConfigRelationshipTreeNode[] {
  return node?.children ?? [];
}

const UNKNOWN_TYPE = "Unknown";

function groupRelationshipNodesByType(nodes: ConfigRelationshipTreeNode[]) {
  const groups = new Map<string, ConfigRelationshipTreeNode[]>();
  for (const node of nodes) {
    const type = node.type || UNKNOWN_TYPE;
    const group = groups.get(type);
    if (group) {
      group.push(node);
    } else {
      groups.set(type, [node]);
    }
  }
  return Array.from(groups.entries()).map(([type, groupNodes]) => ({ type, nodes: groupNodes }));
}

function countRelationshipNodes(nodes: ConfigRelationshipTreeNode[]): number {
  return nodes.reduce((count, node) => count + 1 + countRelationshipNodes(relationshipChildren(node)), 0);
}

function ChangesTab({
  config,
  changes,
  isLoading,
  error,
}: {
  config: ConfigItem;
  changes: ConfigChange[];
  isLoading: boolean;
  error: unknown;
}) {
  if (isLoading) return <LoadingState label="Loading changes" />;
  if (error) return <ErrorState error={error} />;
  if (changes.length === 0) return <DetailEmptyState icon="lucide:git-compare" label="No changes" />;

  return (
    <ConfigChangesSection
      changes={changes.map((change) => configChangeToUIChange(change, config))}
      extensions={defaultConfigChangesExtensions}
      hideConfigName
    />
  );
}

function configChangeToUIChange(change: ConfigChange, config: ConfigItem): UIConfigChange {
  const details = asRecord(change.details);
  const typedChange = asRecord(change.typed_change) ?? asRecord(change.typedChange);

  return {
    id: change.id,
    configID: change.config_id,
    configName: change.config_name || config.name,
    configType: change.config_type || config.type,
    permalink: change.permalink,
    changeType: change.change_type || "Change",
    category: change.category,
    severity: normalizeReportSeverity(change.severity),
    source: change.source,
    summary: change.summary,
    diff: change.diff,
    details,
    typedChange: typedChange?.kind ? typedChange as ConfigTypedChange : undefined,
    createdBy: change.created_by ?? undefined,
    externalCreatedBy: change.external_created_by ?? undefined,
    createdAt: change.created_at,
    firstObserved: change.first_observed,
    count: change.count,
    artifacts: change.artifacts?.map((artifact) => ({
      id: artifact.id ?? "",
      filename: artifact.filename ?? "",
      contentType: artifact.content_type ?? "",
      size: artifact.size ?? 0,
      checksum: artifact.checksum,
      path: artifact.path,
      createdAt: artifact.created_at,
      dataUri: artifact.data_uri,
    })),
  };
}

function normalizeReportSeverity(severity?: string | null): UIConfigSeverity | undefined {
  switch (severity) {
    case "critical":
    case "high":
    case "medium":
    case "low":
    case "info":
      return severity;
    default:
      return undefined;
  }
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined;
}

function InsightsTab({ insights, isLoading, error }: { insights: ConfigAnalysis[]; isLoading: boolean; error: unknown }) {
  if (isLoading) return <LoadingState label="Loading insights" />;
  if (error) return <ErrorState error={error} />;
  if (insights.length === 0) return <DetailEmptyState icon="lucide:lightbulb" label="No insights" />;

  const counts = severityCounts(insights);
  const groups = groupInsightsByType(insights);

  return (
    <div className="flex flex-col gap-4">
      <SeveritySummary counts={counts} />
      {groups.map(([type, items]) => (
        <Section key={type} title={titleCase(type)} icon="lucide:lightbulb" defaultOpen summary={String(items.length)}>
          <DataTable
            data={items as unknown as Record<string, unknown>[]}
            columns={insightColumns}
            getRowId={(row, index) => String(row.id ?? index)}
            renderExpandedRow={(row) => (
              <div className="grid gap-3">
                <JsonView data={row.analysis ?? row} />
                {Array.isArray(row.properties) && <PropertiesList properties={row.properties as ConfigItem["properties"]} />}
              </div>
            )}
          />
        </Section>
      ))}
    </div>
  );
}

const insightColumns: DataTableColumn<Record<string, unknown>>[] = [
  { key: "analyzer", label: "Analyzer", render: (value) => <span className="font-medium">{stringifyValue(value)}</span> },
  { key: "summary", label: "Summary", grow: true, render: (value, row) => stringifyValue(value || row.message) },
  { key: "severity", label: "Severity", render: (value) => <Badge tone={severityTone(String(value ?? ""))} size="xs">{stringifyValue(value || "info")}</Badge> },
  { key: "status", label: "Status", render: (value) => value ? <Badge size="xs">{String(value)}</Badge> : "-" },
  { key: "source", label: "Source" },
  { key: "last_observed", label: "Last Observed", render: (value) => timeAgo(String(value || "")), sortValue: (value) => new Date(String(value)).getTime() },
];

function AccessTab({
  config,
  rows,
  logs,
  isLoading,
  error,
}: {
  config: ConfigItem;
  rows: ConfigAccessSummary[];
  logs: ConfigAccessLog[];
  isLoading: boolean;
  error: unknown;
}) {
  if (isLoading) return <LoadingState label="Loading access" />;
  if (error) return <ErrorState error={error} />;
  if (rows.length === 0 && logs.length === 0) {
    return <DetailEmptyState icon="lucide:shield-check" label="No access records" />;
  }

  return (
    <div className="flex flex-col gap-4">
      <Section title="Access Matrix" icon="lucide:grid-3x3" defaultOpen summary={String(rows.length)}>
        <AccessMatrix config={config} rows={rows} />
      </Section>
      <Section title="Access Logs" icon="lucide:history" defaultOpen={false} summary={String(logs.length)}>
        <DataTable
          data={logs as unknown as Record<string, unknown>[]}
          columns={accessLogColumns}
          getRowId={(row, index) => `${row.external_user_id ?? "user"}-${row.created_at ?? index}`}
        />
      </Section>
    </div>
  );
}

function AccessMatrix({ config, rows }: { config: ConfigItem; rows: ConfigAccessSummary[] }) {
  const resource = useMemo(() => buildRBACResource(config, rows), [config, rows]);
  const matrix = useMemo(() => buildGroupedRBACMatrix(resource), [resource]);
  const defaultExpandedGroups = useMemo(
    () => new Set(matrix.groups.filter((group) => group.users.length <= 3).map((group) => group.key)),
    [matrix],
  );
  const roleStats = useMemo(() => buildRoleStats(matrix), [matrix]);
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(() => new Set());
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(() => new Set());

  const toggleGroup = (key: string, expanded: boolean) => {
    setExpandedGroups((current) => {
      const next = new Set(current);
      if (expanded) next.delete(key);
      else next.add(key);
      return next;
    });
    setCollapsedGroups((current) => {
      const next = new Set(current);
      if (expanded) next.add(key);
      else next.delete(key);
      return next;
    });
  };

  const matrixRows: MatrixTableRow[] = matrix.directUsers.flatMap((user) => [
    matrixUserRow(user, matrix.roles),
  ]);
  for (const group of matrix.groups) {
    const expanded = expandedGroups.has(group.key) || (defaultExpandedGroups.has(group.key) && !collapsedGroups.has(group.key));
    matrixRows.push({
      key: group.key,
      label: (
        <MatrixPrincipalLabel
          leading={group.users.length > 0 ? (
            <button
              type="button"
              className="inline-flex h-4 w-4 items-center justify-center rounded hover:bg-muted"
              onClick={() => toggleGroup(group.key, expanded)}
              aria-label={expanded ? `Collapse ${group.groupName}` : `Expand ${group.groupName}`}
            >
              <Icon name={expanded ? "lucide:chevron-down" : "lucide:chevron-right"} className="text-[11px] text-muted-foreground" />
            </button>
          ) : undefined}
        >
          <a href={accessPrincipalHref(NIL_UUID, group.groupId, `group:${group.groupName}`)} className="flex min-w-0 items-center gap-2 hover:underline">
            <Icon name="lucide:users" className="h-4 max-w-4 shrink-0 text-muted-foreground" />
            <div className="flex min-w-0 items-center gap-2">
              <span className="min-w-0 truncate">{group.groupName}</span>
              {group.users.length > 0 && (
                <Badge size="xxs" className="shrink-0">
                  {group.users.length} {group.users.length === 1 ? "user" : "users"}
                </Badge>
              )}
            </div>
          </a>
        </MatrixPrincipalLabel>
      ),
      cells: matrix.roles.map((role) => {
        const row = group.roles.get(role);
        if (!row) return null;
        return (
          <AccessMatrixTooltip content={<AccessCellTooltip principalName={group.groupName} row={row} />}>
            <a href={accessRoleHref(row)} className="inline-flex w-full justify-center">
              <AccessDot indirect />
            </a>
          </AccessMatrixTooltip>
        );
      }),
    });
    if (expanded) {
      matrixRows.push(...group.users.map((user) => matrixUserRow(user, matrix.roles, true)));
    }
  }

  return (
    <div className="space-y-3">
      <MatrixTable
        columns={matrix.roles.map((role) => matrixHeader(role, roleStats.get(role)))}
        rows={matrixRows}
        corner={<RBACMatrixCorner config={config} accessCount={resource.users.length} />}
        emptyMessage="No access matrix data"
        angledHeaders
        density="compact"
        columnWidth={48}
        headerHeight={120}
        rowLabelClassName="min-w-64"
      />
      <RBACMatrixLegend />
    </div>
  );
}

function matrixUserRow(
  user: ReturnType<typeof buildGroupedRBACMatrix>["directUsers"][number],
  roles: string[],
  child = false,
): MatrixTableRow {
  return {
    key: child ? `${user.groupId ?? "group"}:${user.key}` : user.key,
    label: (
      <MatrixPrincipalLabel child={child}>
        <a
          href={accessPrincipalHref(user.userId, user.groupId, child ? undefined : user.roleSource)}
          className="flex min-w-0 items-center gap-2 hover:underline"
        >
          <Icon name={identityIcon(user.userId, child ? undefined : user.roleSource)} className="h-4 max-w-4 shrink-0 text-muted-foreground" />
          <div className="min-w-0">
            <div className="truncate">{user.name}</div>
            {user.email && <div className="truncate text-xs font-normal text-muted-foreground">{user.email}</div>}
          </div>
        </a>
      </MatrixPrincipalLabel>
    ),
    cells: roles.map((role) => {
      const row = user.roles.get(role);
      if (!row) return null;
      const href = accessRoleHref(row);
      return (
        <AccessMatrixTooltip content={<AccessCellTooltip principalName={user.name} row={row} />}>
          <a href={href} className="inline-flex w-full justify-center">
            <AccessDot indirect={isIndirectAccess(row)} />
          </a>
        </AccessMatrixTooltip>
      );
    }),
  };
}

function buildRoleStats(matrix: ReturnType<typeof buildGroupedRBACMatrix>) {
  const stats = new Map<string, { direct: number; groups: number; members: number }>();
  for (const role of matrix.roles) {
    stats.set(role, { direct: 0, groups: 0, members: 0 });
  }
  for (const user of matrix.directUsers) {
    for (const role of user.roles.keys()) {
      const stat = stats.get(role);
      if (stat) stat.direct += 1;
    }
  }
  for (const group of matrix.groups) {
    for (const role of group.roles.keys()) {
      const stat = stats.get(role);
      if (stat) stat.groups += 1;
    }
    for (const user of group.users) {
      for (const role of user.roles.keys()) {
        const stat = stats.get(role);
        if (stat) stat.members += 1;
      }
    }
  }
  return stats;
}

function MatrixPrincipalLabel({
  leading,
  child = false,
  children,
}: {
  leading?: ReactNode;
  child?: boolean;
  children: ReactNode;
}) {
  return (
    <div className={["flex min-w-0 items-center gap-2", child && "pl-6"].filter(Boolean).join(" ")}>
      <span className="inline-flex h-4 w-4 shrink-0 items-center justify-center">
        {leading}
      </span>
      {children}
    </div>
  );
}

function AccessMatrixTooltip({
  children,
  content,
  focusable = false,
}: {
  children: ReactNode;
  content: ReactNode;
  focusable?: boolean;
}) {
  const triggerRef = useRef<HTMLSpanElement>(null);
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState<{ left: number; top: number; placement: "top" | "bottom" } | null>(null);

  const updatePosition = () => {
    const rect = triggerRef.current?.getBoundingClientRect();
    if (!rect) return;

    const maxTooltipWidth = 352;
    const margin = 12;
    const left = Math.min(
      Math.max(rect.left + rect.width / 2, margin + maxTooltipWidth / 2),
      window.innerWidth - margin - maxTooltipWidth / 2,
    );
    const placement = rect.top > 180 ? "top" : "bottom";
    setPosition({
      left,
      top: placement === "top" ? rect.top - 8 : rect.bottom + 8,
      placement,
    });
  };

  const show = () => {
    updatePosition();
    setOpen(true);
  };

  const hide = () => setOpen(false);

  useEffect(() => {
    if (!open) return;
    window.addEventListener("scroll", updatePosition, true);
    window.addEventListener("resize", updatePosition);
    return () => {
      window.removeEventListener("scroll", updatePosition, true);
      window.removeEventListener("resize", updatePosition);
    };
  }, [open]);

  const tooltip = open && position && typeof document !== "undefined" ? createPortal(
    <div
      role="tooltip"
      className="fixed z-[9999] w-max max-w-[22rem] rounded-md border border-border bg-popover px-3 py-2 text-left text-xs text-popover-foreground shadow-xl"
      style={{
        left: position.left,
        top: position.top,
        transform: position.placement === "top" ? "translate(-50%, -100%)" : "translate(-50%, 0)",
      }}
    >
      {content}
      <span
        className={[
          "absolute left-1/2 h-2 w-2 -translate-x-1/2 rotate-45 border-border bg-popover",
          position.placement === "top" ? "-bottom-1 border-b border-r" : "-top-1 border-l border-t",
        ].join(" ")}
      />
    </div>,
    document.body,
  ) : null;

  return (
    <span
      ref={triggerRef}
      className="inline-flex min-w-0 items-center"
      tabIndex={focusable ? 0 : undefined}
      onMouseEnter={show}
      onMouseLeave={hide}
      onFocus={show}
      onBlur={hide}
    >
      {children}
      {tooltip}
    </span>
  );
}

function RBACMatrixCorner({ config, accessCount }: { config: ConfigItem; accessCount: number }) {
  const pathParts = config.path?.split(".").filter(Boolean) ?? [];
  const labelEntries = tagEntries(config.labels);
  const uniqueTagEntries = dedupeTagEntries(tagEntries(config.tags), labelEntries);
  const entries = [...labelEntries, ...uniqueTagEntries];

  return (
    <div className="max-w-sm space-y-2">
      {pathParts.length > 0 && (
        <div className="flex flex-wrap items-center gap-1 text-[11px] font-normal text-muted-foreground">
          {pathParts.map((part, index) => (
            <span key={`${part}-${index}`} className="inline-flex min-w-0 items-center gap-1">
              {index > 0 && <span>/</span>}
              <span className="max-w-24 truncate font-mono">{part}</span>
            </span>
          ))}
        </div>
      )}
      <div className="flex min-w-0 items-center gap-2">
        <ConfigIcon primary={config.type} className="h-4 max-w-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0">
          <div className="truncate font-semibold">{config.name || config.id}</div>
          {config.type && <div className="truncate font-mono text-[11px] font-normal text-muted-foreground">{config.type}</div>}
        </div>
        <Badge variant="metric" label="Access" value={String(accessCount)} size="xs" />
      </div>
      {entries.length > 0 && <TagList values={entries.map(([key, value]) => `${key}=${value}`)} maxVisible={3} />}
    </div>
  );
}

function accessPrincipalHref(userId: string, groupId?: string | null, roleSource?: string) {
  if (userId && userId !== NIL_UUID) return `/ui/access/users/${encodeURIComponent(userId)}`;
  if (roleSource?.startsWith("group:") && groupId) return `/ui/access/groups/${encodeURIComponent(groupId)}`;
  if (!userId || userId === NIL_UUID) return "/ui/access/groups";
  return `/ui/access/users/${encodeURIComponent(userId)}`;
}

function accessRoleHref(row: { userId: string; groupId?: string | null }) {
  if (row.groupId) return `/ui/access/groups/${encodeURIComponent(row.groupId)}`;
  return accessPrincipalHref(row.userId);
}

function matrixHeader(label: string, stats?: { direct: number; groups: number; members: number }) {
  return (
    <AccessMatrixTooltip focusable content={<AccessHeaderTooltip label={label} stats={stats} />}>
      <span className="inline-flex max-w-full cursor-help truncate outline-none">{label}</span>
    </AccessMatrixTooltip>
  );
}

function AccessHeaderTooltip({ label, stats }: { label: string; stats?: { direct: number; groups: number; members: number } }) {
  return (
    <div className="space-y-2">
      <div className="max-w-[20rem] truncate font-semibold text-foreground">{label}</div>
      <div className="flex flex-wrap gap-1.5">
        <Badge variant="metric" size="xxs" label="Direct" value={String(stats?.direct ?? 0)} />
        <Badge variant="metric" size="xxs" label="Groups" value={String(stats?.groups ?? 0)} />
        <Badge variant="metric" size="xxs" label="Members" value={String(stats?.members ?? 0)} />
      </div>
    </div>
  );
}

function AccessCellTooltip({
  row,
  principalName,
}: {
  row: ReturnType<typeof buildRBACResource>["users"][number];
  principalName: string;
}) {
  const accessType = isIndirectAccess(row) ? "Group / indirect" : "Direct";
  const groupName = row.roleSource.startsWith("group:") ? row.roleSource.replace(/^group:/, "") : row.groupName;

  return (
    <div className="space-y-2">
      <div className="max-w-[20rem] truncate font-semibold text-foreground">{principalName}</div>
      <div className="flex flex-wrap gap-1.5">
        {row.role && <Badge size="xxs">{row.role}</Badge>}
        <Badge size="xxs" tone={isIndirectAccess(row) ? "info" : "success"}>{accessType}</Badge>
        {isIndirectAccess(row) && groupName && <Badge size="xxs" icon="lucide:users">{groupName}</Badge>}
      </div>
      <div className="grid gap-1 text-muted-foreground">
        {row.createdAt && <TooltipField label="Granted" value={formatDate(row.createdAt)} />}
        {row.lastReviewedAt && <TooltipField label="Reviewed" value={formatDate(row.lastReviewedAt)} />}
        {row.lastSignedInAt && <TooltipField label="Last sign in" value={formatDate(row.lastSignedInAt)} />}
      </div>
    </div>
  );
}

function TooltipField({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid grid-cols-[4.75rem_minmax(0,1fr)] gap-2">
      <span>{label}</span>
      <span className="min-w-0 truncate font-medium text-foreground">{value}</span>
    </div>
  );
}

function AccessDot({ indirect = false }: { indirect?: boolean }) {
  return (
    <span className="inline-flex h-5 w-full items-center justify-center">
      <span
        className={
          indirect
            ? "h-3 w-3 rounded-full border-2 border-violet-600 bg-transparent"
            : "h-3 w-3 rounded-full bg-blue-600"
        }
      />
    </span>
  );
}

function RBACMatrixLegend() {
  return (
    <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
      <span className="font-medium text-foreground">Legend:</span>
      <span className="inline-flex items-center gap-1"><AccessDot /> Direct</span>
      <span className="inline-flex items-center gap-1"><AccessDot indirect /> Group / indirect</span>
    </div>
  );
}

function identityIcon(userId: string, roleSource?: string) {
  if (roleSource?.startsWith("group:")) return "lucide:users";
  if (/svc[-_]|service[-_]/i.test(userId)) return "lucide:server";
  if (/bot[-_]|automation[-_]|pipeline[-_]/i.test(userId)) return "lucide:bot";
  return "lucide:user";
}

const accessLogColumns: DataTableColumn<Record<string, unknown>>[] = [
  {
    key: "external_users",
    label: "User",
    render: (value) => {
      const user = value as ConfigAccessLog["external_users"];
      if (!user) return "-";
      return (
        <div className="min-w-0">
          <div className="truncate font-medium">{user.name || user.user_email || "-"}</div>
          {user.user_email && <div className="truncate text-xs text-muted-foreground">{user.user_email}</div>}
        </div>
      );
    },
  },
  { key: "created_at", label: "When", render: (value) => formatDate(String(value || "")), sortValue: (value) => new Date(String(value)).getTime() },
  {
    key: "mfa",
    label: "MFA",
    render: (value) => value === null || value === undefined ? "-" : value ? <Badge tone="success" size="xs">Yes</Badge> : <Badge tone="danger" size="xs">No</Badge>,
  },
  {
    key: "properties",
    label: "Properties",
    grow: true,
    render: (value) => <TagGrid compact entries={objectEntries(value as Record<string, unknown> | null)} emptyLabel="-" />,
    filterValue: (value) => objectEntries(value as Record<string, unknown> | null).map(([key, entryValue]) => `${key}=${entryValue}`),
  },
  { key: "count", label: "Count", align: "right" },
];

function JsonTab({ config }: { config: ConfigItem }) {
  return (
    <div className="rounded-md border border-border p-3">
      <JsonView data={parseConfig(config.config)} defaultOpenDepth={2} />
    </div>
  );
}

function PropertiesList({ config, properties }: { config?: ConfigItem; properties?: ConfigItem["properties"] }) {
  const items = properties ?? config?.properties ?? [];
  if (!items || items.length === 0) {
    return <DetailEmptyState label="No properties" />;
  }
  return (
    <KeyValueList
      items={items.map((property, index) => {
        const label = property.label || property.name || `Property ${index + 1}`;
        return {
          key: `${label}-${index}`,
          label,
          value: (
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              {property.icon && <Icon name={property.icon} />}
              <span className="break-words">{stringifyValue(property.text || property.value)}</span>
              {property.links?.map((link, i) => (
                link.url ? (
                  <a key={`${link.url}-${i}`} href={link.url} className="text-sky-700 hover:underline" target="_blank" rel="noreferrer">
                    {link.label || link.url}
                  </a>
                ) : null
              ))}
            </div>
          ),
        };
      })}
    />
  );
}

function objectEntries(value?: Record<string, unknown> | null): Array<[string, string]> {
  return dedupeTagEntries(
    Object.entries(value ?? {})
      .filter(([key]) => key !== "toString")
      .map(([key, entryValue]) => [key, stringifyValue(entryValue)]),
  );
}

function TagGrid({ entries, emptyLabel, compact = false }: { entries: Array<[string, string]>; emptyLabel: ReactNode; compact?: boolean }) {
  if (entries.length === 0) {
    return <span className="text-sm text-muted-foreground">{emptyLabel}</span>;
  }
  return (
    <div className={`flex flex-wrap ${compact ? "gap-1" : "gap-2"}`}>
      {entries.map(([key, value]) => (
        <Badge
          key={`${key}-${value}`}
          variant="label"
          label={key}
          value={value}
          size={compact ? "xxs" : "xs"}
          truncate="auto"
          maxWidth={compact ? "14rem" : "22rem"}
        />
      ))}
    </div>
  );
}

function SeveritySummary({ counts }: { counts: Record<string, number> }) {
  const severities = ["critical", "high", "medium", "low", "info"];
  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
      {severities.map((severity) => (
        <div key={severity} className="rounded-md border border-border p-3">
          <div className="text-xs font-medium capitalize text-muted-foreground">{severity}</div>
          <div className="mt-1 flex items-center justify-between">
            <span className="text-2xl font-semibold">{counts[severity] ?? 0}</span>
            <Badge tone={severityTone(severity)} size="xs">{severity}</Badge>
          </div>
        </div>
      ))}
    </div>
  );
}

function buildDetailItems(config: ConfigItem, parents: ConfigChildItem[]): KeyValueListItem[] {
  const agent = config.agent_name || (!isNilUUID(config.agent_id) ? config.agent_id : "");
  return [
    { key: "id", label: "ID", value: <span className="font-mono text-xs">{config.id}</span> },
    { key: "name", label: "Name", value: config.name },
    { key: "type", label: "Type", value: config.type ?? "-", hidden: !config.type },
    { key: "class", label: "Class", value: config.config_class ?? "-", hidden: !config.config_class || config.type?.endsWith(config.config_class) },
    { key: "status", label: "Status", value: config.status ?? "-", hidden: !config.status },
    { key: "ready", label: "Ready", value: String(config.ready), hidden: config.ready === undefined },
    { key: "source", label: "Source", value: config.source ?? "-", hidden: !config.source },
    { key: "path", label: "Path", value: <span className="break-all font-mono text-xs">{config.path}</span>, hidden: !config.path },
    { key: "parent_id", label: "Parent ID", value: <span className="font-mono text-xs">{config.parent_id}</span>, hidden: !config.parent_id },
    { key: "external_id", label: "External ID", value: config.external_id?.join(", ") ?? "-", hidden: !config.external_id?.length },
    { key: "scraper", label: "Scraper", value: config.scraper_name || config.scraper_id || "-", hidden: !config.scraper_name && !config.scraper_id },
    { key: "agent", label: "Agent", value: agent || "-", hidden: !agent },
    { key: "created", label: "Created", value: `${formatDate(config.created_at)} (${timeAgo(config.created_at)})`, hidden: !config.created_at },
    { key: "updated", label: "Updated", value: `${formatDate(config.updated_at)} (${timeAgo(config.updated_at)})`, hidden: !config.updated_at },
    { key: "scraped", label: "Last Scraped", value: `${formatDate(config.last_scraped_time)} (${timeAgo(config.last_scraped_time)})`, hidden: !config.last_scraped_time },
    { key: "deleted", label: "Deleted", value: `${formatDate(config.deleted_at)} (${timeAgo(config.deleted_at)})`, hidden: !config.deleted_at },
    { key: "delete_reason", label: "Delete Reason", value: config.delete_reason ?? "-", hidden: !config.delete_reason },
    { key: "parents", label: "Parent Labels", value: parents.map((p) => p.name).join(" / "), hidden: parents.length === 0 },
  ];
}

function LoadingState({ label }: { label: string }) {
  return <div className="p-6 text-sm text-muted-foreground">{label}...</div>;
}

function ErrorState({ error }: { error: unknown }) {
  const diagnostics = errorDiagnosticsFromUnknown(error);
  if (!diagnostics) {
    return (
      <div className="rounded-md border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
        Unknown error
      </div>
    );
  }
  return (
    <div className="p-4">
      <ErrorDetails diagnostics={diagnostics} />
    </div>
  );
}

function relationshipCount(relationships?: ConfigRelationshipsResponse) {
  if (!relationships) return undefined;
  return countRelationshipNodes(relationshipChildren(relationships.incoming)) + countRelationshipNodes(relationshipChildren(relationships.outgoing));
}

function parseConfig(config: unknown) {
  if (typeof config !== "string") return config;
  try {
    return JSON.parse(config);
  } catch {
    return config;
  }
}

function currency(value: number) {
  return new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", maximumFractionDigits: 2 }).format(value);
}

function titleCase(value: string) {
  return value.replace(/[-_]/g, " ").replace(/\b\w/g, (match) => match.toUpperCase());
}
