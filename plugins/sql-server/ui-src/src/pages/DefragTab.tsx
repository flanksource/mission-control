import { useState } from "preact/hooks";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, Loader2, Play, RefreshCw, Square } from "lucide-react";
import { Button } from "@flanksource/clicky-ui";
import { callOp, configIDFromURL } from "../lib/api";
import { ErrorBox, Card } from "./StatsTab";

interface InstallResult {
  installed: boolean;
  version?: string;
  procedureName?: string;
  message?: string;
}

interface Status {
  installed: boolean;
  version?: string;
  maintenanceDatabase?: string;
  procedureName?: string;
  hasLogTable?: boolean;
}

interface Job {
  id: string;
  status: string;
  database?: string;
  table?: string;
  startedAt: string;
  finishedAt?: string;
  error?: string;
  summary: { rows: number; indexes: number; statistics: number; rebuilds: number; reorganizes: number; errors: number };
}

interface HistoryRow {
  [key: string]: unknown;
}

interface Session {
  sessionId: number;
  database?: string;
  startTime?: string;
  status?: string;
}

const inputCls =
  "h-control-h w-[160px] rounded-md border border-input bg-background px-2 text-sm";
const labelCls = "inline-flex items-center gap-density-1 text-xs text-muted-foreground";
const thCls = "px-density-2 py-density-1 text-left font-semibold text-foreground";
const tdCls = "px-density-2 py-density-1 font-mono text-xs";

export function DefragTab() {
  const configID = configIDFromURL();
  const qc = useQueryClient();
  const [database, setDatabase] = useState("");
  const [maintenanceDatabase, setMaintenanceDatabase] = useState("msdb");
  const [table, setTable] = useState("");
  const [timeLimit, setTimeLimit] = useState(60);
  const [minFragmentation, setMinFragmentation] = useState(5);

  const status = useQuery({
    queryKey: ["defrag-status", configID, maintenanceDatabase],
    queryFn: () => callOp<Status>("defrag-status", configID, { maintenanceDatabase }),
  });
  const sessions = useQuery({
    queryKey: ["defrag-sessions", configID, maintenanceDatabase],
    queryFn: () => callOp<Session[]>("defrag-sessions", configID, { maintenanceDatabase }),
    refetchInterval: 5_000,
  });
  const history = useQuery({
    queryKey: ["defrag-history", configID, maintenanceDatabase, database],
    queryFn: () =>
      callOp<HistoryRow[]>("defrag-history", configID, { maintenanceDatabase, database, limit: 50 }),
  });
  const jobs = useQuery({
    queryKey: ["defrag-jobs", configID],
    queryFn: () => callOp<Job[]>("defrag-jobs", configID, {}),
    refetchInterval: 3_000,
  });

  const installMut = useMutation({
    mutationFn: () => callOp<InstallResult>("defrag-install", configID, { maintenanceDatabase }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["defrag-status"] }),
  });
  const runMut = useMutation({
    mutationFn: () =>
      callOp<Job>("defrag-run", configID, {
        database,
        maintenanceDatabase,
        table,
        execute: true,
        timeLimitMinutes: timeLimit,
        minFragmentation,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["defrag-jobs"] }),
  });
  const stopMut = useMutation({
    mutationFn: () => callOp("defrag-stop", configID, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["defrag-jobs"] }),
  });
  const terminateMut = useMutation({
    mutationFn: () => callOp("defrag-terminate", configID, { maintenanceDatabase }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["defrag-sessions"] }),
  });

  return (
    <section className="grid gap-density-2">
      <Card title="Status">
        <div className="flex flex-wrap items-center gap-density-2">
          <label className={labelCls}>
            maintenance db
            <input
              value={maintenanceDatabase}
              onChange={(e) => setMaintenanceDatabase(e.currentTarget.value)}
              className={inputCls}
            />
          </label>
          {status.data ? (
            <span className="text-xs">
              <strong>{status.data.installed ? "Installed" : "Not installed"}</strong>
              {status.data.installed && status.data.version && ` · v${status.data.version}`}
              {status.data.procedureName && ` · ${status.data.procedureName}`}
            </span>
          ) : status.isLoading ? (
            "checking…"
          ) : null}
          <Button size="sm" onClick={() => installMut.mutate()} disabled={installMut.isPending}>
            {installMut.isPending ? <Loader2 size={12} className="spin" /> : <Download size={12} />} Install / update
          </Button>
        </div>
        {status.error && <ErrorBox error={status.error as Error} />}
        {installMut.error && <ErrorBox error={installMut.error as Error} />}
      </Card>

      <Card title="Run">
        <div className="flex flex-wrap items-center gap-density-2">
          <label className={labelCls}>
            database
            <input
              value={database}
              onChange={(e) => setDatabase(e.currentTarget.value)}
              placeholder="(default)"
              className={inputCls}
            />
          </label>
          <label className={labelCls}>
            table
            <input
              value={table}
              onChange={(e) => setTable(e.currentTarget.value)}
              placeholder="schema.table"
              className={inputCls}
            />
          </label>
          <label className={labelCls}>
            time limit (min)
            <input
              type="number"
              value={timeLimit}
              onChange={(e) => setTimeLimit(parseInt(e.currentTarget.value) || 60)}
              className={inputCls + " !w-[80px]"}
            />
          </label>
          <label className={labelCls}>
            min frag %
            <input
              type="number"
              step="0.1"
              value={minFragmentation}
              onChange={(e) => setMinFragmentation(parseFloat(e.currentTarget.value) || 0)}
              className={inputCls + " !w-[80px]"}
            />
          </label>
          <Button size="sm" onClick={() => runMut.mutate()} disabled={runMut.isPending}>
            {runMut.isPending ? <Loader2 size={12} className="spin" /> : <Play size={12} />} Run
          </Button>
          <Button variant="outline" size="sm" onClick={() => stopMut.mutate()}>
            <Square size={12} /> Stop all (this plugin)
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="border-destructive/40 text-destructive hover:bg-destructive/10"
            onClick={() => terminateMut.mutate()}
          >
            Terminate server-side sessions
          </Button>
        </div>
        {runMut.error && <ErrorBox error={runMut.error as Error} />}
      </Card>

      <Card title="Plugin jobs">
        <Button variant="outline" size="sm" onClick={() => jobs.refetch()}>
          <RefreshCw size={12} className={jobs.isFetching ? "spin" : ""} /> Refresh
        </Button>
        <table className="mt-density-1 w-full border-collapse text-xs">
          <thead>
            <tr className="bg-muted/30">
              {"ID Status Database Table Started Finished Errors".split(" ").map((h) => (
                <th key={h} className={thCls}>
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {jobs.data?.map((j) => (
              <tr key={j.id} className="border-t border-border">
                <td className={tdCls}>{j.id}</td>
                <td
                  className={
                    tdCls +
                    " " +
                    (j.status === "failed"
                      ? "text-destructive"
                      : j.status === "running"
                        ? "text-emerald-600"
                        : "")
                  }
                >
                  {j.status}
                </td>
                <td className={tdCls}>{j.database}</td>
                <td className={tdCls}>{j.table}</td>
                <td className={tdCls}>{new Date(j.startedAt).toLocaleTimeString()}</td>
                <td className={tdCls}>
                  {j.finishedAt ? new Date(j.finishedAt).toLocaleTimeString() : ""}
                </td>
                <td className={tdCls + (j.summary.errors ? " text-destructive" : "")}>
                  {j.summary.errors}
                </td>
              </tr>
            ))}
            {(!jobs.data || jobs.data.length === 0) && (
              <tr>
                <td colSpan={7} className="px-density-2 py-density-4 text-center text-muted-foreground">
                  no jobs yet
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </Card>

      <Card title="Server-side sessions">
        {sessions.data?.length ? (
          <table className="w-full border-collapse text-xs">
            <thead>
              <tr className="bg-muted/30">
                {"SPID Database Status Started".split(" ").map((h) => (
                  <th key={h} className={thCls}>
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {sessions.data.map((s) => (
                <tr key={s.sessionId} className="border-t border-border">
                  <td className={tdCls}>{s.sessionId}</td>
                  <td className={tdCls}>{s.database}</td>
                  <td className={tdCls}>{s.status}</td>
                  <td className={tdCls}>
                    {s.startTime ? new Date(s.startTime).toLocaleString() : ""}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <em className="text-muted-foreground">none</em>
        )}
      </Card>

      <Card title="Recent history">
        {history.data?.length ? (
          <pre className="m-0 max-h-[300px] overflow-auto rounded-md bg-muted/30 p-density-2 font-mono text-[11px]">
            {JSON.stringify(history.data, null, 2)}
          </pre>
        ) : (
          <em className="text-muted-foreground">no history rows</em>
        )}
      </Card>
    </section>
  );
}
