import { useMemo, useState } from "preact/hooks";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, RefreshCw, Square, XCircle } from "lucide-react";
import { Badge, Button, DataTable, type DataTableColumn } from "@flanksource/clicky-ui";
import { callOp, configIDFromURL } from "../lib/api";
import { ErrorBox } from "./StatsTab";
import { DatabasePicker } from "./DatabasePicker";

interface Session {
  pid: number;
  user: string;
  database: string;
  applicationName?: string;
  clientAddr?: string;
  state: string;
  waitEventType?: string;
  waitEvent?: string;
  durationMs?: number;
  blockedBy?: number[];
  query?: string;
  [key: string]: unknown;
}

export function SessionsTab() {
  const configID = configIDFromURL();
  const qc = useQueryClient();
  const [database, setDatabase] = useState("");
  const [includeIdle, setIncludeIdle] = useState(false);

  const list = useQuery({
    queryKey: ["sessions", configID, database, includeIdle],
    queryFn: () => callOp<Session[]>("sessions-list", configID, { database, includeIdle }),
    refetchInterval: 5_000,
  });

  const cancelMut = useMutation({
    mutationFn: (pid: number) => callOp("session-cancel", configID, { pid }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["sessions"] }),
  });
  const terminateMut = useMutation({
    mutationFn: (pid: number) => callOp("session-terminate", configID, { pid }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["sessions"] }),
  });

  const columns: DataTableColumn<Session>[] = useMemo(
    () => [
      { key: "pid", label: "PID", align: "right", shrink: true, sortable: true },
      { key: "state", label: "State", kind: "status", filterable: true },
      { key: "user", label: "User", filterable: true },
      { key: "database", label: "Database", filterable: true },
      { key: "applicationName", label: "Application", filterable: true, grow: true },
      { key: "clientAddr", label: "Client", filterable: true },
      {
        key: "blockedBy",
        label: "Blocked",
        render: (v) => (Array.isArray(v) && v.length ? v.join(", ") : ""),
      },
      { key: "waitEvent", label: "Wait", filterable: true },
      {
        key: "durationMs",
        label: "Age(ms)",
        align: "right",
        sortable: true,
        render: (v) => Math.round(Number(v ?? 0)).toLocaleString(),
      },
      { key: "query", label: "Query", grow: true, render: (v) => <code>{String(v ?? "")}</code> },
      {
        key: "_actions",
        label: "",
        shrink: true,
        render: (_v, row) => (
          <div className="flex gap-density-1">
            <button
              title="Cancel query"
              className="inline-flex items-center rounded border border-amber-500/40 bg-background p-1 text-amber-600 hover:bg-amber-500/10"
              disabled={cancelMut.isPending || terminateMut.isPending}
              onClick={() => cancelMut.mutate(row.pid)}
            >
              {cancelMut.isPending && cancelMut.variables === row.pid ? (
                <Loader2 size={11} className="spin" />
              ) : (
                <Square size={11} />
              )}
            </button>
            <button
              title="Terminate backend"
              className="inline-flex items-center rounded border border-destructive/40 bg-background p-1 text-destructive hover:bg-destructive/10"
              disabled={cancelMut.isPending || terminateMut.isPending}
              onClick={() => {
                if (confirm(`Terminate backend ${row.pid}?`)) terminateMut.mutate(row.pid);
              }}
            >
              {terminateMut.isPending && terminateMut.variables === row.pid ? (
                <Loader2 size={11} className="spin" />
              ) : (
                <XCircle size={11} />
              )}
            </button>
          </div>
        ),
      },
    ],
    [cancelMut, terminateMut],
  );

  return (
    <section className="flex flex-col gap-density-2">
      <header className="flex flex-wrap items-center gap-density-2">
        <label className="flex items-center gap-density-1 text-xs text-muted-foreground">
          Database
          <DatabasePicker
            configID={configID}
            value={database}
            onChange={setDatabase}
            emptyLabel="All databases"
          />
        </label>
        <label className="flex items-center gap-density-1 text-sm">
          <input
            type="checkbox"
            checked={includeIdle}
            onChange={(e) => setIncludeIdle(e.currentTarget.checked)}
          />
          include idle
        </label>
        <Button variant="outline" size="sm" onClick={() => list.refetch()}>
          <RefreshCw size={12} className={list.isFetching ? "spin" : ""} /> Refresh
        </Button>
        {list.data && (
          <Badge variant="outline" className="ml-auto">
            {list.data.length} sessions
          </Badge>
        )}
      </header>
      {list.error && <ErrorBox error={list.error as Error} />}
      {cancelMut.error && <ErrorBox error={cancelMut.error as Error} />}
      {terminateMut.error && <ErrorBox error={terminateMut.error as Error} />}
      {list.data && (
        <DataTable
          data={list.data}
          columns={columns}
          autoFilter
          defaultSort={{ key: "durationMs", dir: "desc" }}
          getRowId={(row) => String(row.pid)}
          columnResizeStorageKey="postgres-sessions"
          emptyMessage="no matching sessions"
        />
      )}
    </section>
  );
}
