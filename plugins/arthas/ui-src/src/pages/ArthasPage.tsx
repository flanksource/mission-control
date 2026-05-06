import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bug, Check, ChevronDown, Copy, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { toastManager } from "@/components/ui/toast";
import { callOp, configIDFromURL, pluginURL } from "@/lib/api";
import { ArthasDashboardTab } from "./ArthasDashboardTab";
import { ArthasMBeanTab } from "./ArthasMBeanTab";
import { ArthasOgnlTab } from "./ArthasOgnlTab";
import { ArthasProfilerTab } from "./ArthasProfilerTab";

interface ArthasSession {
  id: string;
  namespace: string;
  kind: string;
  name: string;
  pod: string;
  container: string;
  httpLocalPort: number;
  mcpLocalPort: number;
  startedAt: string;
  javaVersion?: number;
  jdkProvisioned?: boolean;
  sideloadedJavaHome?: string;
  mcpEnabled?: boolean;
}

interface RunningPod {
  namespace: string;
  name: string;
  containers: string[];
  ownerKind?: string;
  ownerName?: string;
}

const SESSIONS_KEY = ["arthas", "sessions"] as const;

export function ArthasPage() {
  const configID = configIDFromURL();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const qc = useQueryClient();

  const sessionsQ = useQuery({
    queryKey: SESSIONS_KEY,
    queryFn: () => callOp<ArthasSession[]>("sessions-list"),
    refetchInterval: 5_000,
  });

  const podsQ = useQuery({
    queryKey: ["arthas", "pods", configID],
    queryFn: () => callOp<RunningPod[]>("pods-list"),
    enabled: !!configID,
    staleTime: 15_000,
  });

  const create = useMutation({
    mutationFn: (body: Record<string, unknown>) => callOp<ArthasSession>("session-create", body),
    onSuccess: (sess) => {
      qc.invalidateQueries({ queryKey: SESSIONS_KEY });
      setSelectedId(sess.id);
      toastManager.add({ title: `Arthas session started on ${sess.pod}`, type: "success" });
    },
  });

  const del = useMutation({
    mutationFn: (id: string) => callOp("session-delete", { id }),
    onSuccess: (_, id) => {
      qc.invalidateQueries({ queryKey: SESSIONS_KEY });
      if (selectedId === id) setSelectedId(null);
    },
  });

  const sessions = sessionsQ.data ?? [];
  const selected = useMemo(
    () => sessions.find((s) => s.id === selectedId) ?? sessions[0] ?? null,
    [sessions, selectedId],
  );

  if (!configID) {
    return (
      <div className="flex h-screen flex-col items-center justify-center gap-2 p-8 text-center text-sm text-muted-foreground">
        <Bug className="h-8 w-8" />
        <p>No config_id in the iframe URL.</p>
      </div>
    );
  }

  return (
    <div className="flex h-screen bg-background p-3 text-foreground">
      <main className="min-w-0 flex-1">
        {selected ? (
          <SessionDetail
            session={selected}
            sessions={sessions}
            selectedId={selected.id}
            sessionsLoading={sessionsQ.isLoading}
            pods={podsQ.data ?? []}
            podsLoading={podsQ.isLoading}
            podsError={podsQ.error}
            creating={create.isPending}
            deletingId={del.isPending ? String(del.variables ?? "") || null : null}
            onSelectSession={setSelectedId}
            onCreateSession={(body) => create.mutate(body)}
            onDeleteSession={(id) => del.mutate(id)}
          />
        ) : (
          <EmptyState
            sessions={sessions}
            selectedId={selectedId}
            sessionsLoading={sessionsQ.isLoading}
            pods={podsQ.data ?? []}
            podsLoading={podsQ.isLoading}
            podsError={podsQ.error}
            creating={create.isPending}
            deletingId={del.isPending ? String(del.variables ?? "") || null : null}
            onSelectSession={setSelectedId}
            onCreateSession={(body) => create.mutate(body)}
            onDeleteSession={(id) => del.mutate(id)}
          />
        )}
      </main>
    </div>
  );
}

type SessionMenuProps = {
  sessions: ArthasSession[];
  selectedId: string | null;
  sessionsLoading: boolean;
  pods: RunningPod[];
  podsLoading: boolean;
  podsError: unknown;
  creating: boolean;
  deletingId: string | null;
  onSelectSession: (id: string) => void;
  onCreateSession: (body: Record<string, unknown>) => void;
  onDeleteSession: (id: string) => void;
};

function SessionMenu({
  sessions,
  selectedId,
  sessionsLoading,
  pods,
  podsLoading,
  podsError,
  creating,
  deletingId,
  onSelectSession,
  onCreateSession,
  onDeleteSession,
}: SessionMenuProps) {
  const [open, setOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const selected = sessions.find((s) => s.id === selectedId) ?? null;
  const targets = useMemo(() => sessionTargets(pods, sessions), [pods, sessions]);

  useEffect(() => {
    if (!open) return;
    const close = (event: MouseEvent) => {
      if (!menuRef.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [open]);

  return (
    <div ref={menuRef} className="relative">
      <Button
        variant="outline"
        className="max-w-[22rem] justify-between"
        onClick={() => setOpen((value) => !value)}
      >
        <span className="min-w-0 truncate">
          {selected ? `${selected.kind}/${selected.name}` : "Sessions"}
        </span>
        <ChevronDown className="h-3 w-3 shrink-0" />
      </Button>

      {open && (
        <div className="absolute right-0 top-full z-30 mt-2 w-[34rem] max-w-[calc(100vw-2rem)] rounded-md border border-border bg-background p-1 shadow-lg">
          <div className="px-2 py-1 text-[11px] font-semibold uppercase text-muted-foreground">Targets</div>
          {sessionsLoading || podsLoading ? (
            <div className="flex items-center gap-2 px-2 py-3 text-xs text-muted-foreground">
              <Spinner className="h-4 w-4" />
              Loading targets
            </div>
          ) : podsError ? (
            <div className="px-2 py-3 text-xs text-red-600">
              {podsError instanceof Error ? podsError.message : "Failed to load pods"}
            </div>
          ) : targets.length === 0 ? (
            <div className="px-2 py-3 text-xs text-muted-foreground">No ready pods resolved for this resource.</div>
          ) : (
            <div className="max-h-96 overflow-auto">
              {targets.map((target) => (
                <div
                  key={`${target.pod.name}/${target.container}`}
                  className={`grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 rounded px-2 py-2 hover:bg-muted ${
                    selectedId === target.session?.id ? "bg-muted" : ""
                  }`}
                >
                  <button
                    type="button"
                    className="min-w-0 text-left text-xs"
                    disabled={!target.session}
                    onClick={() => {
                      if (!target.session) return;
                      onSelectSession(target.session.id);
                      setOpen(false);
                    }}
                  >
                    <span className="block truncate font-medium">{target.pod.name}</span>
                    <span className="block truncate text-muted-foreground">
                      {target.pod.ownerKind ? `${target.pod.ownerKind}/${target.pod.ownerName}` : "pod"}
                      {target.container ? ` / ${target.container}` : ""}
                    </span>
                    {target.session && (
                      <span className="block truncate text-muted-foreground">
                        running since {formatSessionTime(target.session.startedAt)}
                      </span>
                    )}
                  </button>
                  {target.session ? (
                    <Button
                      size="xs"
                      variant="ghost"
                      loading={deletingId === target.session.id}
                      onClick={() => onDeleteSession(target.session!.id)}
                    >
                      <Trash2 className="h-3 w-3" /> Stop
                    </Button>
                  ) : (
                    <Button
                      size="xs"
                      variant="secondary"
                      loading={creating}
                      onClick={() => {
                        onCreateSession({
                          namespace: target.pod.namespace,
                          kind: target.pod.ownerKind || "pod",
                          name: target.pod.ownerName || target.pod.name,
                          pod: target.pod.name,
                          container: target.container,
                        });
                        setOpen(false);
                      }}
                    >
                      <Plus className="h-3 w-3" /> Start
                    </Button>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

type PodSessionTarget = {
  pod: RunningPod;
  container: string;
  session?: ArthasSession;
};

function sessionTargets(pods: RunningPod[], sessions: ArthasSession[]): PodSessionTarget[] {
  const sessionsByPod = new Map<string, ArthasSession>();
  for (const session of sessions) {
    if (!sessionsByPod.has(session.pod)) sessionsByPod.set(session.pod, session);
  }

  return pods.flatMap((pod) => {
    const session = sessionsByPod.get(pod.name);
    if (session) {
      return [{ pod, container: session.container, session }];
    }

    const containers = pod.containers.length > 0 ? pod.containers : [""];
    return containers.map((container) => ({
      pod,
      container,
    }));
  });
}

function formatSessionTime(startedAt: string): string {
  const date = new Date(startedAt);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function EmptyState(props: SessionMenuProps) {
  return (
    <div className="flex h-full flex-col rounded-md border border-border">
      <div className="m-2 flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 px-2 text-sm font-semibold">
          <Bug className="h-4 w-4" />
          Arthas
        </div>
        <SessionMenu {...props} />
      </div>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-2 p-8 text-center text-sm text-muted-foreground">
        <Bug className="h-8 w-8" />
        <p>Start an Arthas session to attach to the JVM running inside this Kubernetes resource.</p>
      </div>
    </div>
  );
}

function SessionDetail({
  session,
  ...menuProps
}: {
  session: ArthasSession;
} & SessionMenuProps) {
  const [tab, setTab] = useState("console");
  const consoleURL = pluginURL(`proxy/${session.id}/`);

  return (
    <Tabs value={tab} onValueChange={setTab} className="flex h-full flex-col rounded-md border border-border">
      <div className="m-2 flex items-center justify-between gap-2">
        <TabsList className="w-fit">
          <TabsTrigger value="console">Web Console</TabsTrigger>
          <TabsTrigger value="ognl">OGNL</TabsTrigger>
          <TabsTrigger value="dashboard">Dashboard</TabsTrigger>
          <TabsTrigger value="mbeans">MBeans</TabsTrigger>
          <TabsTrigger value="profiler">Profiler</TabsTrigger>
          {session.mcpEnabled ? <TabsTrigger value="mcp">MCP</TabsTrigger> : <TabsTrigger value="api">HTTP API</TabsTrigger>}
          <TabsTrigger value="info">Info</TabsTrigger>
        </TabsList>
        <SessionMenu {...menuProps} selectedId={session.id} />
      </div>
      <TabsContent value="console" className="min-h-0 flex-1 p-0">
        <ConsoleFrame src={consoleURL} pod={session.pod} />
      </TabsContent>
      <TabsContent value="dashboard" className="min-h-0 flex-1 overflow-hidden p-0">
        <ArthasDashboardTab sessionId={session.id} />
      </TabsContent>
      <TabsContent value="ognl" className="min-h-0 flex-1 overflow-hidden p-0">
        <ArthasOgnlTab sessionId={session.id} />
      </TabsContent>
      <TabsContent value="mbeans" className="min-h-0 flex-1 overflow-hidden p-0">
        <ArthasMBeanTab sessionId={session.id} />
      </TabsContent>
      <TabsContent value="profiler" className="min-h-0 flex-1 overflow-hidden p-0">
        <ArthasProfilerTab sessionId={session.id} />
      </TabsContent>
      <TabsContent value="api" className="flex-1 overflow-auto p-4">
        <HttpApiInstructions session={session} />
      </TabsContent>
      <TabsContent value="mcp" className="flex-1 overflow-auto p-4">
        <McpInstructions session={session} />
      </TabsContent>
      <TabsContent value="info" className="flex-1 overflow-auto p-4 text-sm">
        <dl className="grid grid-cols-[9rem_1fr] gap-2">
          <dt className="text-muted-foreground">Session ID</dt>
          <dd className="font-mono">{session.id}</dd>
          <dt className="text-muted-foreground">Namespace</dt>
          <dd>{session.namespace}</dd>
          <dt className="text-muted-foreground">Target</dt>
          <dd>{session.kind}/{session.name}</dd>
          <dt className="text-muted-foreground">Pod</dt>
          <dd>{session.pod}</dd>
          <dt className="text-muted-foreground">Container</dt>
          <dd>{session.container}</dd>
          <dt className="text-muted-foreground">Java</dt>
          <dd>{session.javaVersion ?? "unknown"}{session.jdkProvisioned ? ` (JDK side-loaded at ${session.sideloadedJavaHome ?? "/tmp/jdk"})` : ""}</dd>
          <dt className="text-muted-foreground">Started</dt>
          <dd>{new Date(session.startedAt).toLocaleString()}</dd>
        </dl>
      </TabsContent>
    </Tabs>
  );
}

function ConsoleFrame({ src, pod }: { src: string; pod: string }) {
  const [loaded, setLoaded] = useState(false);
  const ref = useRef<HTMLIFrameElement | null>(null);
  return (
    <div className="relative h-full w-full">
      {!loaded && (
        <div className="absolute inset-0 z-10 flex flex-col items-center justify-center gap-2 bg-background text-sm text-muted-foreground">
          <Spinner className="h-6 w-6" />
          <span>Connecting to Arthas web console…</span>
        </div>
      )}
      <iframe
        ref={ref}
        title={`Arthas ${pod}`}
        src={src}
        className="h-full w-full border-0"
        onLoad={() => setLoaded(true)}
      />
    </div>
  );
}

function HttpApiInstructions({ session }: { session: ArthasSession }) {
  const apiURL = pluginURL(`proxy/${session.id}/api`);
  const curlExample = `curl -sS -XPOST "${apiURL}" \\
  -H "Content-Type: application/json" \\
  -d '{"action":"exec","command":"thread -n 3"}'`;
  return (
    <div className="flex flex-col gap-4 text-sm">
      <section>
        <h3 className="mb-1 font-semibold">Endpoint</h3>
        <CopyBlock value={apiURL} />
      </section>
      <section>
        <h3 className="mb-1 font-semibold">Example</h3>
        <CopyBlock value={curlExample} multiline />
      </section>
    </div>
  );
}

function McpInstructions({ session }: { session: ArthasSession }) {
  const sseURL = pluginURL(`mcp/${session.id}/sse`);
  return (
    <div className="flex flex-col gap-4 text-sm">
      <section>
        <h3 className="mb-1 font-semibold">SSE endpoint</h3>
        <CopyBlock value={sseURL} />
      </section>
    </div>
  );
}

function CopyBlock({ value, multiline = false }: { value: string; multiline?: boolean }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="relative">
      <pre className={`overflow-auto rounded-md border border-border bg-muted p-2 text-xs ${multiline ? "whitespace-pre" : "whitespace-pre-wrap"}`}>
        {value}
      </pre>
      <Button
        size="xs"
        variant="ghost"
        className="absolute right-1 top-1"
        onClick={async () => {
          await navigator.clipboard.writeText(value);
          setCopied(true);
          setTimeout(() => setCopied(false), 1200);
        }}
      >
        {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
      </Button>
    </div>
  );
}
