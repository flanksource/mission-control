import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Icon, Section } from "@flanksource/clicky-ui";
import { getPlaybookRuns } from "../api/playbooks";
import { getWhoAmI } from "../api/whoami";
import { displayPlaybookName, statusVisual } from "../playbooks/playbook-ui-helpers";
import { formatRelativeTime } from "../lib/relative-time";
import type { PlaybookRun } from "../api/types";

type Scope = "mine" | "all";

const tones = {
  success: "success",
  danger: "danger",
  info: "info",
  warning: "warning",
  neutral: "neutral",
} as const;

export function PlaybookRunsPanel() {
  const [scope, setScope] = useState<Scope>("mine");
  const meQuery = useQuery({ queryKey: ["whoami"], queryFn: getWhoAmI, staleTime: 60_000 });
  const userId = meQuery.data?.id;

  const effectiveScope: Scope = scope;
  const runsQuery = useQuery({
    queryKey: ["landing", "playbook-runs", effectiveScope, userId ?? null],
    queryFn: async () => {
      if (effectiveScope === "mine" && !userId) {
        return [];
      }
      const result = await getPlaybookRuns({ limit: 10, createdBy: effectiveScope === "mine" ? userId : undefined });
      return result.data ?? [];
    },
  });

  const runs = runsQuery.data ?? [];

  return (
    <Section
      title={
        <span className="flex w-full min-w-0 items-center justify-between gap-3">
          <span>Recent playbook runs</span>
          <ScopeToggle scope={scope} onChange={setScope} disableMine={!userId && !meQuery.isLoading} />
        </span>
      }
      defaultOpen
    >
      {runsQuery.isLoading ? (
        <Loading />
      ) : runs.length === 0 ? (
        <EmptyState scope={effectiveScope} />
      ) : (
        <ul className="divide-y divide-border">
          {runs.map((run) => (
            <li key={run.id}>
              <RunRow run={run} />
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}

function ScopeToggle({
  scope,
  onChange,
  disableMine,
}: {
  scope: Scope;
  onChange: (next: Scope) => void;
  disableMine: boolean;
}) {
  return (
    <div className="inline-flex shrink-0 overflow-hidden rounded-md border border-border text-xs">
      <button
        type="button"
        onClick={() => onChange("mine")}
        disabled={disableMine}
        className={[
          "px-2 py-1 transition-colors",
          scope === "mine" ? "bg-accent text-accent-foreground" : "hover:bg-accent/40",
          disableMine ? "cursor-not-allowed opacity-50" : "",
        ].join(" ")}
        title={disableMine ? "Sign in to see your runs" : undefined}
      >
        Mine
      </button>
      <button
        type="button"
        onClick={() => onChange("all")}
        className={[
          "border-l border-border px-2 py-1 transition-colors",
          scope === "all" ? "bg-accent text-accent-foreground" : "hover:bg-accent/40",
        ].join(" ")}
      >
        All
      </button>
    </div>
  );
}

function RunRow({ run }: { run: PlaybookRun }) {
  const visual = statusVisual(run.status);
  const tone = tones[visual.tone];
  const playbookName = displayPlaybookName(run.playbooks ?? { name: "Playbook" });
  const targetLabel = run.config?.name ?? run.component?.name ?? run.check?.name;
  const targetType = run.config?.type ?? run.component?.type ?? run.check?.type;
  const created = run.created_at;
  return (
    <a
      href={`/ui/playbooks/runs/${encodeURIComponent(run.id)}`}
      className="flex w-full min-w-0 items-center gap-3 px-3 py-2 transition-colors hover:bg-accent/40"
    >
      <Icon name={run.playbooks?.icon || "lucide:book-open-check"} className="h-5 w-5 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium text-foreground">{playbookName}</span>
        {targetLabel && (
          <span className="block truncate text-xs text-muted-foreground">
            {targetType ? `${targetType} · ${targetLabel}` : targetLabel}
          </span>
        )}
      </span>
      <Badge tone={tone} size="xxs">
        {visual.label}
      </Badge>
      {created && (
        <span className="shrink-0 text-xs text-muted-foreground">{formatRelativeTime(created)}</span>
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

function EmptyState({ scope }: { scope: Scope }) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 px-4 py-10 text-sm text-muted-foreground">
      <Icon name="lucide:book-open-check" className="text-2xl" />
      <span>{scope === "mine" ? "You haven't run any playbooks yet" : "No playbook runs yet"}</span>
    </div>
  );
}
