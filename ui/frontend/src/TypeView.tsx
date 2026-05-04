import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Badge,
  DataTable,
  DetailEmptyState,
  ErrorDetails,
  FilterBar,
  Section,
  type DataTableColumn,
  type FilterBarFilter,
  type FilterBarMultiFilterMode,
  type MultiSelectOption,
} from "@flanksource/clicky-ui";
import { errorDiagnosticsFromUnknown } from "./api/http";
import { ConfigIcon } from "./ConfigIcon";
import type { ConfigItem } from "./api/types";
import {
  BASE_GROUP_BY_OPTIONS,
  buildConfigListQuery,
  getConfigLabels,
  getConfigList,
  getConfigStatuses,
  getConfigTags,
  getConfigTypes,
  groupConfigItems,
  healthOptions,
  parseConfigListFilters,
  serializeTriStateParam,
  type ConfigLabelOption,
  type TriStateFilterValue,
} from "./config-list";
import { TagList } from "./config-detail/TagList";
import { DetailPageLayout, EntityHeader } from "./layout/DetailPageLayout";

export type TypeViewProps = {
  configType: string;
};

export function TypeView({ configType }: TypeViewProps) {
  const [searchParams, setSearchParams] = useLocationSearchParams();
  const searchParamsKey = searchParams.toString();
  const filters = useMemo(
    () => parseConfigListFilters(searchParams, configType),
    [configType, searchParamsKey],
  );
  const query = useMemo(() => buildConfigListQuery(filters), [filters]);

  const listQuery = useQuery({
    queryKey: ["config-list", query],
    queryFn: () => getConfigList(query),
  });
  const configTypesQuery = useQuery({
    queryKey: ["config-types"],
    queryFn: getConfigTypes,
    staleTime: 60_000,
  });
  const statusesQuery = useQuery({
    queryKey: ["config-statuses"],
    queryFn: getConfigStatuses,
    staleTime: 60_000,
  });
  const tagsQuery = useQuery({
    queryKey: ["config-tags"],
    queryFn: getConfigTags,
    staleTime: 60_000,
  });
  const labelsQuery = useQuery({
    queryKey: ["config-labels"],
    queryFn: getConfigLabels,
    staleTime: 60_000,
  });

  const configTypeOptions = useMemo(() => {
    const values = new Set([configType, ...(configTypesQuery.data ?? []).map((item) => item.type)].filter(Boolean));
    return Array.from(values)
      .sort((a, b) => a.localeCompare(b))
      .map((value) => ({ value, label: shortConfigTypeLabel(value) }));
  }, [configType, configTypesQuery.data]);

  const labelOptions = useMemo(
    () => configLabelOptions(tagsQuery.data ?? [], labelsQuery.data ?? []),
    [labelsQuery.data, tagsQuery.data],
  );
  const groupByOptions = useMemo(
    () => configGroupByOptions(tagsQuery.data ?? []),
    [tagsQuery.data],
  );
  const statusOptions = useMemo(
    () =>
      (statusesQuery.data ?? [])
        .map((item) => item.status)
        .filter(Boolean)
        .sort((a, b) => a.localeCompare(b))
        .map((value) => ({ value, label: value })),
    [statusesQuery.data],
  );

  const updateParam = useCallback(
    (key: string, value: string | undefined) => {
      setSearchParams((current) => {
        if (value) {
          current.set(key, value);
        } else {
          current.delete(key);
        }
        return current;
      });
    },
    [setSearchParams],
  );

  const filterControls = useMemo<FilterBarFilter[]>(
    () => [
      {
        key: "configType",
        kind: "enum",
        label: "Config Type",
        value: configType,
        options: configTypeOptions,
        onChange: (value) => {
          if (value && value !== configType) navigateToConfigType(value, searchParams);
        },
        disabled: configTypeOptions.length === 0,
      },
      {
        key: "groupBy",
        kind: "select-multi",
        label: "Group By",
        value: filters.groupBy,
        options: groupByOptions,
        placeholder: "None",
        onChange: (value) => updateParam("groupBy", value.length > 0 ? value.join(",") : undefined),
      },
      {
        key: "labels",
        kind: "multi",
        label: "Labels",
        value: filters.labels as Record<string, FilterBarMultiFilterMode>,
        options: labelOptions,
        onChange: (value) => updateParam("labels", serializeTriStateParam(value as TriStateFilterValue)),
        disabled: labelOptions.length === 0,
      },
      {
        key: "status",
        kind: "multi",
        label: "Status",
        value: filters.status as Record<string, FilterBarMultiFilterMode>,
        options: statusOptions,
        onChange: (value) => updateParam("status", serializeTriStateParam(value as TriStateFilterValue)),
        disabled: statusOptions.length === 0,
      },
      {
        key: "health",
        kind: "multi",
        label: "Health",
        value: filters.health as Record<string, FilterBarMultiFilterMode>,
        options: healthOptions(),
        onChange: (value) => updateParam("health", serializeTriStateParam(value as TriStateFilterValue)),
      },
      {
        key: "showDeletedConfigs",
        kind: "boolean",
        label: "Show deleted",
        value: filters.showDeleted,
        onChange: (value) => updateParam("showDeletedConfigs", value ? "true" : undefined),
      },
    ],
    [
      configType,
      configTypeOptions,
      filters.groupBy,
      filters.health,
      filters.labels,
      filters.showDeleted,
      filters.status,
      groupByOptions,
      labelOptions,
      searchParams,
      statusOptions,
      updateParam,
    ],
  );

  const rows = listQuery.data ?? [];
  const groups = useMemo(() => groupConfigItems(rows, filters.groupBy), [filters.groupBy, rows]);
  const total = rows.length;

  return (
    <DetailPageLayout
      header={
        <EntityHeader
          variant="card"
          titleSize="lg"
          icon={<ConfigIcon primary={configType} className="h-5 max-w-5 shrink-0 text-xl" />}
          title={configType}
          description={total === 1 ? "1 config item" : `${total} config items`}
        />
      }
      main={
        <div className="flex min-w-0 flex-col gap-4">
          <FilterBar
            search={{
              value: filters.search,
              onChange: (value) => updateParam("search", value.trim() ? value : undefined),
              placeholder: "Search configs",
              ariaLabel: "Search configs",
            }}
            filters={filterControls}
          />

          {listQuery.isLoading && (
            <div className="text-sm text-muted-foreground">Loading...</div>
          )}
          {listQuery.error && (() => {
            const diagnostics = errorDiagnosticsFromUnknown(listQuery.error) ?? { message: "Failed to load config items", context: [] };
            return <ErrorDetails diagnostics={diagnostics} />;
          })()}
          {!listQuery.isLoading && !listQuery.error && rows.length === 0 && (
            <DetailEmptyState label="No matching config items" />
          )}
          {!listQuery.isLoading && !listQuery.error && rows.length > 0 && (
            <ConfigGroups groups={groups} />
          )}
        </div>
      }
    />
  );
}

function ConfigGroups({ groups }: { groups: ReturnType<typeof groupConfigItems> }) {
  if (groups.length === 1 && groups[0].label === "") {
    return <ConfigTable rows={groups[0].rows} />;
  }

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {groups.map((group) => (
        <Section
          key={group.key}
          title={group.label}
          summary={<Badge size="xs">{group.rows.length}</Badge>}
          defaultOpen
        >
          <ConfigTable rows={group.rows} />
        </Section>
      ))}
    </div>
  );
}

function ConfigTable({ rows }: { rows: ConfigItem[] }) {
  return (
    <DataTable
      data={rows as unknown as Record<string, unknown>[]}
      columns={configColumns}
      getRowId={(row) => String(row.id)}
      getRowHref={(row) => `/ui/item/${encodeURIComponent(String(row.id))}`}
      showGlobalFilter={false}
      defaultSort={{ key: "name", dir: "asc" }}
      emptyMessage="No matching config items"
    />
  );
}

const configColumns: DataTableColumn<Record<string, unknown>>[] = [
  {
    key: "name",
    label: "Name",
    grow: true,
    render: (_value, row) => {
      const item = row as unknown as ConfigItem;
      return (
        <span className="inline-flex min-w-0 items-center gap-2">
          <ConfigIcon primary={item.type} secondary={item.config_class} className="h-4 max-w-4 shrink-0" />
          <span className="truncate font-medium">{item.name}</span>
        </span>
      );
    },
    filterValue: (_value, row) => {
      const item = row as unknown as ConfigItem;
      return [item.name, item.type, item.namespace, item.status, item.health]
        .filter((value): value is string => typeof value === "string" && value.length > 0);
    },
  },
  {
    key: "status",
    label: "Status",
    shrink: true,
    render: (value) => renderBadge(value, "neutral"),
  },
  {
    key: "health",
    label: "Health",
    shrink: true,
    render: (value) => renderHealth(value),
  },
  {
    key: "namespace",
    label: "Namespace",
    render: (value) => renderMuted(value),
  },
  {
    key: "tags",
    label: "Tags",
    grow: true,
    render: (value) => <TagList values={tagEntries(value as ConfigItem["tags"])} maxVisible={2} />,
    filterValue: (value) => tagEntries(value as ConfigItem["tags"]),
  },
  {
    key: "agent_name",
    label: "Agent",
    render: (value, row) => renderMuted(value || (row as unknown as ConfigItem).agent_id),
  },
  {
    key: "updated_at",
    label: "Updated",
    render: (value) => renderMuted(formatDate(value)),
    sortValue: (value) => new Date(String(value ?? "")).getTime() || 0,
  },
];

function useLocationSearchParams(): [
  URLSearchParams,
  (updater: (current: URLSearchParams) => URLSearchParams) => void,
] {
  const [search, setSearch] = useState(() => window.location.search);

  useEffect(() => {
    const onPopState = () => setSearch(window.location.search);
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  const setSearchParams = useCallback((updater: (current: URLSearchParams) => URLSearchParams) => {
    const next = updater(new URLSearchParams(window.location.search));
    const query = next.toString();
    const url = `${window.location.pathname}${query ? `?${query}` : ""}`;
    window.history.replaceState(null, "", url);
    setSearch(window.location.search);
  }, []);

  return [useMemo(() => new URLSearchParams(search), [search]), setSearchParams];
}

function navigateToConfigType(configType: string, currentParams: URLSearchParams) {
  const nextParams = new URLSearchParams(currentParams);
  nextParams.delete("configType");
  const query = nextParams.toString();
  const href = `/ui/type/${encodeURIComponent(configType)}${query ? `?${query}` : ""}`;
  window.history.pushState(null, "", href);
  window.dispatchEvent(new PopStateEvent("popstate"));
}

function configLabelOptions(tags: ConfigLabelOption[], labels: ConfigLabelOption[]): MultiSelectOption[] {
  const seen = new Set<string>();
  const options: MultiSelectOption[] = [];
  for (const item of [...tags, ...labels]) {
    const key = String(item.key ?? "").trim();
    const value = String(item.value ?? "").trim();
    if (!key || !value) continue;
    const id = `${key}____${value}`;
    if (seen.has(id)) continue;
    seen.add(id);
    options.push({ value: id, label: `${key}: ${value}` });
  }
  return options.sort((a, b) => String(a.label).localeCompare(String(b.label)));
}

function configGroupByOptions(tags: ConfigLabelOption[]): MultiSelectOption[] {
  const tagKeys = Array.from(new Set(tags.map((tag) => String(tag.key ?? "").trim()).filter(Boolean)))
    .sort((a, b) => a.localeCompare(b))
    .map((key) => ({ value: `${key}__tag`, label: key }));

  return [...BASE_GROUP_BY_OPTIONS, ...tagKeys];
}

function shortConfigTypeLabel(value: string) {
  const parts = value.split("::").filter(Boolean);
  if (parts.length <= 1) return value;
  return parts.slice(1).join(" ");
}

function tagEntries(tags: ConfigItem["tags"]): string[] {
  return Object.entries(tags ?? {}).map(([key, value]) => `${key}=${value}`);
}

function renderHealth(value: unknown): ReactNode {
  const text = String(value ?? "").trim();
  if (!text) return <span className="text-muted-foreground">-</span>;
  const tone = text === "healthy" ? "success" : text === "warning" ? "warning" : text === "unhealthy" ? "danger" : "neutral";
  return <Badge size="xs" tone={tone}>{text}</Badge>;
}

function renderBadge(value: unknown, tone: "neutral" | "success" | "danger" | "warning" | "info"): ReactNode {
  const text = String(value ?? "").trim();
  if (!text) return <span className="text-muted-foreground">-</span>;
  return <Badge size="xs" tone={tone}>{text}</Badge>;
}

function renderMuted(value: unknown): ReactNode {
  const text = String(value ?? "").trim();
  return text ? <span className="text-muted-foreground">{text}</span> : <span className="text-muted-foreground">-</span>;
}

function formatDate(value: unknown): string {
  const text = String(value ?? "");
  if (!text) return "";
  const date = new Date(text);
  return Number.isNaN(date.getTime()) ? text : date.toLocaleString();
}
