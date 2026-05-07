import { useEffect, useMemo, useState } from "preact/hooks";
import type { ComponentChildren } from "preact";
import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { Activity, Download, ExternalLink, FileText, Flame, Play, RefreshCw, Square, TerminalSquare, Trash2 } from "lucide-react";
import {
  Badge,
  Button,
  ProgressBar,
  SplitPane,
  countGoroutinesByState,
  parseGoroutineDump,
  type ParsedGoroutine,
} from "@flanksource/clicky-ui";
import {
  callOp,
  configIDFromURL,
  pluginURL,
  type GolangSession,
  type GoroutineSnapshot,
  type ProfileKind,
  type ProfileRun,
  type ProfileSource,
  type RunningPod,
  type RuntimeSnapshot,
  type TargetOption,
} from "./api";

const SESSIONS_KEY = ["golang", "sessions"] as const;
const PODS_KEY = ["golang", "pods"] as const;
const PROFILE_KINDS: ProfileKind[] = ["cpu", "trace", "heap"];
const PROFILE_SOURCES: ProfileSource[] = ["auto", "pprof", "gops"];
const HEAP_PALETTE = ["bg-emerald-500", "bg-sky-500", "bg-amber-500", "bg-violet-500"];
const STACK_PALETTE = ["bg-indigo-500", "bg-cyan-500"];
const SYS_PALETTE = ["bg-fuchsia-500", "bg-rose-500", "bg-slate-500"];

type ActiveTab = "dashboard" | "goroutines" | "profiler" | "pprof";

export function App() {
  const configID = configIDFromURL();
  const [selectedTarget, setSelectedTarget] = useState<TargetOption | null>(null);
  const [selectedSessionID, setSelectedSessionID] = useState<string | null>(null);
  const [tab, setTab] = useState<ActiveTab>("dashboard");
  const qc = useQueryClient();

  const podsQ = useQuery({
    queryKey: PODS_KEY,
    queryFn: () => callOp<RunningPod[]>("pods-list"),
    enabled: !!configID,
    refetchInterval: 15_000,
  });

  const sessionsQ = useQuery({
    queryKey: SESSIONS_KEY,
    queryFn: () => callOp<GolangSession[]>("sessions-list"),
    refetchInterval: 5_000,
  });

  const targets = useMemo(() => flattenTargets(podsQ.data ?? []), [podsQ.data]);
  const sessions = sessionsQ.data ?? [];
  const selectedSession = useMemo(
    () => sessions.find((s) => s.id === selectedSessionID) ?? sessions[0] ?? null,
    [sessions, selectedSessionID],
  );

  useEffect(() => {
    if (!selectedTarget && targets.length > 0) setSelectedTarget(targets[0]);
  }, [selectedTarget, targets]);

  useEffect(() => {
    if (!selectedSessionID && sessions.length > 0) setSelectedSessionID(sessions[0].id);
  }, [selectedSessionID, sessions]);

  const startSession = useMutation({
    mutationFn: (target: TargetOption) =>
      callOp<GolangSession>("session-create", {
        namespace: target.namespace,
        pod: target.pod,
        container: target.container,
      }),
    onSuccess: (session) => {
      setSelectedSessionID(session.id);
      qc.invalidateQueries({ queryKey: SESSIONS_KEY });
    },
  });

  const deleteSession = useMutation({
    mutationFn: (id: string) => callOp("session-delete", { id }),
    onSuccess: (_, id) => {
      if (selectedSessionID === id) setSelectedSessionID(null);
      qc.invalidateQueries({ queryKey: SESSIONS_KEY });
    },
  });

  if (!configID) {
    return (
      <div className="flex h-screen flex-col items-center justify-center gap-2 bg-background p-8 text-center text-sm text-muted-foreground">
        <Activity className="h-8 w-8" />
        <p>No config_id in the iframe URL.</p>
      </div>
    );
  }

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <header className="flex shrink-0 items-center justify-between gap-3 border-b bg-card px-4 py-2">
        <div className="flex min-w-0 items-center gap-2">
          <Activity className="h-4 w-4 text-primary" />
          <div className="min-w-0">
            <div className="truncate text-sm font-semibold">Golang Diagnostics</div>
            <div className="truncate text-xs text-muted-foreground">
              {selectedSession ? `${selectedSession.namespace}/${selectedSession.pod}` : "No active session"}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="icon" title="Refresh" onClick={() => refreshAll(qc)}>
            <RefreshCw className="h-4 w-4" />
          </Button>
          <Button
            size="sm"
            loading={startSession.isPending}
            disabled={!selectedTarget}
            onClick={() => selectedTarget && startSession.mutate(selectedTarget)}
          >
            <Play className="h-4 w-4" />
            Start Session
          </Button>
        </div>
      </header>

      <div className="grid min-h-0 flex-1 grid-cols-[minmax(18rem,22rem)_1fr] gap-3 p-3">
        <aside className="flex min-h-0 flex-col rounded-md border bg-card">
          <div className="flex items-center justify-between border-b px-3 py-2">
            <span className="text-xs font-semibold uppercase text-muted-foreground">Targets</span>
            <Badge variant="outline" size="sm">{targets.length}</Badge>
          </div>
          <div className="min-h-0 flex-1 overflow-auto p-2">
            {podsQ.error ? (
              <ErrorText error={podsQ.error} />
            ) : targets.length === 0 ? (
              <Empty>No ready pods resolved for this resource.</Empty>
            ) : (
              <div className="flex flex-col gap-1">
                {targets.map((target) => {
                  const selected =
                    selectedTarget?.pod === target.pod && selectedTarget?.container === target.container;
                  const session = sessions.find((s) => s.pod === target.pod && s.container === target.container);
                  return (
                    <button
                      key={`${target.namespace}/${target.pod}/${target.container}`}
                      className={`flex w-full items-start gap-2 rounded-md px-2 py-2 text-left text-xs hover:bg-accent ${
                        selected ? "bg-primary/10 ring-1 ring-primary/30" : ""
                      }`}
                      onClick={() => {
                        setSelectedTarget(target);
                        if (session) setSelectedSessionID(session.id);
                      }}
                    >
                      <span className="min-w-0 flex-1">
                        <span className="block truncate font-medium">{target.pod}</span>
                        <span className="block truncate text-muted-foreground">
                          {target.owner} / {target.container}
                        </span>
                        {target.ports.length > 0 && (
                          <span className="mt-1 block truncate font-mono text-[11px] text-muted-foreground">
                            ports {target.ports.join(", ")}
                          </span>
                        )}
                      </span>
                      <span className="flex shrink-0 flex-col items-end gap-1">
                        <Badge variant="outline" size="sm">{target.namespace}</Badge>
                        {session && <Badge tone="success" variant="soft" size="sm">active</Badge>}
                      </span>
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        </aside>

        <section className="flex min-h-0 flex-col rounded-md border bg-card">
          <div className="flex shrink-0 items-center justify-between gap-3 border-b px-3 py-2">
            <SessionSummary session={selectedSession} />
            <Button
              size="sm"
              variant="destructive"
              loading={deleteSession.isPending}
              disabled={!selectedSession}
              onClick={() => selectedSession && deleteSession.mutate(selectedSession.id)}
            >
              <Trash2 className="h-4 w-4" />
              Stop
            </Button>
          </div>
          <div className="flex shrink-0 gap-1 border-b px-3 py-2">
            <TabButton tab="dashboard" current={tab} onClick={setTab}>Dashboard</TabButton>
            <TabButton tab="goroutines" current={tab} onClick={setTab}>Goroutines</TabButton>
            <TabButton tab="profiler" current={tab} onClick={setTab}>Profiler</TabButton>
            <TabButton tab="pprof" current={tab} onClick={setTab}>Pprof</TabButton>
          </div>
          <div className="min-h-0 flex-1 overflow-hidden">
            {startSession.error && <div className="p-3"><ErrorText error={startSession.error} /></div>}
            {selectedSession ? (
              <>
                {tab === "dashboard" && <DashboardTab session={selectedSession} />}
                {tab === "goroutines" && <GoroutinesTab session={selectedSession} />}
                {tab === "profiler" && <ProfilerTab session={selectedSession} />}
                {tab === "pprof" && <PprofTab session={selectedSession} />}
              </>
            ) : (
              <Empty>Select a target and start a session.</Empty>
            )}
          </div>
        </section>
      </div>
    </div>
  );
}

function SessionSummary({ session }: { session: GolangSession | null }) {
  if (!session) {
    return (
      <div>
        <div className="text-sm font-semibold">No session</div>
        <div className="text-xs text-muted-foreground">Start a session against a ready pod.</div>
      </div>
    );
  }
  return (
    <div className="min-w-0">
      <div className="truncate text-sm font-semibold">{session.pod} / {session.container}</div>
      <div className="mt-1 flex flex-wrap items-center gap-1 text-xs text-muted-foreground">
        <Badge tone={session.gopsAvailable ? "success" : "neutral"} variant="soft" size="sm">
          gops {session.gopsAvailable ? portText(session.gopsRemotePort) : "unavailable"}
        </Badge>
        <Badge tone={session.pprofAvailable ? "success" : "neutral"} variant="soft" size="sm">
          pprof {session.pprofAvailable ? `${portText(session.pprofRemotePort)}${session.pprofBasePath ?? ""}` : "unavailable"}
        </Badge>
        {session.pid ? <Badge variant="outline" size="sm">pid {session.pid}</Badge> : null}
      </div>
    </div>
  );
}

function DashboardTab({ session }: { session: GolangSession }) {
  const runtimeQ = useQuery({
    queryKey: ["golang", session.id, "runtime"],
    queryFn: () => callOp<RuntimeSnapshot>("runtime-snapshot", { sessionId: session.id }),
    enabled: session.gopsAvailable,
    refetchInterval: session.gopsAvailable ? 5_000 : false,
  });
  const parsed = useMemo(() => parseRuntime(runtimeQ.data), [runtimeQ.data]);

  return (
    <div className="flex h-full min-h-0 flex-col gap-4 overflow-auto p-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold">Runtime Dashboard</h3>
        <span className="text-xs text-muted-foreground">
          {runtimeQ.dataUpdatedAt ? `refreshed ${new Date(runtimeQ.dataUpdatedAt).toLocaleTimeString()}` : ""}
        </span>
      </div>

      <section className="grid grid-cols-1 gap-2 lg:grid-cols-2">
        <InfoCard title="Runtime">
          <KV k="Go" v={(runtimeQ.data?.version ?? "").trim() || "unknown"} />
          <KV k="PID" v={session.pid ? String(session.pid) : "unknown"} />
          <KV k="Goroutines" v={firstValue(parsed.stats, ["goroutines", "goroutine-count"])} />
          <KV k="GOMAXPROCS" v={firstValue(parsed.stats, ["gomaxprocs", "gomax-procs"])} />
          <KV k="CPUs" v={firstValue(parsed.stats, ["numcpu", "num-cpu", "cpus"])} />
        </InfoCard>
        <InfoCard title="GC">
          <KV k="Collections" v={firstValue(parsed.mem, ["num-gc", "numgc"])} />
          <KV k="Forced" v={firstValue(parsed.mem, ["num-forced-gc", "numforcedgc"])} />
          <KV k="Next GC" v={humanMemValue(firstValue(parsed.mem, ["next-gc", "nextgc"]))} />
          <KV k="Pause total" v={firstValue(parsed.mem, ["gc-pause-total", "pausetotalns"])} />
          <KV k="CPU fraction" v={firstValue(parsed.mem, ["gc-cpu-fraction", "gccpufraction"])} />
        </InfoCard>
      </section>

      <section>
        <h4 className="mb-2 text-xs font-semibold uppercase text-muted-foreground">Memory</h4>
        <div className="grid grid-cols-1 gap-3">
          <MemoryRegionCard
            title="Heap"
            used={firstValue(parsed.mem, ["heap-alloc", "heapalloc"])}
            total={firstValue(parsed.mem, ["heap-sys", "heapsys"])}
            palette={HEAP_PALETTE}
            rows={[
              memoryRow("allocated", firstValue(parsed.mem, ["heap-alloc", "heapalloc"]), firstValue(parsed.mem, ["heap-sys", "heapsys"])),
              memoryRow("in-use", firstValue(parsed.mem, ["heap-in-use", "heap-inuse", "heapinuse"]), firstValue(parsed.mem, ["heap-sys", "heapsys"]), true),
              memoryRow("idle", firstValue(parsed.mem, ["heap-idle", "heapidle"]), firstValue(parsed.mem, ["heap-sys", "heapsys"]), true),
              memoryRow("released", firstValue(parsed.mem, ["heap-released", "heapreleased"]), firstValue(parsed.mem, ["heap-sys", "heapsys"])),
              countRow("objects", firstValue(parsed.mem, ["heap-objects", "heapobjects"])),
              countRow("mallocs", firstValue(parsed.mem, ["mallocs"])),
              countRow("frees", firstValue(parsed.mem, ["frees"])),
            ]}
          />
          <MemoryRegionCard
            title="Stack"
            used={firstValue(parsed.mem, ["stack-in-use", "stackinuse"])}
            total={firstValue(parsed.mem, ["stack-sys", "stacksys"])}
            palette={STACK_PALETTE}
            rows={[
              memoryRow("in-use", firstValue(parsed.mem, ["stack-in-use", "stackinuse"]), firstValue(parsed.mem, ["stack-sys", "stacksys"]), true),
              memoryRow("system", firstValue(parsed.mem, ["stack-sys", "stacksys"]), firstValue(parsed.mem, ["sys"])),
            ]}
          />
          <MemoryRegionCard
            title="Runtime"
            used={firstValue(parsed.mem, ["alloc"])}
            total={firstValue(parsed.mem, ["sys"])}
            palette={SYS_PALETTE}
            rows={[
              memoryRow("alloc", firstValue(parsed.mem, ["alloc"]), firstValue(parsed.mem, ["sys"]), true),
              memoryRow("total alloc", firstValue(parsed.mem, ["total-alloc", "totalalloc"]), firstValue(parsed.mem, ["sys"])),
              memoryRow("mspan in-use", firstValue(parsed.mem, ["mspan-in-use", "mspaninuse"]), firstValue(parsed.mem, ["mspan-sys", "mspansys"])),
              memoryRow("mcache in-use", firstValue(parsed.mem, ["mcache-in-use", "mcacheinuse"]), firstValue(parsed.mem, ["mcache-sys", "mcachesys"])),
              memoryRow("gc sys", firstValue(parsed.mem, ["gc-sys", "gcsys"]), firstValue(parsed.mem, ["sys"]), true),
              memoryRow("other sys", firstValue(parsed.mem, ["other-sys", "othersys"]), firstValue(parsed.mem, ["sys"]), true),
            ]}
          />
        </div>
      </section>

      {session.diagnostics?.length ? (
        <section>
          <h4 className="mb-2 text-xs font-semibold uppercase text-muted-foreground">Endpoint Discovery</h4>
          <pre className="max-h-28 overflow-auto rounded-md border bg-muted/30 p-3 font-mono text-xs">
            {session.diagnostics.join("\n")}
          </pre>
        </section>
      ) : null}

      {runtimeQ.error ? <ErrorText error={runtimeQ.error} /> : null}
    </div>
  );
}

function GoroutinesTab({ session }: { session: GolangSession }) {
  const [query, setQuery] = useState("");
  const [hideRuntimeOnly, setHideRuntimeOnly] = useState(true);
  const goroutinesQ = useQuery({
    queryKey: ["golang", session.id, "goroutines"],
    queryFn: () => callOp<GoroutineSnapshot>("goroutines", { sessionId: session.id }),
    enabled: true,
    refetchInterval: 5_000,
    refetchIntervalInBackground: false,
  });
  const dump = goroutinesQ.data?.dump ?? "";
  const parsed = useMemo(() => parseGoroutineDump(dump), [dump]);
  const filtered = useMemo(() => filterGoroutines(parsed, query, hideRuntimeOnly), [parsed, query, hideRuntimeOnly]);
  const counts = useMemo(() => countGoroutinesByState(parsed), [parsed]);

  return (
    <div className="flex h-full min-h-0 flex-col gap-3 p-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold">Goroutines</h3>
          <p className="text-xs text-muted-foreground">
            {goroutinesQ.data?.source ? `source: ${goroutinesQ.data.source}` : "Load the current stack dump."}
          </p>
        </div>
        <Button size="sm" loading={goroutinesQ.isFetching} onClick={() => goroutinesQ.refetch()}>
          <RefreshCw className="h-4 w-4" />
          Refresh
        </Button>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <input
          className="h-8 min-w-72 rounded-md border bg-background px-2 text-xs"
          value={query}
          onInput={(event) => setQuery((event.target as HTMLInputElement).value)}
          placeholder="Filter function, file, or state"
        />
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input
            type="checkbox"
            checked={hideRuntimeOnly}
            onChange={(event) => setHideRuntimeOnly((event.target as HTMLInputElement).checked)}
          />
          Hide runtime-only stacks
        </label>
        {[...counts.entries()].map(([state, count]) => (
          <Badge key={state} variant="outline" size="sm">{state}: {count}</Badge>
        ))}
      </div>
      {goroutinesQ.error ? (
        <ErrorText error={goroutinesQ.error} />
      ) : dump && parsed.length === 0 ? (
        <pre className="min-h-0 flex-1 overflow-auto rounded-md border bg-muted/30 p-3 font-mono text-xs">{dump}</pre>
      ) : (
        <div className="min-h-0 flex-1 overflow-auto rounded-md border p-2">
          {filtered.length === 0 ? (
            <Empty>No goroutines match the current filter.</Empty>
          ) : (
            <div className="flex flex-col divide-y">
              {filtered.map((goroutine) => (
                <GoroutineDetails key={goroutine.id} goroutine={goroutine} query={query} hideRuntimeOnly={hideRuntimeOnly} />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function ProfilerTab({ session }: { session: GolangSession }) {
  const [kind, setKind] = useState<ProfileKind>("cpu");
  const [source, setSource] = useState<ProfileSource>("auto");
  const [seconds, setSeconds] = useState(30);
  const [selectedRunID, setSelectedRunID] = useState<string | null>(null);
  const qc = useQueryClient();

  const runsQ = useQuery({
    queryKey: ["golang", session.id, "profile-runs"],
    queryFn: () => callOp<ProfileRun[]>("profile-runs-list", { sessionId: session.id }),
    refetchInterval: 2_000,
  });
  const runs = runsQ.data ?? [];
  const selected = runs.find((run) => run.id === selectedRunID) ?? runs[0] ?? null;

  const start = useMutation({
    mutationFn: (body: Record<string, unknown>) => callOp<ProfileRun>("profile-start", body),
    onSuccess: (run) => {
      setSelectedRunID(run.id);
      qc.invalidateQueries({ queryKey: ["golang", session.id, "profile-runs"] });
    },
  });

  const stop = useMutation({
    mutationFn: (runID: string) => callOp<ProfileRun>("profile-stop", { sessionId: session.id, runId: runID }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["golang", session.id, "profile-runs"] }),
  });

  const preview = profilePreview(kind, source, seconds, session);
  const controls = (
    <section className="flex min-h-0 flex-col gap-3 p-3">
      <div>
        <h3 className="text-sm font-semibold">Profiler</h3>
        <p className="text-xs text-muted-foreground">Capture Go pprof and gops profiles from the selected process.</p>
      </div>
      <div className="grid grid-cols-2 gap-2">
        <Field label="Event">
          <select className="h-8 rounded-md border bg-background px-2 text-xs" value={kind} onChange={(event) => setKind((event.target as HTMLSelectElement).value as ProfileKind)}>
            {PROFILE_KINDS.map((item) => <option key={item} value={item}>{item}</option>)}
          </select>
        </Field>
        <Field label="Source">
          <select className="h-8 rounded-md border bg-background px-2 text-xs" value={source} onChange={(event) => setSource((event.target as HTMLSelectElement).value as ProfileSource)}>
            {PROFILE_SOURCES.map((item) => <option key={item} value={item}>{item}</option>)}
          </select>
        </Field>
        <Field label="Duration seconds">
          <input className="h-8 rounded-md border bg-background px-2 text-xs" type="number" min={1} max={300} value={seconds} onInput={(event) => setSeconds(Number((event.target as HTMLInputElement).value))} />
        </Field>
      </div>
      <div className="rounded-md border bg-muted/30 p-2">
        <div className="mb-1 text-xs text-muted-foreground">Request preview</div>
        <pre className="overflow-auto font-mono text-xs">{preview}</pre>
      </div>
      <div className="grid grid-cols-2 gap-2">
        <Button size="sm" loading={start.isPending} onClick={() => start.mutate({ sessionId: session.id, kind, source, seconds })}>
          <Play className="h-4 w-4" />
          Start
        </Button>
        <Button size="sm" variant="secondary" loading={start.isPending} onClick={() => start.mutate({ sessionId: session.id, kind, source, seconds })}>
          <Play className="h-4 w-4" />
          Timed sample
        </Button>
        <Button size="sm" variant="outline" onClick={() => runsQ.refetch()}>
          Status
        </Button>
        <Button
          size="sm"
          variant="destructive"
          loading={stop.isPending}
          disabled={!selected || selected.state !== "running"}
          onClick={() => selected && stop.mutate(selected.id)}
        >
          <Square className="h-4 w-4" />
          Stop
        </Button>
      </div>
      {(start.error || stop.error || runsQ.error) && <ErrorText error={start.error ?? stop.error ?? runsQ.error} />}
      <div className="min-h-0 flex-1 overflow-auto rounded-md border">
        {runs.length === 0 ? (
          <Empty>Run a profile to collect output.</Empty>
        ) : (
          runs.map((run) => (
            <button
              key={run.id}
              className={`flex w-full items-center justify-between gap-2 border-b px-3 py-2 text-left text-xs last:border-b-0 hover:bg-accent ${
                selected?.id === run.id ? "bg-primary/10" : ""
              }`}
              onClick={() => setSelectedRunID(run.id)}
            >
              <span className="min-w-0">
                <span className="block truncate font-mono font-semibold">{run.kind}</span>
                <span className="block truncate text-muted-foreground">
                  {run.source || run.preference || "auto"} · {fmtDuration(run.elapsedMs)} · {fmtBytes(run.bytes)}
                </span>
              </span>
              <RunBadge run={run} />
            </button>
          ))
        )}
      </div>
    </section>
  );

  const output = <ProfilerOutputView session={session} run={selected} />;

  return <SplitPane left={controls} right={output} defaultSplit={38} minLeft={28} minRight={36} />;
}

type ProfilerView = "flamegraph" | "top" | "raw";

function ProfilerOutputView({ session, run }: { session: GolangSession; run: ProfileRun | null }) {
  const renderable = !!run && run.state === "completed" && run.kind !== "trace";
  const [view, setView] = useState<ProfilerView>(renderable ? "flamegraph" : "raw");

  useEffect(() => {
    if (!renderable && view !== "raw") setView("raw");
  }, [renderable, view]);

  if (!run) {
    return (
      <section className="flex h-full min-h-0 flex-col gap-3 p-3">
        <Empty>No profile run selected.</Empty>
      </section>
    );
  }

  const downloadName = profileDownloadName(session, run);
  const renderURL = (path: string) => pluginURL(`profiles/${session.id}/${run.id}/${path}`);
  const downloadURL = pluginURL(`profiles/${session.id}/${run.id}`);

  return (
    <section className="flex h-full min-h-0 flex-col gap-2 p-3">
      <div className="flex items-center justify-between gap-2">
        <ProfilerOutputSwitch value={view} onChange={setView} disabled={!renderable} kind={run.kind} />
        <div className="flex items-center gap-1">
          {renderable && (
            <a
              className="inline-flex h-7 items-center gap-1 rounded-md border px-2 text-xs hover:bg-accent"
              href={renderURL("flamegraph")}
              target="_blank"
              rel="noreferrer"
              title="Open pprof viewer in a new tab"
            >
              <ExternalLink className="h-3 w-3" />
              Open viewer
            </a>
          )}
          {run.state === "completed" && (
            <Button
              size="sm"
              variant="outline"
              onClick={() => downloadBlob(downloadURL, downloadName).catch((err) => alert(errorMessage(err)))}
            >
              <Download className="h-4 w-4" />
              Download
            </Button>
          )}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-hidden rounded-md border bg-background">
        {view === "flamegraph" && renderable ? (
          <iframe
            key={`flame-${run.id}`}
            title="Profile flamegraph"
            src={renderURL("flamegraph")}
            className="h-full w-full border-0"
          />
        ) : view === "top" && renderable ? (
          <iframe
            key={`top-${run.id}`}
            title="Profile top functions"
            src={renderURL("top")}
            className="h-full w-full border-0"
          />
        ) : (
          <ProfilerRawView run={run} />
        )}
      </div>
    </section>
  );
}

function ProfilerRawView({ run }: { run: ProfileRun }) {
  return (
    <div className="flex h-full min-h-0 flex-col gap-3 overflow-auto p-3">
      <div className="grid grid-cols-1 gap-2 lg:grid-cols-2">
        <InfoCard title="Run">
          <KV k="Kind" v={run.kind} />
          <KV k="State" v={run.state} />
          <KV k="Source" v={run.source || run.preference || "auto"} />
          <KV k="Bytes" v={fmtBytes(run.bytes)} />
          <KV k="Elapsed" v={fmtDuration(run.elapsedMs)} />
        </InfoCard>
        <InfoCard title="Timing">
          <KV k="Started" v={new Date(run.startedAt).toLocaleString()} />
          <KV k="Completed" v={run.completedAt ? new Date(run.completedAt).toLocaleString() : "running"} />
          <KV k="Duration" v={run.seconds ? `${run.seconds}s` : "default"} />
        </InfoCard>
      </div>
      <pre className="min-h-40 flex-1 overflow-auto rounded-md border bg-muted/30 p-3 font-mono text-xs">
        {run.error || profileHelp(run)}
      </pre>
    </div>
  );
}

function ProfilerOutputSwitch({
  value,
  onChange,
  disabled,
  kind,
}: {
  value: ProfilerView;
  onChange: (value: ProfilerView) => void;
  disabled: boolean;
  kind: string;
}) {
  return (
    <div className="inline-flex items-center gap-1 rounded-md bg-muted p-1" role="tablist" aria-label="Profiler output">
      <ProfilerOutputOption value="flamegraph" current={value} onChange={onChange} disabled={disabled} icon={<Flame className="h-3 w-3" />} label="Flamegraph" />
      <ProfilerOutputOption value="top" current={value} onChange={onChange} disabled={disabled} icon={<FileText className="h-3 w-3" />} label="Top" />
      <ProfilerOutputOption value="raw" current={value} onChange={onChange} disabled={false} icon={<TerminalSquare className="h-3 w-3" />} label={kind === "trace" ? "Trace" : "Raw"} />
    </div>
  );
}

function ProfilerOutputOption({
  value,
  current,
  onChange,
  disabled,
  icon,
  label,
}: {
  value: ProfilerView;
  current: ProfilerView;
  onChange: (value: ProfilerView) => void;
  disabled: boolean;
  icon: ComponentChildren;
  label: string;
}) {
  const checked = value === current;
  const className = `inline-flex h-6 items-center gap-1 rounded px-2 text-xs font-medium ${
    checked ? "bg-background text-foreground shadow" : "text-muted-foreground hover:text-foreground"
  } ${disabled ? "cursor-not-allowed opacity-50" : "cursor-pointer"}`;
  return (
    <button
      type="button"
      role="tab"
      aria-selected={checked}
      disabled={disabled}
      onClick={() => onChange(value)}
      className={className}
    >
      {icon}
      {label}
    </button>
  );
}

async function downloadBlob(url: string, fallbackName: string): Promise<void> {
  const res = await fetch(url, { credentials: "same-origin" });
  if (!res.ok) throw new Error((await res.text()) || res.statusText);
  const filename = parseContentDispositionFilename(res.headers.get("Content-Disposition")) ?? fallbackName;
  const blob = await res.blob();
  const objectURL = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = objectURL;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  setTimeout(() => URL.revokeObjectURL(objectURL), 1000);
}

function parseContentDispositionFilename(header: string | null): string | undefined {
  if (!header) return undefined;
  const utf8 = header.match(/filename\*\s*=\s*UTF-8''([^;]+)/i);
  if (utf8?.[1]) {
    try { return decodeURIComponent(utf8[1].trim()); } catch { /* fallthrough */ }
  }
  const quoted = header.match(/filename\s*=\s*"([^"]+)"/i);
  if (quoted?.[1]) return quoted[1];
  const bare = header.match(/filename\s*=\s*([^;]+)/i);
  return bare?.[1]?.trim();
}

function profileDownloadName(session: GolangSession, run: ProfileRun): string {
  const ext = run.kind === "trace" ? "trace" : "pprof";
  return `golang-${session.id}-${run.id}.${ext}`;
}

function PprofTab({ session }: { session: GolangSession }) {
  if (!session.pprofAvailable) return <Empty>Pprof is not available for this session.</Empty>;
  const url = pluginURL(`pprof/${session.id}/`);
  return (
    <div className="flex h-full min-h-0 flex-col gap-3 p-4">
      <div>
        <Button asChild size="sm" variant="outline">
          <a href={url} target="_blank" rel="noreferrer">
            <FileText className="h-4 w-4" />
            Open pprof index
          </a>
        </Button>
      </div>
      <iframe title="pprof" src={url} className="min-h-0 flex-1 rounded-md border bg-background" />
    </div>
  );
}

function TabButton({
  tab,
  current,
  onClick,
  children,
}: {
  tab: ActiveTab;
  current: ActiveTab;
  onClick: (tab: ActiveTab) => void;
  children: ComponentChildren;
}) {
  return (
    <Button size="sm" variant={current === tab ? "secondary" : "ghost"} onClick={() => onClick(tab)}>
      {children}
    </Button>
  );
}

function InfoCard({ title, children }: { title: string; children: ComponentChildren }) {
  return (
    <div className="rounded-md border bg-muted/20 p-3">
      <h4 className="mb-2 text-xs font-semibold uppercase text-muted-foreground">{title}</h4>
      <dl className="grid grid-cols-[8rem_1fr] gap-y-1 text-xs">{children}</dl>
    </div>
  );
}

function KV({ k, v }: { k: string; v?: string }) {
  return (
    <>
      <dt className="text-muted-foreground">{k}</dt>
      <dd className="truncate">{v || "unknown"}</dd>
    </>
  );
}

type MemoryRow = {
  label: string;
  value?: string;
  bytes?: number | null;
  percent?: number | null;
  countOnly?: boolean;
  segment?: boolean;
};

function MemoryRegionCard({
  title,
  used,
  total,
  palette,
  rows,
}: {
  title: string;
  used?: string;
  total?: string;
  palette: string[];
  rows: MemoryRow[];
}) {
  const usedBytes = parseByteValue(used);
  const totalBytes = parseByteValue(total);
  const pct = usedBytes && totalBytes ? Math.min(100, (usedBytes / totalBytes) * 100) : 0;
  const byteRows = rows.filter((row) => row.segment && !row.countOnly && row.bytes && row.bytes > 0);
  const segments = byteRows.map((row, index) => ({
    count: row.bytes ?? 0,
    color: palette[index % palette.length],
    label: row.label,
  }));
  if (segments.length === 0 && usedBytes) {
    segments.push({ count: usedBytes, color: palette[0], label: "used" });
  }
  const segmentTotal = segments.reduce((sum, segment) => sum + segment.count, 0);
  const barTotal = Math.max(totalBytes || 0, segmentTotal, 1);
  return (
    <div className="flex flex-col gap-3 rounded-md border bg-muted/10 p-3">
      <div className="flex items-center justify-between gap-3 text-xs">
        <strong className="uppercase text-muted-foreground">{title}</strong>
        <span className="font-mono text-muted-foreground">
          {humanMemValue(used) || "unknown"} / {humanMemValue(total) || "unknown"} · {pct ? `${pct.toFixed(0)}%` : "unknown"}
        </span>
      </div>
      <ProgressBar total={barTotal} segments={segments} />
      <div className="grid grid-cols-1 gap-x-6 gap-y-1 md:grid-cols-2">
        {rows.filter((row) => row.value).map((row, index) => (
          <div key={`${title}-${row.label}`} className="grid grid-cols-[1fr_auto_auto] items-center gap-3 text-xs">
            <span className="flex min-w-0 items-center gap-2 text-muted-foreground">
              <span className={`h-2 w-2 shrink-0 rounded-full ${row.countOnly ? "bg-muted-foreground/40" : palette[index % palette.length]}`} />
              <span className="truncate">{row.label}</span>
            </span>
            <span className="font-mono font-semibold">{row.value}</span>
            <span className="w-10 text-right font-mono text-muted-foreground">
              {row.percent == null ? "" : `${row.percent.toFixed(0)}%`}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

function Field({ label, children }: { label: string; children: ComponentChildren }) {
  return (
    <label className="flex flex-col gap-1 text-xs">
      <span className="text-muted-foreground">{label}</span>
      {children}
    </label>
  );
}

function GoroutineDetails({
  goroutine,
  query,
  hideRuntimeOnly,
}: {
  goroutine: ParsedGoroutine;
  query: string;
  hideRuntimeOnly: boolean;
}) {
  const frames = hideRuntimeOnly
    ? goroutine.frames.filter((frame) => !frame.runtime || frame.kind === "created_by")
    : goroutine.frames;
  const open = goroutine.state === "running" || !!query;
  return (
    <details open={open} className="py-1">
      <summary className="cursor-pointer list-none px-2 py-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-xs font-semibold">g{goroutine.id}</span>
          <Badge variant="outline" size="sm">{goroutine.rawState}</Badge>
          <span className="text-[11px] text-muted-foreground">{frames.length} frames</span>
          {goroutine.topFunction && <span className="truncate text-[11px] text-muted-foreground">{goroutine.topFunction}</span>}
        </div>
      </summary>
      <div className="space-y-0.5 px-2 pb-2 pl-5">
        {frames.map((frame, index) => (
          <div key={`${goroutine.id}-${index}`} className={frame.runtime ? "text-muted-foreground" : "text-foreground"}>
            <div className="break-all font-mono text-[11px] font-semibold leading-4">
              {frame.displayName}
              {frame.location && <span className="ml-2 text-[10px] font-normal opacity-80">{frame.location}</span>}
            </div>
          </div>
        ))}
      </div>
    </details>
  );
}

function Empty({ children }: { children: ComponentChildren }) {
  return <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground">{children}</div>;
}

function ErrorText({ error }: { error: unknown }) {
  return <div className="rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{errorMessage(error)}</div>;
}

function RunBadge({ run }: { run: ProfileRun }) {
  const tone = run.state === "completed" ? "success" : run.state === "failed" ? "danger" : run.state === "stopped" ? "warning" : "info";
  return <Badge tone={tone} variant="soft" size="sm">{run.state}</Badge>;
}

function flattenTargets(pods: RunningPod[]): TargetOption[] {
  const out: TargetOption[] = [];
  for (const pod of pods) {
    for (const container of pod.containers ?? []) {
      out.push({
        namespace: pod.namespace,
        pod: pod.name,
        container,
        owner: pod.ownerKind ? `${pod.ownerKind}/${pod.ownerName}` : "pod",
        ports: pod.containerPorts?.[container] ?? [],
      });
    }
  }
  return out;
}

function parseRuntime(data?: RuntimeSnapshot): { stats: Record<string, string>; mem: Record<string, string> } {
  return {
    stats: parseKeyValue(data?.stats ?? ""),
    mem: parseKeyValue(data?.memstats ?? ""),
  };
}

function parseKeyValue(raw: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of raw.split("\n")) {
    const [key, value] = line.split(/:\s*/, 2);
    if (key && value) out[key.trim().toLowerCase().replace(/\s+/g, "-")] = value.trim();
  }
  return out;
}

function firstValue(values: Record<string, string>, keys: string[]): string | undefined {
  for (const key of keys) {
    const value = values[key];
    if (value) return value;
  }
  return undefined;
}

function memoryRow(label: string, value?: string, total?: string, segment = false): MemoryRow {
  const bytes = parseByteValue(value);
  const totalBytes = parseByteValue(total);
  return {
    label,
    value: humanMemValue(value),
    bytes,
    percent: bytes && totalBytes ? Math.min(100, (bytes / totalBytes) * 100) : null,
    segment,
  };
}

function countRow(label: string, value?: string): MemoryRow {
  return {
    label,
    value: formatCount(value),
    countOnly: true,
  };
}

function humanMemValue(raw?: string): string | undefined {
  if (!raw) return undefined;
  const value = raw.trim();
  const paren = value.match(/\((\d+) bytes\)/);
  if (paren?.[1]) {
    const formatted = fmtBytes(Number(paren[1]));
    if (value.startsWith("when ")) {
      return value.replace(/>=\s*.+$/, `>= ${formatted}`);
    }
    return formatted;
  }
  const direct = value.match(/^(\d+) bytes$/);
  if (direct?.[1]) return fmtBytes(Number(direct[1]));
  return value.replace(/(\d+(?:\.\d+)?)([KMGT]B)\b/g, "$1 $2");
}

function formatCount(raw?: string): string | undefined {
  if (!raw) return undefined;
  const n = Number(raw.trim());
  if (!Number.isFinite(n)) return raw;
  return new Intl.NumberFormat().format(n);
}

function parseByteValue(raw?: string): number | null {
  if (!raw) return null;
  const paren = raw.match(/\((\d+) bytes\)/);
  if (paren) return Number(paren[1]);
  const direct = raw.match(/^(\d+) bytes$/);
  if (direct) return Number(direct[1]);
  const leading = raw.match(/^(\d+)$/);
  if (leading) return Number(leading[1]);
  return null;
}

function filterGoroutines(goroutines: ParsedGoroutine[], query: string, hideRuntimeOnly: boolean): ParsedGoroutine[] {
  const q = query.trim().toLowerCase();
  return goroutines.filter((goroutine) => {
    if (hideRuntimeOnly && goroutine.userFrameCount === 0) return false;
    if (!q) return true;
    return goroutine.searchText.includes(q) || goroutine.rawState.toLowerCase().includes(q);
  });
}

function profilePreview(kind: ProfileKind, source: ProfileSource, seconds: number, session: GolangSession): string {
  const effective = source === "auto" ? (session.pprofAvailable ? "pprof" : "gops") : source;
  if (effective === "pprof") {
    if (kind === "cpu") return `GET ${session.pprofBasePath ?? "/debug/pprof"}/profile?seconds=${seconds}`;
    if (kind === "trace") return `GET ${session.pprofBasePath ?? "/debug/pprof"}/trace?seconds=${seconds}`;
    return `GET ${session.pprofBasePath ?? "/debug/pprof"}/heap`;
  }
  if (kind === "cpu") return "gops pprof-cpu";
  if (kind === "trace") return "gops trace";
  return "gops pprof-heap";
}

function profileHelp(run: ProfileRun): string {
  if (run.state === "running") return "Profile is still running. Status refreshes automatically.";
  if (run.state === "stopped") return "Profile run was stopped before producing a downloadable profile.";
  if (run.state === "completed") return "Profile is complete. Use Download to inspect it with go tool pprof or go tool trace.";
  return "Profile run did not produce output.";
}

function portText(port?: number): string {
  return port ? `:${port}` : "unknown";
}

function fmtBytes(n?: number): string {
  if (!n) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = n;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  return `${value.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

function fmtDuration(ms?: number): string {
  if (!ms) return "0s";
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function refreshAll(qc: QueryClient) {
  qc.invalidateQueries({ queryKey: PODS_KEY });
  qc.invalidateQueries({ queryKey: SESSIONS_KEY });
}
