import { Icon, Section } from "@flanksource/clicky-ui";
import { ConfigIcon } from "../ConfigIcon";
import { useRecents, type RecentItem } from "../lib/recents";
import { formatRelativeTime } from "../lib/relative-time";

const VISIBLE = 10;

export function RecentlyUsedPanel() {
  const items = useRecents().slice(0, VISIBLE);
  return (
    <Section title="Recently used" defaultOpen>
      {items.length === 0 ? (
        <EmptyState />
      ) : (
        <ul className="divide-y divide-border">
          {items.map((item) => (
            <li key={`${item.kind}:${item.id}`}>
              <a
                href={item.href}
                className="flex w-full min-w-0 items-center gap-3 px-3 py-2 transition-colors hover:bg-accent/40"
              >
                <RecentIcon item={item} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium text-foreground">{item.name}</span>
                  <span className="block truncate text-xs text-muted-foreground">
                    {describeRecent(item)}
                  </span>
                </span>
                <span className="shrink-0 text-xs text-muted-foreground">{formatRelativeTime(item.lastUsed)}</span>
              </a>
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}

function RecentIcon({ item }: { item: RecentItem }) {
  if (item.kind === "config") {
    return <ConfigIcon primary={item.type} className="h-5 max-w-5 shrink-0 text-muted-foreground" />;
  }
  if (item.kind === "playbook") {
    return (
      <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md border border-border bg-muted/50">
        <Icon name={item.icon || "lucide:book-open-check"} className="text-muted-foreground" />
      </span>
    );
  }
  return (
    <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md border border-border bg-muted/50">
      <Icon name={RECENT_KIND_FALLBACK_ICON[item.kind]} className="text-muted-foreground" />
    </span>
  );
}

const RECENT_KIND_FALLBACK_ICON = {
  config: "lucide:database",
  canary: "lucide:heart-pulse",
  check: "lucide:list-checks",
  config_change: "lucide:git-compare-arrows",
  playbook: "lucide:book-open-check",
  connection: "lucide:cable",
} as const;

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

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center gap-2 px-4 py-10 text-sm text-muted-foreground">
      <Icon name="lucide:clock" className="text-2xl" />
      <span>No recent items yet</span>
      <span className="text-xs">Open a config item or playbook run to see it here</span>
    </div>
  );
}
