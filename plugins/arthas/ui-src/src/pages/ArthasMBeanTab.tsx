import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Tree } from "@flanksource/clicky-ui";
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { execArthas } from "./ArthasDashboardTab";
import { ArthasJulLoggingPanel, JUL_LOGGING_OBJECT_NAME } from "./ArthasJulLoggingPanel";

interface MBeanAttribute {
  name: string;
  value: unknown;
}

interface MBeanAttrMeta {
  name: string;
  description?: string;
  type: string;
  readable: boolean;
  writable: boolean;
  is: boolean;
}

interface MBeanOpParam {
  name: string;
  description?: string;
  type: string;
}

interface MBeanOpMeta {
  name: string;
  description?: string;
  returnType: string;
  signature: MBeanOpParam[];
}

interface MBeanMetadata {
  className?: string;
  description?: string;
  attributes: MBeanAttrMeta[];
  operations: MBeanOpMeta[];
}

export type MBeanTreeNode = {
  key: string;
  label: string;
  objectName?: string;
  children?: MBeanTreeNode[];
};

export function buildMBeanTree(names: string[]): MBeanTreeNode[] {
  const roots = new Map<string, MBeanTreeNode>();
  for (const full of names) {
    const colon = full.indexOf(":");
    if (colon < 0) continue;
    const domain = full.slice(0, colon);
    const props = full.slice(colon + 1);
    const segments = parseSegments(props);
    let domainNode = roots.get(domain);
    if (!domainNode) {
      domainNode = { key: domain, label: domain, children: [] };
      roots.set(domain, domainNode);
    }
    insertSegments(domainNode, segments, full);
  }
  const sorted = sortTree([...roots.values()]);
  return sorted.map(collapseSingleChildren);
}

function collapseSingleChildren(node: MBeanTreeNode): MBeanTreeNode {
  if (!node.children || node.children.length === 0) return node;
  let current = node;
  while (
    !current.objectName &&
    current.children &&
    current.children.length === 1 &&
    !current.children[0].objectName
  ) {
    const only = current.children[0];
    current = {
      key: only.key,
      label: `${current.label}/${only.label}`,
      children: only.children,
    };
  }
  if (current.children) {
    current = { ...current, children: current.children.map(collapseSingleChildren) };
  }
  return current;
}

function parseSegments(props: string): string[] {
  const parts = props.split(",").map((p) => p.trim()).filter(Boolean);
  const typeIdx = parts.findIndex((p) => p.startsWith("type="));
  const head: string[] = [];
  if (typeIdx >= 0) head.push(parts.splice(typeIdx, 1)[0]);
  parts.sort();
  return [...head, ...parts];
}

function insertSegments(parent: MBeanTreeNode, segments: string[], fullName: string): void {
  if (segments.length === 0) {
    parent.children ??= [];
    parent.children.push({ key: fullName, label: "(self)", objectName: fullName });
    return;
  }
  const [head, ...rest] = segments;
  parent.children ??= [];
  if (rest.length === 0) {
    parent.children.push({ key: `${parent.key}/${head}`, label: head, objectName: fullName });
    return;
  }
  let next = parent.children.find((c) => !c.objectName && c.label === head);
  if (!next) {
    next = { key: `${parent.key}/${head}`, label: head, children: [] };
    parent.children.push(next);
  }
  insertSegments(next, rest, fullName);
}

function sortTree(nodes: MBeanTreeNode[]): MBeanTreeNode[] {
  nodes.sort((a, b) => a.label.localeCompare(b.label));
  for (const n of nodes) if (n.children) sortTree(n.children);
  return nodes;
}

export function ArthasMBeanTab({ sessionId }: { sessionId: string }) {
  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState<string | null>(null);

  const list = useQuery({
    queryKey: ["arthas", sessionId, "mbean", "list"],
    queryFn: async () => {
      const { results } = await execArthas(sessionId, "mbean -m");
      for (const r of results as Array<{ mbeanNames?: string[] }>) {
        if (Array.isArray(r?.mbeanNames)) return r.mbeanNames;
      }
      return [] as string[];
    },
    staleTime: 30_000,
  });

  const detail = useQuery({
    queryKey: ["arthas", sessionId, "mbean", "detail", selected ?? ""],
    enabled: !!selected,
    queryFn: async () => {
      const cmd = `mbean '${selected!.replace(/'/g, "\\'")}'`;
      const { results } = await execArthas(sessionId, cmd);
      for (const r of results as Array<{ mbeanAttribute?: Record<string, MBeanAttribute[]> }>) {
        if (r?.mbeanAttribute) return r.mbeanAttribute[selected!] ?? [];
      }
      return [] as MBeanAttribute[];
    },
  });

  const metadata = useQuery<MBeanMetadata | null>({
    queryKey: ["arthas", sessionId, "mbean", "metadata", selected ?? ""],
    enabled: !!selected,
    staleTime: 60_000,
    queryFn: async () => {
      const cmd = `mbean -m '${selected!.replace(/'/g, "\\'")}'`;
      const { results } = await execArthas(sessionId, cmd);
      for (const r of results as Array<{ mbeanMetadata?: Record<string, MBeanMetadata> }>) {
        if (r?.mbeanMetadata) return r.mbeanMetadata[selected!] ?? null;
      }
      return null;
    },
  });

  const tree = useMemo(() => {
    const all = list.data ?? [];
    const q = filter.trim().toLowerCase();
    const filtered = q ? all.filter((n) => n.toLowerCase().includes(q)) : all;
    return buildMBeanTree(filtered);
  }, [list.data, filter]);

  const leafTotal = useMemo(() => list.data?.length ?? 0, [list.data]);
  const leafVisible = useMemo(() => countLeaves(tree), [tree]);

  const selectedNode = useMemo<MBeanTreeNode | null>(
    () => (selected ? findByObjectName(tree, selected) : null),
    [tree, selected],
  );

  return (
    <div className="flex h-full gap-3 p-4">
      <aside className="flex w-96 shrink-0 flex-col gap-2">
        <h3 className="text-sm font-semibold">MBeans</h3>
        <Input
          value={filter}
          onChange={(e: any) => setFilter(e.target.value)}
          placeholder="Filter (e.g. java.lang, Catalina, Coherence)…"
          className="h-7 text-xs"
        />
        {list.isLoading ? (
          <Spinner />
        ) : list.error ? (
          <p className="text-xs text-red-600">
            {list.error instanceof Error ? list.error.message : "Failed to load MBeans"}
          </p>
        ) : (
          <Tree<MBeanTreeNode>
            className="max-h-[calc(100vh-12rem)] rounded-md border"
            roots={tree}
            getKey={(n) => n.objectName ?? n.key}
            getChildren={(n) => n.children}
            selected={selectedNode}
            onSelect={(n) => {
              if (n.objectName) setSelected(n.objectName);
            }}
            defaultOpen={(_n, depth) => depth < 1 || !!filter}
            renderRow={({ node }) => (
              <span className="truncate font-mono text-xs">{node.label}</span>
            )}
            empty={<p className="p-2 text-xs text-muted-foreground">No matches.</p>}
          />
        )}
        {list.data && leafTotal > leafVisible && (
          <p className="text-xs text-muted-foreground">
            Showing {leafVisible} of {leafTotal}.
          </p>
        )}
      </aside>

      <main className="flex-1 overflow-auto rounded-md border bg-muted/20 p-3">
        {!selected ? (
          <p className="text-sm text-muted-foreground">Select an MBean on the left.</p>
        ) : detail.isLoading ? (
          <Spinner />
        ) : detail.error ? (
          <p className="text-sm text-red-600">
            {detail.error instanceof Error ? detail.error.message : "Failed to load MBean"}
          </p>
        ) : (
          <MBeanDetail
            sessionId={sessionId}
            name={selected}
            attrs={detail.data ?? []}
            metadata={metadata.data ?? null}
          />
        )}
      </main>
    </div>
  );
}

function countLeaves(nodes: MBeanTreeNode[]): number {
  let n = 0;
  for (const node of nodes) {
    if (node.objectName) n++;
    if (node.children) n += countLeaves(node.children);
  }
  return n;
}

function findByObjectName(nodes: MBeanTreeNode[], name: string): MBeanTreeNode | null {
  for (const node of nodes) {
    if (node.objectName === name) return node;
    if (node.children) {
      const found = findByObjectName(node.children, name);
      if (found) return found;
    }
  }
  return null;
}

function MBeanDetail({
  sessionId,
  name,
  attrs,
  metadata,
}: {
  sessionId: string;
  name: string;
  attrs: MBeanAttribute[];
  metadata: MBeanMetadata | null;
}) {
  const metaByAttr = useMemo(() => {
    const m = new Map<string, MBeanAttrMeta>();
    for (const a of metadata?.attributes ?? []) m.set(a.name, a);
    return m;
  }, [metadata]);

  const [editAttr, setEditAttr] = useState<MBeanAttribute | null>(null);
  const [runOp, setRunOp] = useState<MBeanOpMeta | null>(null);

  const isJul = name === JUL_LOGGING_OBJECT_NAME;
  const hasOperations = !!metadata?.operations && metadata.operations.length > 0;

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h3 className="break-all font-mono text-sm">{name}</h3>
        {metadata?.className && (
          <p className="text-xs text-muted-foreground">{metadata.className}</p>
        )}
      </div>

      <Tabs defaultValue={isJul ? "loggers" : "attributes"} className="flex flex-col gap-3">
        <TabsList className="w-fit">
          <TabsTrigger value="attributes">Attributes</TabsTrigger>
          {hasOperations && <TabsTrigger value="operations">Operations</TabsTrigger>}
          {isJul && <TabsTrigger value="loggers">Loggers</TabsTrigger>}
        </TabsList>

        <TabsContent value="attributes">
          <table className="w-full border-collapse text-xs">
            <thead>
              <tr className="border-b text-left text-muted-foreground">
                <th className="px-2 py-1">Attribute</th>
                <th className="px-2 py-1">Type</th>
                <th className="px-2 py-1">Value</th>
                <th className="w-16 px-2 py-1"></th>
              </tr>
            </thead>
            <tbody>
              {attrs.map((a) => {
                const meta = metaByAttr.get(a.name);
                return (
                  <tr key={a.name} className="border-b last:border-0 align-top">
                    <td className="px-2 py-1 font-mono">{a.name}</td>
                    <td className="px-2 py-1 font-mono text-muted-foreground">{meta?.type ?? ""}</td>
                    <td className="px-2 py-1">
                      <AttrValue value={a.value} />
                    </td>
                    <td className="px-2 py-1 text-right">
                      {meta?.writable && (
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-6 px-2 text-xs"
                          onClick={() => setEditAttr(a)}
                        >
                          Edit
                        </Button>
                      )}
                    </td>
                  </tr>
                );
              })}
              {attrs.length === 0 && (
                <tr>
                  <td colSpan={4} className="px-2 py-3 text-center text-muted-foreground">
                    No attributes.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </TabsContent>

        {hasOperations && (
          <TabsContent value="operations">
            <div className="flex flex-col gap-1">
              {metadata!.operations.map((op, idx) => (
                <div
                  key={`${op.name}-${idx}`}
                  className="flex items-start justify-between gap-2 rounded-md border bg-background p-2 text-xs"
                >
                  <div className="min-w-0 flex-1">
                    <div className="font-mono text-foreground">
                      <span className="text-muted-foreground">{op.returnType} </span>
                      <span className="font-semibold">{op.name}</span>
                      <span className="text-muted-foreground">
                        (
                        {op.signature
                          .map((p) => `${p.type} ${p.name}`)
                          .join(", ")}
                        )
                      </span>
                    </div>
                    {op.description && op.description !== op.name && (
                      <p className="mt-0.5 text-muted-foreground">{op.description}</p>
                    )}
                  </div>
                  <Button
                    size="sm"
                    variant="outline"
                    className="h-6 shrink-0 px-2 text-xs"
                    onClick={() => setRunOp(op)}
                  >
                    Run
                  </Button>
                </div>
              ))}
            </div>
          </TabsContent>
        )}

        {isJul && (
          <TabsContent value="loggers">
            <ArthasJulLoggingPanel sessionId={sessionId} />
          </TabsContent>
        )}
      </Tabs>

      <WriteAttrDialog
        sessionId={sessionId}
        objectName={name}
        attribute={editAttr}
        attributeMeta={editAttr ? metaByAttr.get(editAttr.name) : undefined}
        onClose={() => setEditAttr(null)}
      />
      <InvokeOpDialog
        sessionId={sessionId}
        objectName={name}
        operation={runOp}
        onClose={() => setRunOp(null)}
      />
    </div>
  );
}

function WriteAttrDialog({
  sessionId,
  objectName,
  attribute,
  attributeMeta,
  onClose,
}: {
  sessionId: string;
  objectName: string;
  attribute: MBeanAttribute | null;
  attributeMeta?: MBeanAttrMeta;
  onClose: () => void;
}) {
  const open = attribute !== null;
  const [value, setValue] = useState("");
  const qc = useQueryClient();

  // Reset the editor whenever a new attribute is opened.
  useEffect(() => {
    if (attribute) setValue(String(attribute.value ?? ""));
  }, [attribute]);

  const mutation = useMutation({
    mutationFn: async (v: string) => {
      if (!attribute) throw new Error("no attribute selected");
      return writeMBeanAttribute(
        sessionId,
        objectName,
        attribute.name,
        attributeMeta?.type ?? "java.lang.String",
        v,
      );
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["arthas", sessionId, "mbean", "detail", objectName] });
      onClose();
    },
  });

  return (
    <Dialog open={open} onOpenChange={(o) => (!o ? onClose() : undefined)}>
      <DialogPopup className="max-w-xl">
        <DialogHeader>
          <DialogTitle>Edit attribute</DialogTitle>
          <DialogDescription>
            <span className="break-all font-mono">{objectName}</span>
            {attribute && (
              <>
                {" · "}
                <span className="font-mono font-semibold">{attribute.name}</span>
                {attributeMeta && (
                  <span className="ml-1 font-mono text-xs opacity-70">({attributeMeta.type})</span>
                )}
              </>
            )}
          </DialogDescription>
        </DialogHeader>
        <DialogPanel>
          <div className="flex flex-col gap-2">
            <label className="text-xs text-muted-foreground">New value</label>
            <Input
              value={value}
              onChange={(e: any) => setValue(e.target.value)}
              className="h-8 text-sm"
              autoFocus
            />
            {mutation.error && (
              <p className="text-xs text-red-600">
                {mutation.error instanceof Error ? mutation.error.message : "Write failed"}
              </p>
            )}
          </div>
        </DialogPanel>
        <DialogFooter>
          <DialogClose render={<Button variant="ghost" />}>Cancel</DialogClose>
          <Button onClick={() => mutation.mutate(value)} disabled={mutation.isPending}>
            {mutation.isPending ? <Spinner /> : "Save"}
          </Button>
        </DialogFooter>
      </DialogPopup>
    </Dialog>
  );
}

function InvokeOpDialog({
  sessionId,
  objectName,
  operation,
  onClose,
}: {
  sessionId: string;
  objectName: string;
  operation: MBeanOpMeta | null;
  onClose: () => void;
}) {
  const open = operation !== null;
  const [params, setParams] = useState<string[]>([]);

  // Reset params whenever a new operation is opened.
  useEffect(() => {
    if (operation) setParams(operation.signature.map(() => ""));
  }, [operation]);

  const mutation = useMutation({
    mutationFn: async (vs: string[]) => {
      if (!operation) throw new Error("no operation selected");
      return invokeMBeanOperation(sessionId, objectName, operation, vs);
    },
  });

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
      <DialogPopup className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {operation?.name}
            <span className="ml-2 font-mono text-sm font-normal text-muted-foreground">
              → {operation?.returnType}
            </span>
          </DialogTitle>
          <DialogDescription>
            <span className="break-all font-mono">{objectName}</span>
          </DialogDescription>
        </DialogHeader>
        <DialogPanel>
          <div className="flex flex-col gap-3">
            {operation && operation.signature.length === 0 && (
              <p className="text-xs text-muted-foreground">This operation takes no parameters.</p>
            )}
            {operation?.signature.map((p, i) => (
              <div key={`${p.name}-${i}`} className="flex flex-col gap-1">
                <label className="font-mono text-xs text-muted-foreground">
                  {p.type} {p.name}
                </label>
                <Input
                  value={params[i] ?? ""}
                  onChange={(e: any) =>
                    setParams((prev) => prev.map((v, j) => (i === j ? e.target.value : v)))
                  }
                  className="h-8 text-sm"
                />
              </div>
            ))}
            {mutation.error && (
              <p className="text-xs text-red-600">
                {mutation.error instanceof Error ? mutation.error.message : "Invoke failed"}
              </p>
            )}
            {mutation.data !== undefined && (
              <div className="rounded-md border bg-muted/30 p-2">
                <p className="mb-1 text-xs font-semibold text-muted-foreground">Result</p>
                <AttrValue value={mutation.data} />
              </div>
            )}
          </div>
        </DialogPanel>
        <DialogFooter>
          <DialogClose render={<Button variant="ghost" />}>Close</DialogClose>
          <Button onClick={() => mutation.mutate(params)} disabled={mutation.isPending}>
            {mutation.isPending ? <Spinner /> : "Invoke"}
          </Button>
        </DialogFooter>
      </DialogPopup>
    </Dialog>
  );
}

// Cast a user-typed value to the closest Java literal for ognl.
function coerceLiteral(raw: string, type: string): string {
  const t = type.replace(/^java\.lang\./, "");
  if (t === "boolean" || t === "Boolean") return raw === "true" ? "true" : "false";
  if (t === "int" || t === "Integer" || t === "long" || t === "Long" || t === "short" || t === "Short") {
    return /^-?\d+$/.test(raw.trim()) ? raw.trim() + (t === "long" || t === "Long" ? "L" : "") : "0";
  }
  if (t === "double" || t === "Double" || t === "float" || t === "Float") {
    return /^-?\d+(\.\d+)?$/.test(raw.trim()) ? raw.trim() : "0.0";
  }
  // Strings and everything else: quote as Java string literal.
  return `"${raw.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

async function writeMBeanAttribute(
  sessionId: string,
  objectName: string,
  attribute: string,
  attributeType: string,
  value: string,
): Promise<void> {
  const literal = coerceLiteral(value, attributeType);
  const expr = [
    `(#server=@java.lang.management.ManagementFactory@getPlatformMBeanServer(),`,
    ` #name=new javax.management.ObjectName("${objectName.replace(/"/g, '\\"')}"),`,
    ` #attr=new javax.management.Attribute("${attribute}", ${literal}),`,
    ` #server.setAttribute(#name, #attr))`,
  ].join("");
  const { results } = await execArthas(sessionId, `ognl '${expr.replace(/'/g, "\\'")}'`);
  throwIfOgnlError(results);
}

async function invokeMBeanOperation(
  sessionId: string,
  objectName: string,
  op: MBeanOpMeta,
  rawParams: string[],
): Promise<unknown> {
  const literals = op.signature.map((p, i) => coerceLiteral(rawParams[i] ?? "", p.type));
  const types = op.signature.map((p) => `"${p.type}"`);
  const expr = [
    `(#server=@java.lang.management.ManagementFactory@getPlatformMBeanServer(),`,
    ` #name=new javax.management.ObjectName("${objectName.replace(/"/g, '\\"')}"),`,
    ` #args=new Object[]{${literals.join(", ")}},`,
    ` #sig=new String[]{${types.join(", ")}},`,
    ` #server.invoke(#name, "${op.name}", #args, #sig))`,
  ].join("");
  const { results } = await execArthas(sessionId, `ognl '${expr.replace(/'/g, "\\'")}'`);
  throwIfOgnlError(results);
  for (const r of results as Array<{ value?: unknown; type?: string }>) {
    if (r?.type === "ognl" && "value" in r) return r.value;
  }
  return undefined;
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

function AttrValue({ value }: { value: unknown }) {
  if (value === null || value === undefined) return <span className="text-muted-foreground">–</span>;
  if (typeof value === "boolean") return <span className="font-mono">{value ? "true" : "false"}</span>;
  if (typeof value === "number") return <span className="font-mono">{value.toLocaleString()}</span>;
  if (typeof value === "string") return <span className="font-mono">{value}</span>;
  const text = JSON.stringify(value, null, 2);
  const short = text.length <= 80;
  return (
    <pre
      className={`m-0 whitespace-pre-wrap break-all font-mono ${
        short ? "" : "max-h-48 overflow-auto"
      }`}
    >
      {text}
    </pre>
  );
}
