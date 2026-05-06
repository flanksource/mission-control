import { useMemo, useState } from "preact/hooks";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Badge, Button, DataTable, type DataTableColumn } from "@flanksource/clicky-ui";
import { Loader2, PackagePlus, RefreshCw } from "lucide-react";
import { callOp, configIDFromURL } from "../lib/api";
import { formatNumber } from "../lib/format";
import { Card, ErrorBox } from "./StatsTab";
import { DatabasePicker } from "./DatabasePicker";

interface SlowQueryResponse {
  available: boolean;
  warning?: string;
  queries?: SlowQuery[];
}

interface InstallResponse {
  installed: boolean;
  extensionVersion?: string;
  sharedPreloadConfigured: boolean;
  sharedPreloadLibraries?: string;
  warning?: string;
}

interface SlowQuery {
  user?: string;
  database?: string;
  query: string;
  calls: number;
  totalExecTimeMs: number;
  meanExecTimeMs: number;
  rows: number;
  sharedBlocksHit: number;
  sharedBlocksRead: number;
  tempBlocksWritten: number;
  [key: string]: unknown;
}

export function SlowQueriesTab() {
  const configID = configIDFromURL();
  const qc = useQueryClient();
  const [database, setDatabase] = useState("");
  const list = useQuery({
    queryKey: ["slow-queries", configID, database],
    queryFn: () => callOp<SlowQueryResponse>("slow-queries", configID, { database, limit: 100 }),
    refetchInterval: 30_000,
  });
  const install = useMutation({
    mutationFn: () => callOp<InstallResponse>("slow-queries-install", configID, { database }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["slow-queries", configID, database] }),
  });

  const columns: DataTableColumn<SlowQuery>[] = useMemo(
    () => [
      { key: "user", label: "User", filterable: true },
      {
        key: "totalExecTimeMs",
        label: "Total(ms)",
        align: "right",
        sortable: true,
        render: (v) => Math.round(Number(v ?? 0)).toLocaleString(),
      },
      {
        key: "meanExecTimeMs",
        label: "Mean(ms)",
        align: "right",
        sortable: true,
        render: (v) => Math.round(Number(v ?? 0)).toLocaleString(),
      },
      {
        key: "calls",
        label: "Calls",
        align: "right",
        sortable: true,
        render: (v) => formatNumber(Number(v ?? 0)),
      },
      {
        key: "rows",
        label: "Rows",
        align: "right",
        sortable: true,
        render: (v) => formatNumber(Number(v ?? 0)),
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
            emptyLabel="Default database"
          />
        </label>
        <Button variant="outline" size="sm" onClick={() => list.refetch()}>
          <RefreshCw size={12} className={list.isFetching ? "spin" : ""} /> Refresh
        </Button>
        {list.data?.queries && (
          <Badge variant="outline" className="ml-auto">
            {list.data.queries.length} queries
          </Badge>
        )}
      </header>
      {list.error && <ErrorBox error={list.error as Error} />}
      {install.error && <ErrorBox error={install.error as Error} />}
      {install.data && (
        <Card title="Installer">
          <dl className="m-0 grid grid-cols-[auto_1fr] gap-x-density-2 gap-y-density-1 text-xs">
            <dt className="text-muted-foreground">Installed</dt>
            <dd className="m-0 text-right">{install.data.installed ? "yes" : "no"}</dd>
            <dt className="text-muted-foreground">Version</dt>
            <dd className="m-0 text-right">{install.data.extensionVersion ?? ""}</dd>
            <dt className="text-muted-foreground">Preloaded</dt>
            <dd className="m-0 text-right">{install.data.sharedPreloadConfigured ? "yes" : "no"}</dd>
          </dl>
          {install.data.warning && <p className="mb-0 mt-density-2 text-sm">{install.data.warning}</p>}
        </Card>
      )}
      {list.data?.warning && (
        <Card title="Unavailable" className="border-amber-300 bg-amber-50 dark:border-amber-700 dark:bg-amber-950/30">
          <p className="m-0 text-sm">{list.data.warning}</p>
          <Button
            size="sm"
            className="mt-density-2"
            disabled={install.isPending}
            onClick={() => install.mutate()}
          >
            {install.isPending ? <Loader2 size={12} className="spin" /> : <PackagePlus size={12} />}
            Install pg_stat_statements
          </Button>
        </Card>
      )}
      {list.data?.queries && (
        <DataTable
          data={list.data.queries}
          columns={columns}
          autoFilter
          defaultSort={{ key: "totalExecTimeMs", dir: "desc" }}
          getRowId={(row, i) => `${row.calls}-${i}`}
          columnResizeStorageKey="postgres-slow-queries"
          emptyMessage="no slow query stats"
        />
      )}
    </section>
  );
}
