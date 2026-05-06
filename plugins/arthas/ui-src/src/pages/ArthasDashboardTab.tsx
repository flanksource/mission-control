import { useMemo, useState } from "react";
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  DiagnosticsDetailPanel,
  ProgressBar,
  SplitPane,
  Tree,
  type ProcessNode,
  type ProgressSegment,
} from "@flanksource/clicky-ui";
import { pluginURL } from "@/lib/api";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";

interface MemoryRow {
  name: string;
  type: string;
  used: number;
  total: number;
  max: number;
}

interface ThreadRow {
  id: number;
  name: string;
  group?: string;
  state?: string;
  cpu: number;
  time: number;
  daemon: boolean;
  priority: number;
  interrupted: boolean;
  deltaTime?: number;
}

interface GCRow {
  name: string;
  collectionCount: number;
  collectionTime: number;
}

interface RuntimeInfo {
  javaHome?: string;
  javaVersion?: string;
  osName?: string;
  osVersion?: string;
  processors?: number;
  systemLoadAverage?: number;
  uptime?: number;
}

interface DashboardResult {
  memoryInfo?: { heap?: MemoryRow[]; nonheap?: MemoryRow[]; buffer_pool?: MemoryRow[] };
  runtimeInfo?: RuntimeInfo;
  gcInfos?: GCRow[];
  threads?: ThreadRow[];
}

async function execArthas(
  sessionId: string,
  command: string,
): Promise<{ results: unknown[]; state: string }> {
  const res = await fetch(pluginURL(`proxy/${sessionId}/api`), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ action: "exec", command }),
  });
  if (!res.ok) throw new Error(`arthas exec (${command}): ${res.status}`);
  const body = await res.json();
  const state = body.state ?? body?.body?.state ?? "UNKNOWN";
  const results = body?.body?.results ?? [];
  if (state !== "SUCCEEDED") {
    throw new Error(`arthas command "${command}" failed (${state}): ${body?.body?.message ?? ""}`);
  }
  return { results, state };
}

function fmtBytes(n: number | undefined): string {
  if (n == null || !Number.isFinite(n) || n < 0) return "–";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

function fmtUptime(ms: number | undefined): string {
  if (!ms) return "–";
  const s = Math.floor(ms / 1000);
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const rem = s % 60;
  const parts = [];
  if (d) parts.push(`${d}d`);
  if (h || d) parts.push(`${h}h`);
  if (m || h || d) parts.push(`${m}m`);
  parts.push(`${rem}s`);
  return parts.join(" ");
}

export function ArthasDashboardTab({ sessionId }: { sessionId: string }) {
  const { data, isLoading, error, dataUpdatedAt } = useQuery({
    queryKey: ["arthas", sessionId, "dashboard"],
    queryFn: async () => {
      const { results } = await execArthas(sessionId, "dashboard -n 1");
      // results is a list; the dashboard payload is the first entry with memoryInfo/etc.
      const payload = (results as DashboardResult[]).find((r) => r?.memoryInfo || r?.threads);
      return payload ?? {};
    },
    refetchInterval: 5_000,
    refetchIntervalInBackground: false,
  });

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <Spinner />
      </div>
    );
  }
  if (error) {
    return (
      <div className="p-4 text-sm text-red-600">
        {error instanceof Error ? error.message : "Failed to load dashboard"}
      </div>
    );
  }
  const d = data ?? {};
  const heap = d.memoryInfo?.heap ?? [];
  const nonheap = d.memoryInfo?.nonheap ?? [];
  const bufpool = d.memoryInfo?.buffer_pool ?? [];
  const threads = d.threads ?? [];
  const rt = d.runtimeInfo ?? {};
  const gc = d.gcInfos ?? [];

  return (
    <div className="flex h-full min-h-0 flex-col gap-4 p-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold">JVM Dashboard</h3>
        <span className="text-xs text-muted-foreground">
          refreshed {new Date(dataUpdatedAt).toLocaleTimeString()}
        </span>
      </div>

      <section className="grid grid-cols-1 gap-2 md:grid-cols-2">
        <InfoCard title="Runtime">
          <KV k="Java" v={`${rt.javaVersion ?? "–"} (${rt.javaHome ?? "–"})`} />
          <KV k="OS" v={`${rt.osName ?? "–"} ${rt.osVersion ?? ""}`} />
          <KV k="CPUs" v={rt.processors?.toString()} />
          <KV k="Load avg" v={rt.systemLoadAverage?.toFixed(2)} />
          <KV k="Uptime" v={fmtUptime(rt.uptime)} />
        </InfoCard>
        <InfoCard title="GC">
          {gc.length === 0 ? (
            <span className="text-xs text-muted-foreground">No GC info.</span>
          ) : (
            gc.map((g) => (
              <KV
                key={g.name}
                k={g.name}
                v={`${g.collectionCount} collections · ${g.collectionTime} ms total`}
              />
            ))
          )}
        </InfoCard>
      </section>

      <section>
        <h4 className="mb-1 text-xs font-semibold uppercase text-muted-foreground">Memory</h4>
        <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
          <MemoryRegionCard title="Heap" rows={heap} palette={HEAP_PALETTE} />
          <MemoryRegionCard title="Non-Heap" rows={nonheap} palette={NONHEAP_PALETTE} />
          <MemoryRegionCard title="Buffer Pools" rows={bufpool} palette={BUFFER_PALETTE} />
        </div>
      </section>

      <section className="flex min-h-0 flex-1 flex-col">
        <h4 className="mb-1 text-xs font-semibold uppercase text-muted-foreground">
          Threads ({threads.length})
        </h4>
        <ThreadsPanel sessionId={sessionId} threads={threads} />
      </section>
    </div>
  );
}

function InfoCard({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="rounded-md border bg-muted/30 p-3">
      <h4 className="mb-2 text-xs font-semibold uppercase text-muted-foreground">{title}</h4>
      <dl className="grid grid-cols-[10rem_1fr] gap-y-1 text-xs">{children}</dl>
    </div>
  );
}

function KV({ k, v }: { k: string; v?: string }) {
  return (
    <>
      <dt className="text-muted-foreground">{k}</dt>
      <dd className="truncate">{v ?? "–"}</dd>
    </>
  );
}

// Tailwind v4 utilities — these must be literal class strings so Tailwind can
// discover them at build time. Do not template color names dynamically.
const HEAP_PALETTE = ["bg-emerald-500", "bg-amber-500", "bg-rose-500", "bg-sky-500"];
const NONHEAP_PALETTE = ["bg-indigo-500", "bg-violet-500", "bg-fuchsia-500", "bg-pink-500"];
const BUFFER_PALETTE = ["bg-cyan-500", "bg-teal-500"];

// Dot variants mirror the bar palette; used for the legend swatches.
const HEAP_DOTS = ["bg-emerald-500", "bg-amber-500", "bg-rose-500", "bg-sky-500"];
const NONHEAP_DOTS = ["bg-indigo-500", "bg-violet-500", "bg-fuchsia-500", "bg-pink-500"];
const BUFFER_DOTS = ["bg-cyan-500", "bg-teal-500"];

function paletteDots(p: string[]): string[] {
  if (p === HEAP_PALETTE) return HEAP_DOTS;
  if (p === NONHEAP_PALETTE) return NONHEAP_DOTS;
  return BUFFER_DOTS;
}

function MemoryRegionCard({
  title,
  rows,
  palette,
}: {
  title: string;
  rows: MemoryRow[];
  palette: string[];
}) {
  if (!rows.length) {
    return (
      <div className="rounded-md border bg-muted/20 p-3">
        <h5 className="text-xs font-semibold uppercase text-muted-foreground">{title}</h5>
        <p className="mt-2 text-xs text-muted-foreground">No data.</p>
      </div>
    );
  }

  // The first row is the region aggregate (e.g. "heap"), remaining rows are sub-pools.
  const [agg, ...pools] = rows;
  const denom = agg.max > 0 ? agg.max : agg.total;
  const usedPct = denom > 0 ? Math.min(100, (agg.used / denom) * 100) : 0;
  const dots = paletteDots(palette);

  const segments: ProgressSegment[] = pools.length
    ? pools.map((p, i) => ({
        count: p.used,
        color: palette[i % palette.length],
        label: p.name,
      }))
    : [{ count: agg.used, color: palette[0], label: agg.name }];

  return (
    <div className="flex flex-col gap-2 rounded-md border bg-muted/10 p-3">
      <div className="flex items-baseline justify-between">
        <h5 className="text-xs font-semibold uppercase text-muted-foreground">{title}</h5>
        <span className="font-mono text-xs text-muted-foreground">
          {fmtBytes(agg.used)} / {fmtBytes(denom)} · {usedPct.toFixed(0)}%
        </span>
      </div>

      <ProgressBar segments={segments} total={denom > 0 ? denom : agg.used} height="h-3" />

      {pools.length > 0 && (
        <ul className="mt-1 grid grid-cols-1 gap-y-1 text-xs">
          {pools.map((p, i) => {
            const pdenom = p.max > 0 ? p.max : p.total > 0 ? p.total : agg.used;
            const ppct = pdenom > 0 ? Math.min(100, (p.used / pdenom) * 100) : 0;
            return (
              <li key={p.name} className="flex items-center gap-2">
                <span
                  className={`h-2 w-2 shrink-0 rounded-full ${dots[i % dots.length]}`}
                  aria-hidden
                />
                <span className="min-w-0 flex-1 truncate font-mono text-muted-foreground">
                  {p.name}
                </span>
                <span className="font-mono text-foreground tabular-nums">
                  {fmtBytes(p.used)}
                </span>
                <span className="w-10 text-right font-mono text-muted-foreground tabular-nums">
                  {ppct.toFixed(0)}%
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

function threadToProcess(t: ThreadRow): ProcessNode {
  return {
    pid: t.id,
    name: t.name,
    status: t.state ?? "UNKNOWN",
    cpu_percent: t.cpu,
    is_root: false,
  };
}

function threadStateColor(state: string | undefined): string {
  switch (state) {
    case "RUNNABLE":
      return "bg-green-500";
    case "BLOCKED":
      return "bg-red-500";
    case "WAITING":
    case "TIMED_WAITING":
      return "bg-blue-500";
    case "NEW":
      return "bg-amber-500";
    default:
      return "bg-muted-foreground/40";
  }
}

type ThreadTreeNode = {
  id: string;
  label: string;
  state?: string;
  cpu?: number;
  count?: number;
  thread?: ThreadRow;
  children?: ThreadTreeNode[];
};

export function baseThreadName(name: string): string {
  // Prefer grouping by a trailing parenthesized family label, since JVM internal
  // threads share a stable family name even when their individual prefix varies
  // (e.g. "Gang worker#3 (Parallel GC Threads)" and any other "… (Parallel GC Threads)"
  // belong to the same logical group).
  const family = name.match(/\(([^()]+)\)\s*$/);
  if (family) return `(${family[1]})`;
  // Strip a trailing numeric suffix: "-3", "#3", or digits directly after a
  // letter ("CompilerThread0" -> "CompilerThread"). Avoid reducing the entire
  // name to empty — "-1" is a legitimate (if ugly) thread name and must stay
  // intact; likewise "#3" on its own.
  const stripped = name
    .replace(/[-#]\d+$/, "")
    .replace(/(?<=[A-Za-z])\d+$/, "");
  return stripped.length > 0 ? stripped : name;
}

function dominantState(threads: ThreadRow[]): string | undefined {
  const counts = new Map<string, number>();
  for (const t of threads) {
    const s = t.state ?? "UNKNOWN";
    counts.set(s, (counts.get(s) ?? 0) + 1);
  }
  let best: string | undefined;
  let bestCount = 0;
  for (const [s, c] of counts) {
    if (c > bestCount) {
      best = s;
      bestCount = c;
    }
  }
  return best;
}

export function buildThreadTree(threads: ThreadRow[]): ThreadTreeNode[] {
  const groups = new Map<string, ThreadRow[]>();
  const order: string[] = [];
  for (const t of threads) {
    const base = baseThreadName(t.name);
    if (!groups.has(base)) {
      groups.set(base, []);
      order.push(base);
    }
    groups.get(base)!.push(t);
  }
  const nodes: ThreadTreeNode[] = [];
  for (const base of order) {
    const bucket = groups.get(base)!;
    // Collapse singletons: only group when there are ≥2 threads sharing the
    // base. A single thread named exactly the base (no suffix stripped) stays
    // flat; a single thread with a suffix (e.g. "Thread-42") also stays flat
    // so we don't show a parent with one child.
    if (bucket.length < 2) {
      const t = bucket[0];
      nodes.push({
        id: `t-${t.id}-${t.name}`,
        label: t.name,
        state: t.state,
        cpu: t.cpu,
        thread: t,
      });
      continue;
    }
    const children = bucket.map((t) => ({
      id: `t-${t.id}-${t.name}`,
      label: t.name,
      state: t.state,
      cpu: t.cpu,
      thread: t,
    }));
    nodes.push({
      id: `g-${base}`,
      label: base,
      count: bucket.length,
      state: dominantState(bucket),
      children,
    });
  }
  return nodes;
}

// Heuristic: threads spawned by the arthas agent itself. Surfacing them in the
// list clutters the picture when a user is diagnosing their own app.
export function isArthasThread(name: string): boolean {
  return (
    name.startsWith("arthas-") ||
    name === "arthas-timer" ||
    name === "arthas-shell-server" ||
    name === "arthas-session-manager" ||
    name === "arthas-command-execute" ||
    name.startsWith("Timer-for-arthas-") ||
    name === "Attach Listener"
  );
}

function ThreadsPanel({ sessionId, threads }: { sessionId: string; threads: ThreadRow[] }) {
  const [filter, setFilter] = useState("");
  const [selectedStates, setSelectedStates] = useState<Set<string>>(new Set());
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [showArthas, setShowArthas] = useState(false);

  const filteredByAgent = useMemo(
    () => (showArthas ? threads : threads.filter((t) => !isArthasThread(t.name))),
    [threads, showArthas],
  );
  const arthasHidden = threads.length - filteredByAgent.length;

  const stateCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const t of filteredByAgent) {
      const s = (t.state ?? "UNKNOWN").toUpperCase();
      counts.set(s, (counts.get(s) ?? 0) + 1);
    }
    return counts;
  }, [filteredByAgent]);

  const visible = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return filteredByAgent.filter((t) => {
      if (selectedStates.size > 0) {
        const s = (t.state ?? "UNKNOWN").toUpperCase();
        if (!selectedStates.has(s)) return false;
      }
      if (q && !t.name.toLowerCase().includes(q) && !t.state?.toLowerCase().includes(q)) {
        return false;
      }
      return true;
    });
  }, [filteredByAgent, filter, selectedStates]);

  const selectedThread = useMemo(
    () => threads.find((t) => t.id === selectedId) ?? null,
    [threads, selectedId],
  );

  const stackQ = useQuery({
    queryKey: ["arthas", sessionId, "thread", selectedId],
    enabled: selectedId != null,
    queryFn: async () => {
      const { results } = await execArthas(sessionId, `thread ${selectedId}`);
      return extractJvmStackText(results);
    },
  });

  const process: ProcessNode | null = selectedThread
    ? {
        ...threadToProcess(selectedThread),
        stack_capture: stackQ.isLoading
          ? { status: "ready", text: "" }
          : stackQ.error
            ? { status: "error", error: String(stackQ.error) }
            : {
                status: "ready",
                text: stackQ.data ?? "",
                collected_at: new Date().toISOString(),
              },
      }
    : null;

  const tree = useMemo(() => buildThreadTree(visible), [visible]);
  const selectedNode = useMemo<ThreadTreeNode | null>(() => {
    if (selectedId == null) return null;
    for (const n of tree) {
      if (n.thread?.id === selectedId) return n;
      if (n.children) {
        const c = n.children.find((ch) => ch.thread?.id === selectedId);
        if (c) return c;
      }
    }
    return null;
  }, [tree, selectedId]);

  return (
    <SplitPane
      className="flex min-h-0 flex-1 rounded-md border"
      defaultSplit={35}
      minLeft={22}
      minRight={30}
      left={
        <div className="flex min-h-0 flex-col gap-2 p-2">
          <Input
            value={filter}
            onChange={(e: any) => setFilter(e.target.value)}
            placeholder="Filter by name or state..."
            className="h-7 text-xs"
          />
          <div className="flex flex-wrap items-center gap-1">
            {Array.from(stateCounts.entries())
              .sort((a, b) => b[1] - a[1])
              .map(([state, count]) => {
                const active = selectedStates.has(state);
                return (
                  <button
                    key={state}
                    type="button"
                    onClick={() =>
                      setSelectedStates((prev) => {
                        const next = new Set(prev);
                        if (next.has(state)) next.delete(state);
                        else next.add(state);
                        return next;
                      })
                    }
                    className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] transition-colors ${
                      active
                        ? "border-primary bg-primary/10 text-primary"
                        : "border-border bg-muted/50 text-muted-foreground hover:bg-background"
                    }`}
                  >
                    <span className={`h-2 w-2 rounded-full ${threadStateColor(state)}`} />
                    {state}
                    <span className="text-[10px] opacity-70">{count}</span>
                  </button>
                );
              })}
            {(filter || selectedStates.size > 0) && (
              <button
                type="button"
                onClick={() => {
                  setFilter("");
                  setSelectedStates(new Set());
                }}
                className="text-[11px] text-muted-foreground hover:text-foreground"
              >
                Clear
              </button>
            )}
            <label
              className="ml-auto inline-flex items-center gap-1 text-[11px] text-muted-foreground"
              title="Show threads spawned by the arthas agent itself"
            >
              <input
                type="checkbox"
                checked={showArthas}
                onChange={(e: any) => setShowArthas(e.target.checked)}
                className="h-3 w-3"
              />
              Show arthas threads
              {arthasHidden > 0 && !showArthas && (
                <span className="opacity-70">({arthasHidden} hidden)</span>
              )}
            </label>
          </div>
          <Tree<ThreadTreeNode>
            className="min-h-0 flex-1 rounded-md border"
            roots={tree}
            getKey={(n) => n.id}
            getChildren={(n) => n.children}
            selected={selectedNode}
            onSelect={(n) => {
              if (n.thread) setSelectedId(n.thread.id);
            }}
            defaultOpen={(_n, depth) => depth < 1 || !!filter}
            empty={<p className="p-2 text-xs text-muted-foreground">No threads match filter.</p>}
            renderRow={({ node }) => (
              <div className="flex min-w-0 flex-1 items-center gap-2 text-xs">
                <span
                  className={`h-2 w-2 shrink-0 rounded-full ${threadStateColor(node.state)}`}
                />
                {node.thread ? (
                  <>
                    <span className="font-mono text-[11px] text-muted-foreground">
                      {node.thread.id}
                    </span>
                    <span className="flex-1 truncate">{node.label}</span>
                    <span className="font-mono text-[11px] text-muted-foreground">
                      {node.thread.cpu.toFixed(1)}%
                    </span>
                  </>
                ) : (
                  <>
                    <span className="flex-1 truncate">{node.label}</span>
                    <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                      {node.count}
                    </span>
                  </>
                )}
              </div>
            )}
          />
          {filteredByAgent.length !== visible.length && (
            <p className="text-xs text-muted-foreground">
              Showing {visible.length} of {filteredByAgent.length}.
            </p>
          )}
        </div>
      }
      right={
        <div className="h-full min-h-0 overflow-hidden bg-muted/10">
          <DiagnosticsDetailPanel
            process={process}
            collectBusy={stackQ.isFetching}
            onCollectStack={async () => {
              await stackQ.refetch();
            }}
          />
        </div>
      }
    />
  );
}

type ArthasFrame = {
  className?: string;
  methodName?: string;
  fileName?: string;
  lineNumber?: number;
};

type ArthasThreadInfo = {
  threadId?: number;
  threadName?: string;
  threadState?: string;
  daemon?: boolean;
  priority?: number;
  stackTrace?: ArthasFrame[];
};

function extractJvmStackText(results: unknown[]): string {
  // Arthas `thread <id>` returns structured JSON; synthesize the canonical
  // `jstack` text format so the clicky-ui JVM parser can colorize it.
  for (const r of results as Array<Record<string, unknown>>) {
    const ti = (r?.threadInfo ?? r?.thread) as ArthasThreadInfo | undefined;
    if (!ti) continue;
    if (Array.isArray(ti.stackTrace)) {
      return renderJvmThreadText(ti);
    }
    // Older arthas builds may still send a prebuilt string.
    const str = (ti as Record<string, unknown>).stacktrace;
    if (typeof str === "string" && str.length > 0) return str;
  }
  return JSON.stringify(results, null, 2);
}

function renderJvmThreadText(ti: ArthasThreadInfo): string {
  const name = ti.threadName ?? "unknown";
  const id = ti.threadId ?? 0;
  const daemon = ti.daemon ? " daemon" : "";
  const prio = ti.priority ? ` prio=${ti.priority}` : "";
  const state = (ti.threadState ?? "UNKNOWN").toUpperCase();
  const lines: string[] = [];
  lines.push(`"${name}" #${id}${daemon}${prio} tid=0x${id.toString(16)} nid=0x0 ${state.toLowerCase()}`);
  lines.push(`   java.lang.Thread.State: ${state}`);
  for (const f of ti.stackTrace ?? []) {
    const fn = `${f.className ?? ""}.${f.methodName ?? ""}`;
    let src: string;
    if (f.lineNumber === -2) src = "Native Method";
    else if (f.fileName && f.lineNumber && f.lineNumber > 0) src = `${f.fileName}:${f.lineNumber}`;
    else if (f.fileName) src = f.fileName;
    else src = "Unknown Source";
    lines.push(`        at ${fn}(${src})`);
  }
  return lines.join("\n");
}

export { execArthas };
