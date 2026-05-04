import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import {
  Badge,
  Button,
  ErrorDetails,
  LogsTable,
  Select,
  type LogsTableInput,
  normalizeErrorDiagnostics,
  type ErrorDiagnostics,
} from "@flanksource/clicky-ui";

type PodRow = {
  namespace: string;
  pod: string;
  phase: string;
  ownedBy?: string;
};

const PLUGIN_BASE = "/api/plugins/kubernetes-logs";
const MAX_LOGS = 5000;

class HttpError extends Error {
  constructor(message: string, readonly diagnostics: ErrorDiagnostics) {
    super(message);
    this.name = "HttpError";
  }
}

async function fetchOrThrow(input: string, init: RequestInit, label: string): Promise<Response> {
  const res = await fetch(input, init);
  if (res.ok) return res;
  const fallback = `${label} failed: HTTP ${res.status}`;
  let payload: unknown;
  try {
    payload = await res.clone().json();
  } catch {
    payload = (await res.text().catch(() => "")) || fallback;
  }
  const diagnostics = normalizeErrorDiagnostics(payload, fallback) ?? {
    message: fallback,
    context: [],
  };
  throw new HttpError(diagnostics.message, diagnostics);
}

async function listPods(configId: string): Promise<PodRow[]> {
  const res = await fetchOrThrow(
    `${PLUGIN_BASE}/operations/list-pods?config_id=${encodeURIComponent(configId)}`,
    {
      method: "POST",
      body: "{}",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
    },
    "list-pods",
  );
  const rows = (await res.json()) as PodRow[];
  return Array.isArray(rows) ? rows : [];
}

export function LogsApp() {
  const params = new URLSearchParams(location.search);
  const configId = params.get("config_id") ?? "";

  const [pods, setPods] = useState<PodRow[]>([]);
  const [selectedPod, setSelectedPod] = useState<string>("");
  const [container, setContainer] = useState<string>("");
  const [tailLines, setTailLines] = useState<number>(200);
  const [follow, setFollow] = useState<boolean>(true);
  const [status, setStatus] = useState<string>("");
  const [error, setError] = useState<ErrorDiagnostics | null>(null);
  const [logs, setLogs] = useState<LogsTableInput[]>([]);
  const streamRef = useRef<EventSource | null>(null);

  useEffect(() => {
    let cancelled = false;
    setStatus("loading pods…");
    setError(null);
    listPods(configId)
      .then((rows) => {
        if (cancelled) return;
        setPods(rows);
        if (rows.length === 0) {
          setStatus("no pods matched");
          return;
        }
        const first = `${rows[0].namespace}|${rows[0].pod}`;
        setSelectedPod(first);
        setStatus(`${rows.length} pod(s)`);
      })
      .catch((err) => {
        if (cancelled) return;
        setStatus("");
        const diagnostics =
          err instanceof HttpError
            ? err.diagnostics
            : normalizeErrorDiagnostics(err instanceof Error ? err.message : String(err)) ?? {
                message: String(err),
                context: [],
              };
        setError(diagnostics);
      })
      .finally(() => {
        window.parent?.postMessage({ type: "mc.tab.ready" }, "*");
      });
    return () => {
      cancelled = true;
    };
  }, [configId]);

  useEffect(() => {
    streamRef.current?.close();
    streamRef.current = null;
    setLogs([]);
    if (!selectedPod) return;
    const [ns, pod] = selectedPod.split("|");
    const url =
      `${PLUGIN_BASE}/ui/logs` +
      `?pod=${encodeURIComponent(pod)}` +
      `&namespace=${encodeURIComponent(ns)}` +
      `&container=${encodeURIComponent(container)}` +
      `&tailLines=${tailLines}` +
      `&follow=${follow ? "true" : "false"}`;

    const es = new EventSource(url, { withCredentials: true });
    streamRef.current = es;
    es.onmessage = (ev) => {
      try {
        const entry = JSON.parse(ev.data) as LogsTableInput;
        setLogs((prev) => {
          const next = prev.length > MAX_LOGS ? prev.slice(prev.length - MAX_LOGS) : prev;
          return [...next, entry];
        });
      } catch {
        setLogs((prev) => [...prev, ev.data]);
      }
    };
    es.onerror = () => {
      setStatus("stream closed");
    };
    return () => {
      es.close();
    };
  }, [selectedPod, container, tailLines, follow]);

  const podOptions = useMemo(
    () =>
      pods.map((p) => ({
        value: `${p.namespace}|${p.pod}`,
        label: `${p.namespace}/${p.pod} (${p.phase})`,
      })),
    [pods],
  );

  const fullscreenTitle = selectedPod ? `Logs · ${selectedPod.replace("|", "/")}` : "Logs";

  return (
    <div class="flex h-screen flex-col gap-density-2 p-density-3">
      <div class="flex flex-wrap items-center gap-density-2">
        <label class="flex items-center gap-density-1 text-xs text-muted-foreground">
          Pod
          <div class="min-w-[260px]">
            <Select
              value={selectedPod}
              options={podOptions}
              onChange={(e: any) => setSelectedPod(e.currentTarget.value)}
              disabled={podOptions.length === 0}
            />
          </div>
        </label>
        <label class="flex items-center gap-density-1 text-xs text-muted-foreground">
          Container
          <input
            class="h-control-h rounded-md border border-input bg-background px-2 text-sm"
            placeholder="(all)"
            value={container}
            onInput={(e: any) => setContainer(e.currentTarget.value)}
          />
        </label>
        <label class="flex items-center gap-density-1 text-xs text-muted-foreground">
          Tail
          <input
            type="number"
            min={1}
            max={5000}
            class="h-control-h w-20 rounded-md border border-input bg-background px-2 text-sm"
            value={tailLines}
            onInput={(e: any) => setTailLines(Number(e.currentTarget.value) || 200)}
          />
        </label>
        <label class="flex items-center gap-density-1 text-xs">
          <input
            type="checkbox"
            checked={follow}
            onChange={(e: any) => setFollow(e.currentTarget.checked)}
          />
          Follow
        </label>
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            setSelectedPod((p) => p);
          }}
        >
          Reload
        </Button>
        {status && <Badge variant="outline">{status}</Badge>}
      </div>
      {error && <ErrorDetails diagnostics={error} />}
      <div class="min-h-0 flex-1">
        <LogsTable
          logs={logs}
          fullscreenTitle={fullscreenTitle}
          className="h-full"
        />
      </div>
    </div>
  );
}
