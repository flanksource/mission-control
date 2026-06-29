import { useEffect, useMemo, useRef, useState } from "react";
import { Badge, Icon, Modal, cn } from "@flanksource/clicky-ui";
import { useQuery } from "@tanstack/react-query";
import { ConfigIcon } from "./ConfigIcon";
import {
  searchResources,
  emptySelectedResources,
  type SearchedResource,
  type SearchResourceType,
  type SearchResourcesRequest,
  type SelectedResources,
  SEARCH_DEFAULT_LIMIT,
} from "./api/search";
import { getConfigChangeConfigMappings, getConfigsByIDs } from "./api/configs";
import { getAgentByIDs, isLocalAgent } from "./api/agents";
import {
  addRecent,
  useRecents,
  type RecentItem,
  type RecentKind,
} from "./lib/recents";
import { formatRelativeTime } from "./lib/relative-time";
import type { ConfigItem } from "./api/types";

type CommandPaletteProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onNavigate: (href: string) => void;
};

type SearchTypeOption = {
  key: SearchResourceType;
  label: string;
  iconName: string;
};

const SEARCH_TYPE_OPTIONS: SearchTypeOption[] = [
  { key: "configs", label: "Config", iconName: "lucide:database" },
  { key: "canaries", label: "Canary", iconName: "lucide:heart-pulse" },
  { key: "checks", label: "Check", iconName: "lucide:list-checks" },
  { key: "config_changes", label: "Change", iconName: "lucide:git-compare-arrows" },
  { key: "playbooks", label: "Playbook", iconName: "lucide:book-open-check" },
  { key: "connections", label: "Connection", iconName: "lucide:cable" },
];

// The #kind directive aliases supported by the query parser. Anything
// in the lookup is treated as a directive token; anything else falls
// through to the underlying PEG grammar.
const SEARCH_DIRECTIVE_RESOURCE_TYPE_MAP: Record<string, SearchResourceType> = {
  config: "configs",
  configs: "configs",
  canary: "canaries",
  canaries: "canaries",
  check: "checks",
  checks: "checks",
  change: "config_changes",
  changes: "config_changes",
  config_change: "config_changes",
  config_changes: "config_changes",
  "config-change": "config_changes",
  "config-changes": "config_changes",
  playbook: "playbooks",
  playbooks: "playbooks",
  connection: "connections",
  connections: "connections",
};

const SUGGESTED_SEARCH_QUERIES = [
  "type=ingress #config",
  "change_type=OOMKilled #change",
  "prometheus #config,change,connections",
  "labels.app=cert-manager #config",
  "health=unhealthy,warning #config",
  "type=postgres #connection",
];

const SEARCH_HISTORY_STORAGE_KEY = "mc.globalSearchHistory.v1";
const SEARCH_ENABLED_TYPES_STORAGE_KEY = "mc.globalSearchEnabledTypes.v1";
const SEARCH_HISTORY_LIMIT = 5;

type EnabledSearchTypeState = Record<SearchResourceType, boolean>;

type ParsedSearchQuery = {
  queryWithoutDirectives: string;
  directiveSearchTypes: SearchResourceType[];
};

type FlattenedSearchResult = {
  key: string;
  value: string;
  href: string;
  title: string;
  description: string;
  resourceType: SearchResourceType;
  resource: SearchedResource;
  indentLevel?: number;
};

function createDisabledSearchTypesState(): EnabledSearchTypeState {
  return Object.fromEntries(
    SEARCH_TYPE_OPTIONS.map(({ key }) => [key, false]),
  ) as EnabledSearchTypeState;
}

function getDefaultEnabledSearchTypes(): EnabledSearchTypeState {
  return {
    configs: true,
    canaries: true,
    checks: false,
    config_changes: false,
    playbooks: false,
    connections: false,
  };
}

// parseSearchQuery strips out the `#kind[,kind2]` directive tokens (e.g.
// `#config,change,connections`) from a query and returns the rest of the
// string plus the set of resource kinds the user asked to scope to.
function parseSearchQuery(query: string): ParsedSearchQuery {
  const directiveSearchTypes = new Set<SearchResourceType>();

  const queryWithoutDirectives = query
    .replace(/(^|\s)#([^\s]+)/g, (_match, prefix: string, directiveValue: string) => {
      directiveValue.split(",").forEach((rawDirectiveToken) => {
        const normalizedDirectiveToken = rawDirectiveToken.trim().toLowerCase();
        if (!normalizedDirectiveToken) return;

        const resourceType =
          SEARCH_DIRECTIVE_RESOURCE_TYPE_MAP[normalizedDirectiveToken];
        if (resourceType) {
          directiveSearchTypes.add(resourceType);
        }
      });
      return prefix;
    })
    .replace(/\s+/g, " ")
    .trim();

  return {
    queryWithoutDirectives,
    directiveSearchTypes: Array.from(directiveSearchTypes),
  };
}

function getEnabledSearchTypesFromDirective(
  directiveSearchTypes: SearchResourceType[],
): EnabledSearchTypeState {
  const next = createDisabledSearchTypesState();
  directiveSearchTypes.forEach((resourceType) => {
    next[resourceType] = true;
  });
  return next;
}

function isEnabledSearchTypeStateEqual(
  left: EnabledSearchTypeState,
  right: EnabledSearchTypeState,
): boolean {
  return SEARCH_TYPE_OPTIONS.every(({ key }) => left[key] === right[key]);
}

function getStoredSearchHistory(): string[] {
  if (typeof window === "undefined") return [];
  try {
    const raw = window.localStorage.getItem(SEARCH_HISTORY_STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed
      .filter((item): item is string => typeof item === "string")
      .map((item) => item.trim())
      .filter(Boolean)
      .slice(0, SEARCH_HISTORY_LIMIT);
  } catch {
    return [];
  }
}

function persistSearchHistory(query: string): string[] {
  if (typeof window === "undefined") return [];
  const trimmed = query.trim();
  if (!trimmed) return getStoredSearchHistory();
  const normalized = trimmed.toLowerCase();
  const existing = getStoredSearchHistory();
  const next = [
    trimmed,
    ...existing.filter((item) => item.toLowerCase() !== normalized),
  ].slice(0, SEARCH_HISTORY_LIMIT);
  try {
    window.localStorage.setItem(
      SEARCH_HISTORY_STORAGE_KEY,
      JSON.stringify(next),
    );
  } catch {
    // Ignore storage write failures.
  }
  return next;
}

function removeSearchHistoryItem(queryToRemove: string): string[] {
  if (typeof window === "undefined") return [];
  const normalized = queryToRemove.trim().toLowerCase();
  const existing = getStoredSearchHistory();
  const next = existing.filter((item) => item.toLowerCase() !== normalized);
  try {
    window.localStorage.setItem(
      SEARCH_HISTORY_STORAGE_KEY,
      JSON.stringify(next),
    );
  } catch {
    // Ignore storage write failures.
  }
  return next;
}

function getStoredEnabledSearchTypes(): EnabledSearchTypeState {
  if (typeof window === "undefined") return getDefaultEnabledSearchTypes();
  const defaults = getDefaultEnabledSearchTypes();
  try {
    const raw = window.localStorage.getItem(SEARCH_ENABLED_TYPES_STORAGE_KEY);
    if (!raw) return defaults;
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object") return defaults;
    for (const { key } of SEARCH_TYPE_OPTIONS) {
      const storedValue = (parsed as Record<string, unknown>)[key];
      if (typeof storedValue === "boolean") {
        defaults[key] = storedValue;
      }
    }
    return defaults;
  } catch {
    return defaults;
  }
}

function persistEnabledSearchTypes(state: EnabledSearchTypeState) {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(
      SEARCH_ENABLED_TYPES_STORAGE_KEY,
      JSON.stringify(state),
    );
  } catch {
    // Ignore storage write failures.
  }
}

function buildSearchRequest(
  queryWithoutDirectives: string,
  enabledSearchTypes: EnabledSearchTypeState,
): SearchResourcesRequest | null {
  const trimmed = queryWithoutDirectives.trim();
  const hasEnabledType = Object.values(enabledSearchTypes).some(Boolean);
  if (trimmed.length < 2 || !hasEnabledType) return null;

  const request: SearchResourcesRequest = {
    limit: SEARCH_DEFAULT_LIMIT,
  };

  if (enabledSearchTypes.configs) {
    request.configs = [{ search: trimmed, agent: "all" }];
  }
  if (enabledSearchTypes.canaries) {
    request.canaries = [{ search: trimmed, agent: "all" }];
  }
  if (enabledSearchTypes.checks) {
    request.checks = [{ search: trimmed, agent: "all" }];
  }
  if (enabledSearchTypes.config_changes) {
    request.config_changes = [{ search: trimmed }];
  }
  if (enabledSearchTypes.playbooks) {
    request.playbooks = [{ search: trimmed }];
  }
  if (enabledSearchTypes.connections) {
    request.connections = [{ search: trimmed }];
  }

  return request;
}

function getResourceHref(
  type: SearchResourceType,
  item: SearchedResource,
): string {
  switch (type) {
    case "configs":
      return `/ui/item/${encodeURIComponent(item.id)}`;
    case "canaries":
      return `/ui/settings/canaries/${encodeURIComponent(item.id)}`;
    case "checks":
      return `/ui/health?checkId=${encodeURIComponent(item.id)}&timeRange=1h`;
    case "config_changes":
      return `/ui/catalog/changes?changeId=${encodeURIComponent(item.id)}`;
    case "playbooks":
      return `/ui/playbooks/runs?playbook=${encodeURIComponent(item.id)}`;
    case "connections":
      return `/ui/settings/connections?id=${encodeURIComponent(item.id)}`;
  }
}

function getResourceTitle(type: SearchResourceType, item: SearchedResource): string {
  if (type === "config_changes") {
    return item.summary || item.name || item.id;
  }
  return item.name || item.id;
}

function sortTagEntries(entries: [string, string][]): [string, string][] {
  const priority: Record<string, number> = {
    cluster: 0,
    account: 1,
    region: 2,
    namespace: 3,
    zone: 4,
  };
  return entries.sort(([a], [b]) => {
    const pa = priority[a.toLowerCase()] ?? Number.MAX_SAFE_INTEGER;
    const pb = priority[b.toLowerCase()] ?? Number.MAX_SAFE_INTEGER;
    if (pa !== pb) return pa - pb;
    return a.localeCompare(b);
  });
}

function getConfigTagEntries(item: SearchedResource): [string, string][] {
  if (!item.tags || typeof item.tags !== "object") return [];
  return sortTagEntries(
    Object.entries(item.tags).filter(([key]) => key !== "toString"),
  );
}

function getResourceDescription(
  type: SearchResourceType,
  item: SearchedResource,
): string {
  if (type === "config_changes") {
    return item.change_type || item.type || "";
  }
  if (type === "configs") {
    const parts: string[] = [item.type].filter(Boolean);
    const tagEntries = getConfigTagEntries(item);
    if (tagEntries.length > 0) {
      tagEntries.forEach(([key, value]) => {
        parts.push(`${key}=${value}`);
      });
    } else if (item.namespace) {
      parts.unshift(item.namespace);
    }
    return parts.join(" · ");
  }
  return [item.namespace, item.type].filter(Boolean).join(" · ");
}

function getRecentKindForResourceType(
  type: SearchResourceType,
): RecentKind | undefined {
  switch (type) {
    case "configs":
      return "config";
    case "canaries":
      return "canary";
    case "checks":
      return "check";
    case "config_changes":
      return "config_change";
    case "playbooks":
      return "playbook";
    case "connections":
      return "connection";
  }
}

function getSearchTypeBadgeClass(searchType: SearchResourceType): string {
  switch (searchType) {
    case "configs":
      return "border-blue-100 bg-blue-50 text-blue-700";
    case "canaries":
      return "border-amber-100 bg-amber-50 text-amber-700";
    case "checks":
      return "border-emerald-100 bg-emerald-50 text-emerald-700";
    case "config_changes":
      return "border-pink-100 bg-pink-50 text-pink-700";
    case "playbooks":
      return "border-violet-100 bg-violet-50 text-violet-700";
    case "connections":
      return "border-cyan-100 bg-cyan-50 text-cyan-700";
  }
}

function getShortcutHint(): string {
  if (typeof navigator === "undefined") return "Ctrl+K";
  return /Mac|iPhone|iPad|iPod/.test(navigator.platform) ? "⌘K" : "Ctrl+K";
}

function renderResultIcon(result: FlattenedSearchResult) {
  switch (result.resourceType) {
    case "configs":
      return (
        <ConfigIcon
          primary={result.resource.type}
          className="h-4 w-4 text-muted-foreground"
        />
      );
    case "config_changes":
      return (
        <ConfigIcon
          primary={result.resource.type}
          className="h-4 w-4 text-muted-foreground"
        />
      );
    case "connections": {
      return (
        <span className="flex h-5 w-5 items-center justify-center text-muted-foreground">
          <Icon name="lucide:cable" />
        </span>
      );
    }
    case "playbooks":
      return (
        <span className="flex h-5 w-5 items-center justify-center text-muted-foreground">
          <Icon
            name={
              result.resource.icon
                ? `mi:${result.resource.icon}`
                : "lucide:book-open-check"
            }
          />
        </span>
      );
    case "canaries":
      return (
        <span className="flex h-5 w-5 items-center justify-center text-muted-foreground">
          <Icon name="lucide:heart-pulse" />
        </span>
      );
    case "checks":
      return (
        <span className="flex h-5 w-5 items-center justify-center text-muted-foreground">
          <Icon name="lucide:list-checks" />
        </span>
      );
  }
}

export function CommandPalette({
  open,
  onOpenChange,
  onNavigate,
}: CommandPaletteProps) {
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const [enabledSearchTypes, setEnabledSearchTypes] =
    useState<EnabledSearchTypeState>(() => getStoredEnabledSearchTypes());
  const [searchHistory, setSearchHistory] = useState<string[]>([]);
  const inputRef = useRef<HTMLInputElement>(null);
  const debounced = useDebouncedValue(query, 300);

  const parsedDebounced = useMemo(
    () => parseSearchQuery(debounced),
    [debounced],
  );
  const parsedQuery = useMemo(() => parseSearchQuery(query), [query]);

  const searchRequest = useMemo(
    () =>
      buildSearchRequest(
        parsedDebounced.queryWithoutDirectives,
        enabledSearchTypes,
      ),
    [enabledSearchTypes, parsedDebounced.queryWithoutDirectives],
  );

  const searchQuery = useQuery<SelectedResources | null>({
    queryKey: ["global-search", searchRequest],
    queryFn: () => searchResources(searchRequest!),
    enabled: open && searchRequest != null,
    staleTime: 30_000,
  });

  const results = searchQuery.data ?? emptySelectedResources();

  const configChangeIds = useMemo(
    () => (results.config_changes ?? []).map((c) => c.id).filter(Boolean),
    [results.config_changes],
  );

  const configChangeMappingsQuery = useQuery({
    queryKey: ["global-search", "config-change-mappings", configChangeIds],
    queryFn: async () => {
      try {
        return await getConfigChangeConfigMappings(configChangeIds);
      } catch {
        return [];
      }
    },
    enabled: open && configChangeIds.length > 0,
    staleTime: 60_000,
  });

  const configIdByChangeId = useMemo(() => {
    const entries: [string, string | undefined][] = (
      results.config_changes ?? []
    ).map((change) => [change.id, change.config_id]);

    (configChangeMappingsQuery.data ?? []).forEach((mapping) => {
      if (mapping.config_id) {
        entries.push([mapping.id, mapping.config_id]);
      }
    });

    return new Map(
      entries.filter(
        (entry): entry is [string, string] => Boolean(entry[1]),
      ),
    );
  }, [configChangeMappingsQuery.data, results.config_changes]);

  const configIds = useMemo(
    () => Array.from(new Set(Array.from(configIdByChangeId.values()))),
    [configIdByChangeId],
  );

  const configsByChangeQuery = useQuery({
    queryKey: ["global-search", "config-change-configs", configIds],
    queryFn: async () => {
      try {
        return await getConfigsByIDs(configIds);
      } catch {
        return [];
      }
    },
    enabled: open && configIds.length > 0,
    staleTime: 60_000,
  });

  const configById = useMemo(
    () => new Map((configsByChangeQuery.data ?? []).map((c) => [c.id, c])),
    [configsByChangeQuery.data],
  );

  const nonLocalAgentIds = useMemo(() => {
    const ids = new Set<string>();
    for (const option of SEARCH_TYPE_OPTIONS) {
      const resources = results[option.key] ?? [];
      resources.forEach((item) => {
        if (!isLocalAgent(item.agent)) ids.add(item.agent);
      });
    }
    return Array.from(ids);
  }, [results]);

  const agentsQuery = useQuery({
    queryKey: ["global-search", "agents", nonLocalAgentIds],
    queryFn: async () => {
      const items = await getAgentByIDs(nonLocalAgentIds);
      return new Map(items.map((agent) => [agent.id, agent.name]));
    },
    enabled: open && nonLocalAgentIds.length > 0,
    staleTime: 5 * 60_000,
  });

  const agentNamesMap = agentsQuery.data ?? new Map<string, string>();

  // ⌘K / Ctrl+K to open
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (
        (event.metaKey || event.ctrlKey) &&
        event.key.toLowerCase() === "k"
      ) {
        event.preventDefault();
        onOpenChange(true);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onOpenChange]);

  // Reset state when opening, and focus the input.
  useEffect(() => {
    if (!open) return;
    setActiveIndex(0);
    setSearchHistory(getStoredSearchHistory());
    window.setTimeout(() => inputRef.current?.focus(), 0);
  }, [open]);

  useEffect(() => {
    setActiveIndex(0);
  }, [debounced]);

  useEffect(() => {
    persistEnabledSearchTypes(enabledSearchTypes);
  }, [enabledSearchTypes]);

  // Persist a debounced query into the search history once it returns at
  // least one meaningful result. Mirrors flanksource-ui behavior: only
  // committed strings (no mid-typing flicker) make it in.
  useEffect(() => {
    const trimmed = debounced.trim();
    if (!trimmed || trimmed.length < 2) return;
    if (!searchRequest) return;
    // Wait until the query is settled to avoid storing in-flight noise.
    if (searchQuery.isFetching) return;
    setSearchHistory(persistSearchHistory(trimmed));
  }, [debounced, searchQuery.isFetching, searchRequest]);

  const applyDirectiveSearchTypes = (inputQuery: string) => {
    const { directiveSearchTypes } = parseSearchQuery(inputQuery);
    if (directiveSearchTypes.length === 0) return;
    const next = getEnabledSearchTypesFromDirective(directiveSearchTypes);
    setEnabledSearchTypes((previous) => {
      if (isEnabledSearchTypeStateEqual(previous, next)) return previous;
      return next;
    });
  };

  const handleQueryChange = (nextQuery: string) => {
    applyDirectiveSearchTypes(nextQuery);
    setQuery(nextQuery);
  };

  const selectSearchQuery = (selectedQuery: string) => {
    applyDirectiveSearchTypes(selectedQuery);
    setQuery(selectedQuery);
  };

  const recentItems = useRecents();
  const showRecents =
    parsedQuery.queryWithoutDirectives.length < 2 &&
    parsedQuery.directiveSearchTypes.length === 0 &&
    recentItems.length > 0;

  // Build the flat list of result rows. Config changes are grouped under
  // their owning config so the user sees the asset, not a wall of diffs.
  const flattenedResults = useMemo<FlattenedSearchResult[]>(() => {
    if (!searchRequest) return [];
    if (searchQuery.error) return [];

    const entries: FlattenedSearchResult[] = [];

    for (const option of SEARCH_TYPE_OPTIONS) {
      const searchType = option.key;
      if (!enabledSearchTypes[searchType]) continue;
      const resources = results[searchType] ?? [];

      if (searchType !== "config_changes") {
        resources.forEach((item, index) => {
          const title = getResourceTitle(searchType, item);
          const description = getResourceDescription(searchType, item);
          entries.push({
            key: `${searchType}-${item.id}-${index}`,
            value: `${searchType}-${item.id}-${title}-${description}-${index}`,
            href: getResourceHref(searchType, item),
            title,
            description,
            resourceType: searchType,
            resource: item,
          });
        });
        continue;
      }

      // Group changes by their parent config
      const changesByConfig = new Map<string, SearchedResource[]>();
      const ungroupedChanges: SearchedResource[] = [];

      resources.forEach((change) => {
        const configId =
          change.config_id || configIdByChangeId.get(change.id);
        if (!configId) {
          ungroupedChanges.push(change);
          return;
        }
        const bucket = changesByConfig.get(configId) ?? [];
        bucket.push({ ...change, config_id: configId });
        changesByConfig.set(configId, bucket);
      });

      changesByConfig.forEach((changesForConfig, configId) => {
        const config: ConfigItem | undefined = configById.get(configId);
        const configResource: SearchedResource = {
          id: configId,
          name: config?.name || configId,
          type: config?.type || "",
          namespace: config?.namespace || "",
          agent: config?.agent_id || "",
          labels: {},
        };

        const configTitle = getResourceTitle("configs", configResource);
        const configDescription = getResourceDescription(
          "configs",
          configResource,
        );

        entries.push({
          key: `config-change-group-${configId}`,
          value: `config-change-group-${configId}-${configTitle}`,
          href: getResourceHref("configs", configResource),
          title: configTitle,
          description: configDescription,
          resourceType: "configs",
          resource: configResource,
        });

        changesForConfig.forEach((change, index) => {
          const title = getResourceTitle(searchType, change);
          const description = getResourceDescription(searchType, change);
          entries.push({
            key: `${searchType}-${configId}-${change.id}-${index}`,
            value: `${searchType}-${configId}-${change.id}-${title}-${description}-${index}`,
            href: getResourceHref(searchType, change),
            title,
            description,
            resourceType: searchType,
            resource: change,
            indentLevel: 1,
          });
        });
      });

      ungroupedChanges.forEach((item, index) => {
        const title = getResourceTitle(searchType, item);
        const description = getResourceDescription(searchType, item);
        entries.push({
          key: `${searchType}-ungrouped-${item.id}-${index}`,
          value: `${searchType}-ungrouped-${item.id}-${title}-${description}-${index}`,
          href: getResourceHref(searchType, item),
          title,
          description,
          resourceType: searchType,
          resource: item,
        });
      });
    }

    return entries;
  }, [
    configById,
    configIdByChangeId,
    enabledSearchTypes,
    results,
    searchQuery.error,
    searchRequest,
  ]);

  // Keep the active index within bounds as results change.
  useEffect(() => {
    if (activeIndex >= flattenedResults.length) {
      setActiveIndex(Math.max(0, flattenedResults.length - 1));
    }
  }, [activeIndex, flattenedResults.length]);

  const showSuggestions = parsedDebounced.queryWithoutDirectives.length < 2;
  const emptyResults =
    !searchQuery.isFetching &&
    !searchQuery.error &&
    searchRequest != null &&
    flattenedResults.length === 0;

  const chooseResult = (result: FlattenedSearchResult | undefined) => {
    if (!result) return;
    onOpenChange(false);
    setQuery("");

    const kind = getRecentKindForResourceType(result.resourceType);
    if (kind) {
      addRecent({
        kind,
        id: result.resource.id,
        name: result.title,
        type: result.resource.type,
        href: result.href,
      });
    }

    onNavigate(result.href);
  };

  const chooseRecent = (item: RecentItem) => {
    onOpenChange(false);
    setQuery("");
    addRecent({
      kind: item.kind,
      id: item.id,
      name: item.name,
      type: item.type,
      icon: item.icon,
      href: item.href,
    });
    onNavigate(item.href);
  };

  const handleInputKeyDown = (
    event: React.KeyboardEvent<HTMLInputElement>,
  ) => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      if (showRecents) return;
      setActiveIndex((index) =>
        Math.min(index + 1, Math.max(flattenedResults.length - 1, 0)),
      );
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      if (showRecents) return;
      setActiveIndex((index) => Math.max(index - 1, 0));
    } else if (event.key === "Enter") {
      event.preventDefault();
      chooseResult(flattenedResults[activeIndex]);
    }
  };

  return (
    <Modal
      open={open}
      onClose={() => onOpenChange(false)}
      hideClose
      size="full"
      className="max-h-[82vh] max-w-3xl overflow-hidden [&>*]:min-h-0"
      headerSlot={
        <div className="flex w-full flex-col gap-0">
          <div className="flex min-w-0 items-center gap-2 px-4 py-3">
            <Icon
              name="lucide:search"
              className="shrink-0 text-muted-foreground"
            />
            <input
              ref={inputRef}
              value={query}
              onChange={(event) => handleQueryChange(event.target.value)}
              onKeyDown={handleInputKeyDown}
              placeholder="Search resources..."
              className="h-9 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
            />
            <kbd className="rounded border border-border bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
              esc
            </kbd>
          </div>
          <div className="flex flex-wrap items-center gap-3 border-t border-border px-4 py-2">
            {SEARCH_TYPE_OPTIONS.map((option) => (
              <label
                key={option.key}
                className="inline-flex cursor-pointer items-center gap-2 text-xs text-muted-foreground"
              >
                <input
                  type="checkbox"
                  className="h-3.5 w-3.5 cursor-pointer accent-primary"
                  checked={enabledSearchTypes[option.key]}
                  onChange={(event) =>
                    setEnabledSearchTypes((previous) => ({
                      ...previous,
                      [option.key]: event.target.checked,
                    }))
                  }
                />
                <span>{option.label}</span>
              </label>
            ))}
          </div>
        </div>
      }
      footer={
        <div className="flex items-center gap-4 text-[10px] text-muted-foreground">
          <span className="flex items-center gap-1">
            <kbd className="rounded border border-border bg-muted px-1.5 py-0.5 font-mono">
              esc
            </kbd>
            <span>to close</span>
          </span>
          <span className="flex items-center gap-1">
            <kbd className="rounded border border-border bg-muted px-1.5 py-0.5 font-mono">
              ↑↓
            </kbd>
            <span>to navigate</span>
          </span>
          <span className="flex items-center gap-1">
            <kbd className="rounded border border-border bg-muted px-1.5 py-0.5 font-mono">
              ↵
            </kbd>
            <span>to open</span>
          </span>
          <span className="flex items-center gap-1">
            <kbd className="rounded border border-border bg-muted px-1.5 py-0.5 font-mono">
              {getShortcutHint()}
            </kbd>
            <span>to open search</span>
          </span>
        </div>
      }
    >
      <div className="min-h-[18rem]">
        {showRecents ? (
          <RecentList items={recentItems} onChoose={chooseRecent} />
        ) : showSuggestions ? (
          <SuggestionView
            searchHistory={searchHistory}
            onSelectHistory={selectSearchQuery}
            onRemoveHistory={(item) =>
              setSearchHistory(removeSearchHistoryItem(item))
            }
            onSelectSuggestion={selectSearchQuery}
          />
        ) : searchQuery.isFetching ? (
          <EmptyState
            icon="lucide:loader-2"
            label="Searching..."
            spinning
          />
        ) : searchQuery.error ? (
          <EmptyState
            icon="lucide:triangle-alert"
            label={
              searchQuery.error instanceof Error
                ? searchQuery.error.message
                : "Search failed"
            }
          />
        ) : emptyResults ? (
          <EmptyState icon="lucide:search-x" label="No matching resources" />
        ) : (
          <ResultList
            results={flattenedResults}
            activeIndex={activeIndex}
            onActiveIndexChange={setActiveIndex}
            onChoose={chooseResult}
            agentNamesMap={agentNamesMap}
          />
        )}
      </div>
    </Modal>
  );
}

export function CommandPaletteButton({ onClick }: { onClick: () => void }) {
  const isMac =
    typeof navigator !== "undefined" &&
    /Mac|iPhone|iPad/.test(navigator.platform);
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full items-center gap-2 rounded-md border border-border bg-background px-3 py-2 text-left text-sm text-muted-foreground transition hover:bg-accent/50 hover:text-foreground"
    >
      <Icon name="lucide:search" className="shrink-0" />
      <span className="min-w-0 flex-1 truncate">Search</span>
      <span className="rounded border border-border bg-muted px-1.5 py-0.5 text-[10px]">
        {isMac ? "⌘K" : "Ctrl K"}
      </span>
    </button>
  );
}

function ResultList({
  results,
  activeIndex,
  onActiveIndexChange,
  onChoose,
  agentNamesMap,
}: {
  results: FlattenedSearchResult[];
  activeIndex: number;
  onActiveIndexChange: (index: number) => void;
  onChoose: (result: FlattenedSearchResult) => void;
  agentNamesMap: Map<string, string>;
}) {
  if (results.length === 0) return null;

  return (
    <div className="space-y-3">
      {results.map((result, index) => {
        const searchTypeLabel = getSearchTypeLabel(result.resourceType);
        const showAgentBadge =
          !isLocalAgent(result.resource.agent) &&
          agentNamesMap.get(result.resource.agent);
        return (
          <button
            key={result.key}
            type="button"
            onMouseEnter={() => onActiveIndexChange(index)}
            onClick={() => onChoose(result)}
            className={cn(
              "flex w-full min-w-0 items-start gap-3 rounded-md px-2 py-2 text-left transition-colors",
              index === activeIndex
                ? "bg-accent text-accent-foreground"
                : "hover:bg-accent/50",
              result.indentLevel ? "ml-6" : "",
            )}
          >
            <span className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center">
              {renderResultIcon(result)}
            </span>
            <span className="min-w-0 flex-1">
              <span className="flex items-center gap-2">
                <span className="truncate text-sm font-medium">
                  {result.title}
                </span>
                {result.resourceType === "configs" && result.resource.type && (
                  <span className="shrink-0 truncate text-xs text-muted-foreground">
                    {result.resource.type}
                  </span>
                )}
              </span>
              {result.resourceType === "configs" ? (
                <div className="mt-0.5 flex flex-wrap items-center gap-1">
                  {getConfigTagEntries(result.resource).map(([key, value]) => (
                    <span
                      key={key}
                      className="rounded-md bg-muted px-1 py-0.5 text-[10px] text-muted-foreground"
                    >
                      {key}: {value}
                    </span>
                  ))}
                </div>
              ) : result.description ? (
                <span className="block truncate text-xs text-muted-foreground">
                  {result.description}
                </span>
              ) : null}
            </span>
            <span className="flex shrink-0 items-center gap-1.5 self-start pt-0.5">
              {showAgentBadge ? (
                <Badge
                  size="xxs"
                  variant="outline"
                  className="border-orange-200 bg-orange-50 text-orange-700"
                >
                  {agentNamesMap.get(result.resource.agent)}
                </Badge>
              ) : null}
              <Badge
                size="xxs"
                variant="outline"
                className={cn(
                  "uppercase",
                  getSearchTypeBadgeClass(result.resourceType),
                )}
              >
                {searchTypeLabel}
              </Badge>
            </span>
          </button>
        );
      })}
    </div>
  );
}

function getSearchTypeLabel(type: SearchResourceType): string {
  return SEARCH_TYPE_OPTIONS.find((o) => o.key === type)?.label ?? type;
}

function SuggestionView({
  searchHistory,
  onSelectHistory,
  onRemoveHistory,
  onSelectSuggestion,
}: {
  searchHistory: string[];
  onSelectHistory: (q: string) => void;
  onRemoveHistory: (q: string) => void;
  onSelectSuggestion: (q: string) => void;
}) {
  return (
    <div className="space-y-3">
      {searchHistory.length > 0 ? (
        <div>
          <div className="mb-1 flex items-center gap-2 px-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
            <Icon name="lucide:clock" />
            <span>Recent searches</span>
          </div>
          <div className="space-y-1">
            {searchHistory.map((historyQuery) => (
              <button
                key={`history-${historyQuery}`}
                type="button"
                onClick={() => onSelectHistory(historyQuery)}
                className="group flex w-full min-w-0 items-center gap-3 rounded-md px-2 py-2 text-left transition-colors hover:bg-accent/50"
              >
                <Icon
                  name="lucide:history"
                  className="shrink-0 text-muted-foreground"
                />
                <span className="min-w-0 flex-1 truncate text-sm text-foreground">
                  {historyQuery}
                </span>
                <button
                  type="button"
                  className="shrink-0 rounded p-0.5 text-muted-foreground opacity-0 transition-opacity hover:bg-muted hover:text-foreground group-hover:opacity-100"
                  title="Remove from history"
                  onClick={(event) => {
                    event.stopPropagation();
                    onRemoveHistory(historyQuery);
                  }}
                >
                  <Icon name="lucide:x" className="text-sm" />
                </button>
              </button>
            ))}
          </div>
        </div>
      ) : null}

      <div>
        <div className="mb-1 flex items-center gap-2 px-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          <Icon name="lucide:sparkles" />
          <span>Suggestions</span>
        </div>
        <div className="space-y-1">
          {SUGGESTED_SEARCH_QUERIES.map((suggestion) => (
            <button
              key={suggestion}
              type="button"
              onClick={() => onSelectSuggestion(suggestion)}
              className="flex w-full min-w-0 items-center gap-3 rounded-md px-2 py-2 text-left transition-colors hover:bg-accent/50"
            >
              <Icon
                name="lucide:search"
                className="shrink-0 text-muted-foreground"
              />
              <span className="truncate text-sm text-foreground">
                {suggestion}
              </span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

function RecentList({
  items,
  onChoose,
}: {
  items: RecentItem[];
  onChoose: (item: RecentItem) => void;
}) {
  return (
    <div className="space-y-3">
      <div>
        <div className="mb-1 flex items-center gap-2 px-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          <Icon name="lucide:clock" />
          <span>Recently used</span>
        </div>
        <div className="space-y-1">
          {items.map((item) => (
            <button
              key={`${item.kind}:${item.id}`}
              type="button"
              onClick={() => onChoose(item)}
              className="flex w-full min-w-0 items-center gap-3 rounded-md px-2 py-2 text-left transition-colors hover:bg-accent/50"
            >
              <RecentIcon item={item} />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium">
                  {item.name}
                </span>
                <span className="block truncate text-xs text-muted-foreground">
                  {describeRecent(item)}
                </span>
              </span>
              <span className="shrink-0 text-xs text-muted-foreground">
                {formatRelativeTime(item.lastUsed)}
              </span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

const RECENT_KIND_FALLBACK_ICON: Record<RecentKind, string> = {
  config: "lucide:database",
  canary: "lucide:heart-pulse",
  check: "lucide:list-checks",
  config_change: "lucide:git-compare-arrows",
  playbook: "lucide:book-open-check",
  connection: "lucide:cable",
};

function describeRecent(item: RecentItem): string {
  switch (item.kind) {
    case "config":
      return item.type || "Config";
    case "canary":
      return item.type ? `Canary · ${item.type}` : "Canary";
    case "check":
      return item.type ? `Check · ${item.type}` : "Check";
    case "config_change":
      return item.type ? `Change · ${item.type}` : "Change";
    case "playbook":
      return "Playbook";
    case "connection":
      return item.type ? `Connection · ${item.type}` : "Connection";
  }
}

function RecentIcon({ item }: { item: RecentItem }) {
  if (item.kind === "config") {
    return (
      <ConfigIcon
        primary={item.type}
        className="h-5 max-w-5 shrink-0 text-muted-foreground"
      />
    );
  }
  if (item.kind === "playbook") {
    return (
      <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md border border-border bg-muted/50">
        <Icon
          name={item.icon || "lucide:book-open-check"}
          className="text-muted-foreground"
        />
      </span>
    );
  }
  return (
    <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md border border-border bg-muted/50">
      <Icon
        name={RECENT_KIND_FALLBACK_ICON[item.kind]}
        className="text-muted-foreground"
      />
    </span>
  );
}

function EmptyState({
  icon,
  label,
  spinning,
}: {
  icon: string;
  label: string;
  spinning?: boolean;
}) {
  return (
    <div className="flex min-h-[18rem] flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
      <Icon
        name={icon}
        className={cn("text-2xl", spinning && "animate-spin")}
      />
      <span>{label}</span>
    </div>
  );
}

function useDebouncedValue<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}
