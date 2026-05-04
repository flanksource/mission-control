import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useQuery } from "@tanstack/react-query";
import { Badge, Icon } from "@flanksource/clicky-ui";
import { ConfigIcon } from "../ConfigIcon";
import { searchConfigResources, type ResourceSelector } from "../api/configs";
import { useConfigDetail } from "../api/hooks";
import type { ConfigItem } from "../api/types";

type ConfigItemSelectorProps = {
  valueId?: string;
  valueLabel?: string;
  selectors?: ResourceSelector[];
  placeholder?: string;
  onSelect: (config: ConfigItem | null) => void;
};

export function ConfigItemSelector({
  valueId,
  valueLabel,
  selectors,
  placeholder = "Search configs...",
  onSelect,
}: ConfigItemSelectorProps) {
  const anchorRef = useRef<HTMLLabelElement | null>(null);
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [menuRect, setMenuRect] = useState<{ left: number; top: number; width: number } | null>(null);
  const [activeIndex, setActiveIndex] = useState(0);
  const selectedQuery = useConfigDetail(valueId ?? "");
  const searchQuery = useQuery({
    queryKey: ["config-selector", selectors ?? [], debouncedQuery],
    queryFn: () => searchConfigResources(debouncedQuery, selectors),
    enabled: open,
  });
  const options = searchQuery.data ?? [];

  useEffect(() => {
    const handle = window.setTimeout(() => setDebouncedQuery(query), 250);
    return () => window.clearTimeout(handle);
  }, [query]);

  useEffect(() => {
    setActiveIndex(0);
  }, [debouncedQuery, options.length]);

  useEffect(() => {
    if (!open) return;

    const updateMenuRect = () => {
      const rect = anchorRef.current?.getBoundingClientRect();
      if (!rect) return;
      setMenuRect({
        left: rect.left,
        top: rect.bottom + 4,
        width: rect.width,
      });
    };

    updateMenuRect();
    window.addEventListener("resize", updateMenuRect);
    window.addEventListener("scroll", updateMenuRect, true);
    return () => {
      window.removeEventListener("resize", updateMenuRect);
      window.removeEventListener("scroll", updateMenuRect, true);
    };
  }, [open]);

  const selectOption = (option: ConfigItem) => {
    onSelect(option);
    setQuery("");
    setOpen(false);
  };

  const menu = open && menuRect ? (
    <div
      role="listbox"
      className="fixed z-[9999] max-h-72 overflow-auto rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-xl"
      style={{
        left: menuRect.left,
        top: menuRect.top,
        width: menuRect.width,
      }}
    >
      {searchQuery.error ? (
        <div className="p-2 text-xs text-destructive">{searchQuery.error instanceof Error ? searchQuery.error.message : String(searchQuery.error)}</div>
      ) : options.length === 0 ? (
        <div className="p-2 text-xs text-muted-foreground">{searchQuery.isFetching ? "Searching..." : "No configs found"}</div>
      ) : (
        options.map((option, index) => (
          <button
            key={option.id}
            type="button"
            role="option"
            aria-selected={index === activeIndex}
            onMouseDown={(event) => event.preventDefault()}
            onMouseEnter={() => setActiveIndex(index)}
            onClick={() => selectOption(option)}
            className={[
              "flex w-full min-w-0 items-start gap-2 rounded-md px-2 py-2 text-left text-sm",
              index === activeIndex ? "bg-accent/70" : "hover:bg-accent/60",
            ].join(" ")}
          >
            <ConfigIcon primary={option.type || option.config_class || "config"} className="mt-0.5 h-4 max-w-4 shrink-0 text-muted-foreground" />
            <span className="min-w-0 flex-1">
              <span className="block truncate font-medium">{option.name || option.id}</span>
              <span className="block truncate text-xs text-muted-foreground">{option.type || option.config_class || option.path || option.id}</span>
            </span>
          </button>
        ))
      )}
    </div>
  ) : null;

  return (
    <div className="relative grid gap-2">
      {valueId && (
        <div className="flex min-w-0 items-center justify-between gap-2 rounded-md border border-border bg-background px-2 py-1.5">
          <ConfigLink config={selectedQuery.data ?? undefined} configId={valueId} labelFallback={valueLabel || valueId} />
          <button
            type="button"
            onClick={() => onSelect(null)}
            className="shrink-0 text-xs text-muted-foreground hover:text-foreground"
          >
            Clear
          </button>
        </div>
      )}
      <label ref={anchorRef} className="flex h-9 min-w-0 items-center gap-2 rounded-md border border-border bg-background px-2 text-sm focus-within:border-primary">
        <Icon name="lucide:search" className="shrink-0 text-muted-foreground" />
        <input
          value={query}
          onFocus={() => setOpen(true)}
          onBlur={() => window.setTimeout(() => setOpen(false), 150)}
          onChange={(event) => {
            setQuery(event.target.value);
            setOpen(true);
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              setOpen(false);
              return;
            }
            if (event.key === "ArrowDown") {
              event.preventDefault();
              setOpen(true);
              setActiveIndex((current) => Math.min(current + 1, Math.max(options.length - 1, 0)));
              return;
            }
            if (event.key === "ArrowUp") {
              event.preventDefault();
              setOpen(true);
              setActiveIndex((current) => Math.max(current - 1, 0));
              return;
            }
            if (event.key === "Enter" && open) {
              event.preventDefault();
              const option = options[activeIndex] ?? options[0];
              if (option) {
                selectOption(option);
              }
            }
          }}
          placeholder={placeholder}
          className="min-w-0 flex-1 bg-transparent outline-none placeholder:text-muted-foreground"
        />
        {searchQuery.isFetching && <Icon name="lucide:loader-2" className="shrink-0 animate-spin text-muted-foreground" />}
      </label>
      {typeof document === "undefined" ? menu : createPortal(menu, document.body)}
    </div>
  );
}

function ConfigLink({
  config,
  configId,
  labelFallback,
}: {
  config?: {
    id?: string | null;
    name?: string | null;
    type?: string | null;
    config_class?: string | null;
    deleted_at?: string | null;
  } | null;
  configId?: string | null;
  labelFallback?: string;
}) {
  const id = config?.id ?? configId;
  if (!id) return <span className="text-muted-foreground">-</span>;
  const label = config?.name || labelFallback || id;
  return (
    <a href={`/ui/item/${encodeURIComponent(id)}`} className="inline-flex max-w-full min-w-0 items-center gap-2 text-sm text-foreground hover:text-primary">
      <ConfigIcon primary={config?.type || config?.config_class || "config"} className="h-4 max-w-4 shrink-0 text-muted-foreground" />
      <span className="min-w-0 truncate">{label}</span>
      {config?.deleted_at && <Badge tone="danger" size="xxs">Deleted</Badge>}
    </a>
  );
}
