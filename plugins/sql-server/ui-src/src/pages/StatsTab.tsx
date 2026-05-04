import type { ReactNode } from "preact/compat";
import { useQuery } from "@tanstack/react-query";
import {
  Activity,
  Clock,
  Cpu,
  Database,
  HardDrive,
  MemoryStick,
  Network,
  RefreshCw,
  Server,
  Tag,
} from "lucide-react";
import {
  Button,
  ErrorDetails,
  JsonView,
  ProgressBar,
  type ProgressSegment,
  normalizeErrorDiagnostics,
} from "@flanksource/clicky-ui";
import { callOp, configIDFromURL, OpError } from "../lib/api";
import { formatBytes, formatNumber, formatPercent, formatDuration } from "../lib/format";

interface StatsResponse {
  capturedAt: string;
  instance?: {
    serverName?: string;
    databaseName?: string;
    productVersion?: string;
    edition?: string;
    cpuCount?: number;
    schedulerCount?: number;
    uptimeSeconds?: number;
  };
  cpu?: {
    pending: boolean;
    processPercent: number;
    elapsedSeconds?: number;
    schedulerCount?: number;
  };
  memory?: {
    processPhysicalBytes?: number;
    totalPhysicalBytes?: number;
    availablePhysicalBytes?: number;
    memoryUtilizationPercent?: number;
  };
  disk?: {
    totalBytes: number;
    dataBytes: number;
    logBytes: number;
    tempdbBytes: number;
    databaseCount: number;
  };
  io?: {
    readIops: number;
    writeIops: number;
    readBytesPerSecond: number;
    writeBytesPerSecond: number;
    pending: boolean;
  };
  warnings?: string[];
}

type Tone = "neutral" | "success" | "warning" | "danger" | "info";

// percentTone maps a 0-100 utilization figure onto a colour tone so red/
// yellow/green track operator intuition: <60 healthy, <80 heads-up, ≥80 hot.
function percentTone(pct: number | null | undefined): Tone {
  if (pct == null || !Number.isFinite(pct)) return "neutral";
  if (pct >= 80) return "danger";
  if (pct >= 60) return "warning";
  return "success";
}

const toneStroke: Record<Tone, string> = {
  neutral: "stroke-foreground",
  success: "stroke-emerald-500",
  warning: "stroke-amber-500",
  danger: "stroke-red-500",
  info: "stroke-blue-500",
};

const toneText: Record<Tone, string> = {
  neutral: "text-foreground",
  success: "text-emerald-600 dark:text-emerald-400",
  warning: "text-amber-600 dark:text-amber-400",
  danger: "text-red-600 dark:text-red-400",
  info: "text-blue-600 dark:text-blue-400",
};

// RadialGauge — a simple SVG donut. value/max drives the arc length;
// `tone` colours the stroke + numeric label. The middle slot shows the
// integer value with `suffix` (default %), and `caption` is a small line
// underneath for context (e.g. "of host CPU").
function RadialGauge({
  value,
  max = 100,
  tone = "neutral",
  suffix = "%",
  size = 96,
  thickness = 9,
  caption,
}: {
  value: number;
  max?: number;
  tone?: Tone;
  suffix?: string;
  size?: number;
  thickness?: number;
  caption?: ReactNode;
}) {
  const pct = max > 0 ? Math.max(0, Math.min(100, (value / max) * 100)) : 0;
  const r = (size - thickness) / 2;
  const c = 2 * Math.PI * r;
  const offset = c - (pct / 100) * c;
  return (
    <div className="flex flex-col items-center gap-density-1">
      <div className="relative" style={{ width: size, height: size }}>
        <svg width={size} height={size} className="-rotate-90">
          <circle
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            strokeWidth={thickness}
            className="stroke-muted"
          />
          <circle
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            strokeWidth={thickness}
            strokeLinecap="round"
            strokeDasharray={c}
            strokeDashoffset={offset}
            className={toneStroke[tone] + " transition-[stroke-dashoffset] duration-500"}
          />
        </svg>
        <div
          className={
            "absolute inset-0 flex flex-col items-center justify-center font-semibold tabular-nums " +
            toneText[tone]
          }
        >
          <span className="text-xl leading-none">{Math.round(value)}</span>
          {suffix && <span className="text-[10px] opacity-70">{suffix}</span>}
        </div>
      </div>
      {caption && (
        <div className="text-center text-[11px] text-muted-foreground">{caption}</div>
      )}
    </div>
  );
}

export function StatsTab() {
  const configID = configIDFromURL();
  const { data, isLoading, refetch, isFetching, error } = useQuery({
    queryKey: ["stats", configID],
    queryFn: () => callOp<StatsResponse>("stats", configID, {}),
    refetchInterval: 10_000,
  });

  return (
    <section>
      <header className="mb-density-2 flex items-center justify-between">
        <h3 className="m-0">Instance health</h3>
        <Button variant="outline" size="sm" onClick={() => refetch()} disabled={isFetching}>
          <RefreshCw size={12} className={isFetching ? "spin" : ""} /> Refresh
        </Button>
      </header>

      {isLoading && <p>Loading…</p>}
      {error && <ErrorBox error={error as Error} />}
      {data && (
        <div className="grid gap-density-2 [grid-template-columns:repeat(auto-fit,minmax(260px,1fr))]">
          <InstanceCard instance={data.instance} />
          <CpuCard cpu={data.cpu} />
          <MemoryCard memory={data.memory} />
          <DiskCard disk={data.disk} />
          <IoCard io={data.io} />
        </div>
      )}

      {data?.warnings && data.warnings.length > 0 && (
        <Card
          title="Warnings"
          icon={<Activity size={14} />}
          className="mt-density-2 border-amber-300 bg-amber-50 dark:border-amber-700 dark:bg-amber-950/30"
        >
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

function InstanceCard({ instance }: { instance: StatsResponse["instance"] }) {
  if (!instance) {
    return (
      <Card title="Instance" icon={<Server size={14} />}>
        <em>unavailable</em>
      </Card>
    );
  }
  return (
    <Card title="Instance" icon={<Server size={14} />}>
      <dl className="m-0 grid grid-cols-[auto_1fr] gap-x-density-2 gap-y-density-1 text-xs">
        <Stat icon={<Server size={12} />} k="Server" v={instance.serverName} mono />
        <Stat icon={<Database size={12} />} k="Database" v={instance.databaseName} mono />
        <Stat icon={<Tag size={12} />} k="Version" v={instance.productVersion} />
        <Stat icon={<Tag size={12} />} k="Edition" v={instance.edition} />
        <Stat icon={<Cpu size={12} />} k="CPUs" v={formatNumber(instance.cpuCount)} />
        <Stat
          icon={<Activity size={12} />}
          k="Schedulers"
          v={formatNumber(instance.schedulerCount)}
        />
        <Stat
          icon={<Clock size={12} />}
          k="Uptime"
          v={formatDuration(instance.uptimeSeconds)}
        />
      </dl>
    </Card>
  );
}

function CpuCard({ cpu }: { cpu: StatsResponse["cpu"] }) {
  if (!cpu) {
    return (
      <Card title="CPU" icon={<Cpu size={14} />}>
        <em>unavailable</em>
      </Card>
    );
  }
  const pct = cpu.processPercent ?? 0;
  const segments: ProgressSegment[] = [
    { count: Math.round(pct), color: "bg-blue-500", label: "SQL Server" },
    {
      count: Math.max(0, 100 - Math.round(pct)),
      color: "bg-emerald-500/50",
      label: "Available",
    },
  ];
  return (
    <Card title="CPU" icon={<Cpu size={14} />}>
      <div className="flex items-center gap-density-2">
        <RadialGauge
          value={cpu.pending ? 0 : pct}
          tone={cpu.pending ? "neutral" : percentTone(pct)}
          caption={cpu.pending ? "sampling…" : "SQL Server"}
        />
        <dl className="m-0 grid flex-1 grid-cols-[auto_1fr] gap-x-density-2 gap-y-density-1 text-xs">
          <Legend color="bg-blue-500" k="SQL Server" v={formatPercent(pct)} />
          <Legend
            color="bg-emerald-500/50"
            k="Available"
            v={formatPercent(Math.max(0, 100 - pct))}
          />
          {cpu.elapsedSeconds && cpu.elapsedSeconds > 0 ? (
            <Legend
              color="bg-transparent"
              k="Sample"
              v={`${cpu.elapsedSeconds.toFixed(1)} s`}
            />
          ) : null}
        </dl>
      </div>
      <div className="mt-density-2">
        <ProgressBar segments={segments} total={100} height="h-2" />
      </div>
      {cpu.pending && (
        <p className="mt-density-1 text-[11px] text-muted-foreground">
          <em>(sampling — refresh in a moment for delta)</em>
        </p>
      )}
    </Card>
  );
}

function MemoryCard({ memory }: { memory: StatsResponse["memory"] }) {
  if (!memory) {
    return (
      <Card title="Memory" icon={<MemoryStick size={14} />}>
        <em>unavailable</em>
      </Card>
    );
  }
  const total = memory.totalPhysicalBytes ?? 0;
  const sqlUsed = memory.processPhysicalBytes ?? 0;
  const available = memory.availablePhysicalBytes ?? 0;
  const otherUsed = Math.max(0, total - sqlUsed - available);
  const usedPct = total > 0 ? ((total - available) / total) * 100 : 0;
  const segments: ProgressSegment[] = [
    {
      count: total > 0 ? Math.round((sqlUsed / total) * 100) : 0,
      color: "bg-blue-500",
      label: "SQL Server",
    },
    {
      count: total > 0 ? Math.round((otherUsed / total) * 100) : 0,
      color: "bg-purple-500",
      label: "Other",
    },
    {
      count: total > 0 ? Math.round((available / total) * 100) : 0,
      color: "bg-emerald-500/50",
      label: "Free",
    },
  ];
  return (
    <Card title="Memory" icon={<MemoryStick size={14} />}>
      <div className="flex items-center gap-density-2">
        <RadialGauge value={usedPct} tone={percentTone(usedPct)} caption="host used" />
        <dl className="m-0 grid flex-1 grid-cols-[auto_1fr] gap-x-density-2 gap-y-density-1 text-xs">
          <Legend color="bg-blue-500" k="SQL Server" v={formatBytes(sqlUsed)} />
          <Legend color="bg-purple-500" k="Other" v={formatBytes(otherUsed)} />
          <Legend color="bg-emerald-500/50" k="Free" v={formatBytes(available)} />
          <Legend
            color="bg-transparent"
            k="SQL util"
            v={formatPercent(memory.memoryUtilizationPercent)}
          />
        </dl>
      </div>
      <div className="mt-density-2">
        <ProgressBar segments={segments} total={100} height="h-2" />
      </div>
    </Card>
  );
}

function DiskCard({ disk }: { disk: StatsResponse["disk"] }) {
  if (!disk) {
    return (
      <Card title="Disk" icon={<HardDrive size={14} />}>
        <em>unavailable</em>
      </Card>
    );
  }
  const total = disk.totalBytes || 0;
  const data = disk.dataBytes || 0;
  const log = disk.logBytes || 0;
  const tempdb = disk.tempdbBytes || 0;
  const other = Math.max(0, total - data - log - tempdb);
  // The "primary" gauge is data-file share of total allocation — that's the
  // metric operators watch for capacity. Falls back to 0 when total is zero.
  const dataPct = total > 0 ? (data / total) * 100 : 0;
  const segments: ProgressSegment[] = [
    {
      count: total > 0 ? Math.round((data / total) * 100) : 0,
      color: "bg-blue-500",
      label: "Data",
    },
    {
      count: total > 0 ? Math.round((log / total) * 100) : 0,
      color: "bg-amber-500",
      label: "Log",
    },
    {
      count: total > 0 ? Math.round((tempdb / total) * 100) : 0,
      color: "bg-purple-500",
      label: "tempdb",
    },
    {
      count: total > 0 ? Math.round((other / total) * 100) : 0,
      color: "bg-muted-foreground/40",
      label: "Other",
    },
  ];
  return (
    <Card title="Disk" icon={<HardDrive size={14} />}>
      <div className="flex items-center gap-density-2">
        <RadialGauge value={dataPct} tone={percentTone(dataPct)} caption={formatBytes(total)} />
        <dl className="m-0 grid flex-1 grid-cols-[auto_1fr] gap-x-density-2 gap-y-density-1 text-xs">
          <Legend color="bg-blue-500" k="Data" v={formatBytes(data)} />
          <Legend color="bg-amber-500" k="Log" v={formatBytes(log)} />
          <Legend color="bg-purple-500" k="tempdb" v={formatBytes(tempdb)} />
          <Legend
            color="bg-transparent"
            k="Databases"
            v={formatNumber(disk.databaseCount)}
          />
        </dl>
      </div>
      <div className="mt-density-2">
        <ProgressBar segments={segments} total={100} height="h-2" />
      </div>
    </Card>
  );
}

function IoCard({ io }: { io: StatsResponse["io"] }) {
  if (!io) {
    return (
      <Card title="I/O" icon={<Network size={14} />}>
        <em>unavailable</em>
      </Card>
    );
  }
  return (
    <Card title="I/O" icon={<Network size={14} />}>
      <dl className="m-0 grid grid-cols-[auto_1fr] gap-x-density-2 gap-y-density-1 text-xs">
        <Stat
          icon={<Activity size={12} />}
          k="Read IOPS"
          v={formatNumber(Math.round(io.readIops))}
        />
        <Stat
          icon={<Activity size={12} />}
          k="Write IOPS"
          v={formatNumber(Math.round(io.writeIops))}
        />
        <Stat
          icon={<Network size={12} />}
          k="Read"
          v={`${formatBytes(io.readBytesPerSecond)}/s`}
        />
        <Stat
          icon={<Network size={12} />}
          k="Write"
          v={`${formatBytes(io.writeBytesPerSecond)}/s`}
        />
      </dl>
      {io.pending && (
        <p className="mt-density-1 text-[11px] text-muted-foreground">
          <em>(sampling — refresh in a moment for delta)</em>
        </p>
      )}
    </Card>
  );
}

function Stat({
  icon,
  k,
  v,
  mono = false,
}: {
  icon: ReactNode;
  k: string;
  v: ReactNode;
  mono?: boolean;
}) {
  return (
    <>
      <dt className="inline-flex items-center gap-density-1 text-muted-foreground">
        {icon}
        {k}
      </dt>
      <dd className={"m-0 truncate " + (mono ? "font-mono" : "")}>{v ?? "—"}</dd>
    </>
  );
}

function Legend({
  color,
  k,
  v,
}: {
  color: string;
  k: string;
  v: ReactNode;
}) {
  return (
    <>
      <dt className="inline-flex items-center gap-density-1 text-muted-foreground">
        <span className={"inline-block h-2 w-2 rounded-full " + color} />
        {k}
      </dt>
      <dd className="m-0 truncate text-right tabular-nums">{v ?? "—"}</dd>
    </>
  );
}

export function Card({
  title,
  icon,
  children,
  className = "",
}: {
  title: string;
  icon?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={"rounded-lg border border-border bg-card p-density-2 " + className}>
      <h4 className="m-0 mb-density-2 inline-flex items-center gap-density-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
        {icon}
        {title}
      </h4>
      {children}
    </div>
  );
}

export function ErrorBox({ error }: { error: Error }) {
  // Lift any structured oops payload out of OpError.body — falls back to the
  // raw Error message when the operation returned plain text.
  const source = error instanceof OpError ? error.body ?? error : error;
  const diagnostics =
    normalizeErrorDiagnostics(source, error.message) ??
    normalizeErrorDiagnostics(error.message);
  if (!diagnostics) {
    return (
      <div className="rounded-lg border border-destructive/40 bg-destructive/10 p-density-2 text-destructive">
        <strong>Error:</strong> {error.message}
      </div>
    );
  }
  return (
    <ErrorDetails
      diagnostics={diagnostics}
      renderJsonContext={({ data }) => <JsonView data={data} />}
    />
  );
}
