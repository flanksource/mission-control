import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Select } from "@flanksource/clicky-ui";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogPopup,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogPanel,
  DialogFooter,
  DialogClose,
} from "@/components/ui/dialog";
import { execArthas } from "./ArthasDashboardTab";

export const JUL_LOGGING_OBJECT_NAME = "java.util.logging:type=Logging";

const JUL_LEVELS = [
  "OFF",
  "SEVERE",
  "WARNING",
  "INFO",
  "CONFIG",
  "FINE",
  "FINER",
  "FINEST",
  "ALL",
] as const;

const INHERIT_SENTINEL = "__inherit__";

interface LoggerRow {
  name: string;
  level: string | null;
  parent: string | null;
}

export function ArthasJulLoggingPanel({ sessionId }: { sessionId: string }) {
  const [filter, setFilter] = useState("");
  const [editing, setEditing] = useState<LoggerRow | null>(null);

  const query = useQuery({
    queryKey: ["arthas", sessionId, "jul", "loggers"],
    queryFn: () => fetchLoggerRows(sessionId),
    staleTime: 15_000,
  });

  const rows = query.data ?? [];
  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return rows;
    return rows.filter((r) => r.name.toLowerCase().includes(q));
  }, [rows, filter]);

  return (
    <div className="flex flex-col gap-3">
      <p className="text-xs text-muted-foreground">
        java.util.logging loggers. Click <em>Edit</em> to change a level; choosing{" "}
        <code>(inherit)</code> clears the logger so it falls through to its parent.
      </p>

      <div className="flex items-center gap-2">
        <Input
          value={filter}
          onChange={(e: any) => setFilter(e.target.value)}
          placeholder="Filter loggers (e.g. org.apache, io.prometheus)…"
          className="h-7 max-w-sm text-xs"
        />
        {query.isLoading && <Spinner />}
        {query.data && (
          <span className="text-xs text-muted-foreground">
            {filtered.length} of {rows.length} loggers
          </span>
        )}
      </div>

      {query.error && (
        <p className="text-xs text-red-600">
          {query.error instanceof Error ? query.error.message : "Failed to load loggers"}
        </p>
      )}

      {query.data && (
        <table className="w-full border-collapse text-xs">
          <thead>
            <tr className="border-b text-left text-muted-foreground">
              <th className="px-2 py-1">Logger</th>
              <th className="w-32 px-2 py-1">Level</th>
              <th className="px-2 py-1">Parent</th>
              <th className="w-16 px-2 py-1"></th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((row) => (
              <tr key={row.name || "<root>"} className="border-b last:border-0 align-middle">
                <td className="px-2 py-1 font-mono">{row.name === "" ? "<root>" : row.name}</td>
                <td className="px-2 py-1 font-mono">
                  {row.level ?? <span className="text-muted-foreground">(inherit)</span>}
                </td>
                <td className="px-2 py-1 font-mono text-muted-foreground">
                  {row.parent === null ? "–" : row.parent === "" ? "<root>" : row.parent}
                </td>
                <td className="px-2 py-1 text-right">
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-6 px-2 text-xs"
                    onClick={() => setEditing(row)}
                  >
                    Edit
                  </Button>
                </td>
              </tr>
            ))}
            {filtered.length === 0 && (
              <tr>
                <td colSpan={4} className="px-2 py-3 text-center text-muted-foreground">
                  No matches.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      )}

      <EditLevelDialog
        sessionId={sessionId}
        row={editing}
        onClose={() => setEditing(null)}
      />
    </div>
  );
}

function EditLevelDialog({
  sessionId,
  row,
  onClose,
}: {
  sessionId: string;
  row: LoggerRow | null;
  onClose: () => void;
}) {
  const open = row !== null;
  const [choice, setChoice] = useState<string>(INHERIT_SENTINEL);
  const qc = useQueryClient();

  useEffect(() => {
    if (row) setChoice(row.level ?? INHERIT_SENTINEL);
  }, [row]);

  const mutation = useMutation({
    mutationFn: async (next: string) => {
      if (!row) throw new Error("no logger selected");
      await setLoggerLevelRemote(
        sessionId,
        row.name,
        next === INHERIT_SENTINEL ? null : next,
      );
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["arthas", sessionId, "jul", "loggers"] });
      onClose();
    },
  });

  const displayName = row?.name === "" ? "<root>" : row?.name;
  const currentLabel = row?.level ?? "(inherit)";

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) {
          mutation.reset();
          onClose();
        }
      }}
    >
      <DialogPopup className="max-w-md">
        <DialogHeader>
          <DialogTitle>Set logger level</DialogTitle>
          <DialogDescription>
            <span className="break-all font-mono">{displayName}</span>
            <span className="ml-2 text-xs text-muted-foreground">currently: {currentLabel}</span>
          </DialogDescription>
        </DialogHeader>
        <DialogPanel>
          <div className="flex flex-col gap-2">
            <label className="text-xs text-muted-foreground">New level</label>
            <Select
              className="h-8 text-sm"
              value={choice}
              onChange={(e: any) => setChoice(e.target.value)}
              options={[
                { value: INHERIT_SENTINEL, label: "(inherit)" },
                ...JUL_LEVELS.map((l) => ({ value: l, label: l })),
              ]}
            />
            {mutation.error && (
              <p className="text-xs text-red-600">
                {mutation.error instanceof Error ? mutation.error.message : "Update failed"}
              </p>
            )}
          </div>
        </DialogPanel>
        <DialogFooter>
          <DialogClose render={<Button variant="ghost" />}>Cancel</DialogClose>
          <Button onClick={() => mutation.mutate(choice)} disabled={mutation.isPending}>
            {mutation.isPending ? <Spinner /> : "Confirm"}
          </Button>
        </DialogFooter>
      </DialogPopup>
    </Dialog>
  );
}

async function fetchLoggerRows(sessionId: string): Promise<LoggerRow[]> {
  const names = await fetchLoggerNames(sessionId);
  const results = await Promise.all(
    names.map(async (name) => {
      const [level, parent] = await Promise.all([
        invokeLoggerStringOp(sessionId, "getLoggerLevel", name),
        invokeLoggerStringOp(sessionId, "getParentLoggerName", name),
      ]);
      return { name, level, parent };
    }),
  );
  results.sort((a, b) => a.name.localeCompare(b.name));
  return results;
}

async function fetchLoggerNames(sessionId: string): Promise<string[]> {
  const cmd = `mbean '${JUL_LOGGING_OBJECT_NAME}'`;
  const { results } = await execArthas(sessionId, cmd);
  for (const r of results as Array<{
    mbeanAttribute?: Record<string, Array<{ name: string; value: unknown }>>;
  }>) {
    const attrs = r?.mbeanAttribute?.[JUL_LOGGING_OBJECT_NAME];
    if (!attrs) continue;
    const entry = attrs.find((a) => a.name === "LoggerNames");
    if (entry && Array.isArray(entry.value)) {
      return (entry.value as unknown[]).filter(
        (v): v is string => typeof v === "string",
      );
    }
  }
  throw new Error("LoggerNames attribute missing from java.util.logging:type=Logging");
}

async function invokeLoggerStringOp(
  sessionId: string,
  op: "getLoggerLevel" | "getParentLoggerName",
  name: string,
): Promise<string | null> {
  const expr = [
    `(#server=@java.lang.management.ManagementFactory@getPlatformMBeanServer(),`,
    ` #name=new javax.management.ObjectName("${JUL_LOGGING_OBJECT_NAME}"),`,
    ` #args=new Object[]{${javaString(name)}},`,
    ` #sig=new String[]{"java.lang.String"},`,
    ` #server.invoke(#name, "${op}", #args, #sig))`,
  ].join("");
  const { results } = await execArthas(sessionId, `ognl '${escapeSingleQuotes(expr)}'`);
  throwIfOgnlError(results);
  for (const r of results as Array<{ value?: unknown; type?: string }>) {
    if (r?.type === "ognl" && "value" in r) {
      const v = r.value;
      if (v === null || v === undefined) return null;
      if (typeof v === "string") return v;
      return String(v);
    }
  }
  return null;
}

async function setLoggerLevelRemote(
  sessionId: string,
  name: string,
  level: string | null,
): Promise<void> {
  const levelLiteral = level === null ? "null" : javaString(level);
  const expr = [
    `(#server=@java.lang.management.ManagementFactory@getPlatformMBeanServer(),`,
    ` #name=new javax.management.ObjectName("${JUL_LOGGING_OBJECT_NAME}"),`,
    ` #args=new Object[]{${javaString(name)}, ${levelLiteral}},`,
    ` #sig=new String[]{"java.lang.String", "java.lang.String"},`,
    ` #server.invoke(#name, "setLoggerLevel", #args, #sig))`,
  ].join("");
  const { results } = await execArthas(sessionId, `ognl '${escapeSingleQuotes(expr)}'`);
  throwIfOgnlError(results);
}

function javaString(s: string): string {
  return `"${s.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

function escapeSingleQuotes(s: string): string {
  return s.replace(/'/g, "\\'");
}

function throwIfOgnlError(results: unknown[]): void {
  for (const r of results as Array<{ type?: string; message?: string; statusCode?: number }>) {
    if (r?.type === "status" && typeof r.statusCode === "number" && r.statusCode !== 0) {
      const msg = r.message ?? `arthas ognl failed (status ${r.statusCode})`;
      const hint =
        msg.includes("IllegalAccessException") && msg.includes("module java.management")
          ? " — JDK 9+ blocks reflective JMX access here; this action requires a JDK 8 target or an agent with --add-opens."
          : "";
      throw new Error(msg + hint);
    }
  }
}
