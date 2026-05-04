import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Play, RefreshCw, Square, Trash2 } from "lucide-react";
import {
  Badge,
  Button,
  LogsTable,
  type LogsTableInput,
  type DataTableColumn,
  type LogsTableRow,
} from "@flanksource/clicky-ui";
import { callOp, configIDFromURL, openTraceStream } from "../lib/api";
import { readPref, writePref } from "../lib/prefs";
import { ErrorBox, Card } from "./StatsTab";
import { DatabasePicker } from "./DatabasePicker";

type EventOrder = "newest" | "oldest";
const EVENT_ORDER_KEY = "sql-server-trace-order";
const EVENT_ORDERS = ["newest", "oldest"] as const;

interface ActiveTrace {
  id: string;
  sessionName: string;
  database: string;
  configItemId: string;
  startedAt: string;
  stoppedAt?: string;
}

interface TraceEvent {
  // Loose shape — xetrace.Event has many optional fields and varies by event
  // type. We just render whatever the server sends.
  [key: string]: unknown;
}

// Column layout: Timestamp + Event on the left; the statement (or error
// message) takes the elastic middle; the per-row dimensions the operator
// scans most often (database, user, SPID) sit on the right where they
// stay aligned across rows. Full record + remaining tags are in the
// row-detail expansion.
const TRACE_LOG_COLUMNS: DataTableColumn<LogsTableRow>[] = [
  { key: "timestamp", label: "Timestamp", kind: "timestamp", shrink: true, minWidth: 180 },
  { key: "level", label: "Event", kind: "status", shrink: true, minWidth: 140, status: { showLabel: true } },
  {
    key: "message",
    label: "Statement",
    grow: true,
    minWidth: 360,
    cellClassName: "font-mono text-xs truncate max-w-0",
    render: (value) => {
      const text = typeof value === "string" ? value : value == null ? "" : String(value);
      // Collapse whitespace so multi-line SQL renders on one line.
      const collapsed = text.replace(/\s+/g, " ").trim();
      return <span title={text}>{collapsed}</span>;
    },
  },
  // pod/logger/thread are LogsTableRow's existing slots (the type can't
  // be extended freely) — relabel them as Database / User / SPID for the
  // SQL Server domain.
  { key: "pod", label: "Database", shrink: true, minWidth: 140, filterable: true },
  { key: "logger", label: "User", shrink: true, minWidth: 120, filterable: true },
  {
    key: "thread",
    label: "SPID",
    shrink: true,
    minWidth: 70,
    align: "right",
    filterable: true,
  },
  {
    // Duration isn't a top-level LogsTableRow field — pull it off
    // row.raw (the original xetrace.Event payload) so we don't need to
    // shoehorn it through the normalizer.
    key: "duration",
    label: "Duration",
    shrink: true,
    minWidth: 100,
    align: "right",
    sortable: true,
    sortValue: (_value, row) => durationNanos((row as LogsTableRow).raw) ?? -1,
    render: (_value, row) => {
      const ns = durationNanos((row as LogsTableRow).raw);
      if (ns == null) return "";
      return <span className="font-mono text-xs">{formatNanos(ns)}</span>;
    },
  },
];

function durationNanos(raw: unknown): number | null {
  if (!raw || typeof raw !== "object") return null;
  const value = (raw as Record<string, unknown>).duration;
  if (typeof value === "number" && Number.isFinite(value) && value > 0) return value;
  return null;
}

// Convert one xetrace.Event into the shape LogsTable consumes. Maps the
// event name onto level (so the status cell renders a coloured pill),
// the SQL onto message, and the database/user/session-id onto the
// LogsTableRow's pod/logger/thread slots (relabelled in TRACE_LOG_COLUMNS).
//
// Error events (error_reported, error_number > 0) substitute the error
// message for the SQL so it shows in the table; the original statement
// is still in the raw payload, visible in the row inspector.
function eventToLog(e: TraceEvent): LogsTableInput {
  const name = stringField(e.name) ?? "event";
  const errMsg = stringField(e.error_message);
  const errNum = typeof e.error_number === "number" ? e.error_number : 0;
  const isError = errNum > 0 || isErrorEvent(name);

  const level = isError ? "error" : name;
  const message = isError
    ? errMsg
      ? `[error ${errNum || "?"}] ${errMsg}`
      : `[${name}]`
    : stringField(e.statement) ?? stringField(e.raw_statement) ?? name;

  // Remaining metrics + client app go into labels so the row inspector
  // surfaces them. Database/user/session_id are promoted to columns above
  // and so are NOT duplicated here.
  const labels: Record<string, unknown> = {};
  for (const key of [
    "client_app_name",
    "row_count",
    "logical_reads",
    "physical_reads",
    "writes",
  ]) {
    const value = e[key];
    if (value === undefined || value === null || value === "") continue;
    labels[key] = String(value);
  }
  for (const key of ["duration", "cpu_time"]) {
    const value = e[key];
    if (value === undefined || value === null || value === 0) continue;
    labels[key] = formatNanos(value);
  }
  if (isError) {
    if (errNum) labels["error_number"] = String(errNum);
    if (errMsg) labels["error_message"] = errMsg;
    const stmt = stringField(e.statement) ?? stringField(e.raw_statement);
    if (stmt) labels["failed_statement"] = stmt;
  }

  return {
    timestamp: stringField(e.timestamp),
    level,
    message,
    pod: stringField(e.database_name) ?? "",
    logger: stringField(e.username) ?? "",
    thread: stringField(e.session_id) ?? "",
    labels,
    raw: e,
    line: JSON.stringify(e),
  };
}

function stringField(value: unknown): string | undefined {
  if (value === undefined || value === null) return undefined;
  if (typeof value === "string") return value || undefined;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return undefined;
}

// SQL Server's error stream surfaces under several event names depending
// on session config. Treat any of them as an error row even when the
// numeric error_number wasn't lifted from the payload.
function isErrorEvent(name: string): boolean {
  switch (name) {
    case "error_reported":
    case "errorlog_written":
    case "exception_ring_buffer_recorded":
      return true;
    default:
      return false;
  }
}

function formatNanos(value: unknown): string {
  // xetrace.Event encodes time.Duration as nanoseconds.
  const n = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(n)) return String(value);
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)} ms`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(2)} µs`;
  return `${n} ns`;
}

const inputCls =
  "h-control-h w-[200px] rounded-md border border-input bg-background px-2 text-sm";
const labelCls = "inline-flex items-center gap-density-1 text-xs text-muted-foreground";
const thCls = "px-density-2 py-density-1 text-left font-semibold text-foreground";
const tdCls = "px-density-2 py-density-1 font-mono text-xs";

export function TraceTab() {
  const configID = configIDFromURL();
  const qc = useQueryClient();
  const [database, setDatabase] = useState("");
  const [duration, setDuration] = useState(30);
  const [minDurationMicros, setMinDurationMicros] = useState(0);
  const [activeTraceID, setActiveTraceID] = useState<string | null>(null);
  const [events, setEvents] = useState<TraceEvent[]>([]);
  const esRef = useRef<EventSource | null>(null);

  const list = useQuery({
    queryKey: ["traces", configID],
    queryFn: () => callOp<ActiveTrace[]>("trace-list", configID, {}),
    refetchInterval: 5_000,
  });

  const startMut = useMutation({
    mutationFn: () =>
      callOp<ActiveTrace>("trace-start", configID, {
        database,
        durationSeconds: duration,
        minDurationMicros,
      }),
    onSuccess: (t) => {
      setActiveTraceID(t.id);
      setEvents([]);
      qc.invalidateQueries({ queryKey: ["traces"] });
    },
  });
  const stopMut = useMutation({
    mutationFn: (id: string) => callOp("trace-stop", configID, { id }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["traces"] }),
  });
  const deleteMut = useMutation({
    mutationFn: (id: string) => callOp("trace-delete", configID, { id }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["traces"] });
      if (activeTraceID === deleteMut.variables) {
        setActiveTraceID(null);
        setEvents([]);
      }
    },
  });

  useEffect(() => {
    if (!activeTraceID) return;
    esRef.current?.close();
    const es = openTraceStream(
      activeTraceID,
      (e) => setEvents((prev) => [...prev, e as TraceEvent]),
      () => qc.invalidateQueries({ queryKey: ["traces"] }),
    );
    esRef.current = es;
    return () => es.close();
  }, [activeTraceID, qc]);

  return (
    <section className="flex h-[calc(100vh-80px)] flex-col gap-density-2">
      <Card title="Start a trace">
        <div className="flex flex-wrap items-center gap-density-2">
          <label className={labelCls}>
            Database
            <DatabasePicker
              configID={configID}
              value={database}
              onChange={setDatabase}
              emptyLabel="All databases"
            />
          </label>
          <label className={labelCls}>
            duration (s)
            <input
              type="number"
              min={1}
              value={duration}
              onChange={(e) => setDuration(parseInt(e.currentTarget.value) || 30)}
              className={inputCls + " !w-[80px]"}
            />
          </label>
          <label className={labelCls}>
            min duration (μs)
            <input
              type="number"
              min={0}
              value={minDurationMicros}
              onChange={(e) => setMinDurationMicros(parseInt(e.currentTarget.value) || 0)}
              className={inputCls + " !w-[100px]"}
            />
          </label>
          <Button size="sm" onClick={() => startMut.mutate()} disabled={startMut.isPending}>
            {startMut.isPending ? <Loader2 size={12} className="spin" /> : <Play size={12} />} Start
          </Button>
        </div>
      </Card>

      {startMut.error && <ErrorBox error={startMut.error as Error} />}

      <Card title="Active and recent traces">
        <div className="mb-density-1 flex items-center justify-between">
          <span className="text-xs text-muted-foreground">{list.data?.length ?? 0} sessions</span>
          <Button variant="outline" size="sm" onClick={() => list.refetch()}>
            <RefreshCw size={12} className={list.isFetching ? "spin" : ""} /> Refresh
          </Button>
        </div>
        <table className="w-full border-collapse text-xs">
          <thead>
            <tr className="bg-muted/30">
              {["ID", "Session", "Database", "Started", "Stopped", ""].map((h) => (
                <th key={h} className={thCls}>
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {list.data?.map((t) => (
              <tr
                key={t.id}
                className={
                  "cursor-pointer border-t border-border " +
                  (t.id === activeTraceID ? "bg-primary/10" : "hover:bg-muted/40")
                }
                onClick={() => setActiveTraceID(t.id)}
              >
                <td className={tdCls}>{t.id}</td>
                <td className={tdCls}>{t.sessionName}</td>
                <td className={tdCls}>{t.database || <em>all</em>}</td>
                <td className={tdCls}>{new Date(t.startedAt).toLocaleTimeString()}</td>
                <td className={tdCls}>
                  {t.stoppedAt ? (
                    new Date(t.stoppedAt).toLocaleTimeString()
                  ) : (
                    <Badge variant="outline" className="border-emerald-500 text-emerald-600">
                      running
                    </Badge>
                  )}
                </td>
                <td className={tdCls}>
                  <div className="flex gap-density-1">
                    {!t.stoppedAt && (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={(e) => {
                          e.stopPropagation();
                          stopMut.mutate(t.id);
                        }}
                        title="Stop"
                      >
                        <Square size={11} />
                      </Button>
                    )}
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={(e) => {
                        e.stopPropagation();
                        deleteMut.mutate(t.id);
                      }}
                      title="Delete"
                      className="border-destructive/40 text-destructive hover:bg-destructive/10"
                    >
                      <Trash2 size={11} />
                    </Button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>

      {activeTraceID && (
        <div className="flex min-h-0 flex-1 flex-col">
          <TraceEventsView traceID={activeTraceID} events={events} />
        </div>
      )}
    </section>
  );
}

function TraceEventsView({ traceID, events }: { traceID: string; events: TraceEvent[] }) {
  const logs = useMemo(() => events.map(eventToLog), [events]);
  const [order, setOrder] = useState<EventOrder>(() =>
    readPref<EventOrder>(EVENT_ORDER_KEY, EVENT_ORDERS, "newest"),
  );
  const updateOrder = (next: EventOrder) => {
    setOrder(next);
    writePref(EVENT_ORDER_KEY, next);
  };

  // LogsTable's defaultSort doesn't react to changes after mount — give
  // the table a key tied to the order so it re-mounts and resorts.
  return (
    <Card title={`Events — ${traceID}`} className="flex min-h-0 flex-1 flex-col">
      <div className="mb-density-1 flex shrink-0 items-center gap-density-2">
        <span className="text-xs text-muted-foreground">
          {events.length} events captured (live tail)
        </span>
        <div className="ml-auto flex items-center gap-density-1 text-xs text-muted-foreground">
          New events:
          <OrderToggle value={order} onChange={updateOrder} />
        </div>
      </div>
      {events.length === 0 ? (
        <em className="text-muted-foreground text-xs">waiting for events…</em>
      ) : (
        <LogsTable
          key={order}
          logs={logs}
          columns={TRACE_LOG_COLUMNS}
          defaultSort={{ key: "timestamp", dir: order === "newest" ? "desc" : "asc" }}
          fullscreenTitle={`Trace ${traceID}`}
          className="min-h-0 flex-1"
        />
      )}
    </Card>
  );
}

function OrderToggle({
  value,
  onChange,
}: {
  value: EventOrder;
  onChange: (next: EventOrder) => void;
}) {
  const opt = (key: EventOrder, label: string) => (
    <button
      type="button"
      onClick={() => onChange(key)}
      className={
        "rounded px-density-2 py-density-1 text-xs " +
        (value === key
          ? "bg-background font-semibold text-foreground"
          : "text-muted-foreground hover:text-foreground")
      }
    >
      {label}
    </button>
  );
  return (
    <div className="inline-flex rounded-md border border-border bg-muted/40 p-[2px]">
      {opt("newest", "Top")}
      {opt("oldest", "Bottom")}
    </div>
  );
}
