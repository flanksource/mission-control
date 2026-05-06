import React, { useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  DataTable,
  DensityProvider,
  ThemeProvider,
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
  Wrench,
  X
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
  const eventRows = useMemo(
    () => events.map((event) => ({
      ...event,
      timeLabel: event.time ? new Date(event.time).toLocaleTimeString() : "",
      summary: event.error || summarize(event)
    })),
    [events]
  );
  const eventColumns: DataTableColumn<EventTableRow>[] = useMemo(
    () => [
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
        minWidth: 140,
        filterable: true
      },
      {
        key: "summary",
        label: "Event",
        grow: true,
        minWidth: 360,
        filterable: true,
        cellClassName: "font-mono text-xs truncate max-w-0",
        render: (value) => <code title={String(value || "")}>{String(value || "")}</code>
      }
    ],
    []
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
          <button onClick={() => setStartDialogOpen(true)} disabled={!configId()}>
            <Play size={14} />
            Start trace
          </button>
          <button className="icon-button" onClick={() => refresh().catch((err) => setError(String(err)))} title="Refresh">
            <RefreshCw size={16} />
          </button>
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
            <button
              className="icon-button secondary"
              onClick={() => setSessionsOpen((open) => !open)}
              title={sessionsOpen ? "Collapse sessions" : "Expand sessions"}
            >
              {sessionsOpen ? <ChevronLeft size={15} /> : <ChevronRight size={15} />}
            </button>
          </div>
          {sessionsOpen && (
            <>
              {sessions.length === 0 ? <div className="empty">No sessions</div> : sessions.map((session) => {
                const stoppable = isStoppable(session);
                const stopping = busy === `stop:${session.id}`;
                return (
                  <div key={session.id} className={`session ${session.id === selectedSession ? "selected" : ""}`}>
                    <button className="session-main" onClick={() => setSelectedSession(session.id)}>
                      <span>{session.gadgetName || session.gadgetId}</span>
                      <span className={`session-state ${session.state}`}>{session.state}</span>
                      <span>{session.eventCount} events</span>
                      <span className="session-countdown">
                        <Clock size={13} />
                        {sessionTimerLabel(session, nowMs)}
                      </span>
                    </button>
                    {stoppable && (
                      <button className="session-stop" onClick={() => stopTrace(session.id)} disabled={stopping} title="Stop trace">
                        {stopping ? <Loader2 className="spin" size={14} /> : <Square size={14} />}
                      </button>
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
              <a className="export" href={pluginUiPath(`/sessions/${activeSession.id}/export`)} download={`${activeSession.id}.ndjson`}>
                <Download size={14} /> NDJSON
              </a>
            )}
          </div>
          <DataTable
            className="events-table"
            data={eventRows}
            columns={eventColumns}
            autoFilter
            defaultSort={{ key: "sequence", dir: "asc" }}
            getRowId={(row) => `${row.sessionId}-${row.sequence}`}
            columnResizeStorageKey="inspektor-gadget-events"
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
    <div className="dialog-backdrop" role="presentation" onMouseDown={onClose}>
      <section
        className="dialog-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="start-trace-title"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <header className="dialog-header">
          <div>
            <h2 id="start-trace-title">Start trace</h2>
            {selectedGadgetSpec && <span>{selectedGadgetSpec.image}</span>}
          </div>
          <button className="icon-button secondary" onClick={onClose} title="Close">
            <X size={16} />
          </button>
        </header>
        <div className="dialog-body">
          <div>
            <div className="panel-title">Trace Type</div>
            <div className="gadget-picker dialog-picker">
              {categories.map((category) => (
                <div key={category} className="gadget-group">
                  <div className="gadget-category">{category}</div>
                  <div className="gadget-cards">
                    {gadgets.filter((gadget) => gadget.category === category).map((gadget) => {
                      const Icon = iconFor(gadget);
                      return (
                        <button
                          key={gadget.id}
                          className={`gadget-card ${gadget.id === selectedGadget ? "selected" : ""}`}
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
                <button
                  key={preset.value}
                  className={durationSec === preset.value ? "selected" : ""}
                  onClick={() => setDurationSec(preset.value)}
                  type="button"
                >
                  {preset.label}
                </button>
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
        <footer className="dialog-footer">
          <button className="secondary" onClick={onClose} type="button">Cancel</button>
          <button onClick={onStart} disabled={busy === "start" || !configId()} type="button">
            {busy === "start" ? <Loader2 className="spin" size={14} /> : <Play size={14} />}
            Start
          </button>
        </footer>
      </section>
    </div>
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

function originalEvent(row: EventTableRow): TraceEvent {
  const { timeLabel: _timeLabel, summary: _summary, ...event } = row;
  return event;
}

function KeyValue({ label, value, mono = false }: { label: string; value?: string; mono?: boolean }) {
  return (
    <div className="kv">
      <span>{label}</span>
      <code className={mono ? "" : "plain"}>{value || ""}</code>
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
