export interface RunningPod {
  namespace: string;
  name: string;
  node?: string;
  containers: string[];
  containerPorts?: Record<string, number[]>;
  ownerKind?: string;
  ownerName?: string;
}

export interface TargetOption {
  namespace: string;
  pod: string;
  container: string;
  owner: string;
  ports: number[];
}

export interface GolangSession {
  id: string;
  namespace: string;
  kind: string;
  name: string;
  pod: string;
  container: string;
  pid?: number;
  gopsRemotePort?: number;
  pprofRemotePort?: number;
  pprofBasePath?: string;
  gopsAvailable: boolean;
  pprofAvailable: boolean;
  startedAt: string;
  diagnostics?: string[];
}

export interface RuntimeSnapshot {
  sessionId: string;
  version?: string;
  stats?: string;
  memstats?: string;
  error?: string;
}

export interface GoroutineSnapshot {
  sessionId: string;
  source: string;
  dump: string;
}

export type ProfileKind = "cpu" | "trace" | "heap";
export type ProfileSource = "auto" | "pprof" | "gops";
export type ProfileState = "running" | "completed" | "failed" | "stopped";

export interface ProfileRun {
  id: string;
  sessionId: string;
  kind: ProfileKind;
  source?: string;
  preference?: ProfileSource;
  state: ProfileState;
  seconds?: number;
  startedAt: string;
  completedAt?: string;
  elapsedMs: number;
  bytes?: number;
  error?: string;
  url?: string;
}

export function configIDFromURL(): string {
  return new URLSearchParams(window.location.search).get("config_id") ?? "";
}

function operationURL(op: string): string {
  const base = window.location.pathname.replace(/\/ui\/.*$/, "");
  const url = new URL(base + "/operations/" + op, window.location.origin);
  const configID = configIDFromURL();
  if (configID) url.searchParams.set("config_id", configID);
  return url.toString();
}

export function pluginURL(path: string): string {
  const base = window.location.pathname.replace(/\/ui\/.*$/, "");
  return new URL(base + "/" + path.replace(/^\//, ""), window.location.origin).toString();
}

export async function callOp<T>(op: string, params: Record<string, unknown> = {}): Promise<T> {
  const res = await fetch(operationURL(op), {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(params),
  });
  if (!res.ok) throw new Error(await res.text() || res.statusText);
  return (await res.json()) as T;
}
