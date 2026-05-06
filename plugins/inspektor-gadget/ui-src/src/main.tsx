import React, { useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Button,
  DataTable,
  DensityProvider,
  HoverCard,
  Modal,
  ThemeProvider,
  cn,
  type DataTableColumn
} from "@flanksource/clicky-ui";
import {
  Activity,
  Clock,
  Cpu,
  Download,
  FileSearch,
  Gauge,
  Globe,
  HardDrive,
  Box,
  ChevronLeft,
  ChevronRight,
  Loader2,
  Lock,
  Network,
  Play,
  Radar,
  RefreshCw,
  Shield,
  Square,
  Terminal,
  Wrench
} from "lucide-react";
import { pluginBuildDate, pluginVersion } from "./version";
import "./styles.css";

type GadgetSpec = {
  id: string;
  name: string;
  image: string;
  description: string;
  kind: string;
  category: string;
  icon: string;
  docsUrl: string;
  streaming: boolean;
  options?: GadgetOption[];
  eventSchema?: EventSchema;
};

type EventSchema = {
  sourceStruct?: string;
  columns?: EventColumnSpec[];
};

type EventColumnSpec = {
  key: string;
  label: string;
  path: string;
  kind?: string;
  description?: string;
  hidden?: boolean;
};

type GadgetOption = {
  name: string;
  type: string;
  description?: string;
  default?: unknown;
};

type Status = {
  namespace: string;
  installed: boolean;
  ready: boolean;
  version?: string;
  expectedTag: string;
  desired?: number;
  readyPods?: number;
  problems?: string[];
};

type Session = {
  id: string;
  gadgetId: string;
  gadgetName: string;
  gadgetKind: string;
  gadgetImage: string;
  gadgetTag: string;
  docsUrl?: string;
  state: string;
  target: {
    namespace: string;
    kind?: string;
    name?: string;
    pod?: string;
    container?: string;
    node?: string;
  };
  startedAt: string;
  stoppedAt?: string;
  error?: string;
  eventCount: number;
  diagnostics: {
    runtime: string;
    connection: string;
    gadgetImage: string;
    gadgetTag: string;
    gadgetDocsUrl?: string;
    durationSec: number;
    maxEvents: number;
    maxSessions: number;
    resolvedPods?: Array<{ namespace: string; name: string; node?: string; containers: string[] }>;
    runtimeParams?: Record<string, string>;
    userOptions?: Record<string, unknown>;
    startedByEmail?: string;
  };
};

type TraceEvent = {
  sessionId: string;
  sequence: number;
  time: string;
  node?: string;
  raw?: string;
  error?: string;
  data?: Record<string, unknown>;
};

type EventTableRow = TraceEvent & {
  timeLabel: string;
  summary: string;
};

const durationPresets = [
  { label: "10s", value: 10 },
  { label: "30s", value: 30 },
  { label: "1m", value: 60 },
  { label: "5m", value: 300 },
  { label: "15m", value: 900 }
];

function configId() {
  return new URLSearchParams(window.location.search).get("config_id") || "";
}

function hostOperationUrl(op: string) {
  const params = new URLSearchParams(window.location.search);
  const id = params.get("config_id") || "";
  return `/api/plugins/inspektor-gadget/operations/${op}${id ? `?config_id=${encodeURIComponent(id)}` : ""}`;
}

async function invoke<T>(op: string, body: unknown = {}): Promise<T> {
  const res = await fetch(hostOperationUrl(op), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body)
  });
  if (!res.ok) {
    throw new Error(await res.text());
  }
  return res.json() as Promise<T>;
}

function pluginUiPath(path: string) {
  const base = window.location.pathname.replace(/\/$/, "");
  const query = window.location.search || "";
  return `${base}${path}${query}`;
}

function App() {
  const [status, setStatus] = useState<Status | null>(null);
  const [gadgets, setGadgets] = useState<GadgetSpec[]>([]);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [selectedGadget, setSelectedGadget] = useState("trace_exec");
  const [container, setContainer] = useState("");
  const [durationSec, setDurationSec] = useState(300);
  const [optionValues, setOptionValues] = useState<Record<string, unknown>>({});
  const [argText, setArgText] = useState("");
  const [events, setEvents] = useState<TraceEvent[]>([]);
  const [selectedSession, setSelectedSession] = useState<string>("");
  const [busy, setBusy] = useState<string>("");
  const [error, setError] = useState<string>("");
  const [nowMs, setNowMs] = useState(() => Date.now());
  const [startDialogOpen, setStartDialogOpen] = useState(false);
  const [sessionsOpen, setSessionsOpen] = useState(true);
  const [sessionsWidth, setSessionsWidth] = useState(320);
  const esRef = useRef<EventSource | null>(null);

  async function refresh() {
    setError("");
    const [nextStatus, nextGadgets, nextSessions] = await Promise.all([
      invoke<Status>("status"),
      invoke<GadgetSpec[]>("traces-list"),
      invoke<Session[]>("trace-list")
    ]);
    setStatus(nextStatus);
    setGadgets(nextGadgets);
    setSessions(nextSessions);
    if (!selectedSession && nextSessions.length > 0) {
      setSelectedSession(nextSessions[0].id);
    }
  }

  useEffect(() => {
    refresh().catch((err) => setError(String(err)));
    window.parent?.postMessage({ type: "mc.tab.ready" }, "*");
  }, []);

  useEffect(() => {
    const timer = window.setInterval(() => {
      invoke<Session[]>("trace-list").then(setSessions).catch(() => undefined);
    }, 5000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    const timer = window.setInterval(() => setNowMs(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    esRef.current?.close();
    setEvents([]);
    if (!selectedSession) return;
    const es = new EventSource(pluginUiPath(`/sessions/${selectedSession}/events`));
    es.onmessage = (msg) => {
      try {
        const event = JSON.parse(msg.data) as TraceEvent;
        setEvents((prev) => [...prev.slice(-999), event]);
      } catch {
        return;
      }
    };
    es.addEventListener("done", () => es.close());
    esRef.current = es;
    return () => es.close();
  }, [selectedSession]);

  const activeSession = useMemo(
    () => sessions.find((session) => session.id === selectedSession) || null,
    [sessions, selectedSession]
  );
  const activeGadgetSpec = useMemo(
    () => gadgets.find((gadget) => gadget.id === activeSession?.gadgetId) || null,
    [gadgets, activeSession?.gadgetId]
  );
  const eventRows = useMemo(
    () => events.map((event) => ({
      ...event,
      timeLabel: event.time ? new Date(event.time).toLocaleTimeString() : "",
      summary: event.error || summarize(event)
    })),
    [events]
  );
  const eventColumns: DataTableColumn<EventTableRow>[] = useMemo(
    () => eventTableColumns(activeGadgetSpec, activeSession),
    [activeGadgetSpec, activeSession]
  );
  const selectedGadgetSpec = useMemo(
    () => gadgets.find((gadget) => gadget.id === selectedGadget) || null,
    [gadgets, selectedGadget]
  );
  const categories = useMemo(() => {
    return Array.from(new Set(gadgets.map((gadget) => gadget.category)));
  }, [gadgets]);

  useEffect(() => {
    setOptionValues({});
  }, [selectedGadget]);

  async function startTrace() {
    setBusy("start");
    setError("");
    try {
      const options = {
        ...selectedOptionPayload(selectedGadgetSpec, optionValues),
        ...parseArgumentText(argText)
      };
      const session = await invoke<Session>("trace-start", {
        gadget: selectedGadget,
        container: container || undefined,
        durationSec,
        options
      });
      setSelectedSession(session.id);
      setStartDialogOpen(false);
      await refresh();
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy("");
    }
  }

  async function stopTrace(sessionId: string) {
    setBusy(`stop:${sessionId}`);
    try {
      await invoke<Session>("trace-stop", { id: sessionId });
      await refresh();
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy("");
    }
  }

  async function install() {
    setBusy("install");
    setError("");
    try {
      await invoke("install");
      await refresh();
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy("");
    }
  }

  function beginSessionsResize(event: React.MouseEvent<HTMLDivElement>) {
    event.preventDefault();
    const startX = event.clientX;
    const startWidth = sessionsWidth;
    const move = (moveEvent: MouseEvent) => {
      const next = Math.min(520, Math.max(240, startWidth + moveEvent.clientX - startX));
      setSessionsWidth(next);
    };
    const up = () => {
      window.removeEventListener("mousemove", move);
      window.removeEventListener("mouseup", up);
    };
    window.addEventListener("mousemove", move);
    window.addEventListener("mouseup", up);
  }

  return (
    <div className="app">
      <header className={`header-card ${status?.ready ? "ok" : "warn"}`}>
        <div className="brand">
          <Radar size={18} />
          <span>Inspektor Gadget</span>
          <span className="version">v{pluginVersion}{pluginBuildDate ? ` ${pluginBuildDate}` : ""}</span>
        </div>
        <div className="status-strip">
          <strong>{status?.ready ? "Ready" : status?.installed ? "Installed, not ready" : "Not installed"}</strong>
          <span>namespace {status?.namespace || "gadget"}</span>
          <span>expected {status?.expectedTag || ""}</span>
          {status?.version && <span>image {status.version}</span>}
          {status?.desired !== undefined && <span>pods {status.readyPods || 0}/{status.desired}</span>}
          {!status?.ready && (
            <button className="secondary" onClick={install} disabled={busy === "install"}>
              {busy === "install" ? <Loader2 className="spin" size={14} /> : <Wrench size={14} />}
              Install
            </button>
          )}
        </div>
        {status?.problems?.length ? <div className="header-problems" title={status.problems.join(" ")}>{status.problems.join(" ")}</div> : null}
        <div className="header-actions">
          <Button size="sm" onClick={() => setStartDialogOpen(true)} disabled={!configId()}>
            <Play size={14} />
            Start trace
          </Button>
          <Button variant="outline" size="icon" onClick={() => refresh().catch((err) => setError(String(err)))} title="Refresh">
            <RefreshCw size={16} />
          </Button>
        </div>
      </header>
      {error && <div className="error">{error}</div>}

      <main
        className={`workspace ${sessionsOpen ? "" : "sessions-collapsed"}`}
        style={{ "--sessions-width": `${sessionsWidth}px` } as React.CSSProperties}
      >
        <section className={`panel sessions ${sessionsOpen ? "" : "collapsed"}`}>
          <div className="sessions-heading">
            {sessionsOpen && <div className="panel-title">Sessions</div>}
            <Button
              variant="outline"
              size="icon"
              onClick={() => setSessionsOpen((open) => !open)}
              title={sessionsOpen ? "Collapse sessions" : "Expand sessions"}
            >
              {sessionsOpen ? <ChevronLeft size={15} /> : <ChevronRight size={15} />}
            </Button>
          </div>
          {sessionsOpen && (
            <>
              {sessions.length === 0 ? <div className="empty">No sessions</div> : sessions.map((session) => {
                const stoppable = isStoppable(session);
                const stopping = busy === `stop:${session.id}`;
                const SessionIcon = sessionIconFor(session, gadgets);
                return (
                  <div key={session.id} className={cn("session", session.id === selectedSession && "selected")}>
                    <button className="session-main" onClick={() => setSelectedSession(session.id)}>
                      <span className="session-title-row">
                        <SessionIcon size={14} />
                        <span className="session-name">{session.gadgetName || session.gadgetId}</span>
                        <span className={`session-state ${session.state}`}>{session.state}</span>
                        <span className="session-count">{session.eventCount}</span>
                      </span>
                      {stoppable && (
                        <span className="session-countdown">
                          <Clock size={13} />
                          {sessionTimerLabel(session, nowMs)}
                        </span>
                      )}
                    </button>
                    {stoppable && (
                      <Button variant="ghost" size="icon" className="session-stop" onClick={() => stopTrace(session.id)} disabled={stopping} title="Stop trace">
                        {stopping ? <Loader2 className="spin" size={14} /> : <Square size={14} />}
                      </Button>
                    )}
                  </div>
                );
              })}
              {activeSession && (
                <div className="session-details">
                  <div className="panel-title">Run Diagnostics</div>
                  {activeSession.error && (
                    <div className="failure-reason">
                      <span>Failed reason</span>
                      <code>{activeSession.error}</code>
                    </div>
                  )}
                  <KeyValue label="Image" value={activeSession.gadgetImage} mono />
                  <KeyValue label="Tag" value={activeSession.gadgetTag} />
                  <KeyValue label="Runtime" value={activeSession.diagnostics?.runtime} />
                  <KeyValue label="Connection" value={activeSession.diagnostics?.connection} />
                  <KeyValue label="Duration" value={`${activeSession.diagnostics?.durationSec || 0}s`} />
                  <KeyValue label="Max events" value={String(activeSession.diagnostics?.maxEvents || 0)} />
                  <KeyValue label="Target" value={targetLabel(activeSession)} mono />
                  <KeyValue label="Started" value={new Date(activeSession.startedAt).toLocaleString()} />
                  {activeSession.diagnostics?.startedByEmail && <KeyValue label="User" value={activeSession.diagnostics.startedByEmail} />}
                  {activeSession.diagnostics?.resolvedPods?.length ? (
                    <div className="pod-list">
                      {activeSession.diagnostics.resolvedPods.slice(0, 5).map((pod) => (
                        <code key={`${pod.namespace}/${pod.name}`}>{pod.namespace}/{pod.name}{pod.node ? ` @ ${pod.node}` : ""}</code>
                      ))}
                    </div>
                  ) : null}
                  <details>
                    <summary>Runtime params</summary>
                    <pre>{JSON.stringify(activeSession.diagnostics?.runtimeParams || {}, null, 2)}</pre>
                  </details>
                </div>
              )}
            </>
          )}
        </section>
        {sessionsOpen && <div className="resize-handle" onMouseDown={beginSessionsResize} title="Resize sessions" />}

        <section className="panel events">
          <div className="panel-heading">
            <div className="panel-title">Events</div>
            {activeSession && (
              <Button asChild size="sm">
                <a href={pluginUiPath(`/sessions/${activeSession.id}/export`)} download={`${activeSession.id}.ndjson`}>
                  <Download size={14} /> NDJSON
                </a>
              </Button>
            )}
          </div>
          <DataTable
            className="events-table"
            data={eventRows}
            columns={eventColumns}
            autoFilter
            defaultSort={{ key: "sequence", dir: "asc" }}
            getRowId={(row) => `${row.sessionId}-${row.sequence}`}
            columnResizeStorageKey={`inspektor-gadget-events-${activeGadgetSpec?.id || "generic"}`}
            emptyMessage={activeSession ? "Waiting for events" : "Select a session"}
            renderExpandedRow={(row) => <pre className="event-json">{JSON.stringify(originalEvent(row), null, 2)}</pre>}
          />
        </section>
      </main>
      {startDialogOpen && (
        <StartTraceDialog
          gadgets={gadgets}
          categories={categories}
          selectedGadget={selectedGadget}
          selectedGadgetSpec={selectedGadgetSpec}
          setSelectedGadget={setSelectedGadget}
          container={container}
          setContainer={setContainer}
          durationSec={durationSec}
          setDurationSec={setDurationSec}
          optionValues={optionValues}
          setOptionValues={setOptionValues}
          argText={argText}
          setArgText={setArgText}
          busy={busy}
          onClose={() => setStartDialogOpen(false)}
          onStart={startTrace}
        />
      )}
    </div>
  );
}

function StartTraceDialog({
  gadgets,
  categories,
  selectedGadget,
  selectedGadgetSpec,
  setSelectedGadget,
  container,
  setContainer,
  durationSec,
  setDurationSec,
  optionValues,
  setOptionValues,
  argText,
  setArgText,
  busy,
  onClose,
  onStart
}: {
  gadgets: GadgetSpec[];
  categories: string[];
  selectedGadget: string;
  selectedGadgetSpec: GadgetSpec | null;
  setSelectedGadget: (value: string) => void;
  container: string;
  setContainer: (value: string) => void;
  durationSec: number;
  setDurationSec: (value: number) => void;
  optionValues: Record<string, unknown>;
  setOptionValues: React.Dispatch<React.SetStateAction<Record<string, unknown>>>;
  argText: string;
  setArgText: (value: string) => void;
  busy: string;
  onClose: () => void;
  onStart: () => void;
}) {
  return (
    <Modal
      open
      onClose={onClose}
      size="xl"
      title={
        <div className="min-w-0">
          <div className="text-base font-semibold">Start trace</div>
          {selectedGadgetSpec && <div className="truncate font-mono text-xs text-muted-foreground">{selectedGadgetSpec.image}</div>}
        </div>
      }
      footer={
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={onClose} type="button">Cancel</Button>
          <Button onClick={onStart} disabled={busy === "start" || !configId()} type="button">
            {busy === "start" ? <Loader2 className="spin" size={14} /> : <Play size={14} />}
            Start
          </Button>
        </div>
      }
    >
      <div className="grid min-h-0 gap-4 md:grid-cols-[minmax(300px,1.15fr)_minmax(280px,0.85fr)]">
        <div>
          <div className="panel-title">Trace Type</div>
          <div className="gadget-picker max-h-[calc(100vh-230px)]">
            {categories.map((category) => (
              <div key={category} className="gadget-group">
                <div className="gadget-category">{category}</div>
                <div className="gadget-cards">
                  {gadgets.filter((gadget) => gadget.category === category).map((gadget) => {
                    const Icon = iconFor(gadget);
                    return (
                      <button
                        key={gadget.id}
                        className={cn("gadget-card", gadget.id === selectedGadget && "selected")}
                        onClick={() => setSelectedGadget(gadget.id)}
                        title={gadget.image}
                        type="button"
                      >
                        <Icon size={16} />
                        <span>{gadget.name}</span>
                      </button>
                    );
                  })}
                </div>
              </div>
            ))}
          </div>
        </div>
        <div className="dialog-form">
          <label>
            Container
            <input value={container} onChange={(e) => setContainer(e.target.value)} placeholder="auto" />
          </label>
          <label>
            Duration
            <input type="number" min={10} max={900} value={durationSec} onChange={(e) => setDurationSec(Number(e.target.value))} />
          </label>
          <div className="duration-presets" aria-label="Duration presets">
            {durationPresets.map((preset) => (
              <Button
                key={preset.value}
                variant={durationSec === preset.value ? "default" : "outline"}
                size="sm"
                onClick={() => setDurationSec(preset.value)}
                type="button"
              >
                {preset.label}
              </Button>
            ))}
          </div>
          {selectedGadgetSpec?.options?.length ? (
            <div className="gadget-options">
              <div className="panel-title">Arguments</div>
              {selectedGadgetSpec.options.map((option) => (
                <GadgetOptionInput
                  key={option.name}
                  option={option}
                  value={optionValues[option.name]}
                  onChange={(value) => setOptionValues((prev) => ({ ...prev, [option.name]: value }))}
                />
              ))}
            </div>
          ) : null}
          <label>
            Extra arguments
            <textarea
              value={argText}
              onChange={(e) => setArgText(e.target.value)}
              placeholder={"filter=proc.comm == \"curl\"\noperator.Sort.sort=timestamp\n--custom-flag"}
              rows={4}
            />
          </label>
          {selectedGadgetSpec && (
            <div className="diagnostics">
              <div className="hint">{selectedGadgetSpec.description}</div>
              <KeyValue label="Image" value={selectedGadgetSpec.image} mono />
              <KeyValue label="Kind" value={`${selectedGadgetSpec.kind} / ${selectedGadgetSpec.streaming ? "streaming" : "snapshot"}`} />
              <a href={selectedGadgetSpec.docsUrl} target="_blank" rel="noreferrer">Docs</a>
            </div>
          )}
        </div>
      </div>
    </Modal>
  );
}

function summarize(event: TraceEvent) {
  const data = event.data || {};
  const proc = data.proc as Record<string, unknown> | undefined;
  const k8s = data.k8s as Record<string, unknown> | undefined;
  const parts = [
    k8s?.namespace,
    k8s?.podName,
    k8s?.containerName,
    proc?.comm || data.comm,
    data.type,
    data.dst || data.name || data.fname || data.args
  ].filter(Boolean);
  return parts.length > 0 ? parts.join(" / ") : event.raw || "";
}

function eventTableColumns(gadget: GadgetSpec | null, session: Session | null): DataTableColumn<EventTableRow>[] {
  const columns: DataTableColumn<EventTableRow>[] = [
    {
      key: "sequence",
      label: "#",
      align: "right",
      shrink: true,
      minWidth: 56,
      sortable: true
    },
    {
      key: "timeLabel",
      label: "Time",
      shrink: true,
      minWidth: 96,
      sortable: true,
      sortValue: (_value, row) => Date.parse(row.time || "") || 0
    },
    {
      key: "node",
      label: "Node",
      shrink: true,
      minWidth: 130,
      filterable: true
    },
    {
      key: "data.k8s",
      label: "Workload",
      minWidth: 220,
      filterable: true,
      filterValue: (_value, row) => formatK8s(row.data?.k8s, session),
      render: (_value, row) => <K8sCell value={row.data?.k8s} session={session} />
    }
  ];

  for (const spec of gadget?.eventSchema?.columns || []) {
    if (spec.hidden) continue;
    columns.push(eventColumn(spec));
  }

  columns.push({
    key: "summary",
    label: "Summary",
    grow: true,
    minWidth: 280,
    filterable: true,
    cellClassName: "font-mono text-xs truncate max-w-0",
    render: (value) => <code title={String(value || "")}>{String(value || "")}</code>
  });
  return columns;
}

function eventColumn(spec: EventColumnSpec): DataTableColumn<EventTableRow> {
  const key = `data.${spec.path}`;
  const numeric = spec.kind === "number" || spec.kind === "bytes";
  return {
    key,
    label: spec.label || spec.path,
    align: numeric ? "right" : "left",
    shrink: spec.kind !== "json" && spec.kind !== "text",
    minWidth: columnMinWidth(spec),
    filterable: true,
    sortValue: (_value, row) => sortValue(eventDataValue(row, spec.path), spec.kind),
    filterValue: (_value, row) => displayEventValue(eventDataValue(row, spec.path), spec.kind),
    cellClassName: spec.kind === "json" ? "font-mono text-xs truncate max-w-0" : undefined,
    render: (_value, row) => {
      const value = eventDataValue(row, spec.path);
      if (spec.kind === "process") return <ProcessCell value={value} />;
      if (spec.kind === "endpoint") return <EndpointCell value={value} row={row} path={spec.path} />;
      const display = displayEventValue(value, spec.kind);
      return <code title={display}>{display}</code>;
    }
  };
}

function columnMinWidth(spec: EventColumnSpec) {
  if (spec.kind === "process") return 170;
  if (spec.kind === "endpoint") return 190;
  if (spec.kind === "bytes" || spec.kind === "number") return 96;
  if (spec.kind === "json") return 180;
  if (/(path|file|args|address|destination|source|buffer|parameters)/i.test(spec.label)) return 220;
  return 130;
}

function eventDataValue(row: EventTableRow, path: string) {
  return valueAtPath(row.data || {}, path);
}

function valueAtPath(value: unknown, path: string): unknown {
  let current = value;
  for (const part of path.split(".")) {
    if (!part) continue;
    if (current == null || typeof current !== "object") return undefined;
    current = (current as Record<string, unknown>)[part];
  }
  return current;
}

function sortValue(value: unknown, kind?: string) {
  if (kind === "number" || kind === "bytes") {
    const number = Number(value);
    return Number.isFinite(number) ? number : 0;
  }
  return displayEventValue(value, kind);
}

function displayEventValue(value: unknown, kind?: string): string {
  if (value == null || value === "") return "";
  if (kind === "process") return formatProcess(value);
  if (kind === "endpoint") return formatEndpoint(value);
  if (kind === "bytes") return formatBytes(value);
  if (kind === "boolean") return Boolean(value) ? "true" : "false";
  if (typeof value === "object") return compactJSON(value);
  return String(value);
}

function EndpointCell({ value, row, path }: { value: unknown; row: EventTableRow; path: string }) {
  const endpoint = endpointParts(value, row, path);
  if (!endpoint.addr && !endpoint.port && !endpoint.proto && !endpoint.k8s) return <span />;
  const title = endpointTitle(endpoint);
  const trigger = (
    <span className="inline-flex max-w-full items-center gap-1.5 align-top" title={title}>
      <Network size={14} className="shrink-0 text-muted-foreground" />
      <span className="min-w-0 truncate text-foreground">{endpoint.addr || ""}</span>
      {endpoint.port && <span className="shrink-0 font-mono text-[11px] text-muted-foreground">:{endpoint.port}</span>}
      {endpoint.proto && <span className="shrink-0 rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-semibold text-muted-foreground">{endpoint.proto}</span>}
    </span>
  );
  return (
    <HoverCard trigger={trigger} placement="bottom" delay={120} cardClassName="w-[min(28rem,calc(100vw-2rem))]">
      <DetailRows rows={endpointDetails(endpoint)} />
    </HoverCard>
  );
}

function K8sCell({ value, session }: { value: unknown; session: Session | null }) {
  const record = value && typeof value === "object" ? value as Record<string, unknown> : {};
  const namespace = record.namespace || session?.target?.namespace;
  const name = record.podName || record.pod || record.name || session?.target?.pod || session?.target?.name;
  const container = record.containerName || record.container || session?.target?.container;
  if (!namespace && !name && !container) return <span />;
  const trigger = (
    <span className="inline-flex max-w-full items-center gap-1.5 align-top" title={formatK8s(value, session)}>
      <Box size={14} className="shrink-0 text-muted-foreground" />
      <span className="min-w-0 truncate text-foreground">{String(name || "")}</span>
      {container && <span className="shrink-0 rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-semibold text-muted-foreground">{String(container)}</span>}
    </span>
  );
  return (
    <HoverCard trigger={trigger} placement="bottom" delay={120} cardClassName="w-[min(28rem,calc(100vw-2rem))]">
      <DetailRows rows={[
        ["Kind", stringValue(record.kind || session?.target?.kind || "pod")],
        ["Namespace", stringValue(namespace)],
        ["Name", stringValue(name)],
        ["Container", stringValue(container)],
        ["Labels", stringValue(record.labels)],
        ["Selector", stringValue(record.podSelector)]
      ]} />
    </HoverCard>
  );
}

function ProcessCell({ value }: { value: unknown }) {
  const record = value && typeof value === "object" ? value as Record<string, unknown> : {};
  const comm = record.comm || record.pcomm || record.name;
  const pid = record.pid;
  const pidLabel = stringValue(pid);
  if (!comm && !pidLabel) {
    const display = displayEventValue(value);
    return <code title={display}>{display}</code>;
  }
  const trigger = (
    <span className="inline-flex max-w-full items-center gap-1.5 align-top" title={formatProcess(value)}>
      <Terminal size={14} className="shrink-0 text-muted-foreground" />
      <span className="min-w-0 truncate text-foreground">{String(comm || "")}</span>
      {pidLabel && <span className="shrink-0 font-mono text-[11px] text-muted-foreground">pid {pidLabel}</span>}
    </span>
  );
  return (
    <HoverCard trigger={trigger} placement="bottom" delay={120} cardClassName="w-[min(24rem,calc(100vw-2rem))]">
      <DetailRows rows={[
        ["Command", stringValue(comm)],
        ["PID", stringValue(pid)],
        ["TID", stringValue(record.tid)],
        ["PPID", stringValue(record.ppid)],
        ["UID", stringValue(record.uid ?? record.uidRaw)],
        ["GID", stringValue(record.gid ?? record.gidRaw)],
        ["Parent", stringValue(record.pcomm)]
      ]} />
    </HoverCard>
  );
}

function DetailRows({ rows }: { rows: Array<[string, string]> }) {
  const visible = rows.filter(([, value]) => value !== "");
  if (visible.length === 0) return null;
  return (
    <div className="flex flex-col gap-1 text-xs">
      {visible.map(([label, value]) => (
        <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2" key={label}>
          <span className="font-semibold text-muted-foreground">{label}</span>
          <code className="min-w-0 break-words">{value}</code>
        </div>
      ))}
    </div>
  );
}

function endpointParts(value: unknown, row?: EventTableRow, path?: string) {
  const record = value && typeof value === "object" ? value as Record<string, unknown> : {};
  const rowData = row?.data || {};
  const addr = stringValue(record.addr || record.ip || record.name || (typeof value === "string" || typeof value === "number" ? value : ""));
  const port = stringValue(record.port || (path === "addr" ? rowData.port : undefined));
  const proto = stringValue(record.proto || (path === "addr" ? rowData.proto : undefined));
  const version = stringValue(record.version || (path === "addr" ? rowData.version : undefined));
  const k8s = record.k8s && typeof record.k8s === "object" ? record.k8s as Record<string, unknown> : undefined;
  return { addr, port, proto, version, k8s, raw: value };
}

function endpointDetails(endpoint: ReturnType<typeof endpointParts>): Array<[string, string]> {
  return [
    ["Address", endpoint.addr],
    ["Port", endpoint.port],
    ["Protocol", endpoint.proto],
    ["IP Version", endpoint.version],
    ["K8s Namespace", stringValue(endpoint.k8s?.namespace)],
    ["K8s Kind", stringValue(endpoint.k8s?.kind)],
    ["K8s Name", stringValue(endpoint.k8s?.name || endpoint.k8s?.podName || endpoint.k8s?.pod)],
    ["Labels", stringValue(endpoint.k8s?.labels)],
    ["Selector", stringValue(endpoint.k8s?.podSelector)]
  ];
}

function endpointTitle(endpoint: ReturnType<typeof endpointParts>) {
  return endpointDetails(endpoint).filter(([, value]) => value).map(([label, value]) => `${label}: ${value}`).join("\n");
}

function formatEndpoint(value: unknown) {
  const endpoint = endpointParts(value);
  return [endpoint.addr, endpoint.port ? `:${endpoint.port}` : "", endpoint.proto ? ` ${endpoint.proto}` : ""].join("");
}

function formatProcess(value: unknown) {
  if (!value || typeof value !== "object") return displayEventValue(value);
  const record = value as Record<string, unknown>;
  const comm = record.comm || record.pcomm || record.name;
  const pid = record.pid;
  const tid = record.tid;
  const uid = record.uid ?? record.uidRaw;
  const parts = [comm, pid ? `pid ${pid}` : "", tid && tid !== pid ? `tid ${tid}` : "", uid !== undefined ? `uid ${uid}` : ""].filter(Boolean);
  return parts.length ? parts.join(" / ") : compactJSON(record);
}

function formatK8s(value: unknown, session: Session | null) {
  const record = value && typeof value === "object" ? value as Record<string, unknown> : {};
  const namespace = record.namespace || session?.target?.namespace;
  const pod = record.podName || record.pod || record.name || session?.target?.pod || session?.target?.name;
  const container = record.containerName || record.container || session?.target?.container;
  return [namespace, pod, container].filter(Boolean).join(" / ");
}

function stringValue(value: unknown) {
  if (value == null) return "";
  if (typeof value === "object") return compactJSON(value);
  return String(value);
}

function formatBytes(value: unknown) {
  const number = Number(value);
  if (!Number.isFinite(number)) return String(value ?? "");
  if (Math.abs(number) < 1024) return `${number} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let scaled = number / 1024;
  let unit = units[0];
  for (let i = 1; i < units.length && Math.abs(scaled) >= 1024; i++) {
    scaled /= 1024;
    unit = units[i];
  }
  return `${scaled.toFixed(scaled >= 10 ? 1 : 2)} ${unit}`;
}

function compactJSON(value: unknown) {
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function originalEvent(row: EventTableRow): TraceEvent {
  const { timeLabel: _timeLabel, summary: _summary, ...event } = row;
  return event;
}

function KeyValue({ label, value, mono = false }: { label: string; value?: string; mono?: boolean }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5 text-xs">
      <span className="text-[11px] font-semibold uppercase text-muted-foreground">{label}</span>
      <code className={cn("truncate", !mono && "font-sans")}>{value || ""}</code>
    </div>
  );
}

function GadgetOptionInput({
  option,
  value,
  onChange
}: {
  option: GadgetOption;
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  const effective = value ?? option.default ?? "";
  if (option.type === "boolean") {
    return (
      <label className="option-row">
        <input
          type="checkbox"
          checked={Boolean(effective)}
          onChange={(e) => onChange(e.target.checked)}
        />
        <span>{option.name}</span>
        {option.description && <small>{option.description}</small>}
      </label>
    );
  }
  return (
    <label>
      {option.name}
      <input
        value={String(effective)}
        onChange={(e) => onChange(e.target.value)}
        placeholder={option.description || ""}
      />
    </label>
  );
}

function targetLabel(session: Session) {
  const target = session.target;
  return [
    target.namespace,
    target.pod || `${target.name || ""}`,
    target.container,
    target.node
  ].filter(Boolean).join(" / ");
}

function selectedOptionPayload(gadget: GadgetSpec | null, values: Record<string, unknown>) {
  const options: Record<string, unknown> = {};
  for (const option of gadget?.options || []) {
    const value = values[option.name] ?? option.default;
    if (value !== undefined && value !== "") {
      options[option.name] = value;
    }
  }
  return options;
}

function parseArgumentText(text: string) {
  const trimmed = text.trim();
  if (!trimmed) return {};
  if (trimmed.startsWith("{")) {
    const parsed = JSON.parse(trimmed) as Record<string, unknown>;
    if (!parsed || Array.isArray(parsed) || typeof parsed !== "object") {
      throw new Error("extra arguments JSON must be an object");
    }
    return parsed;
  }
  const options: Record<string, string | boolean> = {};
  for (const rawLine of trimmed.split(/\r?\n/)) {
    let line = rawLine.trim();
    if (!line || line.startsWith("#")) continue;
    line = line.replace(/^--/, "");
    let idx = line.indexOf("=");
    if (idx < 0) idx = line.indexOf(":");
    if (idx < 0) {
      options[line] = true;
      continue;
    }
    const key = line.slice(0, idx).trim();
    const value = line.slice(idx + 1).trim();
    if (!key) throw new Error(`invalid extra argument: ${rawLine}`);
    options[key] = value;
  }
  return options;
}

function isStoppable(session: Session) {
  return session.state === "starting" || session.state === "running";
}

function sessionTimerLabel(session: Session, nowMs: number) {
  if (!isStoppable(session)) {
    if (session.stoppedAt) return new Date(session.stoppedAt).toLocaleTimeString();
    return session.state;
  }
  const remaining = remainingSeconds(session, nowMs);
  if (remaining === null) return "timed";
  return `${formatDuration(remaining)} left`;
}

function remainingSeconds(session: Session, nowMs: number) {
  const duration = session.diagnostics?.durationSec || 0;
  const startedAt = Date.parse(session.startedAt);
  if (!duration || Number.isNaN(startedAt)) return null;
  return Math.max(0, Math.ceil((startedAt + duration * 1000 - nowMs) / 1000));
}

function formatDuration(totalSeconds: number) {
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

function iconFor(gadget: GadgetSpec) {
  if (gadget.category === "Network") return Network;
  if (gadget.category === "Security") return Shield;
  if (gadget.category === "Files") return FileSearch;
  if (gadget.category === "Runtime") return Terminal;
  if (gadget.category === "Profile") return Cpu;
  if (gadget.category === "Observability") return Activity;
  if (gadget.kind === "top") return Gauge;
  if (gadget.kind === "snapshot") return HardDrive;
  if (gadget.id.includes("dns") || gadget.id.includes("sni")) return Globe;
  if (gadget.id.includes("ssl") || gadget.id.includes("seccomp")) return Lock;
  return Radar;
}

function sessionIconFor(session: Session, gadgets: GadgetSpec[]) {
  const gadget = gadgets.find((candidate) => candidate.id === session.gadgetId);
  if (gadget) return iconFor(gadget);
  if (session.gadgetKind === "top") return Gauge;
  if (session.gadgetKind === "snapshot") return HardDrive;
  return Radar;
}

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");
const queryClient = new QueryClient();
createRoot(root).render(
  <ThemeProvider>
    <DensityProvider>
      <QueryClientProvider client={queryClient}>
        <App />
      </QueryClientProvider>
    </DensityProvider>
  </ThemeProvider>
);
