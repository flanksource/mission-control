import type { ComponentChildren } from "preact";
import { useState } from "preact/hooks";
import { useQuery } from "@tanstack/react-query";
import { Activity, HardDrive, RefreshCw, Server, Users } from "lucide-react";
import { Button, ProgressBar, type ProgressSegment } from "@flanksource/clicky-ui";
import { callOp, configIDFromURL } from "../lib/api";
import { formatBytes, formatNumber, formatPercent, formatDuration } from "../lib/format";
import { DatabasePicker } from "./DatabasePicker";

interface StatsResponse {
  capturedAt: string;
  instance?: {
    serverName?: string;
    databaseName?: string;
    productVersion?: string;
    maxConnections?: number;
    uptimeSeconds?: number;
  };
  connections?: {
    total: number;
    active: number;
    idle: number;
    waiting: number;
    max: number;
  };
  database?: {
    sizeBytes: number;
    transactions: number;
    rollbacks: number;
    deadlocks: number;
    tempBytes: number;
    cacheHitPercent: number;
    conflicts: number;
  };
  warnings?: string[];
}

export function StatsTab() {
  const configID = configIDFromURL();
  const [database, setDatabase] = useState("");
  const { data, isLoading, refetch, isFetching, error } = useQuery({
    queryKey: ["stats", configID, database],
    queryFn: () => callOp<StatsResponse>("stats", configID, { database }),
    refetchInterval: 10_000,
  });

  return (
    <section>
      <header className="mb-density-2 flex flex-wrap items-center gap-density-2">
        <h3 className="m-0">Postgres health</h3>
        <label className="ml-auto flex items-center gap-density-1 text-xs text-muted-foreground">
          Database
          <DatabasePicker
            configID={configID}
            value={database}
            onChange={setDatabase}
            emptyLabel="Default database"
          />
        </label>
        <Button variant="outline" size="sm" onClick={() => refetch()} disabled={isFetching}>
          <RefreshCw size={12} className={isFetching ? "spin" : ""} /> Refresh
        </Button>
      </header>

      {isLoading && <p>Loading...</p>}
      {error && <ErrorBox error={error as Error} />}
      {data && (
        <div className="grid gap-density-2 [grid-template-columns:repeat(auto-fit,minmax(260px,1fr))]">
          <InstanceCard instance={data.instance} />
          <ConnectionsCard connections={data.connections} />
          <DatabaseCard database={data.database} />
        </div>
      )}

      {data?.warnings && data.warnings.length > 0 && (
        <Card title="Warnings" icon={<Activity size={14} />} className="mt-density-2 border-amber-300 bg-amber-50 dark:border-amber-700 dark:bg-amber-950/30">
          <ul className="m-0 pl-4">
            {data.warnings.map((w, i) => (
              <li key={i}>{w}</li>
            ))}
          </ul>
        </Card>
      )}
    </section>
  );
}

function InstanceCard({ instance }: { instance?: StatsResponse["instance"] }) {
  return (
    <Card title="Instance" icon={<Server size={14} />}>
      <dl className="m-0 grid grid-cols-[auto_1fr] gap-x-density-2 gap-y-density-1 text-xs">
        <Stat k="Server" v={instance?.serverName || "local socket"} />
        <Stat k="Database" v={instance?.databaseName} mono />
        <Stat k="Max connections" v={formatNumber(instance?.maxConnections)} />
        <Stat k="Uptime" v={formatDuration(instance?.uptimeSeconds)} />
      </dl>
      <p className="mt-density-2 line-clamp-3 text-xs text-muted-foreground">
        {instance?.productVersion}
      </p>
    </Card>
  );
}

function ConnectionsCard({ connections }: { connections?: StatsResponse["connections"] }) {
  const used = connections?.total ?? 0;
  const max = connections?.max ?? 0;
  const segments: ProgressSegment[] = [
    { count: connections?.active ?? 0, color: "bg-blue-500", label: "Active" },
    { count: connections?.idle ?? 0, color: "bg-emerald-500", label: "Idle" },
    { count: connections?.waiting ?? 0, color: "bg-amber-500", label: "Waiting" },
  ];
  return (
    <Card title="Connections" icon={<Users size={14} />}>
      <div className="mb-density-2 flex items-end justify-between">
        <div className="text-2xl font-semibold tabular-nums">{formatNumber(used)}</div>
        <div className="text-xs text-muted-foreground">of {formatNumber(max)}</div>
      </div>
      <ProgressBar segments={segments} total={max || 1} height="h-2" />
      <dl className="mt-density-2 grid grid-cols-3 gap-density-1 text-xs">
        <Stat k="Active" v={formatNumber(connections?.active)} />
        <Stat k="Idle" v={formatNumber(connections?.idle)} />
        <Stat k="Waiting" v={formatNumber(connections?.waiting)} />
      </dl>
    </Card>
  );
}

function DatabaseCard({ database }: { database?: StatsResponse["database"] }) {
  return (
    <Card title="Database" icon={<HardDrive size={14} />}>
      <dl className="m-0 grid grid-cols-[auto_1fr] gap-x-density-2 gap-y-density-1 text-xs">
        <Stat k="Size" v={formatBytes(database?.sizeBytes)} />
        <Stat k="Cache hit" v={formatPercent(database?.cacheHitPercent)} />
        <Stat k="Transactions" v={formatNumber(database?.transactions)} />
        <Stat k="Rollbacks" v={formatNumber(database?.rollbacks)} />
        <Stat k="Deadlocks" v={formatNumber(database?.deadlocks)} />
        <Stat k="Temp bytes" v={formatBytes(database?.tempBytes)} />
        <Stat k="Conflicts" v={formatNumber(database?.conflicts)} />
      </dl>
    </Card>
  );
}

function Stat({ k, v, mono = false }: { k: string; v?: string | number; mono?: boolean }) {
  return (
    <>
      <dt className="text-muted-foreground">{k}</dt>
      <dd className={(mono ? "font-mono " : "") + "m-0 min-w-0 truncate text-right"}>{v ?? ""}</dd>
    </>
  );
}

export function Card({
  title,
  icon,
  className = "",
  children,
}: {
  title: string;
  icon?: ComponentChildren;
  className?: string;
  children: ComponentChildren;
}) {
  return (
    <section className={"rounded-md border border-border bg-background p-density-3 " + className}>
      <h4 className="mb-density-2 mt-0 flex items-center gap-density-1 text-sm">
        {icon}
        {title}
      </h4>
      {children}
    </section>
  );
}

export function ErrorBox({ error }: { error: Error }) {
  return (
    <pre className="mb-density-2 overflow-auto rounded-md border border-destructive/30 bg-destructive/10 p-density-2 text-xs text-destructive">
      {error.message}
    </pre>
  );
}
