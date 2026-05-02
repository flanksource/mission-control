import { useEffect, useMemo, useState, type CSSProperties, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Badge,
  DataTable,
  DetailEmptyState,
  Icon,
  JsonView,
  Modal,
  Section,
  type DataTableColumn,
} from "@flanksource/clicky-ui";
import { ConfigIcon } from "../ConfigIcon";
import {
  DetailPageLayout,
  EntityHeader,
  HeaderPill,
  PageBreadcrumbs,
} from "../layout/DetailPageLayout";
import {
  approvePlaybookRun,
  cancelPlaybookRun,
  deletePlaybook,
  getPlaybookParams,
  isFinalPlaybookRunStatus,
  submitPlaybookRun,
  updatePlaybook,
  type PlaybookRunsOptions,
  type PlaybookUpdateRequest,
} from "../api/playbooks";
import {
  useConfigDetail,
  usePlaybookRunDetail,
  usePlaybookRuns,
  usePlaybooks,
  useRunnablePlaybooksForConfig,
} from "../api/hooks";
import { type ResourceSelector } from "../api/configs";
import { ConfigItemSelector } from "../components/ConfigItemSelector";
import type {
  ConfigItem,
  Playbook,
  PlaybookArtifact,
  PlaybookListItem,
  PlaybookParameter,
  PlaybookRun,
  PlaybookRunAction,
  PlaybookRunSubmitRequest,
  PlaybookRunTarget,
} from "../api/types";
import { stringifyValue, timeAgo } from "../config-detail/utils";
import {
  actionProgress,
  actorName,
  buildPlaybookSections,
  displayPlaybookName,
  playbookFallbackIcon,
  recentTargetsForPlaybook,
  statusVisual,
  targetSummaryFromRun,
  type PlaybookRunTargetSummary,
  type PlaybookSection,
  type PlaybookStatusVisual,
} from "./playbook-ui-helpers";

export { playbookStatusTone } from "./playbook-ui-helpers";

export type PlaybookBrowserProps = {
  mode: "list" | "runs" | "run";
  runId?: string;
};

type RunnablePlaybook = Pick<Playbook, "id" | "name" | "title" | "icon" | "description" | "category" | "source" | "namespace" | "created_at"> & {
  parameters?: PlaybookParameter[] | unknown;
  spec?: Playbook["spec"];
};

type RunDialogSelection = {
  playbook: RunnablePlaybook;
  target?: PlaybookRunTarget;
  resourceLabel?: string;
};

type StepParameterGroup = {
  key: string;
  label: string;
  entries: Array<[string, string]>;
  index?: number;
  tone: PlaybookStatusVisual["tone"];
};

type RunTimelineEvent = {
  timestamp: string;
  label: string;
  tone?: PlaybookStatusVisual["tone"];
};

type ErrorDiagnostics = {
  message: string;
  trace?: string;
  time?: string;
  stacktrace?: string;
  context: Array<[string, string]>;
  raw?: unknown;
};

type ParsedStackTrace = {
  headline?: string;
  frames: StackTraceFrame[];
  unparsed: string[];
  raw: string;
};

type StackTraceFrame = {
  raw: string;
  file: string;
  line: number;
  functionName?: string;
};

export function PlaybookBrowser({ mode, runId }: PlaybookBrowserProps) {
  if (mode === "run") {
    return <PlaybookRunDetailPage runId={runId ?? ""} />;
  }
  if (mode === "runs") {
    return <PlaybookRunsPage />;
  }
  return <PlaybooksListPage />;
}

export function stepOutputMaxHeight(stepCount: number): CSSProperties {
  return { maxHeight: `min(20vh, calc((100vh - 200px) / ${Math.max(1, stepCount)}))` };
}

export function ConfigPlaybooksTab({ config }: { config: ConfigItem }) {
  const runnableQuery = useRunnablePlaybooksForConfig(config.id);
  const runsQuery = usePlaybookRuns({ configId: config.id, limit: 50 });
  const runnable = (runnableQuery.data ?? []).map(runnableListItemToPlaybook);
  const runs = runsQuery.data?.data ?? [];
  const [selected, setSelected] = useState<RunDialogSelection | null>(null);

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <Section
        title="Runnable Playbooks"
        icon="lucide:play-square"
        defaultOpen
        summary={runnableQuery.isLoading ? "Loading" : String(runnable.length)}
      >
        <QueryState query={runnableQuery} emptyIcon="lucide:play-square" emptyLabel="No runnable playbooks" />
        {runnable.length > 0 && (
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {runnable.map((playbook) => (
              <PlaybookRunCard
                key={playbook.id}
                playbook={playbook}
                onRun={() => setSelected({ playbook, target: { config_id: config.id }, resourceLabel: config.name || config.id })}
              />
            ))}
          </div>
        )}
      </Section>
      <Section
        title="Recent Runs"
        icon="lucide:history"
        defaultOpen
        summary={runsQuery.isLoading ? "Loading" : String(runs.length)}
      >
        <QueryState
          query={{ ...runsQuery, data: runs }}
          emptyIcon="lucide:history"
          emptyLabel="No playbook runs"
        />
        {runs.length > 0 && <PlaybookRunsTable runs={runs} />}
      </Section>
      {selected && (
        <SubmitPlaybookRunDialog
          open
          playbook={selected.playbook}
          target={selected.target}
          resourceLabel={selected.resourceLabel}
          onClose={() => setSelected(null)}
        />
      )}
    </div>
  );
}

function PlaybooksListPage() {
  const playbooksQuery = usePlaybooks();
  const recentRunsOptions = useMemo(() => ({ limit: 100 }), []);
  const runsQuery = usePlaybookRuns(recentRunsOptions);
  const playbooks = playbooksQuery.data ?? [];
  const runs = runsQuery.data?.data ?? [];
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<RunDialogSelection | null>(null);
  const [editing, setEditing] = useState<RunnablePlaybook | null>(null);
  const sections = useMemo(
    () => buildPlaybookSections(playbooks, runs, search),
    [playbooks, runs, search],
  );
  const favoriteCount = sections.find((section) => section.id === "favorites")?.playbooks.length ?? 0;
  const visibleSections = sections.filter((section) => section.playbooks.length > 0 || section.id === "favorites");

  return (
    <PlaybookShell
      title="Playbooks"
      subtitle="Run operational automation against configs, components, checks, or a free-form target."
      icon="playbook"
      headerMeta={playbooksQuery.data ? <HeaderPill icon="playbook" label={`${playbooks.length} playbooks`} /> : undefined}
    >
      <PlaybookPageTabs active="playbooks" />
      <LibraryToolbar
        search={search}
        onSearch={setSearch}
        playbookCount={playbooks.length}
        runCount={runs.length}
        favoriteCount={favoriteCount}
      />
      <QueryState query={playbooksQuery} emptyIcon="lucide:book-open-check" emptyLabel="No playbooks" />
      {playbooks.length > 0 && (
        <div className="grid min-w-0 gap-5 xl:grid-cols-[minmax(0,1fr)_28rem]">
          <div className="flex min-w-0 flex-col gap-4">
            <CategoryAnchorBar sections={visibleSections} />
            {visibleSections.map((section) => (
              <PlaybookLibrarySection
                key={section.id}
                section={section}
                runs={runs}
                onRun={(playbook, target, resourceLabel) => setSelected({ playbook, target, resourceLabel })}
                onEdit={(playbook) => setEditing(playbook)}
              />
            ))}
          </div>
          <RecentRunsPanel runs={runs} loading={runsQuery.isLoading} />
        </div>
      )}
      {selected && (
        <SubmitPlaybookRunDialog
          open
          playbook={selected.playbook}
          target={selected.target}
          resourceLabel={selected.resourceLabel}
          onClose={() => setSelected(null)}
        />
      )}
      {editing && (
        <EditPlaybookDialog
          playbook={editing}
          onClose={() => setEditing(null)}
        />
      )}
    </PlaybookShell>
  );
}

function PlaybookRunsPage() {
  const filters = usePlaybookRunFilters();
  const query = usePlaybookRuns(filters);
  const runs = query.data?.data ?? [];

  return (
    <PlaybookShell
      title="Playbook Runs"
      subtitle="Execution history"
      icon="activity-feed"
      headerMeta={query.data ? <HeaderPill icon="activity-feed" label={`${query.data.total ?? runs.length} runs`} /> : undefined}
    >
      <PlaybookPageTabs active="runs" />
      <RunFilters status={filters.status} playbookId={filters.playbookId} />
      <QueryState query={{ ...query, data: runs }} emptyIcon="lucide:list-restart" emptyLabel="No playbook runs" />
      {runs.length > 0 && <PlaybookRunsTable runs={runs} />}
    </PlaybookShell>
  );
}

function PlaybookRunDetailPage({ runId }: { runId: string }) {
  const query = usePlaybookRunDetail(runId);
  const detail = query.data;
  const run = detail?.run;
  const actions = detail?.actions ?? [];
  const childRuns = detail?.childRuns ?? [];
  const actionDetailsError = detail?.actionDetailsError;
  const runDiagnostics = run ? errorDiagnosticsFromRun(run) : null;
  const runStateData = !runId ? null : query.data ? run ?? null : query.data;
  const queryClient = useQueryClient();
  const [rerun, setRerun] = useState<RunDialogSelection | null>(null);
  const approveMutation = useMutation({
    mutationFn: () => approvePlaybookRun(runId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["playbook_run", runId] }),
  });
  const cancelMutation = useMutation({
    mutationFn: () => cancelPlaybookRun(runId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["playbook_run", runId] }),
  });
  const onRerun = () => {
    if (!run) return;
    const target = targetSummaryFromRun(run);
    setRerun({
      playbook: runToRunnablePlaybook(run),
      target: target?.target,
      resourceLabel: target?.label,
    });
  };

  return (
    <div className="flex h-full min-h-0 flex-col bg-muted/10">
      <DetailPageLayout
        breadcrumbs={run ? <RunDetailBreadcrumbs run={run} /> : undefined}
        actions={run ? (
          <RunDetailActions
            run={run}
            actions={actions}
            approving={approveMutation.isPending}
            cancelling={cancelMutation.isPending}
            onApprove={() => approveMutation.mutate()}
            onCancel={() => cancelMutation.mutate()}
            onRerun={onRerun}
          />
        ) : undefined}
        header={run ? <RunHero run={run} actions={actions} /> : undefined}
        main={
          <>
            <QueryState
              query={{ ...query, data: runStateData }}
              emptyIcon="lucide:activity"
              emptyLabel="Playbook run not found"
            />
            {runDiagnostics && <RunErrorPanel diagnostics={runDiagnostics} />}
            {run && <ActionTimeline actions={actions} actionDetailsError={actionDetailsError} />}
          </>
        }
        sidebars={run ? <RunSideRail run={run} actions={actions} childRuns={childRuns} /> : undefined}
      />
      {rerun && (
        <SubmitPlaybookRunDialog
          open
          playbook={rerun.playbook}
          target={rerun.target}
          resourceLabel={rerun.resourceLabel}
          onClose={() => setRerun(null)}
        />
      )}
    </div>
  );
}

function RunErrorPanel({ diagnostics }: { diagnostics: ErrorDiagnostics }) {
  return (
    <section className="overflow-hidden rounded-lg border border-destructive/30 bg-background shadow-sm">
      <div className="flex min-w-0 items-center gap-2 border-b border-destructive/20 bg-destructive/5 px-4 py-3">
        <Icon name="lucide:triangle-alert" className="shrink-0 text-destructive" />
        <h2 className="truncate text-base font-semibold text-destructive">Run error</h2>
      </div>
      <div className="p-4">
        <ErrorDetails diagnostics={diagnostics} />
      </div>
    </section>
  );
}

function RunDetailBreadcrumbs({ run }: { run: PlaybookRun }) {
  const name = playbookName(run.playbooks, run.playbook_id);
  return (
    <PageBreadcrumbs
      items={[
        { label: "Playbook runs", href: "/ui/playbooks/runs", icon: "lucide:chevron-left" },
        { label: name, title: name, className: "max-w-[18rem]" },
        { label: shortRunId(run.id), mono: true },
      ]}
    />
  );
}

function RunDetailActions({
  run,
  actions,
  approving,
  cancelling,
  onApprove,
  onCancel,
  onRerun,
}: {
  run: PlaybookRun;
  actions: PlaybookRunAction[];
  approving: boolean;
  cancelling: boolean;
  onApprove: () => void;
  onCancel: () => void;
  onRerun: () => void;
}) {
  const final = isFinalPlaybookRunStatus(run.status);
  return (
    <>
      <button
        type="button"
        onClick={() => downloadRunLogs(run, actions)}
        className="inline-flex h-9 items-center gap-2 rounded-md border border-border bg-background px-3 text-sm font-medium shadow-sm hover:bg-accent/50"
      >
        <Icon name="lucide:download" />
        Download logs
      </button>
      {run.status === "pending_approval" && (
        <ActionButton label="Approve" icon={approving ? "activity-feed" : "approval"} loading={approving} onClick={onApprove} />
      )}
      <ActionButton label="Re-run" icon="restart" onClick={onRerun} />
      {!final && (
        <ActionButton label="Cancel run" icon={cancelling ? "activity-feed" : "remove-badge"} loading={cancelling} danger onClick={onCancel} />
      )}
    </>
  );
}

function LibraryToolbar({
  search,
  onSearch,
  playbookCount,
  runCount,
  favoriteCount,
}: {
  search: string;
  onSearch: (value: string) => void;
  playbookCount: number;
  runCount: number;
  favoriteCount: number;
}) {
  return (
    <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center">
      <label className="flex h-10 min-w-0 items-center gap-2 rounded-md border border-border bg-background px-3 text-sm shadow-sm">
        <Icon name="lucide:search" className="shrink-0 text-muted-foreground" />
        <input
          value={search}
          onChange={(event) => onSearch(event.target.value)}
          placeholder="Search playbooks, categories, sources, namespaces..."
          className="min-w-0 flex-1 bg-transparent outline-none placeholder:text-muted-foreground"
        />
        {search && (
          <button type="button" onClick={() => onSearch("")} className="text-muted-foreground hover:text-foreground" aria-label="Clear search">
            <Icon name="lucide:x" />
          </button>
        )}
      </label>
      <div className="flex flex-wrap gap-2">
        <MetricChip icon="playbook" label="Library" value={playbookCount} />
        <MetricChip icon="heart-checkmark" label="Favorites" value={favoriteCount} />
        <MetricChip icon="activity-feed" label="Recent runs" value={runCount} />
      </div>
    </div>
  );
}

function CategoryAnchorBar({ sections }: { sections: PlaybookSection[] }) {
  if (sections.length === 0) return null;
  return (
    <nav className="sticky top-0 z-10 -mx-1 flex min-w-0 gap-2 overflow-x-auto border-b border-border bg-background/95 px-1 py-2 backdrop-blur">
      {sections.map((section) => (
        <a
          key={section.id}
          href={`#${section.id}`}
          className="inline-flex h-8 shrink-0 items-center gap-2 rounded-md border border-border bg-background px-2.5 text-xs font-medium text-muted-foreground hover:border-primary/40 hover:text-foreground"
        >
          <ConfigIcon primary={section.icon} className="h-4 max-w-4" />
          <span>{section.label}</span>
          <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px]">{section.playbooks.length}</span>
        </a>
      ))}
    </nav>
  );
}

function PlaybookLibrarySection({
  section,
  runs,
  onRun,
  onEdit,
}: {
  section: PlaybookSection;
  runs: PlaybookRun[];
  onRun: (playbook: RunnablePlaybook, target?: PlaybookRunTarget, resourceLabel?: string) => void;
  onEdit: (playbook: RunnablePlaybook) => void;
}) {
  return (
    <section id={section.id} className="scroll-mt-16">
      <div className="mb-3 flex min-w-0 items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-border bg-muted/40">
            <ConfigIcon primary={section.icon} className="h-5 max-w-5" />
          </div>
          <div className="min-w-0">
            <h2 className="truncate text-base font-semibold">{section.label}</h2>
            <div className="text-xs text-muted-foreground">{section.playbooks.length} playbooks</div>
          </div>
        </div>
      </div>
      {section.playbooks.length === 0 ? (
        <div className="rounded-md border border-dashed border-border p-5 text-sm text-muted-foreground">
          No playbooks in this section.
        </div>
      ) : (
        <div className="grid gap-3 md:grid-cols-2 2xl:grid-cols-3">
          {section.playbooks.map((playbook) => (
            <PlaybookLibraryCard
              key={`${section.id}-${playbook.id}`}
              playbook={recordToRunnablePlaybook(playbook as unknown as Record<string, unknown>)}
              recentTargets={recentTargetsForPlaybook(playbook.id, runs)}
              onRun={onRun}
              onEdit={onEdit}
            />
          ))}
        </div>
      )}
    </section>
  );
}

function PlaybookLibraryCard({
  playbook,
  recentTargets,
  onRun,
  onEdit,
}: {
  playbook: RunnablePlaybook;
  recentTargets: PlaybookRunTargetSummary[];
  onRun: (playbook: RunnablePlaybook, target?: PlaybookRunTarget, resourceLabel?: string) => void;
  onEdit: (playbook: RunnablePlaybook) => void;
}) {
  const paramCount = normalizeParameterList(playbook.parameters ?? playbook.spec?.parameters).length;
  const [menuOpen, setMenuOpen] = useState(false);
  const queryClient = useQueryClient();
  const deleteMutation = useMutation({
    mutationFn: () => deletePlaybook(playbook.id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["playbooks"] }),
  });
  const historyHref = `/ui/playbooks/runs?playbook=${encodeURIComponent(playbook.id)}`;

  return (
    <article
      role="button"
      tabIndex={0}
      onClick={() => onRun(playbook)}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onRun(playbook);
        }
      }}
      className="group relative flex min-h-[11rem] cursor-pointer flex-col rounded-md border border-border bg-background p-4 shadow-sm outline-none transition hover:border-primary/40 hover:shadow-md focus:border-primary/60"
    >
      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-3">
          <div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-md border border-primary/20 bg-primary/10 text-primary">
            <PlaybookIcon playbook={playbook} className="h-9 max-w-9" />
          </div>
          <div className="min-w-0">
            <h3 className="line-clamp-2 text-sm font-semibold leading-5">{displayPlaybookName(playbook)}</h3>
            <div className="mt-1 flex flex-wrap gap-1.5">
              {playbook.category && <Badge size="xxs">{playbook.category}</Badge>}
              {playbook.source && <Badge size="xxs">{playbook.source}</Badge>}
              <Badge size="xxs">{paramCount} params</Badge>
            </div>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <button
            type="button"
            onClick={(event) => {
              event.stopPropagation();
              setMenuOpen((open) => !open);
            }}
            className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground hover:bg-accent/50 hover:text-foreground"
            aria-label="Playbook actions"
            aria-expanded={menuOpen}
          >
            <Icon name="lucide:ellipsis" />
          </button>
          {menuOpen && (
            <PlaybookCardMenu
              historyHref={historyHref}
              deleting={deleteMutation.isPending}
              onRun={() => {
                setMenuOpen(false);
                onRun(playbook);
              }}
              onEdit={() => {
                setMenuOpen(false);
                onEdit(playbook);
              }}
              onDelete={() => {
                setMenuOpen(false);
                if (!window.confirm(`Delete playbook ${displayPlaybookName(playbook)}?`)) return;
                deleteMutation.mutate();
              }}
            />
          )}
        </div>
      </div>
      {playbook.description && <p className="mt-3 line-clamp-1 text-sm leading-5 text-muted-foreground">{playbook.description}</p>}
      {recentTargets.length > 0 && (
        <div className="mt-auto pt-4">
          <div className="flex flex-wrap gap-1.5">
            {recentTargets.map((target) => (
              <button
                key={target.key}
                type="button"
                onClick={(event) => {
                  event.stopPropagation();
                  onRun(playbook, target.target, target.label);
                }}
                className="inline-flex h-7 max-w-full items-center gap-1.5 rounded-md border border-border bg-muted/30 px-2 text-xs hover:bg-accent/60"
              >
                <ConfigIcon primary={target.icon} className="h-3.5 max-w-3.5 shrink-0" />
                <span className="truncate">{target.label}</span>
                {target.count > 1 && <span className="font-mono text-[10px] text-muted-foreground">x{target.count}</span>}
              </button>
            ))}
          </div>
        </div>
      )}
      {deleteMutation.error && (
        <div className="mt-3 rounded-md border border-destructive/30 bg-destructive/5 px-2 py-1.5 text-xs text-destructive">
          {deleteMutation.error instanceof Error ? deleteMutation.error.message : String(deleteMutation.error)}
        </div>
      )}
    </article>
  );
}

function PlaybookCardMenu({
  historyHref,
  deleting,
  onRun,
  onEdit,
  onDelete,
}: {
  historyHref: string;
  deleting: boolean;
  onRun: () => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const itemClass = "flex h-9 w-full items-center gap-2 px-3 text-left text-sm hover:bg-accent/50";
  return (
    <div
      className="absolute right-4 top-12 z-30 w-40 overflow-hidden rounded-md border border-border bg-popover py-1 text-popover-foreground shadow-lg"
      onClick={(event) => event.stopPropagation()}
    >
      <a href={historyHref} className={itemClass}>
        <Icon name="lucide:history" />
        History
      </a>
      <button type="button" onClick={onRun} className={itemClass}>
        <Icon name="lucide:play" />
        Run
      </button>
      <button type="button" onClick={onEdit} className={itemClass}>
        <Icon name="lucide:pencil" />
        Edit
      </button>
      <button
        type="button"
        onClick={onDelete}
        disabled={deleting}
        className={`${itemClass} text-destructive disabled:cursor-not-allowed disabled:opacity-50`}
      >
        <Icon name={deleting ? "lucide:loader-2" : "lucide:trash-2"} className={deleting ? "animate-spin" : undefined} />
        Delete
      </button>
    </div>
  );
}

function RecentRunsPanel({ runs, loading }: { runs: PlaybookRun[]; loading: boolean }) {
  return (
    <aside className="min-w-0 xl:sticky xl:top-14 xl:self-start">
      <div className="rounded-md border border-border bg-background shadow-sm">
        <div className="flex items-center justify-between gap-2 border-b border-border px-3 py-2">
          <div className="flex min-w-0 items-center gap-2">
            <ConfigIcon primary="activity-feed" className="h-4 max-w-4" />
            <h2 className="truncate text-sm font-semibold">Recent runs</h2>
          </div>
          <Badge size="xxs">{runs.length}</Badge>
        </div>
        <div className="max-h-[42rem] overflow-auto p-2">
          {loading ? (
            <div className="p-3 text-sm text-muted-foreground">Loading...</div>
          ) : runs.length === 0 ? (
            <DetailEmptyState icon="lucide:history" label="No recent runs" />
          ) : (
            <div className="grid gap-1.5">
              {runs.slice(0, 12).map((run) => {
                const target = targetSummaryFromRun(run);
                return (
                  <a
                    key={run.id}
                    href={`/ui/playbooks/runs/${encodeURIComponent(run.id)}`}
                    className="grid min-w-0 gap-2 rounded-md border border-transparent p-2 text-sm hover:border-border hover:bg-accent/30"
                  >
                    <div className="flex min-w-0 items-center justify-between gap-2">
                      <span className="truncate font-medium">{playbookName(run.playbooks, run.playbook_id)}</span>
                      <StatusDot status={run.status} />
                    </div>
                    <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
                      {target && <ConfigIcon primary={target.icon} className="h-3.5 max-w-3.5 shrink-0" />}
                      <span className="truncate">{target?.label ?? "No target"}</span>
                      <span className="shrink-0">{timeAgo(run.start_time || run.scheduled_time || run.created_at || "")}</span>
                    </div>
                  </a>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </aside>
  );
}

function RunHero({ run, actions }: { run: PlaybookRun; actions: PlaybookRunAction[] }) {
  const target = targetSummaryFromRun(run);

  return (
    <EntityHeader
      variant="card"
      titleSize="lg"
      title={playbookName(run.playbooks, run.playbook_id)}
      tags={
        <>
          <span className="rounded-md bg-muted px-2 py-1 font-mono text-sm text-muted-foreground">{shortRunId(run.id)}</span>
          <StatusBadge status={run.status} />
        </>
      }
      aside={
        <div className="flex min-w-0 flex-wrap items-center justify-end gap-1.5 text-sm text-muted-foreground">
          <span>on</span>
          {run.config_id ? (
            <ConfigLink
              config={run.config ?? undefined}
              configId={run.config_id}
              labelFallback={target?.label}
              className="min-w-0 max-w-[18rem] text-sm font-semibold text-foreground hover:text-primary"
            />
          ) : (
            <span className="font-semibold text-foreground">{target?.label ?? "No target"}</span>
          )}
          <span>· by</span>
          <span className="font-semibold text-muted-foreground">{actorName(run)}</span>
          <span>·</span>
          <span>{timeAgo(run.start_time || run.scheduled_time || run.created_at || "")}</span>
        </div>
      }
    >
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_7rem] xl:items-center">
        <RunStepRibbon actions={actions} />
        <div className="hidden border-l border-border pl-5 xl:block">
          <div className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">Elapsed</div>
          <div className="mt-1 font-mono text-lg font-semibold text-foreground">{runElapsed(run)}</div>
        </div>
      </div>
    </EntityHeader>
  );
}

function RunStepRibbon({ actions }: { actions: PlaybookRunAction[] }) {
  if (actions.length === 0) {
    return (
      <div className="text-sm text-muted-foreground">
        No actions have been created for this run.
      </div>
    );
  }
  return (
    <div className="min-w-0 overflow-x-auto pb-1">
      <div className="flex min-w-max items-center">
        {actions.map((action, index) => {
          const status = actionDisplayStatus(action);
          const visual = statusVisual(status);
          return (
            <div key={action.id} className="flex items-center">
              <div className="flex min-w-[12rem] items-center gap-3">
                <StepCircle status={status} index={index} />
                <div className="min-w-0">
                  <div className="truncate text-sm font-semibold">{action.name}</div>
                  {status && (
                    <div className={["truncate text-xs font-bold uppercase tracking-widest", toneTextClass(visual.tone)].join(" ")}>
                      {visual.label}
                    </div>
                  )}
                </div>
              </div>
              {index < actions.length - 1 && (
                <div className={["mx-4 h-px w-14", action.status === "completed" ? "bg-emerald-200" : "bg-border"].join(" ")} />
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function StepCircle({ status, index }: { status?: string | null; index: number }) {
  const visual = statusVisual(status);
  const running = status === "running" || status === "retrying";
  return (
    <span className={["flex h-8 w-8 shrink-0 items-center justify-center rounded-full border-2 text-sm font-semibold", toneStepCircleClass(visual.tone, running)].join(" ")}>
      {status === "completed" || status === "success" || status === "done" ? (
        <Icon name="lucide:check" />
      ) : status === "failed" || status === "timed_out" ? (
        <Icon name="lucide:x" />
      ) : running ? (
        <Icon name="lucide:refresh-cw" className="animate-spin" />
      ) : status === "skipped" ? (
        <Icon name="lucide:chevrons-right" />
      ) : status === "scheduled" ? (
        <ConfigIcon primary="add-clock" className="h-4 max-w-4" />
      ) : (
        <span>{index + 1}</span>
      )}
    </span>
  );
}

function ActionTimeline({ actions, actionDetailsError }: { actions: PlaybookRunAction[]; actionDetailsError?: string }) {
  const progress = actionProgress(actions);
  if (actions.length === 0) {
    return (
      <section className="rounded-lg border border-border bg-background p-6 shadow-sm">
        <DetailEmptyState icon="lucide:list-checks" label="No actions" />
      </section>
    );
  }
  return (
    <section className="overflow-hidden rounded-lg border border-border bg-background shadow-sm">
      <div className="flex min-w-0 items-center justify-between gap-3 border-b border-border px-4 py-3">
        <div className="flex min-w-0 items-center gap-2">
          <h2 className="truncate text-base font-semibold">Steps</h2>
          <span className="rounded-full bg-muted px-2 py-0.5 text-xs font-semibold text-muted-foreground">{actions.length}</span>
        </div>
        <div className="text-sm font-medium text-muted-foreground">{progress.complete}/{progress.total} complete</div>
      </div>
      <div className="relative">
        {actions.map((action, index) => (
          <ActionTimelineItem
            key={action.id}
            action={action}
            index={index}
            last={index === actions.length - 1}
            stepCount={actions.length}
            actionDetailsError={actionDetailsError}
          />
        ))}
      </div>
    </section>
  );
}

function ActionTimelineItem({
  action,
  index,
  last,
  stepCount,
  actionDetailsError,
}: {
  action: PlaybookRunAction;
  index: number;
  last: boolean;
  stepCount: number;
  actionDetailsError?: string;
}) {
  const status = actionDisplayStatus(action);
  const visual = statusVisual(status);
  const hasBody = Boolean(action.result || action.error || (action.artifacts?.length ?? 0) > 0);
  const hasDetailWarning = Boolean(actionDetailsError && action.status === "failed" && !hasBody);
  const compact = !hasBody && !hasDetailWarning && status !== "running";
  const progressText = actionProgressText(action);
  const progressPercent = actionProgressPercent(action);
  const startTime = actionStartTime(action);
  const duration = actionDuration(action);

  return (
    <article className={["relative grid min-w-0 grid-cols-[4.5rem_minmax(0,1fr)] gap-0 border-b border-border last:border-b-0", compact ? "py-4" : "py-5"].join(" ")}>
      {!last && <div className="absolute bottom-0 left-[2.18rem] top-10 w-px bg-border" />}
      <div className="z-[1] flex justify-center">
        <StepCircle status={status} index={index} />
      </div>
      <div className="min-w-0 pr-4">
        <div className="flex min-w-0 flex-wrap items-baseline justify-between gap-2">
          <div className="flex min-w-0 flex-wrap items-baseline gap-2">
            <h3 className="truncate text-base font-semibold">{action.name}</h3>
            {status && (
              <span className={["text-xs font-bold uppercase tracking-widest", toneTextClass(visual.tone)].join(" ")}>{visual.label}</span>
            )}
          </div>
          {(startTime !== "-" || duration !== "-") && (
            <div className="ml-auto flex shrink-0 items-center gap-2 text-sm text-muted-foreground">
              {startTime !== "-" && <span className="font-mono">{startTime}</span>}
              {startTime !== "-" && duration !== "-" && <span>-</span>}
              {duration !== "-" && (
                <span className="inline-flex items-center gap-1 font-mono">
                  <Icon name="lucide:clock" className="h-3.5 w-3.5" />
                  {duration}
                </span>
              )}
            </div>
          )}
        </div>
        {progressText && <div className="mt-2 text-sm font-medium text-muted-foreground">{progressText}</div>}
        {progressPercent !== null && (
          <div className="mt-2 flex items-center gap-3">
            <div className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-muted">
              <div className="h-full rounded-full bg-sky-500" style={{ width: `${progressPercent}%` }} />
            </div>
            <span className="font-mono text-xs text-muted-foreground">{progressPercent}%</span>
          </div>
        )}
        {(hasBody || hasDetailWarning) && (
          <RunStepOutput action={action} stepCount={stepCount} actionDetailsError={hasDetailWarning ? actionDetailsError : undefined} />
        )}
      </div>
    </article>
  );
}

function RunStepOutput({ action, stepCount, actionDetailsError }: { action: PlaybookRunAction; stepCount: number; actionDetailsError?: string }) {
  const output = primaryActionOutput(action);
  const diagnostics = errorDiagnosticsFromAction(action);
  const maxHeight = stepOutputMaxHeight(stepCount);
  const [menuOpen, setMenuOpen] = useState(false);
  const [detailView, setDetailView] = useState<OutputDetailView | null>(null);
  return (
    <div className="mt-3 grid gap-2">
      {diagnostics && <ErrorDetails diagnostics={diagnostics} />}
      {output ? (
        <div className="relative">
          <OutputTextBlock output={output} style={maxHeight} />
          <OutputToolbar
            action={action}
            output={output}
            open={menuOpen}
            onOpenChange={setMenuOpen}
            onDetailView={setDetailView}
          />
        </div>
      ) : action.result && !diagnostics ? (
        <div className="relative overflow-hidden rounded-md border border-border">
          <div className="overflow-auto" style={maxHeight}>
            <JsonView data={action.result} defaultOpenDepth={1} />
          </div>
          <OutputToolbar
            action={action}
            output={output}
            open={menuOpen}
            onOpenChange={setMenuOpen}
            onDetailView={setDetailView}
          />
        </div>
      ) : actionDetailsError ? (
        <div className="rounded-md border border-amber-300/50 bg-amber-500/10 px-3 py-2 text-sm text-amber-900">
          Failed to load action details: {actionDetailsError}
        </div>
      ) : null}
      {(action.artifacts?.length ?? 0) > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {(action.artifacts ?? []).map((artifact, artifactIndex) => (
            <ArtifactBadge key={artifact.id ?? artifact.path ?? artifactIndex} artifact={artifact} index={artifactIndex} />
          ))}
        </div>
      )}
      {detailView && <OutputDetailDialog view={detailView} action={action} output={output} onClose={() => setDetailView(null)} />}
    </div>
  );
}

type OutputDetailView = "expanded" | "raw" | "fields";

function OutputTextBlock({ output, className = "", style }: { output: string; className?: string; style?: CSSProperties }) {
  const mode = outputTextMode(output);
  const table = mode === "table";
  const segments = ansiSegments(output);
  return (
    <div className={["overflow-auto rounded-md bg-slate-950 shadow-inner", className].filter(Boolean).join(" ")} style={style}>
      <pre
        className={[
          "p-4 pr-20 font-mono text-sm leading-6 text-slate-200",
          table ? "min-w-max whitespace-pre" : "whitespace-pre-wrap break-words [overflow-wrap:anywhere]",
        ].join(" ")}
      >
        {segments.map((segment, index) => (
          <span key={index} style={segment.style}>{segment.text}</span>
        ))}
      </pre>
    </div>
  );
}

function OutputToolbar({
  action,
  output,
  open,
  onOpenChange,
  onDetailView,
}: {
  action: PlaybookRunAction;
  output: string | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDetailView: (view: OutputDetailView) => void;
}) {
  const itemClass = "flex h-8 w-full items-center gap-2 px-3 text-left text-xs hover:bg-accent/50 disabled:cursor-not-allowed disabled:opacity-50";
  const hasOutput = Boolean(output);
  const hasResult = Boolean(action.result);
  const hasError = Boolean(action.error);
  return (
    <div className="absolute right-2 top-2 z-10 flex items-center gap-1">
      <button
        type="button"
        onClick={() => onDetailView("expanded")}
        className="flex h-7 w-7 items-center justify-center rounded-md border border-white/10 bg-black/40 text-slate-300 backdrop-blur hover:bg-black/70 hover:text-white"
        aria-label="Expand output"
        title="Expand output"
      >
        <Icon name="lucide:expand" />
      </button>
      <button
        type="button"
        onClick={() => onOpenChange(!open)}
        className="flex h-7 w-7 items-center justify-center rounded-md border border-white/10 bg-black/40 text-slate-300 backdrop-blur hover:bg-black/70 hover:text-white"
        aria-label="Output actions"
        aria-expanded={open}
      >
        <Icon name="lucide:ellipsis" />
      </button>
      {open && (
        <div className="absolute right-0 top-8 z-20 w-44 overflow-hidden rounded-md border border-border bg-popover py-1 text-popover-foreground shadow-lg">
          <button type="button" disabled={!hasOutput} className={itemClass} onClick={() => {
            copyText(output ?? "");
            onOpenChange(false);
          }}>
            <Icon name="lucide:copy" />
            Copy output
          </button>
          <button type="button" disabled={!hasOutput} className={itemClass} onClick={() => {
            downloadText(output ?? "", `${action.name || action.id}.txt`);
            onOpenChange(false);
          }}>
            <Icon name="lucide:download" />
            Download output
          </button>
          {hasError && (
            <button type="button" className={itemClass} onClick={() => {
              copyText(action.error ?? "");
              onOpenChange(false);
            }}>
              <Icon name="lucide:triangle-alert" />
              Copy error
            </button>
          )}
          <button type="button" disabled={!hasResult} className={itemClass} onClick={() => {
            onDetailView("raw");
            onOpenChange(false);
          }}>
            <Icon name="lucide:braces" />
            View raw result
          </button>
          <button type="button" disabled={!hasResult && !hasError && (action.artifacts?.length ?? 0) === 0} className={itemClass} onClick={() => {
            onDetailView("fields");
            onOpenChange(false);
          }}>
            <Icon name="lucide:list-tree" />
            View all fields
          </button>
        </div>
      )}
    </div>
  );
}

function OutputDetailDialog({
  view,
  action,
  output,
  onClose,
}: {
  view: OutputDetailView;
  action: PlaybookRunAction;
  output: string | null;
  onClose: () => void;
}) {
  const data = view === "raw" ? action.result : actionDetailData(action, output);
  const title = view === "expanded" ? "Action output" : view === "raw" ? "Raw result" : "Action result fields";
  return (
    <Modal
      open
      onClose={onClose}
      size={view === "expanded" ? "full" : "lg"}
      className={view === "expanded" ? "h-[95vh] overflow-hidden" : "max-h-[88vh] overflow-hidden"}
      headerSlot={<span className="min-w-0 flex-1 truncate text-sm font-semibold">{title}</span>}
    >
      {view === "expanded" ? (
        <ExpandedOutputBody action={action} output={output} />
      ) : (
        <div className="max-h-[72vh] overflow-auto rounded-md border border-border bg-background p-3">
          {data ? <JsonView data={data} defaultOpenDepth={2} /> : <DetailEmptyState icon="lucide:file-question" label="No result" />}
        </div>
      )}
    </Modal>
  );
}

function ExpandedOutputBody({ action, output }: { action: PlaybookRunAction; output: string | null }) {
  if (output) {
    return <OutputTextBlock output={output} className="h-[calc(95vh-8rem)]" />;
  }
  const data = action.result ?? actionDetailData(action, output);
  return (
    <div className="h-[calc(95vh-8rem)] overflow-auto rounded-md border border-border bg-background p-3">
      {data ? <JsonView data={data} defaultOpenDepth={2} /> : <DetailEmptyState icon="lucide:file-question" label="No result" />}
    </div>
  );
}

function actionDetailData(action: PlaybookRunAction, output: string | null) {
  return {
    output,
    error: action.error ?? null,
    result: action.result ?? null,
    artifacts: action.artifacts ?? [],
  };
}

function ErrorDetails({ diagnostics }: { diagnostics: ErrorDiagnostics }) {
  const scalarContext = diagnostics.context.filter(([, value]) => !parseInlineJsonContextValue(value));
  const jsonContext = diagnostics.context
    .map(([label, value]) => ({ label, value, data: parseInlineJsonContextValue(value) }))
    .filter((entry): entry is { label: string; value: string; data: unknown } => entry.data !== null);
  return (
    <details className="group rounded-md border border-destructive/30 bg-destructive/5">
      <summary className="flex cursor-pointer list-none items-start gap-2 p-3">
        <Icon name="lucide:triangle-alert" className="mt-0.5 shrink-0 text-destructive" />
        <div className="min-w-0 flex-1">
          <div className="text-sm font-semibold text-destructive">Error</div>
          <div className="mt-1 whitespace-pre-wrap text-sm text-destructive">{diagnostics.message}</div>
        </div>
        <Icon name="lucide:chevron-right" className="mt-0.5 shrink-0 text-muted-foreground transition-transform group-open:rotate-90" />
      </summary>
      <div className="grid gap-3 border-t border-destructive/20 p-3 pt-2">
        {(diagnostics.trace || diagnostics.time) && (
          <div className="flex min-w-0 flex-wrap gap-2">
            {diagnostics.trace && (
              <CopyBadge label="Trace" value={diagnostics.trace} className="max-w-full" />
            )}
            {diagnostics.time && (
              <span className="inline-flex max-w-full items-center overflow-hidden rounded-md border border-border bg-background/80 text-xs">
                <span className="shrink-0 bg-muted px-2 py-1 font-medium text-muted-foreground">Time</span>
                <span className="min-w-0 truncate px-2 py-1 font-mono text-foreground">{diagnostics.time}</span>
              </span>
            )}
          </div>
        )}
        {diagnostics.context.length > 0 && (
          <div className="min-w-0">
            <div className="mb-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Context</div>
            {scalarContext.length > 0 && (
              <div className="flex flex-wrap gap-1.5">
                {scalarContext.map(([label, value]) => (
                  <CopyBadge key={`${label}:${value}`} label={label} value={value} />
                ))}
              </div>
            )}
            {jsonContext.length > 0 && (
              <div className="mt-2 grid gap-2">
                {jsonContext.map(({ label, value, data }) => (
                  <JsonContextValue key={`${label}:${value}`} label={label} value={value} data={data} lines={3} />
                ))}
              </div>
            )}
          </div>
        )}
        {diagnostics.stacktrace && (
          <div className="min-w-0">
            <div className="mb-1.5 flex min-w-0 items-center justify-between gap-3">
              <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Stack trace</div>
              <button
                type="button"
                onClick={() => copyText(diagnostics.stacktrace ?? "")}
                className="inline-flex items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground"
              >
                <Icon name="lucide:copy" />
                copy
              </button>
            </div>
            <PrettyStackTrace stacktrace={diagnostics.stacktrace} />
          </div>
        )}
      </div>
    </details>
  );
}

function PrettyStackTrace({ stacktrace }: { stacktrace: string }) {
  const parsed = parseDiagnosticsStackTrace(stacktrace);
  if (parsed.frames.length === 0) {
    return (
      <pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded-md bg-slate-950 p-3 font-mono text-xs leading-5 text-slate-200">
        {stacktrace}
      </pre>
    );
  }

  return (
    <div className="max-h-96 min-h-40 overflow-auto rounded-md border border-border bg-background p-2">
      <div className="mb-2 flex items-center justify-between gap-2 text-[11px] text-muted-foreground">
        <div className="flex items-center gap-1.5">
          <span className="rounded-full bg-red-100 px-2 py-0.5 text-red-700">error</span>
          <span>{parsed.frames.length} frames</span>
        </div>
      </div>
      {parsed.headline && (
        <div className="mb-2 rounded-md bg-red-50 px-2 py-1.5 font-mono text-[11px] leading-4 text-red-700">
          {parsed.headline}
        </div>
      )}
      <div className="space-y-0.5">
        {parsed.frames.map((frame, index) => (
          <StackFrameRow key={`${frame.file}:${frame.line}:${index}`} frame={frame} index={index} />
        ))}
      </div>
      {parsed.unparsed.length > 0 && (
        <pre className="mt-2 whitespace-pre-wrap rounded bg-muted p-2 font-mono text-[11px] leading-4 text-muted-foreground">
          {parsed.unparsed.join("\n")}
        </pre>
      )}
    </div>
  );
}

function StackFrameRow({ frame, index }: { frame: StackTraceFrame; index: number }) {
  const appFrame = isApplicationStackFrame(frame.file);
  return (
    <button
      type="button"
      onClick={() => copyText(frame.raw)}
      title="Copy stack frame"
      className={["block w-full rounded px-1.5 py-1 text-left hover:bg-accent/50", appFrame ? "text-foreground" : "text-muted-foreground"].join(" ")}
    >
      <div className="flex min-w-0 items-start gap-1.5">
        <Icon
          name={appFrame ? "codicon:symbol-method" : "codicon:debug-step-over"}
          className="mt-0.5 shrink-0 text-[11px]"
        />
        <div className="min-w-0">
          <div className="break-all font-mono text-[11px] font-semibold leading-4">
            <span className="mr-2 text-[10px] font-normal opacity-60">#{index + 1}</span>
            {frame.functionName || "unknown function"}
            <span className="ml-2 text-[10px] font-normal opacity-80">
              {compactStackPath(frame.file)}:{frame.line}
            </span>
          </div>
        </div>
      </div>
    </button>
  );
}

function CopyBadge({ label, value, className = "" }: { label: string; value: string; className?: string }) {
  return (
    <button
      type="button"
      onClick={() => copyText(value)}
      title={`Copy ${label}`}
      className={["inline-flex max-w-full items-center overflow-hidden rounded-md border border-border bg-background/80 text-left text-xs hover:border-primary/40 hover:bg-background", className].filter(Boolean).join(" ")}
    >
      <span className="shrink-0 bg-muted px-2 py-1 font-medium text-muted-foreground">{label}</span>
      <span className="min-w-0 truncate px-2 py-1 font-mono text-foreground">{value}</span>
      <Icon name="lucide:copy" className="mr-1.5 h-3 w-3 shrink-0 text-muted-foreground" />
    </button>
  );
}

function JsonContextValue({
  label,
  value,
  data,
  lines,
}: {
  label: string;
  value: string;
  data: unknown;
  lines: number;
}) {
  const [expanded, setExpanded] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const canExpand = jsonLineCount(data) > lines;
  const formatted = JSON.stringify(data, null, 2);
  return (
    <div className="relative min-w-[18rem] max-w-full overflow-hidden rounded-md border border-border bg-background/80 text-xs">
      <div className="flex min-w-0 items-center justify-between gap-2 bg-muted px-2 py-1">
        <span className="min-w-0 truncate font-mono font-semibold text-muted-foreground">{label}</span>
        <JsonValueToolbar
          label={label}
          rawValue={value}
          formattedValue={formatted}
          open={menuOpen}
          onOpenChange={setMenuOpen}
          onExpand={() => setDialogOpen(true)}
        />
      </div>
      <div
        className="overflow-hidden px-2 py-1.5"
        style={expanded ? undefined : { maxHeight: `${lines * 1.5}rem` }}
      >
        <JsonView data={data} defaultOpenDepth={2} />
      </div>
      {canExpand && (
        <button
          type="button"
          onClick={() => setExpanded((current) => !current)}
          className="flex w-full items-center justify-center gap-1 border-t border-border px-2 py-1 text-xs font-medium text-muted-foreground hover:bg-accent/40 hover:text-foreground"
        >
          <span>{expanded ? "less" : "more"}</span>
          <Icon name={expanded ? "lucide:chevron-up" : "lucide:chevron-down"} className="h-3 w-3" />
        </button>
      )}
      {dialogOpen && (
        <JsonValueDialog label={label} data={data} onClose={() => setDialogOpen(false)} />
      )}
    </div>
  );
}

function JsonValueToolbar({
  label,
  rawValue,
  formattedValue,
  open,
  onOpenChange,
  onExpand,
}: {
  label: string;
  rawValue: string;
  formattedValue: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onExpand: () => void;
}) {
  const itemClass = "flex h-8 w-full items-center gap-2 px-3 text-left text-xs hover:bg-accent/50";
  return (
    <div className="relative z-10 flex shrink-0 items-center gap-1">
      <button
        type="button"
        onClick={onExpand}
        className="flex h-6 w-6 items-center justify-center rounded-md text-muted-foreground hover:bg-background hover:text-foreground"
        aria-label={`Expand ${label}`}
        title={`Expand ${label}`}
      >
        <Icon name="lucide:expand" className="h-3.5 w-3.5" />
      </button>
      <button
        type="button"
        onClick={() => onOpenChange(!open)}
        className="flex h-6 w-6 items-center justify-center rounded-md text-muted-foreground hover:bg-background hover:text-foreground"
        aria-label={`${label} actions`}
        aria-expanded={open}
      >
        <Icon name="lucide:ellipsis" className="h-3.5 w-3.5" />
      </button>
      {open && (
        <div className="absolute right-0 top-7 z-20 w-40 overflow-hidden rounded-md border border-border bg-popover py-1 text-popover-foreground shadow-lg">
          <button type="button" className={itemClass} onClick={() => {
            copyText(formattedValue);
            onOpenChange(false);
          }}>
            <Icon name="lucide:copy" />
            Copy JSON
          </button>
          <button type="button" className={itemClass} onClick={() => {
            copyText(rawValue);
            onOpenChange(false);
          }}>
            <Icon name="lucide:clipboard" />
            Copy raw
          </button>
          <button type="button" className={itemClass} onClick={() => {
            downloadText(formattedValue, `${safeFilename(label)}.json`);
            onOpenChange(false);
          }}>
            <Icon name="lucide:download" />
            Download JSON
          </button>
        </div>
      )}
    </div>
  );
}

function JsonValueDialog({ label, data, onClose }: { label: string; data: unknown; onClose: () => void }) {
  return (
    <Modal
      open
      onClose={onClose}
      size="full"
      className="h-[95vh] overflow-hidden"
      headerSlot={<span className="min-w-0 flex-1 truncate text-sm font-semibold">{label}</span>}
    >
      <div className="h-[calc(95vh-8rem)] overflow-auto rounded-md border border-border bg-background p-3">
        <JsonView data={data} defaultOpenDepth={2} />
      </div>
    </Modal>
  );
}

function safeFilename(value: string) {
  return value.toLowerCase().replace(/[^a-z0-9._-]+/g, "-").replace(/^-|-$/g, "") || "value";
}

function RunSideRail({
  run,
  actions,
  childRuns,
}: {
  run: PlaybookRun;
  actions: PlaybookRunAction[];
  childRuns: PlaybookRun[];
}) {
  const artifacts = flattenArtifacts(actions);
  const parameterGroups = stepParameterGroups(run, actions);
  const runDiagnostics = errorDiagnosticsFromRun(run);
  return (
    <aside className="grid min-w-0 gap-4 xl:self-start">
      <RailPanel
        title="Step parameters"
        icon="config"
        count={parameterGroups.reduce((total, group) => total + group.entries.length, 0)}
        action={parameterGroups.length > 0 ? (
          <button
            type="button"
            onClick={() => copyText(parameterGroupsToText(parameterGroups))}
            className="text-sm font-medium text-muted-foreground hover:text-foreground"
          >
            copy all
          </button>
        ) : undefined}
      >
        <StepParameterGroups groups={parameterGroups} />
      </RailPanel>
      {runDiagnostics && (
        <RailPanel title="Run error" icon="scorecard-fail">
          <ErrorDetails diagnostics={runDiagnostics} />
        </RailPanel>
      )}
      <RailPanel title="Artifacts" icon="logs">
        {artifacts.length === 0 ? (
          <div className="text-sm text-muted-foreground">No artifacts</div>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {artifacts.map((artifact, index) => (
              <ArtifactBadge key={artifact.id ?? artifact.path ?? index} artifact={artifact} index={index} />
            ))}
          </div>
        )}
      </RailPanel>
      <RailPanel title="Related runs" icon="activity-feed">
        {childRuns.length === 0 ? (
          <div className="text-sm text-muted-foreground">No child runs</div>
        ) : (
          <div className="grid gap-2">
            {childRuns.map((child) => (
              <a
                key={child.id}
                href={`/ui/playbooks/runs/${encodeURIComponent(child.id)}`}
                className="grid gap-1 rounded-md border border-border p-2 text-sm hover:bg-accent/30"
              >
                <div className="flex min-w-0 items-center justify-between gap-2">
                  <span className="truncate font-medium">{playbookName(child.playbooks, child.playbook_id)}</span>
                  <StatusDot status={child.status} />
                </div>
                <span className="truncate font-mono text-xs text-muted-foreground">{child.id}</span>
              </a>
            ))}
          </div>
        )}
      </RailPanel>
    </aside>
  );
}

function RailPanel({
  title,
  icon,
  count,
  action,
  children,
}: {
  title: string;
  icon: string;
  count?: number;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="overflow-hidden rounded-lg border border-border bg-background shadow-sm">
      <div className="flex min-w-0 items-center justify-between gap-3 border-b border-border px-4 py-3">
        <div className="flex min-w-0 items-center gap-2">
          <ConfigIcon primary={icon} className="h-4 max-w-4" />
          <h2 className="truncate text-base font-semibold">{title}</h2>
          {typeof count === "number" && (
            <span className="rounded-full bg-muted px-2 py-0.5 text-xs font-semibold text-muted-foreground">{count}</span>
          )}
        </div>
        {action}
      </div>
      <div className="p-4">{children}</div>
    </section>
  );
}

function StepParameterGroups({ groups }: { groups: StepParameterGroup[] }) {
  if (groups.length === 0) return <div className="text-sm text-muted-foreground">No parameters</div>;
  return (
    <div className="grid gap-3">
      {groups.map((group) => (
        <div key={group.key} className="min-w-0">
          <div className="mb-1.5 flex min-w-0 items-center gap-2 text-sm font-semibold">
            <span className={["flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-xs", toneCircleClass(group.tone)].join(" ")}>
              {group.index !== undefined ? group.index + 1 : <ConfigIcon primary="config" className="h-3 max-w-3" />}
            </span>
            <span className="truncate">{group.label}</span>
          </div>
          <div className="flex flex-wrap gap-1.5">
            {group.entries.map(([key, value]) => (
              <span key={key} className="inline-flex max-w-full items-center overflow-hidden rounded-md border border-border bg-muted/30 text-xs">
                <span className="shrink-0 bg-muted px-2 py-1 font-mono font-semibold text-muted-foreground">{key}</span>
                <span className="min-w-0 truncate px-2 py-1 font-mono text-foreground">{value}</span>
              </span>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function RunTimeline({ events }: { events: RunTimelineEvent[] }) {
  if (events.length === 0) return <div className="text-sm text-muted-foreground">No timeline events</div>;
  return (
    <div className="grid gap-0">
      {events.map((event, index) => (
        <div key={`${event.label}-${event.timestamp}-${index}`} className="flex min-w-0 gap-3 border-b border-border/60 py-2 text-sm last:border-b-0">
          <span className="w-16 shrink-0 font-mono text-muted-foreground">{formatClockTime(event.timestamp)}</span>
          <span className={["min-w-0", event.tone ? toneTextClass(event.tone) : "text-foreground"].join(" ")}>{event.label}</span>
        </div>
      ))}
    </div>
  );
}

function SubmitPlaybookRunDialog({
  open,
  playbook,
  target = {},
  resourceLabel,
  onClose,
}: {
  open: boolean;
  playbook: RunnablePlaybook;
  target?: PlaybookRunTarget;
  resourceLabel?: string;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [parameters, setParameters] = useState<PlaybookParameter[]>([]);
  const [values, setValues] = useState<Record<string, string>>({});
  const [runTarget, setRunTarget] = useState<PlaybookRunTarget>(target);
  const [paramsError, setParamsError] = useState<string | null>(null);
  const [loadingParams, setLoadingParams] = useState(false);
  const fixedNonConfigTarget = Boolean(target.component_id || target.check_id);
  const configTargetSelectors = playbookResourceSelectors(playbook.spec, "configs");
  const mutation = useMutation({
    mutationFn: (request: PlaybookRunSubmitRequest) => submitPlaybookRun(request),
    onSuccess: (response) => {
      queryClient.invalidateQueries({ queryKey: ["playbook_runs"] });
      window.history.pushState(null, "", `/ui/playbooks/runs/${encodeURIComponent(response.run_id)}`);
      window.dispatchEvent(new PopStateEvent("popstate"));
      onClose();
    },
  });

  useEffect(() => {
    if (!open) return;
    setRunTarget(target);
  }, [open, target.config_id, target.component_id, target.check_id]);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setLoadingParams(true);
    setParamsError(null);
    getPlaybookParams(playbook.id, runTarget)
      .then((resolved) => {
        if (cancelled) return;
        setParameters(resolved);
        setValues(normalizePlaybookParams(resolved));
      })
      .catch((err) => {
        if (cancelled) return;
        const fallback = normalizeParameterList(playbook.parameters ?? playbook.spec?.parameters);
        setParameters(fallback);
        setValues(normalizePlaybookParams(fallback));
        setParamsError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (!cancelled) setLoadingParams(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, playbook.id, playbook.parameters, playbook.spec, runTarget.config_id, runTarget.component_id, runTarget.check_id]);

  const canSubmit = !loadingParams && !mutation.isPending && requiredParamsPresent(parameters, values);

  return (
    <Modal
      open={open}
      onClose={onClose}
      size="lg"
      className="max-h-[88vh] overflow-hidden"
      headerSlot={
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <PlaybookIcon playbook={playbook} className="h-5 max-w-5" />
          <span className="truncate text-sm font-semibold">Run {displayPlaybookName(playbook)}</span>
        </div>
      }
    >
      <form
        className="flex max-h-[72vh] min-h-[22rem] flex-col gap-4 overflow-hidden"
        onSubmit={(event) => {
          event.preventDefault();
          mutation.mutate(buildSubmitPayload(playbook.id, runTarget, values));
        }}
      >
        <RunTargetSelector
          target={runTarget}
          fixedLabel={resourceLabel}
          fixedNonConfigTarget={fixedNonConfigTarget}
          configSelectors={configTargetSelectors}
          onChange={setRunTarget}
        />
        {loadingParams ? (
          <div className="flex flex-1 items-center justify-center gap-2 text-sm text-muted-foreground">
            <Icon name="lucide:loader-2" className="animate-spin" />
            <span>Loading parameters...</span>
          </div>
        ) : (
          <div className="min-h-0 flex-1 overflow-auto rounded-md border border-border p-3">
            {parameters.length === 0 ? (
              <DetailEmptyState icon="lucide:list-checks" label="No parameters" />
            ) : (
              <div className="grid gap-3">
                {parameters.map((parameter) => (
                  <PlaybookParameterField
                    key={parameter.name}
                    parameter={parameter}
                    value={values[parameter.name] ?? ""}
                    onChange={(value) => setValues((current) => ({ ...current, [parameter.name]: value }))}
                  />
                ))}
              </div>
            )}
          </div>
        )}
        {(paramsError || mutation.error) && (
          <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">
            {mutation.error instanceof Error ? mutation.error.message : paramsError}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <button type="button" onClick={onClose} className="h-9 rounded-md border border-border px-3 text-sm hover:bg-accent/50">
            Cancel
          </button>
          <button
            type="submit"
            disabled={!canSubmit}
            className="inline-flex h-9 items-center gap-2 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground disabled:cursor-not-allowed disabled:opacity-50"
          >
            <Icon name={mutation.isPending ? "lucide:loader-2" : "lucide:play"} className={mutation.isPending ? "animate-spin" : undefined} />
            <span>Run</span>
          </button>
        </div>
      </form>
    </Modal>
  );
}

function EditPlaybookDialog({
  playbook,
  onClose,
}: {
  playbook: RunnablePlaybook;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [namespace, setNamespace] = useState(playbook.namespace ?? "");
  const [name, setName] = useState(playbook.name ?? "");
  const [title, setTitle] = useState(playbook.title ?? "");
  const [category, setCategory] = useState(playbook.category ?? "");
  const [icon, setIcon] = useState(playbook.icon ?? "");
  const [description, setDescription] = useState(playbook.description ?? "");
  const [specText, setSpecText] = useState(() => JSON.stringify(playbook.spec ?? {}, null, 2));
  const [validationError, setValidationError] = useState<string | null>(null);
  const mutation = useMutation({
    mutationFn: (request: PlaybookUpdateRequest) => updatePlaybook(request),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["playbooks"] });
      onClose();
    },
  });

  return (
    <Modal
      open
      onClose={onClose}
      size="lg"
      className="max-h-[88vh] overflow-hidden"
      headerSlot={
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <PlaybookIcon playbook={playbook} className="h-5 max-w-5" />
          <span className="truncate text-sm font-semibold">Edit {displayPlaybookName(playbook)}</span>
        </div>
      }
    >
      <form
        className="flex max-h-[74vh] min-h-[28rem] flex-col gap-4 overflow-hidden"
        onSubmit={(event) => {
          event.preventDefault();
          setValidationError(null);
          let spec: PlaybookUpdateRequest["spec"];
          try {
            spec = JSON.parse(specText);
          } catch (err) {
            setValidationError(err instanceof Error ? err.message : String(err));
            return;
          }
          mutation.mutate({
            id: playbook.id,
            namespace: namespace.trim(),
            name: name.trim(),
            title: title.trim(),
            category: category.trim(),
            icon: icon.trim(),
            description: description.trim(),
            source: playbook.source ?? "",
            spec,
          });
        }}
      >
        <div className="grid gap-3 md:grid-cols-2">
          <label className="grid gap-1.5 text-sm">
            <span className="font-medium">Namespace</span>
            <input value={namespace} onChange={(event) => setNamespace(event.target.value)} required className={formInputClass} />
          </label>
          <label className="grid gap-1.5 text-sm">
            <span className="font-medium">Name</span>
            <input value={name} onChange={(event) => setName(event.target.value)} required className={formInputClass} />
          </label>
          <label className="grid gap-1.5 text-sm">
            <span className="font-medium">Title</span>
            <input value={title} onChange={(event) => setTitle(event.target.value)} className={formInputClass} />
          </label>
          <label className="grid gap-1.5 text-sm">
            <span className="font-medium">Category</span>
            <input value={category} onChange={(event) => setCategory(event.target.value)} className={formInputClass} />
          </label>
          <label className="grid gap-1.5 text-sm md:col-span-2">
            <span className="font-medium">Icon</span>
            <input value={icon} onChange={(event) => setIcon(event.target.value)} className={formInputClass} />
          </label>
          <label className="grid gap-1.5 text-sm md:col-span-2">
            <span className="font-medium">Description</span>
            <input value={description} onChange={(event) => setDescription(event.target.value)} className={formInputClass} />
          </label>
        </div>
        <label className="grid min-h-0 flex-1 gap-1.5 text-sm">
          <span className="font-medium">Spec</span>
          <textarea
            value={specText}
            onChange={(event) => setSpecText(event.target.value)}
            className={`${formInputClass} min-h-0 flex-1 resize-none font-mono text-xs leading-5`}
          />
        </label>
        {(validationError || mutation.error) && (
          <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">
            {mutation.error instanceof Error ? mutation.error.message : validationError}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <button type="button" onClick={onClose} className="h-9 rounded-md border border-border px-3 text-sm hover:bg-accent/50">
            Cancel
          </button>
          <button
            type="submit"
            disabled={mutation.isPending || !namespace.trim() || !name.trim()}
            className="inline-flex h-9 items-center gap-2 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground disabled:cursor-not-allowed disabled:opacity-50"
          >
            <Icon name={mutation.isPending ? "lucide:loader-2" : "lucide:save"} className={mutation.isPending ? "animate-spin" : undefined} />
            <span>Save</span>
          </button>
        </div>
      </form>
    </Modal>
  );
}

const formInputClass = "w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm outline-none focus:border-primary";

function RunTargetSelector({
  target,
  fixedLabel,
  fixedNonConfigTarget,
  configSelectors,
  onChange,
}: {
  target: PlaybookRunTarget;
  fixedLabel?: string;
  fixedNonConfigTarget: boolean;
  configSelectors?: ResourceSelector[];
  onChange: (target: PlaybookRunTarget) => void;
}) {
  if (fixedNonConfigTarget) {
    return (
      <div className="rounded-md border border-border bg-muted/20 p-3 text-sm">
        <div className="mb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">Target</div>
        <div className="flex min-w-0 items-center gap-2">
          <ConfigIcon primary="target" className="h-4 max-w-4 shrink-0" />
          <span className="truncate font-medium">{fixedLabel || target.component_id || target.check_id || "Selected target"}</span>
        </div>
      </div>
    );
  }

  return (
    <div className="grid gap-2 rounded-md border border-border bg-muted/15 p-3">
      <div className="flex items-center justify-between gap-2">
        <label className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Run target</label>
        {target.config_id && (
          <button
            type="button"
            onClick={() => onChange({})}
            className="text-xs text-muted-foreground hover:text-foreground"
          >
            Clear
          </button>
        )}
      </div>
      <ConfigItemSelector
        valueId={target.config_id}
        valueLabel={fixedLabel}
        selectors={configSelectors}
        placeholder={target.config_id ? "Search to change target..." : "Search configs to run against..."}
        onSelect={(config) => onChange(config ? { config_id: config.id } : {})}
      />
      {!target.config_id && (
        <div className="text-xs text-muted-foreground">
          Leave empty to run without a config target.
        </div>
      )}
    </div>
  );
}

function PlaybookParameterField({
  parameter,
  value,
  onChange,
}: {
  parameter: PlaybookParameter;
  value: string;
  onChange: (value: string) => void;
}) {
  const label = parameter.label || parameter.name;
  const commonClass = "w-full rounded-md border border-border bg-background px-2 py-1.5 text-sm outline-none focus:border-primary";
  const properties = parameter.properties ?? {};
  const options = normalizeOptions(properties.options);
  const multiline = properties.multiline === true || parameter.type === "code";

  return (
    <label className="grid gap-1.5 text-sm">
      <span className="flex min-w-0 items-center gap-2 font-medium">
        {parameter.icon && <SmartIcon name={parameter.icon} className="h-4 max-w-4 shrink-0 text-muted-foreground" />}
        <span className="truncate">{label}</span>
        {parameter.required && <Badge tone="warning" size="xxs">Required</Badge>}
        {parameter.type && <Badge size="xxs">{parameter.type}</Badge>}
      </span>
      {parameter.description && <span className="text-xs text-muted-foreground">{parameter.description}</span>}
      {parameter.type === "checkbox" ? (
        <span className="inline-flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={value === "true"}
            onChange={(event) => onChange(event.target.checked ? "true" : "false")}
          />
          Enabled
        </span>
      ) : parameter.type === "list" && options.length > 0 ? (
        <select value={value} onChange={(event) => onChange(event.target.value)} className={`h-9 ${commonClass}`}>
          <option value="">Select...</option>
          {options.map((option) => (
            <option key={option.value} value={option.value}>{option.label}</option>
          ))}
        </select>
      ) : parameter.type === "config" ? (
        <ConfigItemSelector
          valueId={value || undefined}
          selectors={configParameterSelectors(parameter)}
          placeholder="Search configs..."
          onSelect={(config) => onChange(config?.id ?? "")}
        />
      ) : multiline ? (
        <textarea
          value={value}
          onChange={(event) => onChange(event.target.value)}
          required={parameter.required}
          className={`${commonClass} min-h-28 font-mono text-xs`}
        />
      ) : (
        <input
          value={value}
          type={parameter.type === "secret" ? "password" : inputType(properties.format)}
          min={stringProp(properties.min)}
          max={stringProp(properties.max)}
          minLength={numberProp(properties.minLength)}
          maxLength={numberProp(properties.maxLength)}
          pattern={stringProp(properties.regex)}
          onChange={(event) => onChange(event.target.value)}
          required={parameter.required}
          className={`h-9 ${commonClass}`}
        />
      )}
    </label>
  );
}

function PlaybookRunsTable({ runs }: { runs: PlaybookRun[] }) {
  return (
    <DataTable
      data={runs as unknown as Record<string, unknown>[]}
      columns={runColumns}
      getRowId={(row) => String(row.id)}
      getRowHref={(row) => `/ui/playbooks/runs/${encodeURIComponent(String(row.id))}`}
      defaultSort={{ key: "created_at", dir: "desc" }}
      autoFilter
      renderExpandedRow={(row) => <JsonView data={row} defaultOpenDepth={1} />}
    />
  );
}

function PlaybookRunCard({ playbook, onRun }: { playbook: RunnablePlaybook; onRun: () => void }) {
  return (
    <button
      type="button"
      onClick={onRun}
      className="flex min-w-0 items-start justify-between gap-3 rounded-md border border-border bg-background p-3 text-left hover:border-primary/40 hover:bg-accent/20"
    >
      <div className="flex min-w-0 items-start gap-2">
        <PlaybookIcon playbook={playbook} className="h-5 max-w-5 shrink-0" />
        <div className="min-w-0">
          <div className="truncate font-medium">{displayPlaybookName(playbook)}</div>
          {playbook.description && <div className="mt-1 line-clamp-2 text-xs text-muted-foreground">{playbook.description}</div>}
          <div className="mt-2 flex flex-wrap gap-1">
            {playbook.category && <Badge size="xxs">{playbook.category}</Badge>}
            <Badge size="xxs">{normalizeParameterList(playbook.parameters).length} params</Badge>
          </div>
        </div>
      </div>
      <span className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md border border-border px-2 text-xs font-medium">
        <Icon name="lucide:play" />
        Run
      </span>
    </button>
  );
}

function PlaybookShell({
  title,
  subtitle,
  icon,
  backHref,
  backLabel,
  headerMeta,
  children,
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  icon: string;
  backHref?: string;
  backLabel?: string;
  headerMeta?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <header className="border-b border-border px-6 py-4">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md border border-border bg-muted/50">
            <SmartIcon name={icon} className="h-6 max-w-6 text-xl" />
          </div>
          <div className="min-w-0">
            {backHref && (
              <a href={backHref} className="mb-1 inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
                <Icon name="lucide:arrow-left" />
                {backLabel}
              </a>
            )}
            <h1 className="truncate text-xl font-semibold">{title}</h1>
            {subtitle && <div className="mt-1 truncate text-sm text-muted-foreground">{subtitle}</div>}
            {headerMeta && <div className="mt-2 flex min-w-0 flex-wrap items-center gap-2">{headerMeta}</div>}
          </div>
        </div>
      </header>
      <div className="min-h-0 flex-1 overflow-auto p-5">
        <div className="flex min-w-0 flex-col gap-4">{children}</div>
      </div>
    </div>
  );
}

function PlaybookPageTabs({ active }: { active: "playbooks" | "runs" }) {
  const tabs = [
    { id: "playbooks" as const, label: "Playbooks", icon: "playbook", href: "/ui/playbooks" },
    { id: "runs" as const, label: "Runs", icon: "activity-feed", href: "/ui/playbooks/runs" },
  ];
  return (
    <div className="border-b border-border pb-2">
      <div className="flex flex-wrap items-center gap-2" role="tablist">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={active === tab.id}
            onClick={() => navigateTo(tab.href)}
            className={[
              "inline-flex h-9 items-center gap-2 rounded-md border px-3 text-sm font-medium",
              active === tab.id
                ? "border-primary/40 bg-primary/10 text-primary"
                : "border-border bg-background text-muted-foreground hover:bg-accent/50 hover:text-foreground",
            ].join(" ")}
          >
            <ConfigIcon primary={tab.icon} className="h-4 max-w-4" />
            {tab.label}
          </button>
        ))}
      </div>
    </div>
  );
}

function RunFilters({ status, playbookId }: { status?: string; playbookId?: string }) {
  if (!status && !playbookId) return null;
  return (
    <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
      <span>Filters:</span>
      {status && <Badge size="xs" icon="lucide:activity">Status {status}</Badge>}
      {playbookId && <Badge size="xs" icon="lucide:book-open-check">Playbook {playbookId}</Badge>}
      <a href="/ui/playbooks/runs" className="inline-flex h-7 items-center gap-1 rounded-md border border-border px-2 hover:bg-accent/50">
        <Icon name="lucide:x" />
        Clear
      </a>
    </div>
  );
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
  if (query.isLoading) {
    return <div className="text-sm text-muted-foreground">Loading...</div>;
  }
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

function MetricChip({ icon, label, value }: { icon: string; label: string; value: number }) {
  return (
    <div className="inline-flex h-10 items-center gap-2 rounded-md border border-border bg-muted/25 px-3">
      <ConfigIcon primary={icon} className="h-4 max-w-4" />
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="font-mono text-sm font-semibold">{value}</span>
    </div>
  );
}

function PlaybookIcon({
  playbook,
  className,
}: {
  playbook: Pick<Playbook, "icon" | "name" | "title" | "category">;
  className?: string;
}) {
  return <SmartIcon name={playbookFallbackIcon(playbook)} className={className ?? "h-5 max-w-5"} />;
}

function SmartIcon({ name, className }: { name?: string | null; className?: string }) {
  if (!name) return <ConfigIcon primary="playbook" className={className} />;
  if (name.includes(":") && !name.includes("::")) {
    return <Icon name={name} className={className} />;
  }
  return <ConfigIcon primary={name} className={className} />;
}

function ConfigLink({
  config,
  configId,
  className = "min-w-0 text-sm text-foreground hover:text-primary",
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
  className?: string;
  labelFallback?: string;
}) {
  const query = useConfigDetail(config ? "" : configId ?? "");
  const data = config ?? query.data;
  const id = data?.id ?? configId;
  if (!id) return <span className="text-muted-foreground">-</span>;
  const label = data?.name || labelFallback || id;
  return (
    <a href={`/ui/item/${encodeURIComponent(id)}`} className={["inline-flex max-w-full items-center gap-2", className].join(" ")}>
      <ConfigIcon primary={data?.type || data?.config_class || "config"} className="h-4 max-w-4 shrink-0 text-muted-foreground" />
      <span className="min-w-0 truncate">{label}</span>
      {data?.deleted_at && <Badge tone="danger" size="xxs">Deleted</Badge>}
    </a>
  );
}

function ResourceCell({ run }: { run: PlaybookRun }) {
  const target = targetSummaryFromRun(run);
  if (!target) return <span className="text-muted-foreground">-</span>;
  if (run.config_id) {
    return <ConfigLink config={run.config ?? undefined} configId={run.config_id} labelFallback={target.label} />;
  }
  const content = (
    <span className="flex min-w-0 items-center gap-2">
      <ConfigIcon primary={target.icon} className="h-4 max-w-4 shrink-0 text-muted-foreground" />
      <span className="min-w-0">
        <span className="block truncate font-medium">{target.label}</span>
        {target.detail && <span className="block truncate font-mono text-xs text-muted-foreground">{target.detail}</span>}
      </span>
    </span>
  );
  return content;
}

function StatusBadge({ status }: { status?: string | null }) {
  const visual = statusVisual(status);
  return (
    <span className={["inline-flex h-6 max-w-full items-center gap-1.5 rounded-md border px-2 text-xs font-medium", toneBadgeClass(visual.tone)].join(" ")}>
      <ConfigIcon primary={visual.icon} className="h-3.5 max-w-3.5 shrink-0" />
      <span className="truncate">{visual.label}</span>
    </span>
  );
}

function StatusDot({ status }: { status?: string | null }) {
  const visual = statusVisual(status);
  return (
    <span className={["flex h-5 w-5 shrink-0 items-center justify-center rounded-full", toneCircleClass(visual.tone)].join(" ")} title={visual.label}>
      <ConfigIcon primary={visual.icon} className="h-3 max-w-3" />
    </span>
  );
}

function ActionButton({
  label,
  icon,
  loading = false,
  danger = false,
  onClick,
}: {
  label: string;
  icon: string;
  loading?: boolean;
  danger?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={loading}
      className={[
        "inline-flex h-9 items-center gap-2 rounded-md border px-3 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-60",
        danger
          ? "border-destructive/40 text-destructive hover:bg-destructive/5"
          : "border-border text-foreground hover:bg-accent/50",
      ].join(" ")}
    >
      {loading ? <Icon name="lucide:loader-2" className="animate-spin" /> : <ConfigIcon primary={icon} className="h-4 max-w-4" />}
      {label}
    </button>
  );
}

function ArtifactBadge({ artifact, index }: { artifact: PlaybookArtifact; index: number }) {
  const label = artifact.filename || artifact.path || artifact.id || `Artifact ${index + 1}`;
  return (
    <Badge size="xs" icon="lucide:paperclip" maxWidth="16rem" truncate="auto">
      {label}
    </Badge>
  );
}

const runColumns: DataTableColumn<Record<string, unknown>>[] = [
  {
    key: "playbooks",
    label: "Playbook",
    grow: true,
    render: (value, row) => {
      const playbook = value as Playbook | null;
      return (
        <div className="flex min-w-0 items-center gap-2">
          <PlaybookIcon playbook={playbook ?? { id: String(row.playbook_id ?? ""), name: String(row.playbook_id ?? "") }} className="h-5 max-w-5 shrink-0" />
          <div className="min-w-0">
            <div className="truncate font-medium">{playbookName(playbook, String(row.playbook_id ?? ""))}</div>
            {row.id !== undefined && row.id !== null && (
              <div className="truncate font-mono text-xs text-muted-foreground">{String(row.id).slice(0, 8)}</div>
            )}
          </div>
        </div>
      );
    },
    filterValue: (value, row) => {
      const playbook = value as Playbook | null;
      return [playbook?.title ?? "", playbook?.name ?? "", String(row.playbook_id ?? "")];
    },
  },
  { key: "status", label: "Status", shrink: true, render: (value) => <StatusBadge status={String(value || "")} /> },
  {
    key: "config",
    label: "Resource",
    grow: true,
    render: (_value, row) => <ResourceCell run={row as unknown as PlaybookRun} />,
  },
  { key: "start_time", label: "Started", shrink: true, render: (value, row) => runTimeCell(value, row.scheduled_time), sortValue: (value, row) => new Date(String(value || row.scheduled_time || "")).getTime() },
  { key: "end_time", label: "Duration", shrink: true, render: (_value, row) => playbookRunDuration(row as unknown as PlaybookRun) },
  { key: "created_at", label: "Created", shrink: true, render: (value) => timeAgo(String(value || "")), sortValue: (value) => new Date(String(value)).getTime() },
];

function usePlaybookRunFilters(): PlaybookRunsOptions {
  const search = typeof window === "undefined" ? "" : window.location.search;
  return useMemo(() => {
    const params = new URLSearchParams(search);
    return {
      playbookId: params.get("playbook") || undefined,
      status: params.get("status") || undefined,
      limit: 50,
    };
  }, [search]);
}

function runnableListItemToPlaybook(item: PlaybookListItem): RunnablePlaybook {
  return {
    id: item.id,
    name: item.name,
    title: item.title ?? item.name,
    icon: item.icon,
    description: item.description,
    category: item.category,
    source: item.source,
    namespace: item.namespace,
    created_at: item.created_at,
    parameters: item.parameters,
    spec: item.spec,
  };
}

function recordToRunnablePlaybook(row: Record<string, unknown>): RunnablePlaybook {
  const spec = row.spec && typeof row.spec === "object" ? row.spec as Playbook["spec"] : undefined;
  return {
    id: String(row.id ?? ""),
    name: String(row.name ?? row.title ?? ""),
    title: row.title ? String(row.title) : undefined,
    icon: row.icon ? String(row.icon) : undefined,
    description: row.description ? String(row.description) : undefined,
    category: row.category ? String(row.category) : undefined,
    source: row.source ? String(row.source) : undefined,
    namespace: row.namespace ? String(row.namespace) : undefined,
    created_at: row.created_at ? String(row.created_at) : undefined,
    parameters: row.parameters ?? (spec as { parameters?: PlaybookParameter[] } | null)?.parameters,
    spec,
  };
}

function runToRunnablePlaybook(run: PlaybookRun): RunnablePlaybook {
  if (run.playbooks) {
    return {
      ...run.playbooks,
      parameters: (run.playbooks.spec as { parameters?: PlaybookParameter[] } | null)?.parameters,
    };
  }
  return {
    id: run.playbook_id,
    name: run.playbook_id,
    title: run.playbook_id,
  };
}

function playbookName(playbook: Pick<Playbook, "name" | "title"> | null | undefined, fallback: string) {
  return playbook ? displayPlaybookName(playbook) : fallback;
}

export function shortRunId(id?: string | null) {
  if (!id) return "-";
  if (id.startsWith("run_")) return id;
  return id.length > 8 ? id.slice(0, 8) : id;
}

export function normalizePlaybookParams(parameters: PlaybookParameter[]) {
  return Object.fromEntries(parameters.map((parameter) => [parameter.name, parameterDefaultValue(parameter)]));
}

export function buildSubmitPayload(
  playbookId: string,
  target: PlaybookRunTarget,
  params: Record<string, string>,
): PlaybookRunSubmitRequest {
  const cleanedParams = Object.fromEntries(
    Object.entries(params).filter(([, value]) => value !== ""),
  );
  return {
    id: playbookId,
    ...target,
    params: cleanedParams,
  };
}

export function playbookRunDuration(run: PlaybookRun) {
  const start = parseDate(run.start_time ?? run.scheduled_time);
  if (!start) return "-";
  const end = parseDate(run.end_time) ?? (isFinalPlaybookRunStatus(run.status) ? null : new Date());
  if (!end) return "-";
  return formatDuration(end.getTime() - start.getTime());
}

export function runElapsed(run: PlaybookRun) {
  const start = parseDate(run.start_time ?? run.scheduled_time ?? run.created_at);
  if (!start) return "00:00:00";
  const end = parseDate(run.end_time) ?? (isFinalPlaybookRunStatus(run.status) ? start : new Date());
  return formatClockDuration(end.getTime() - start.getTime());
}

export function stepParameterGroups(run: PlaybookRun, actions: PlaybookRunAction[]): StepParameterGroup[] {
  const groups: StepParameterGroup[] = [];
  actions.forEach((action, index) => {
    const entries = parameterEntries(actionParameters(action));
    if (entries.length === 0) return;
    groups.push({
      key: action.id,
      label: action.name,
      entries,
      index,
      tone: statusVisual(action.status).tone,
    });
  });
  const runEntries = parameterEntries(run.parameters ?? run.request?.params ?? run.request?.parameters);
  if (runEntries.length > 0) {
    groups.unshift({
      key: "run",
      label: "Run parameters",
      entries: runEntries,
      tone: "neutral",
    });
  }
  return groups;
}

export function runTimelineEvents(run: PlaybookRun, actions: PlaybookRunAction[]): RunTimelineEvent[] {
  const events: RunTimelineEvent[] = [];
  addTimelineEvent(events, run.created_at, "Run created");
  addTimelineEvent(events, run.scheduled_time, "Run scheduled", "warning");
  addTimelineEvent(events, run.start_time, "Run started", "info");
  actions.forEach((action, index) => {
    const stepLabel = `Step ${index + 1} - ${action.name}`;
    addTimelineEvent(events, action.start_time ?? action.scheduled_time, `${stepLabel} started`, "info");
    if (action.end_time) {
      addTimelineEvent(events, action.end_time, `${stepLabel} ${action.status === "failed" ? "failed" : "completed"}`, statusVisual(action.status).tone);
    }
  });
  addTimelineEvent(events, run.end_time, `Run ${statusVisual(run.status).label.toLowerCase()}`, statusVisual(run.status).tone);
  return events.sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime());
}

export function errorDiagnosticsFromAction(action: PlaybookRunAction): ErrorDiagnostics | null {
  const record = action as PlaybookRunAction & { diagnostics?: unknown };
  return errorDiagnosticsFromSources(action.error, [
    action.result?.error,
    action.result?.diagnostics,
    record.diagnostics,
    action.result,
  ]);
}

export function errorDiagnosticsFromRun(run: PlaybookRun): ErrorDiagnostics | null {
  const record = run as PlaybookRun & { diagnostics?: unknown };
  return errorDiagnosticsFromSources(run.error, [
    record.diagnostics,
    run.request?.error,
    run.request?.diagnostics,
    run.spec?.error,
    run.spec?.diagnostics,
  ]);
}

export function parseDiagnosticsStackTrace(stacktrace: string): ParsedStackTrace {
  const lines = stacktrace.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
  const frames: StackTraceFrame[] = [];
  const unparsed: string[] = [];
  let headline: string | undefined;

  for (const line of lines) {
    const frame = parseStackTraceFrame(line);
    if (frame) {
      frames.push(frame);
      continue;
    }
    if (!headline && !line.startsWith("--- at ")) {
      headline = line;
      continue;
    }
    unparsed.push(line);
  }

  return { headline, frames, unparsed, raw: stacktrace };
}

function actionDuration(action: PlaybookRunAction) {
  const start = parseDate(action.start_time ?? action.scheduled_time);
  if (!start) return "-";
  const end = actionTerminalDate(action) ?? (isFinalActionStatus(action.status) ? null : new Date());
  if (!end) return "-";
  return formatDuration(end.getTime() - start.getTime());
}

function actionStartTime(action: PlaybookRunAction) {
  return formatClockTime(action.start_time ?? action.scheduled_time);
}

function actionProgressText(action: PlaybookRunAction) {
  const result = action.result ?? {};
  const explicit = firstString(result, ["progress", "progressText", "statusText", "message"]);
  if (explicit) return explicit;
  const done = firstNumber(result, ["done", "completed", "current", "restarted"]);
  const total = firstNumber(result, ["total", "count", "replicas"]);
  if (done !== null && total !== null && total > 0) {
    return `${done} of ${total} complete`;
  }
  return null;
}

function actionProgressPercent(action: PlaybookRunAction) {
  const result = action.result ?? {};
  const explicit = firstNumber(result, ["percent", "progressPercent", "progress_percent", "percentage"]);
  if (explicit !== null) return clampPercent(explicit);
  const done = firstNumber(result, ["done", "completed", "current", "restarted"]);
  const total = firstNumber(result, ["total", "count", "replicas"]);
  if (done !== null && total !== null && total > 0) {
    return clampPercent(Math.round((done / total) * 100));
  }
  return null;
}

export function primaryActionOutput(action: PlaybookRunAction) {
  const result = action.result;
  if (!result) return action.error?.trim() || null;
  const parts = [
    firstString(result, ["stdout", "stdOut", "out", "logs", "log", "output"]),
    firstString(result, ["stderr", "stdErr", "err", "error"]),
  ].filter(Boolean);
  if (parts.length > 0) return parts.join("\n");
  return action.error?.trim() || null;
}

export function actionDisplayStatus(action: PlaybookRunAction) {
  if (isFinalActionStatus(action.status)) return action.status;
  if (actionStartedWithoutTerminalDate(action)) return "running";
  return action.status;
}

export function outputTextMode(output: string): "table" | "text" {
  const lines = output.split(/\r?\n/).filter((line) => line.trim().length > 0);
  if (lines.length < 2) return "text";
  const tableLines = lines.filter((line) => {
    const trimmed = line.trim();
    const pipeCount = (trimmed.match(/\|/g) ?? []).length;
    if (pipeCount >= 2) return true;
    if (/^[+\-|=\s]+$/.test(trimmed) && /[-=]{3,}/.test(trimmed)) return true;
    return /\S+\s{2,}\S+\s{2,}\S+/.test(trimmed);
  });
  return tableLines.length >= 2 ? "table" : "text";
}

type AnsiSegment = {
  text: string;
  style?: CSSProperties;
};

type AnsiStyleState = {
  bold?: boolean;
  dim?: boolean;
  underline?: boolean;
  inverse?: boolean;
  color?: string;
  backgroundColor?: string;
};

const ANSI_COLOR_MAP: Record<number, string> = {
  30: "#475569",
  31: "#f87171",
  32: "#4ade80",
  33: "#facc15",
  34: "#60a5fa",
  35: "#c084fc",
  36: "#22d3ee",
  37: "#e2e8f0",
  90: "#64748b",
  91: "#fca5a5",
  92: "#86efac",
  93: "#fde047",
  94: "#93c5fd",
  95: "#d8b4fe",
  96: "#67e8f9",
  97: "#f8fafc",
};

const ANSI_ESCAPE_RE = /\x1b\[((?:\d|;)*?)m/g;

export function ansiSegments(output: string): AnsiSegment[] {
  const segments: AnsiSegment[] = [];
  const state: AnsiStyleState = {};
  let cursor = 0;
  for (const match of output.matchAll(ANSI_ESCAPE_RE)) {
    const index = match.index ?? 0;
    if (index > cursor) {
      segments.push({ text: output.slice(cursor, index), style: ansiStyle(state) });
    }
    applyAnsiCodes(state, parseAnsiCodes(match[1]));
    cursor = index + match[0].length;
  }
  if (cursor < output.length) {
    segments.push({ text: output.slice(cursor), style: ansiStyle(state) });
  }
  return segments.length > 0 ? segments : [{ text: output }];
}

function parseAnsiCodes(value: string): number[] {
  if (!value) return [0];
  return value.split(";").map((part) => Number(part || "0")).filter((code) => Number.isFinite(code));
}

function applyAnsiCodes(state: AnsiStyleState, codes: number[]) {
  for (let index = 0; index < codes.length; index += 1) {
    const code = codes[index];
    if (code === 0) {
      resetAnsiStyle(state);
    } else if (code === 1) {
      state.bold = true;
      state.dim = false;
    } else if (code === 2) {
      state.dim = true;
      state.bold = false;
    } else if (code === 4) {
      state.underline = true;
    } else if (code === 7) {
      state.inverse = true;
    } else if (code === 22) {
      state.bold = false;
      state.dim = false;
    } else if (code === 24) {
      state.underline = false;
    } else if (code === 27) {
      state.inverse = false;
    } else if (code === 39) {
      delete state.color;
    } else if (code === 49) {
      delete state.backgroundColor;
    } else if ((code >= 30 && code <= 37) || (code >= 90 && code <= 97)) {
      state.color = ANSI_COLOR_MAP[code];
    } else if ((code >= 40 && code <= 47) || (code >= 100 && code <= 107)) {
      state.backgroundColor = ANSI_COLOR_MAP[code - 10] ?? ANSI_COLOR_MAP[code - 60];
    } else if (code === 38 || code === 48) {
      const color = parseExtendedAnsiColor(codes, index);
      if (color) {
        if (code === 38) state.color = color.value;
        else state.backgroundColor = color.value;
        index = color.nextIndex;
      }
    }
  }
}

function parseExtendedAnsiColor(codes: number[], index: number): { value: string; nextIndex: number } | null {
  const mode = codes[index + 1];
  if (mode === 2 && codes.length > index + 4) {
    const [red, green, blue] = codes.slice(index + 2, index + 5).map(clampAnsiColorChannel);
    return { value: `rgb(${red}, ${green}, ${blue})`, nextIndex: index + 4 };
  }
  if (mode === 5 && codes.length > index + 2) {
    return { value: ansi256Color(codes[index + 2]), nextIndex: index + 2 };
  }
  return null;
}

function ansi256Color(code: number) {
  const value = Math.max(0, Math.min(255, code));
  if (value < 16) return ANSI_COLOR_MAP[value >= 8 ? value + 82 : value + 30] ?? "#e2e8f0";
  if (value >= 232) {
    const channel = 8 + (value - 232) * 10;
    return `rgb(${channel}, ${channel}, ${channel})`;
  }
  const offset = value - 16;
  const red = Math.floor(offset / 36);
  const green = Math.floor((offset % 36) / 6);
  const blue = offset % 6;
  const channel = (part: number) => part === 0 ? 0 : 55 + part * 40;
  return `rgb(${channel(red)}, ${channel(green)}, ${channel(blue)})`;
}

function ansiStyle(state: AnsiStyleState): CSSProperties | undefined {
  const color = state.inverse ? state.backgroundColor : state.color;
  const backgroundColor = state.inverse ? state.color : state.backgroundColor;
  const style: CSSProperties = {};
  if (color) style.color = color;
  if (backgroundColor) style.backgroundColor = backgroundColor;
  if (state.bold) style.fontWeight = 700;
  if (state.dim) style.opacity = 0.72;
  if (state.underline) style.textDecoration = "underline";
  return Object.keys(style).length > 0 ? style : undefined;
}

function resetAnsiStyle(state: AnsiStyleState) {
  delete state.bold;
  delete state.dim;
  delete state.underline;
  delete state.inverse;
  delete state.color;
  delete state.backgroundColor;
}

function clampAnsiColorChannel(value: number) {
  return Math.max(0, Math.min(255, value));
}

function isFinalActionStatus(status?: string | null) {
  return status === "completed" || status === "failed" || status === "skipped" || status === "cancelled" || status === "canceled";
}

function actionStartedWithoutTerminalDate(action: PlaybookRunAction) {
  return Boolean(parseDate(action.start_time ?? action.scheduled_time) && !actionTerminalDate(action));
}

function actionTerminalDate(action: PlaybookRunAction) {
  const record = action as PlaybookRunAction & Record<string, unknown>;
  return parseDate(action.end_time)
    ?? parseDate(record.cancelled_at)
    ?? parseDate(record.canceled_at)
    ?? parseDate(record.cancelled_time)
    ?? parseDate(record.canceled_time)
    ?? parseDate(record.cancelled_date)
    ?? parseDate(record.canceled_date);
}

function formatDuration(ms: number) {
  if (!Number.isFinite(ms) || ms < 0) return "-";
  const seconds = Math.round(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  if (minutes < 60) return rest ? `${minutes}m ${rest}s` : `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const restMinutes = minutes % 60;
  return restMinutes ? `${hours}h ${restMinutes}m` : `${hours}h`;
}

function formatClockDuration(ms: number) {
  if (!Number.isFinite(ms) || ms < 0) return "00:00:00";
  const totalSeconds = Math.round(ms / 1000);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  return [hours, minutes, seconds].map((part) => String(part).padStart(2, "0")).join(":");
}

function formatClockTime(value?: string | null) {
  const date = parseDate(value);
  if (!date) return "-";
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });
}

function runTimeCell(value: unknown, scheduled: unknown) {
  const actual = String(value || "");
  if (actual) return timeAgo(actual);
  const fallback = String(scheduled || "");
  return fallback ? `Scheduled ${timeAgo(fallback)}` : "-";
}

function parseDate(value?: string | null) {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

function normalizeParameterList(value: unknown): PlaybookParameter[] {
  if (!value) return [];
  if (Array.isArray(value)) return value as PlaybookParameter[];
  if (typeof value === "string") {
    try {
      const parsed = JSON.parse(value);
      return Array.isArray(parsed) ? parsed as PlaybookParameter[] : [];
    } catch {
      return [];
    }
  }
  return [];
}

function parameterDefaultValue(parameter: PlaybookParameter) {
  if (parameter.default === null || parameter.default === undefined) {
    return parameter.type === "checkbox" ? "false" : "";
  }
  if (typeof parameter.default === "boolean") return parameter.default ? "true" : "false";
  if (typeof parameter.default === "string") return parameter.default;
  return JSON.stringify(parameter.default);
}

function requiredParamsPresent(parameters: PlaybookParameter[], values: Record<string, string>) {
  return parameters.every((parameter) => !parameter.required || values[parameter.name]?.trim());
}

function normalizeOptions(value: unknown): Array<{ label: string; value: string }> {
  if (!Array.isArray(value)) return [];
  return value.map((option) => {
    if (typeof option === "string" || typeof option === "number" || typeof option === "boolean") {
      return { label: String(option), value: String(option) };
    }
    if (option && typeof option === "object") {
      const record = option as Record<string, unknown>;
      const rawValue = record.value ?? record.id ?? record.name ?? record.label;
      const label = record.label ?? record.name ?? rawValue;
      return { label: stringifyValue(label), value: stringifyValue(rawValue) };
    }
    return { label: stringifyValue(option), value: stringifyValue(option) };
  });
}

function inputType(format: unknown) {
  if (format === "number" || format === "email" || format === "url") return format;
  return "text";
}

function stringProp(value: unknown) {
  return value === undefined || value === null ? undefined : String(value);
}

function numberProp(value: unknown) {
  const number = Number(value);
  return Number.isFinite(number) ? number : undefined;
}

function configParameterSelectors(parameter: PlaybookParameter): ResourceSelector[] | undefined {
  const properties = parameter.properties ?? {};
  const filter = resourceSelectorsFromUnknown(properties.filter ?? properties.filters ?? properties.configs);
  if (filter.length > 0) return filter;

  const types = stringArrayProp(properties.types ?? properties.configTypes ?? properties.config_types);
  if (types.length > 0) return [{ types }];

  const type = stringProp(properties.configType ?? properties.config_type ?? properties.resourceType ?? properties.type);
  return type ? [{ types: [type] }] : undefined;
}

function playbookResourceSelectors(spec: RunnablePlaybook["spec"], key: "configs" | "components" | "checks"): ResourceSelector[] | undefined {
  const record = objectRecord(spec);
  if (!record) return undefined;
  const selectors = resourceSelectorsFromUnknown(record[key]);
  return selectors.length > 0 ? selectors : undefined;
}

function resourceSelectorsFromUnknown(value: unknown): ResourceSelector[] {
  if (Array.isArray(value)) {
    return value
      .map((item) => objectRecord(item))
      .filter((item): item is Record<string, unknown> => Boolean(item))
      .map(normalizeResourceSelector);
  }
  const record = objectRecord(value);
  return record ? [normalizeResourceSelector(record)] : [];
}

function normalizeResourceSelector(selector: Record<string, unknown>): ResourceSelector {
  const types = stringArrayProp(selector.types ?? selector.type);
  return {
    ...selector,
    ...(types.length > 0 ? { types } : {}),
  };
}

function stringArrayProp(value: unknown) {
  if (Array.isArray(value)) {
    return value.map((item) => stringProp(item)).filter((item): item is string => Boolean(item));
  }
  if (typeof value === "string") {
    return value.split(",").map((item) => item.trim()).filter(Boolean);
  }
  return [];
}

function errorDiagnosticsFromSources(fallback: string | null | undefined, candidates: unknown[]): ErrorDiagnostics | null {
  for (const candidate of candidates) {
    const diagnostics = normalizeErrorDiagnostics(candidate, fallback);
    if (diagnostics) return diagnostics;
  }
  return fallback ? { message: fallback, context: [] } : null;
}

function normalizeErrorDiagnostics(value: unknown, fallback?: string | null): ErrorDiagnostics | null {
  if (!value) return null;
  if (typeof value === "string") {
    return value.trim() ? { message: value, context: [] } : null;
  }
  const record = objectRecord(value);
  if (!record) return null;
  const nested = objectRecord(record.error) ?? objectRecord(record.diagnostics);
  if (nested && nested !== record) {
    return normalizeErrorDiagnostics(nested, fallback);
  }
  const message = firstString(record, ["error", "message", "msg", "reason", "detail", "details"]) ?? fallback;
  const trace = firstString(record, ["trace", "trace_id", "traceId", "traceID"]);
  const stacktrace = firstString(record, ["stacktrace", "stack_trace", "stackTrace", "stack"]);
  const time = firstString(record, ["time", "timestamp", "created_at"]);
  const context = contextEntries(record.context);
  if (!message && !trace && !stacktrace && context.length === 0) return null;
  return {
    message: message ?? "Action failed",
    trace: trace ?? undefined,
    time: time ?? undefined,
    stacktrace: stacktrace ?? undefined,
    context,
    raw: value,
  };
}

function parseStackTraceFrame(line: string): StackTraceFrame | null {
  const match = line.match(/^--- at (.+):(\d+)(?:\s+(.+))?$/);
  if (!match) return null;
  return {
    raw: line,
    file: match[1],
    line: Number(match[2]),
    functionName: match[3]?.trim(),
  };
}

function compactStackPath(file: string) {
  return file
    .replace(/^github\.com\/flanksource\/incident-commander\//, "")
    .replace(/^github\.com\/flanksource\//, "flanksource/")
    .replace(/^.*\/go\/pkg\/mod\//, "pkg/mod/")
    .replace(/^.*\/incident-commander\//, "");
}

function isApplicationStackFrame(file: string) {
  return file.includes("github.com/flanksource/incident-commander/") || file.includes("/incident-commander/");
}

function objectRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

function contextEntries(value: unknown): Array<[string, string]> {
  const record = objectRecord(value);
  if (!record) return [];
  return Object.entries(record)
    .filter(([key, entryValue]) => !isNativeActionOutputField(key) && entryValue !== undefined && entryValue !== null && entryValue !== "")
    .map(([key, entryValue]) => [key, stringifyValue(entryValue)]);
}

const NATIVE_ACTION_OUTPUT_CONTEXT_FIELDS = new Set([
  "stdout",
  "stdOut",
  "out",
  "output",
  "logs",
  "log",
  "stderr",
  "stdErr",
  "err",
]);

function isNativeActionOutputField(key: string) {
  return NATIVE_ACTION_OUTPUT_CONTEXT_FIELDS.has(key);
}

function parseInlineJsonContextValue(value: string): unknown | null {
  const trimmed = value.trim();
  if (!trimmed || !/^[{[]/.test(trimmed)) return null;
  try {
    return JSON.parse(trimmed);
  } catch {
    return null;
  }
}

function jsonLineCount(value: unknown) {
  return JSON.stringify(value, null, 2).split("\n").length;
}

function actionParameters(action: PlaybookRunAction) {
  const record = action as PlaybookRunAction & {
    parameters?: unknown;
    params?: unknown;
    request?: Record<string, unknown> | null;
  };
  return record.parameters ?? record.params ?? record.request?.params ?? action.result?.parameters ?? action.result?.params;
}

function parameterEntries(value: unknown): Array<[string, string]> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return [];
  return Object.entries(value as Record<string, unknown>)
    .filter(([, entryValue]) => entryValue !== undefined && entryValue !== null && entryValue !== "")
    .map(([key, entryValue]) => [key, stringifyValue(entryValue)]);
}

function addTimelineEvent(
  events: RunTimelineEvent[],
  timestamp: string | null | undefined,
  label: string,
  tone?: PlaybookStatusVisual["tone"],
) {
  if (!timestamp || !parseDate(timestamp)) return;
  events.push({ timestamp, label, tone });
}

function firstString(record: Record<string, unknown>, keys: string[]) {
  for (const key of keys) {
    const value = record[key];
    if (typeof value === "string" && value.trim()) return value;
  }
  return null;
}

function firstNumber(record: Record<string, unknown>, keys: string[]) {
  for (const key of keys) {
    const value = record[key];
    if (typeof value === "number" && Number.isFinite(value)) return value;
    if (typeof value === "string" && value.trim()) {
      const parsed = Number(value);
      if (Number.isFinite(parsed)) return parsed;
    }
  }
  return null;
}

function clampPercent(value: number) {
  return Math.max(0, Math.min(100, Math.round(value)));
}

function parameterGroupsToText(groups: StepParameterGroup[]) {
  return groups
    .map((group) => [
      group.label,
      ...group.entries.map(([key, value]) => `${key}=${value}`),
    ].join("\n"))
    .join("\n\n");
}

function copyText(value: string) {
  if (typeof navigator !== "undefined" && navigator.clipboard) {
    void navigator.clipboard.writeText(value);
  }
}

function downloadText(value: string, filename: string) {
  const blob = new Blob([value], { type: "text/plain;charset=utf-8" });
  const href = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = href;
  anchor.download = filename.replace(/[^\w.-]+/g, "-").toLowerCase();
  anchor.click();
  URL.revokeObjectURL(href);
}

function downloadRunLogs(run: PlaybookRun, actions: PlaybookRunAction[]) {
  const lines = [
    `${playbookName(run.playbooks, run.playbook_id)} ${shortRunId(run.id)}`,
    `status=${run.status ?? "unknown"}`,
    "",
    ...actions.flatMap((action, index) => {
      const output = primaryActionOutput(action);
      return [
        `# Step ${index + 1}: ${action.name}`,
        `status=${action.status ?? "unknown"}`,
        action.error ? `error=${action.error}` : "",
        output ?? (action.result ? JSON.stringify(action.result, null, 2) : ""),
        "",
      ].filter(Boolean);
    }),
  ].join("\n");
  downloadText(lines, `${shortRunId(run.id)}.log`);
}

function flattenArtifacts(actions: PlaybookRunAction[]) {
  return actions.flatMap((action) => action.artifacts ?? []);
}

function toneBadgeClass(tone: PlaybookStatusVisual["tone"]) {
  switch (tone) {
    case "success":
      return "border-emerald-200 bg-emerald-500/10 text-emerald-700";
    case "danger":
      return "border-rose-200 bg-rose-500/10 text-rose-700";
    case "warning":
      return "border-amber-200 bg-amber-500/10 text-amber-800";
    case "info":
      return "border-sky-200 bg-sky-500/10 text-sky-700";
    default:
      return "border-border bg-muted/40 text-muted-foreground";
  }
}

function toneCircleClass(tone: PlaybookStatusVisual["tone"]) {
  switch (tone) {
    case "success":
      return "bg-emerald-500/15 text-emerald-700";
    case "danger":
      return "bg-rose-500/15 text-rose-700";
    case "warning":
      return "bg-amber-500/15 text-amber-800";
    case "info":
      return "bg-sky-500/15 text-sky-700";
    default:
      return "bg-muted text-muted-foreground";
  }
}

function toneTextClass(tone: PlaybookStatusVisual["tone"]) {
  switch (tone) {
    case "success":
      return "text-emerald-700";
    case "danger":
      return "text-rose-700";
    case "warning":
      return "text-amber-800";
    case "info":
      return "text-sky-700";
    default:
      return "text-muted-foreground";
  }
}

function toneStepCircleClass(tone: PlaybookStatusVisual["tone"], emphasized = false) {
  switch (tone) {
    case "success":
      return "border-emerald-200 bg-emerald-50 text-emerald-700";
    case "danger":
      return "border-rose-200 bg-rose-50 text-rose-700";
    case "warning":
      return "border-amber-200 bg-amber-50 text-amber-800";
    case "info":
      return emphasized
        ? "border-sky-200 bg-sky-100 text-sky-700 ring-4 ring-sky-100"
        : "border-sky-200 bg-sky-50 text-sky-700";
    default:
      return "border-border bg-background text-muted-foreground";
  }
}

function navigateTo(href: string) {
  window.history.pushState(null, "", href);
  window.dispatchEvent(new PopStateEvent("popstate"));
}
