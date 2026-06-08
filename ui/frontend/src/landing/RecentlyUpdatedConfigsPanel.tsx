import { useQuery } from "@tanstack/react-query";
import { Badge, Icon, Section } from "@flanksource/clicky-ui";
import { fetchPostgrest } from "../api/http";
import { ConfigIcon } from "../ConfigIcon";
import { resolveChangeAccent } from "../config-detail/config-changes/change-accent";
import { formatRelativeTime } from "../lib/relative-time";

type RecentChange = {
  id: string;
  config_id: string;
  config_name?: string | null;
  config_type?: string | null;
  change_type: string;
  category?: string | null;
  severity?: string | null;
  summary?: string | null;
  created_at?: string;
};

const VISIBLE = 10;
const FETCH = 30;

async function fetchRecentChanges(): Promise<RecentChange[]> {
  const params = new URLSearchParams({
    select: "id,config_id,config_name,config_type,change_type,category,severity,summary,created_at",
    order: "created_at.desc",
    limit: String(FETCH),
  });
  const result = await fetchPostgrest<RecentChange[]>(`/db/config_changes?${params.toString()}`);
  return result.data ?? [];
}

function dedupeByConfig(changes: RecentChange[]): RecentChange[] {
  const seen = new Set<string>();
  const result: RecentChange[] = [];
  for (const change of changes) {
    if (seen.has(change.config_id)) continue;
    seen.add(change.config_id);
    result.push(change);
    if (result.length >= VISIBLE) break;
  }
  return result;
}

function severityTone(severity?: string | null): "danger" | "warning" | "info" | undefined {
  switch (severity) {
    case "critical":
    case "high":
      return "danger";
    case "medium":
      return "warning";
    case "low":
      return "info";
    default:
      return undefined;
  }
}

export function RecentlyUpdatedConfigsPanel() {
  const query = useQuery({
    queryKey: ["landing", "recent-changes"],
    queryFn: fetchRecentChanges,
  });
  const changes = dedupeByConfig(query.data ?? []);

  return (
    <Section title="Recently updated configs" defaultOpen>
      {query.isLoading ? (
        <Loading />
      ) : changes.length === 0 ? (
        <EmptyState />
      ) : (
        <ul className="divide-y divide-border">
          {changes.map((change) => (
            <li key={change.id}>
              <ChangeRow change={change} />
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}

function ChangeRow({ change }: { change: RecentChange }) {
  const accent = resolveChangeAccent({
    changeType: change.change_type,
    category: change.category ?? undefined,
    label: change.change_type,
  });
  const tone = severityTone(change.severity);
  const configName = change.config_name || change.config_id;
  return (
    <a
      href={`/ui/item/${encodeURIComponent(change.config_id)}`}
      className="flex w-full min-w-0 items-center gap-3 px-3 py-2 transition-colors hover:bg-accent/40"
    >
      <ConfigIcon primary={change.config_type ?? undefined} className="h-5 max-w-5 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium text-foreground">{configName}</span>
        {change.summary && (
          <span className="block truncate text-xs text-muted-foreground">{change.summary}</span>
        )}
      </span>
      <span className={["inline-flex shrink-0 items-center rounded border px-1.5 py-0.5 text-[10px] font-medium", accent.color, accent.textColor, accent.borderColor].join(" ")}>
        {change.change_type}
      </span>
      {tone && (
        <Badge tone={tone} size="xxs">
          {change.severity}
        </Badge>
      )}
      {change.created_at && (
        <span className="shrink-0 text-xs text-muted-foreground">{formatRelativeTime(change.created_at)}</span>
      )}
    </a>
  );
}

function Loading() {
  return (
    <div className="flex items-center justify-center gap-2 px-4 py-8 text-sm text-muted-foreground">
      <Icon name="lucide:loader-2" className="animate-spin" />
      <span>Loading…</span>
    </div>
  );
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center gap-2 px-4 py-10 text-sm text-muted-foreground">
      <Icon name="lucide:git-compare" className="text-2xl" />
      <span>No recent config changes</span>
    </div>
  );
}
