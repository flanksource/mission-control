import { useMemo, useState } from "preact/hooks";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, DataTable, type DataTableColumn } from "@flanksource/clicky-ui";
import { RefreshCw } from "lucide-react";
import { callOp, configIDFromURL } from "../lib/api";
import { formatBytes, formatNumber, formatPercent } from "../lib/format";
import { ErrorBox } from "./StatsTab";
import { DatabasePicker } from "./DatabasePicker";

interface VacuumRow {
  schema: string;
  table: string;
  sizeBytes: number;
  liveTuples: number;
  deadTuples: number;
  deadTuplePct: number;
  lastVacuum?: string;
  lastAutovacuum?: string;
  lastAnalyze?: string;
  lastAutoanalyze?: string;
  vacuumCount: number;
  autovacuumCount: number;
  [key: string]: unknown;
}

export function VacuumTab() {
  const configID = configIDFromURL();
  const [database, setDatabase] = useState("");
  const list = useQuery({
    queryKey: ["vacuum", configID, database],
    queryFn: () => callOp<VacuumRow[]>("vacuum-stats", configID, { database, limit: 100 }),
    refetchInterval: 30_000,
  });

  const columns: DataTableColumn<VacuumRow>[] = useMemo(
    () => [
      { key: "schema", label: "Schema", filterable: true },
      { key: "table", label: "Table", filterable: true, grow: true },
      {
        key: "sizeBytes",
        label: "Size",
        align: "right",
        sortable: true,
        render: (v) => formatBytes(Number(v ?? 0)),
      },
      {
        key: "deadTuples",
        label: "Dead",
        align: "right",
        sortable: true,
        render: (v) => formatNumber(Number(v ?? 0)),
      },
      {
        key: "deadTuplePct",
        label: "Dead %",
        align: "right",
        sortable: true,
        render: (v) => formatPercent(Number(v ?? 0)),
      },
      { key: "lastAutovacuum", label: "Autovacuum", render: formatDate },
      { key: "lastAutoanalyze", label: "Autoanalyze", render: formatDate },
      {
        key: "autovacuumCount",
        label: "Auto vacuums",
        align: "right",
        sortable: true,
        render: (v) => formatNumber(Number(v ?? 0)),
      },
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
            emptyLabel="Default database"
          />
        </label>
        <Button variant="outline" size="sm" onClick={() => list.refetch()}>
          <RefreshCw size={12} className={list.isFetching ? "spin" : ""} /> Refresh
        </Button>
        {list.data && (
          <Badge variant="outline" className="ml-auto">
            {list.data.length} tables
          </Badge>
        )}
      </header>
      {list.error && <ErrorBox error={list.error as Error} />}
      {list.data && (
        <DataTable
          data={list.data}
          columns={columns}
          autoFilter
          defaultSort={{ key: "deadTuples", dir: "desc" }}
          getRowId={(row) => `${row.schema}.${row.table}`}
          columnResizeStorageKey="postgres-vacuum"
          emptyMessage="no table stats"
        />
      )}
    </section>
  );
}

function formatDate(v: unknown) {
  const s = String(v ?? "");
  if (!s || s.startsWith("0001-")) return "";
  return new Date(s).toLocaleString();
}
