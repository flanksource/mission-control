import { useMemo, useState } from "preact/hooks";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, DataTable, type DataTableColumn } from "@flanksource/clicky-ui";
import { RefreshCw } from "lucide-react";
import { callOp, configIDFromURL } from "../lib/api";
import { ErrorBox } from "./StatsTab";
import { DatabasePicker } from "./DatabasePicker";

interface LockRow {
  pid: number;
  database?: string;
  user?: string;
  state?: string;
  lockType: string;
  mode: string;
  granted: boolean;
  relation?: string;
  blockedBy?: number[];
  waitEvent?: string;
  query?: string;
  ageMs?: number;
  [key: string]: unknown;
}

export function LocksTab() {
  const configID = configIDFromURL();
  const [database, setDatabase] = useState("");
  const [onlyBlocked, setOnlyBlocked] = useState(true);
  const list = useQuery({
    queryKey: ["locks", configID, database, onlyBlocked],
    queryFn: () => callOp<LockRow[]>("locks-list", configID, { database, onlyBlocked }),
    refetchInterval: 5_000,
  });

  const columns: DataTableColumn<LockRow>[] = useMemo(
    () => [
      { key: "pid", label: "PID", align: "right", shrink: true, sortable: true },
      { key: "granted", label: "Granted", shrink: true, render: (v) => (v ? "yes" : "no") },
      { key: "database", label: "Database", filterable: true },
      { key: "user", label: "User", filterable: true },
      { key: "lockType", label: "Type", filterable: true },
      { key: "mode", label: "Mode", filterable: true },
      { key: "relation", label: "Relation", filterable: true, grow: true },
      {
        key: "blockedBy",
        label: "Blocked by",
        render: (v) => (Array.isArray(v) && v.length ? v.join(", ") : ""),
      },
      { key: "waitEvent", label: "Wait", filterable: true },
      {
        key: "ageMs",
        label: "Age(ms)",
        align: "right",
        sortable: true,
        render: (v) => Math.round(Number(v ?? 0)).toLocaleString(),
      },
      { key: "query", label: "Query", grow: true, render: (v) => <code>{String(v ?? "")}</code> },
    ],
    [],
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
            checked={onlyBlocked}
            onChange={(e) => setOnlyBlocked(e.currentTarget.checked)}
          />
          blocked only
        </label>
        <Button variant="outline" size="sm" onClick={() => list.refetch()}>
          <RefreshCw size={12} className={list.isFetching ? "spin" : ""} /> Refresh
        </Button>
        {list.data && (
          <Badge variant="outline" className="ml-auto">
            {list.data.length} locks
          </Badge>
        )}
      </header>
      {list.error && <ErrorBox error={list.error as Error} />}
      {list.data && (
        <DataTable
          data={list.data}
          columns={columns}
          autoFilter
          defaultSort={{ key: "ageMs", dir: "desc" }}
          getRowId={(row, i) => `${row.pid}-${row.lockType}-${row.mode}-${i}`}
          columnResizeStorageKey="postgres-locks"
          emptyMessage="no matching locks"
        />
      )}
    </section>
  );
}
