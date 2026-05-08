import { useEffect, useMemo, useRef, useState } from "react";
import { Badge, Icon, Modal, cn } from "@flanksource/clicky-ui";
import { ConfigIcon } from "./ConfigIcon";
import { useQuery } from "@tanstack/react-query";
import { globalSearch, type GlobalSearchKind, type GlobalSearchResult } from "./api/search";

type CommandPaletteProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onNavigate: (href: string) => void;
};

const labels: Record<GlobalSearchKind, string> = {
  config: "Config",
  external_user: "User",
  external_group: "Group",
};

const icons: Record<GlobalSearchKind, string> = {
  config: "lucide:box",
  external_user: "lucide:user",
  external_group: "lucide:users",
};

export function CommandPalette({ open, onOpenChange, onNavigate }: CommandPaletteProps) {
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const debounced = useDebouncedValue(query, 150);

  const searchQuery = useQuery({
    queryKey: ["global-search", debounced],
    queryFn: () => globalSearch(debounced),
    enabled: open && debounced.trim().length >= 2,
  });

  const results = searchQuery.data ?? [];
  const grouped = useMemo(() => groupResults(results), [results]);
  const active = results[activeIndex];

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        onOpenChange(true);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onOpenChange]);

  useEffect(() => {
    if (!open) return;
    setActiveIndex(0);
    window.setTimeout(() => inputRef.current?.focus(), 0);
  }, [open]);

  useEffect(() => {
    setActiveIndex(0);
  }, [debounced]);

  function choose(result: GlobalSearchResult | undefined) {
    if (!result) return;
    onOpenChange(false);
    setQuery("");
    onNavigate(result.href);
  }

  return (
    <Modal
      open={open}
      onClose={() => onOpenChange(false)}
      hideClose
      size="lg"
      className="max-h-[82vh] overflow-hidden"
      headerSlot={
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <Icon name="lucide:search" className="shrink-0 text-muted-foreground" />
          <input
            ref={inputRef}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "ArrowDown") {
                event.preventDefault();
                setActiveIndex((index) => Math.min(index + 1, Math.max(results.length - 1, 0)));
              } else if (event.key === "ArrowUp") {
                event.preventDefault();
                setActiveIndex((index) => Math.max(index - 1, 0));
              } else if (event.key === "Enter") {
                event.preventDefault();
                choose(active);
              }
            }}
            placeholder="Search configs, users, and groups"
            className="h-9 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
          <kbd className="rounded border border-border bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">esc</kbd>
        </div>
      }
    >
      <div className="min-h-[18rem]">
        {query.trim().length < 2 ? (
          <EmptyState icon="lucide:command" label="Type at least 2 characters" />
        ) : searchQuery.isLoading ? (
          <EmptyState icon="lucide:loader-2" label="Searching..." />
        ) : searchQuery.error ? (
          <EmptyState icon="lucide:triangle-alert" label={searchQuery.error instanceof Error ? searchQuery.error.message : "Search failed"} />
        ) : results.length === 0 ? (
          <EmptyState icon="lucide:search-x" label="No results" />
        ) : (
          <div className="space-y-3">
            {grouped.map((group) => (
              <div key={group.kind}>
                <div className="mb-1 px-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  {labels[group.kind]}
                </div>
                <div className="space-y-1">
                  {group.results.map((result) => {
                    const index = results.indexOf(result);
                    return (
                      <button
                        key={result.id}
                        type="button"
                        onMouseEnter={() => setActiveIndex(index)}
                        onClick={() => choose(result)}
                        className={cn(
                          "flex w-full min-w-0 items-center gap-3 rounded-md px-2 py-2 text-left transition-colors",
                          index === activeIndex ? "bg-accent text-accent-foreground" : "hover:bg-accent/50",
                        )}
                      >
                        <ResultIcon result={result} />
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-sm font-medium">{result.title}</span>
                          {result.subtitle && (
                            <span className="block truncate text-xs text-muted-foreground">{result.subtitle}</span>
                          )}
                        </span>
                        {result.meta && (
                          <Badge size="xxs" variant="outline" maxWidth="9rem">
                            {result.meta}
                          </Badge>
                        )}
                      </button>
                    );
                  })}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </Modal>
  );
}

export function CommandPaletteButton({ onClick }: { onClick: () => void }) {
  const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);
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

function ResultIcon({ result }: { result: GlobalSearchResult }) {
  if (result.kind === "config") {
    return <ConfigIcon primary={result.subtitle} className="h-5 max-w-5 shrink-0 text-muted-foreground" />;
  }
  return (
    <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-border bg-muted/50">
      <Icon name={icons[result.kind]} className="text-muted-foreground" />
    </span>
  );
}

function EmptyState({ icon, label }: { icon: string; label: string }) {
  return (
    <div className="flex min-h-[18rem] flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
      <Icon name={icon} className="text-2xl" />
      <span>{label}</span>
    </div>
  );
}

function groupResults(results: GlobalSearchResult[]) {
  const order: GlobalSearchKind[] = ["config", "external_user", "external_group"];
  return order
    .map((kind) => ({ kind, results: results.filter((result) => result.kind === kind) }))
    .filter((group) => group.results.length > 0);
}

function useDebouncedValue<T>(value: T, delay: number) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}
