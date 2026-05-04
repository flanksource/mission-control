import { useMemo, useState } from "preact/hooks";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, RefreshCw, Skull } from "lucide-react";
import {
  Badge,
  Button,
  DataTable,
  type DataTableColumn,
} from "@flanksource/clicky-ui";
import { callOp, configIDFromURL } from "../lib/api";
import { ErrorBox } from "./StatsTab";
import { DatabasePicker } from "./DatabasePicker";

interface Process {
  sessionId: number;
  status: string;
  login: string;
  host: string;
  program: string;
  database: string;
  command: string;
  blockedBy: number;
  waitType?: string;
  waitDuration: number;
  cpuTime: number;
  logicalReads: number;
  sql?: string;
  [key: string]: unknown;
}

const ALL_DATABASES = "";

export function ProcessesTab() {
  const configID = configIDFromURL();
  const qc = useQueryClient();
  const [database, setDatabase] = useState(ALL_DATABASES);
  const [includeSleeping, setIncludeSleeping] = useState(false);

  const list = useQuery({
    queryKey: ["processes", configID, database, includeSleeping],
    queryFn: () => callOp<Process[]>("processes-list", configID, { database, includeSleeping }),
    refetchInterval: 5_000,
  });

  const killMut = useMutation({
    mutationFn: (sessionId: number) => callOp("process-kill", configID, { sessionId }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["processes"] }),
  });

  const columns: DataTableColumn<Process>[] = useMemo(
    () => [
      { key: "sessionId", label: "SPID", align: "right", shrink: true, sortable: true },
      { key: "status", label: "Status", kind: "status" },
      { key: "login", label: "Login", filterable: true },
      { key: "host", label: "Host", filterable: true },
      { key: "program", label: "Program", filterable: true, grow: true },
      { key: "database", label: "Database", filterable: true },
      { key: "command", label: "Command", filterable: true },
      {
        key: "blockedBy",
        label: "Blocked",
        align: "right",
        shrink: true,
        sortable: true,
        render: (value) => {
          const n = Number(value ?? 0);
          if (!n) return "";
          return <span className="font-semibold text-destructive">{n}</span>;
        },
      },
      { key: "waitType", label: "Wait", filterable: true, render: (v) => (v ? String(v) : "") },
      {
        key: "cpuTime",
        label: "CPU(ms)",
        align: "right",
        shrink: true,
        sortable: true,
        sortValue: (v) => Number(v ?? 0),
        render: (v) => Math.round(Number(v ?? 0) / 1_000_000).toLocaleString(),
      },
      {
        key: "logicalReads",
        label: "Reads",
        align: "right",
        shrink: true,
        sortable: true,
        sortValue: (v) => Number(v ?? 0),
        render: (v) => Number(v ?? 0).toLocaleString(),
      },
      {
        key: "_kill",
        label: "",
        sortable: false,
        filterable: false,
        hideable: false,
        shrink: true,
        render: (_v, row) => (
          <button
            onClick={() => {
              if (confirm(`KILL ${row.sessionId} (${row.login}@${row.host})? Not recoverable.`)) {
                killMut.mutate(row.sessionId);
              }
            }}
            disabled={killMut.isPending}
            title="Kill"
            className="inline-flex items-center rounded border border-destructive/40 bg-background p-1 text-destructive hover:bg-destructive/10 disabled:opacity-50"
          >
            {killMut.isPending && killMut.variables === row.sessionId ? (
              <Loader2 size={11} className="spin" />
            ) : (
              <Skull size={11} />
            )}
          </button>
        ),
      },
    ],
    [killMut],
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
            checked={includeSleeping}
            onChange={(e) => setIncludeSleeping(e.currentTarget.checked)}
          />
          include sleeping
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
      {killMut.error && <ErrorBox error={killMut.error as Error} />}
      {list.data && (
        <DataTable
          data={list.data}
          columns={columns}
          autoFilter
          defaultSort={{ key: "cpuTime", dir: "desc" }}
          getRowId={(row) => String(row.sessionId)}
          columnResizeStorageKey="sql-server-processes"
          emptyMessage="no matching sessions"
        />
      )}
    </section>
  );
}
