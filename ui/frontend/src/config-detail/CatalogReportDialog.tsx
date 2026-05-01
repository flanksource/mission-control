import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Badge, Button, DetailEmptyState, Icon, Modal, cn } from "@flanksource/clicky-ui";
import { ConfigIcon } from "../ConfigIcon";
import { ConfigItemSelector } from "../components/ConfigItemSelector";
import {
  CatalogReportError,
  generateCatalogReport,
  previewCatalogReport,
  type CatalogReportErrorBody,
  type CatalogReportProgress,
} from "../api/configs";
import type {
  CatalogReportFormat,
  CatalogReportRequest,
  CatalogReportRoot,
  ConfigItem,
  ConfigRelationshipTreeNode,
} from "../api/types";

type CatalogReportDialogProps = {
  open: boolean;
  config: ConfigItem;
  onClose: () => void;
};

type ReportDialogError = {
  message: string;
  status?: number;
  statusText?: string;
  context?: Record<string, unknown>;
  stacktrace?: string;
  time?: string;
  trace?: string;
  rawBody?: string;
};

type ReportWizardStep = "resources" | "options";

export type ReportOptions = Required<Omit<CatalogReportRequest, "roots" | "selectedIds">> & {
  filtersText: string;
};

const defaultOptions: ReportOptions = {
  format: "facet-pdf",
  title: "",
  since: "30d",
  recursive: true,
  groupBy: "none",
  changeArtifacts: false,
  audit: false,
  expandGroups: false,
  limit: 50,
  maxItems: 50,
  maxChanges: 100,
  maxItemArtifacts: 0,
  staleDays: 0,
  reviewOverdueDays: 0,
  filters: [],
  filtersText: "",
  changes: true,
  insights: true,
  relationships: true,
  access: true,
  accessLogs: false,
  configJSON: false,
};

export function CatalogReportDialog({ open, config, onClose }: CatalogReportDialogProps) {
  const [options, setOptions] = useState<ReportOptions>(() => optionsForConfig(config));
  const [step, setStep] = useState<ReportWizardStep>("resources");
  const [roots, setRoots] = useState<ConfigRelationshipTreeNode[]>([]);
  const [selectedIds, setSelectedIds] = useState<string[]>([]);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [reportProgress, setReportProgress] = useState<CatalogReportProgress | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [reportError, setReportError] = useState<ReportDialogError | null>(null);
  const [expandedChildrenIds, setExpandedChildrenIds] = useState<Set<string>>(() => new Set());
  const selectedSet = useMemo(() => new Set(selectedIds), [selectedIds]);

  useEffect(() => {
    if (!open) return;
    const initialOptions = optionsForConfig(config);
    setOptions(initialOptions);
    setStep("resources");
    setRoots([]);
    setSelectedIds([]);
    setExpandedChildrenIds(new Set());
    setError(null);
    setReportError(null);
    void loadPreview([], [catalogReportRoot(config.id)]);
  }, [open, config.id]);

  async function loadPreview(baseSelectedIds: string[], additions: Array<{ id: string; includeChildren: boolean }>) {
    setPreviewLoading(true);
    setError(null);
    try {
      const preview = await previewCatalogReport({
        selectedIds: baseSelectedIds,
        roots: additions,
      });
      setRoots(preview.roots);
      setSelectedIds(preview.selectedIds);
      return true;
    } catch (err) {
      setError(errorMessage(err));
      return false;
    } finally {
      setPreviewLoading(false);
    }
  }

  function updateOption<K extends keyof ReportOptions>(key: K, value: ReportOptions[K]) {
    setOptions((current) => ({ ...current, [key]: value }));
  }

  function toggleNode(node: ConfigRelationshipTreeNode, checked: boolean) {
    const ids = collectNodeIDs(node);
    setSelectedIds((current) => {
      const next = new Set(current);
      for (const id of ids) {
        if (checked) {
          next.add(id);
        } else {
          next.delete(id);
        }
      }
      return flattenSelectedIDs(roots, next);
    });
  }

  async function addConfig(item: ConfigItem | null) {
    if (!item) return;
    await loadPreview(selectedIds, [catalogReportRoot(item.id)]);
  }

  async function expandChildren(node: ConfigRelationshipTreeNode) {
    const expanded = await loadPreview(selectedIds, [catalogReportRoot(node.id, true)]);
    if (expanded) {
      setExpandedChildrenIds((current) => new Set(current).add(node.id));
    }
  }

  async function downloadReport() {
    setGenerating(true);
    setReportProgress({ stage: "rendering" });
    setError(null);
    setReportError(null);
    try {
      const request = buildReportRequest(options, selectedIds);
      const { blob, filename } = await generateCatalogReport(request, setReportProgress);
      saveBlob(blob, filename);
    } catch (err) {
      setReportError(toReportDialogError(err));
    } finally {
      setGenerating(false);
      setReportProgress(null);
    }
  }

  const canDownload = selectedIds.length > 0 && !previewLoading && !generating;
  const canContinue = selectedIds.length > 0 && !previewLoading;

  return (
    <>
      <Modal
        open={open}
        onClose={onClose}
        size="lg"
        className="max-h-[88vh] overflow-hidden"
        headerSlot={
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <Icon name="lucide:file-down" className="shrink-0 text-muted-foreground" />
            <span className="truncate text-sm font-semibold">Catalog report</span>
            <Badge size="xxs">{selectedIds.length}</Badge>
          </div>
        }
      >
        <div className="flex min-h-[34rem] flex-col gap-4">
          <ReportWizardSteps step={step} />
          {step === "resources" ? (
            <div className="flex min-h-0 flex-1 flex-col gap-3">
              <div className="grid gap-3">
                <div className="min-w-0">
                  <div className="mb-1 text-xs font-medium text-muted-foreground">Resources</div>
                  <ConfigItemSelector
                    placeholder="Search resources to add..."
                    onSelect={(item) => void addConfig(item)}
                  />
                </div>
              </div>
              <div className="min-h-0 flex-1 overflow-auto rounded-md border border-border">
                {previewLoading ? (
                  <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
                    <Icon name="lucide:loader-2" className="animate-spin" />
                    <span>Loading resources...</span>
                  </div>
                ) : roots.length === 0 ? (
                  <DetailEmptyState icon="lucide:box" label="No resources" />
                ) : (
                  <div className="p-2">
                    {roots.map((node) => (
                      <ReportTreeNode
                        key={node.id}
                        node={node}
                        selectedIds={selectedSet}
                        onToggle={toggleNode}
                        onExpandChildren={expandChildren}
                        expandedChildrenIds={expandedChildrenIds}
                        disabled={previewLoading}
                      />
                    ))}
                  </div>
                )}
              </div>
              {error && (
                <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">
                  {error}
                </div>
              )}
            </div>
          ) : (
            <div className="grid min-h-0 flex-1 gap-4 overflow-hidden lg:grid-cols-[20rem_minmax(0,1fr)]">
              <div className="min-h-0 overflow-auto border-r border-border pr-4">
                <div className="space-y-4">
                  <Field label="Title">
                    <input
                      value={options.title}
                      onChange={(event) => updateOption("title", event.target.value)}
                      className="h-9 w-full rounded-md border border-border bg-background px-2 text-sm outline-none focus:border-primary"
                    />
                  </Field>
                  <div className="grid grid-cols-2 gap-3">
                    <Field label="Format">
                      <select
                        value={options.format}
                        onChange={(event) => updateOption("format", event.target.value as CatalogReportFormat)}
                        className="h-9 w-full rounded-md border border-border bg-background px-2 text-sm outline-none focus:border-primary"
                      >
                        <option value="facet-pdf">PDF</option>
                        <option value="facet-html">HTML</option>
                        <option value="json">JSON</option>
                      </select>
                    </Field>
                    <Field label="Since">
                      <input
                        value={options.since}
                        onChange={(event) => updateOption("since", event.target.value)}
                        className="h-9 w-full rounded-md border border-border bg-background px-2 text-sm outline-none focus:border-primary"
                      />
                    </Field>
                  </div>
                  <Field label="Group by">
                    <select
                      value={options.groupBy}
                      onChange={(event) => updateOption("groupBy", event.target.value)}
                      className="h-9 w-full rounded-md border border-border bg-background px-2 text-sm outline-none focus:border-primary"
                    >
                      <option value="none">None</option>
                      <option value="merged">Merged</option>
                      <option value="config">Config</option>
                    </select>
                  </Field>
                  <OptionGroup title="Sections">
                    <CheckOption label="Changes" checked={options.changes} onChange={(value) => updateOption("changes", value)} />
                    <CheckOption label="Insights" checked={options.insights} onChange={(value) => updateOption("insights", value)} />
                    <CheckOption label="Relationships" checked={options.relationships} onChange={(value) => updateOption("relationships", value)} />
                    <CheckOption label="Access" checked={options.access} onChange={(value) => updateOption("access", value)} />
                    <CheckOption label="Access logs" checked={options.accessLogs} onChange={(value) => updateOption("accessLogs", value)} />
                    <CheckOption label="Config JSON" checked={options.configJSON} onChange={(value) => updateOption("configJSON", value)} />
                  </OptionGroup>
                  <OptionGroup title="Options">
                    <CheckOption label="Recursive" checked={options.recursive} onChange={(value) => updateOption("recursive", value)} />
                    <CheckOption label="Change artifacts" checked={options.changeArtifacts} onChange={(value) => updateOption("changeArtifacts", value)} />
                    <CheckOption label="Expand groups" checked={options.expandGroups} onChange={(value) => updateOption("expandGroups", value)} />
                    <CheckOption label="Audit" checked={options.audit} onChange={(value) => updateOption("audit", value)} />
                  </OptionGroup>
                  <div className="grid grid-cols-2 gap-3">
                    <NumberField label="Limit" value={options.limit} onChange={(value) => updateOption("limit", value)} />
                    <NumberField label="Max items" value={options.maxItems} onChange={(value) => updateOption("maxItems", value)} />
                    <NumberField label="Max changes" value={options.maxChanges} onChange={(value) => updateOption("maxChanges", value)} />
                    <NumberField label="Max artifacts" value={options.maxItemArtifacts} onChange={(value) => updateOption("maxItemArtifacts", value)} />
                    <NumberField label="Stale days" value={options.staleDays} onChange={(value) => updateOption("staleDays", value)} />
                    <NumberField label="Review days" value={options.reviewOverdueDays} onChange={(value) => updateOption("reviewOverdueDays", value)} />
                  </div>
                  <Field label="Filters">
                    <textarea
                      value={options.filtersText}
                      onChange={(event) => updateOption("filtersText", event.target.value)}
                      className="min-h-20 w-full rounded-md border border-border bg-background px-2 py-1.5 font-mono text-xs outline-none focus:border-primary"
                    />
                  </Field>
                </div>
              </div>
              <div className="flex min-h-0 min-w-0 flex-col gap-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="text-sm font-medium">Selected resources</div>
                  <Badge size="xxs">{selectedIds.length}</Badge>
                </div>
                <div className="min-h-0 flex-1 overflow-auto rounded-md border border-border">
                  {roots.length === 0 ? (
                    <DetailEmptyState icon="lucide:box" label="No resources" />
                  ) : (
                    <div className="p-2">
                      {roots.map((node) => (
                        <ReportTreeNode
                          key={node.id}
                          node={node}
                          selectedIds={selectedSet}
                          onToggle={toggleNode}
                          onExpandChildren={expandChildren}
                          expandedChildrenIds={expandedChildrenIds}
                          disabled={previewLoading}
                        />
                      ))}
                    </div>
                  )}
                </div>
                {error && (
                  <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">
                    {error}
                  </div>
                )}
              </div>
            </div>
          )}
          <div className="flex justify-between gap-2 border-t border-border pt-3">
            <button type="button" onClick={onClose} className="h-9 rounded-md border border-border px-3 text-sm hover:bg-accent/50">
              Cancel
            </button>
            {step === "resources" ? (
              <button
                type="button"
                disabled={!canContinue}
                onClick={() => setStep("options")}
                className="inline-flex h-9 items-center gap-2 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground disabled:cursor-not-allowed disabled:opacity-50"
              >
                <span>Next</span>
                <Icon name="lucide:arrow-right" />
              </button>
            ) : (
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={() => setStep("resources")}
                  className="inline-flex h-9 items-center gap-2 rounded-md border border-border px-3 text-sm hover:bg-accent/50"
                >
                  <Icon name="lucide:arrow-left" />
                  <span>Back</span>
                </button>
                <Button
                  type="button"
                  disabled={!canDownload}
                  loading={generating}
                  loadingLabel={progressLabel(reportProgress)}
                  onClick={() => void downloadReport()}
                >
                  <Icon name="lucide:download" />
                  <span>Download</span>
                </Button>
              </div>
            )}
          </div>
        </div>
      </Modal>
      <ReportErrorDialog error={reportError} onClose={() => setReportError(null)} />
    </>
  );
}

function optionsForConfig(config: ConfigItem): ReportOptions {
  return {
    ...defaultOptions,
    title: `${config.name || "Catalog"} Report`,
  };
}

export function catalogReportRoot(id: string, includeChildren = false): CatalogReportRoot {
  return { id, includeChildren };
}

export function buildReportRequest(options: ReportOptions, selectedIds: string[]): CatalogReportRequest {
  return {
    format: options.format,
    selectedIds,
    title: options.title,
    since: options.since,
    recursive: options.recursive,
    groupBy: options.groupBy,
    changeArtifacts: options.changeArtifacts,
    audit: options.audit,
    expandGroups: options.expandGroups,
    limit: options.limit,
    maxItems: options.maxItems,
    maxChanges: options.maxChanges,
    maxItemArtifacts: options.maxItemArtifacts,
    staleDays: options.staleDays,
    reviewOverdueDays: options.reviewOverdueDays,
    filters: options.filtersText.split(/\r?\n/).map((line) => line.trim()).filter(Boolean),
    changes: options.changes,
    insights: options.insights,
    relationships: options.relationships,
    access: options.access,
    accessLogs: options.accessLogs,
    configJSON: options.configJSON,
  };
}

export function progressLabel(progress: CatalogReportProgress | null): string {
  if (progress?.stage === "downloading") {
    if (progress.total && progress.loaded !== undefined) {
      return `Downloading report (${formatBytes(progress.loaded)} of ${formatBytes(progress.total)})`;
    }
    return "Downloading report";
  }
  return "Rendering report";
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }
  const formatted = value >= 10 || unitIndex === 0 || Number.isInteger(value)
    ? Math.round(value).toString()
    : value.toFixed(1);
  return `${formatted} ${units[unitIndex]}`;
}

function ReportErrorDialog({ error, onClose }: { error: ReportDialogError | null; onClose: () => void }) {
  if (!error) return null;

  const metadata = [
    error.status ? ["Status", `${error.status}${error.statusText ? ` ${error.statusText}` : ""}`] : undefined,
    error.trace ? ["Trace", error.trace] : undefined,
    error.time ? ["Time", error.time] : undefined,
  ].filter((item): item is [string, string] => Boolean(item));

  return (
    <Modal
      open
      onClose={onClose}
      size="lg"
      className="max-h-[82vh] overflow-hidden"
      headerSlot={
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <Icon name="lucide:triangle-alert" className="shrink-0 text-destructive" />
          <span className="truncate text-sm font-semibold">Report generation failed</span>
        </div>
      }
    >
      <div className="space-y-4">
        <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">
          {error.message}
        </div>
        {metadata.length > 0 && (
          <div className="grid gap-2 sm:grid-cols-3">
            {metadata.map(([label, value]) => (
              <div key={label} className="min-w-0 rounded-md border border-border px-3 py-2">
                <div className="text-xs font-medium text-muted-foreground">{label}</div>
                <div className="mt-1 truncate font-mono text-xs">{value}</div>
              </div>
            ))}
          </div>
        )}
        {error.context && Object.keys(error.context).length > 0 && (
          <DetailsBlock title="Context">
            <JsonBlock value={error.context} />
          </DetailsBlock>
        )}
        {error.stacktrace && (
          <DetailsBlock title="Stacktrace">
            <pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded-md bg-muted p-3 font-mono text-xs">
              {error.stacktrace}
            </pre>
          </DetailsBlock>
        )}
        {!error.stacktrace && error.rawBody && (
          <DetailsBlock title="Response">
            <pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded-md bg-muted p-3 font-mono text-xs">
              {error.rawBody}
            </pre>
          </DetailsBlock>
        )}
        <div className="flex justify-end">
          <button
            type="button"
            onClick={onClose}
            className="h-9 rounded-md border border-border px-3 text-sm hover:bg-accent/50"
          >
            Close
          </button>
        </div>
      </div>
    </Modal>
  );
}

function ReportWizardSteps({ step }: { step: ReportWizardStep }) {
  const steps: Array<{ id: ReportWizardStep; label: string; icon: string }> = [
    { id: "resources", label: "Resources", icon: "lucide:box" },
    { id: "options", label: "Options", icon: "lucide:sliders-horizontal" },
  ];
  const activeIndex = steps.findIndex((item) => item.id === step);

  return (
    <div className="grid grid-cols-2 gap-2">
      {steps.map((item, index) => {
        const active = item.id === step;
        const complete = index < activeIndex;
        return (
          <div
            key={item.id}
            className={cn(
              "flex min-w-0 items-center gap-2 rounded-md border px-3 py-2 text-sm",
              active && "border-primary/40 bg-primary/10 text-primary",
              complete && "border-emerald-200 bg-emerald-50 text-emerald-700",
              !active && !complete && "border-border bg-muted/20 text-muted-foreground",
            )}
          >
            <Icon name={complete ? "lucide:check" : item.icon} className="shrink-0" />
            <span className="truncate">{item.label}</span>
          </div>
        );
      })}
    </div>
  );
}

function DetailsBlock({ title, children }: { title: string; children: ReactNode }) {
  return (
    <details className="rounded-md border border-border">
      <summary className="cursor-pointer px-3 py-2 text-sm font-medium">{title}</summary>
      <div className="border-t border-border p-3">{children}</div>
    </details>
  );
}

function JsonBlock({ value }: { value: unknown }) {
  return (
    <pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded-md bg-muted p-3 font-mono text-xs">
      {JSON.stringify(value, null, 2)}
    </pre>
  );
}

export function toReportDialogError(err: unknown): ReportDialogError {
  if (err instanceof CatalogReportError) {
    return {
      message: err.message,
      status: err.status,
      statusText: err.statusText,
      rawBody: err.rawBody,
      ...catalogReportErrorBodyFields(err.body),
    };
  }
  return {
    message: errorMessage(err),
  };
}

function catalogReportErrorBodyFields(body?: CatalogReportErrorBody): Partial<ReportDialogError> {
  if (!body) return {};
  return {
    context: body.context,
    stacktrace: body.stacktrace,
    time: body.time,
    trace: body.trace,
  };
}

function ReportTreeNode({
  node,
  selectedIds,
  onToggle,
  onExpandChildren,
  expandedChildrenIds,
  disabled,
  depth = 0,
}: {
  node: ConfigRelationshipTreeNode;
  selectedIds: Set<string>;
  onToggle: (node: ConfigRelationshipTreeNode, checked: boolean) => void;
  onExpandChildren: (node: ConfigRelationshipTreeNode) => void;
  expandedChildrenIds: Set<string>;
  disabled?: boolean;
  depth?: number;
}) {
  const children = node.children ?? [];
  const subtreeIDs = collectNodeIDs(node);
  const checkedCount = subtreeIDs.filter((id) => selectedIds.has(id)).length;
  const checked = checkedCount === subtreeIDs.length;
  const partial = checkedCount > 0 && !checked;
  const canExpandChildren = children.length === 0 && !expandedChildrenIds.has(node.id);

  return (
    <div className="min-w-0">
      <div
        className={cn(
          "flex min-w-0 items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-accent/50",
          !checked && !partial && "text-muted-foreground",
        )}
        style={{ paddingLeft: `${8 + depth * 18}px` }}
      >
        <label className="flex min-w-0 flex-1 items-center gap-2">
          <input
            type="checkbox"
            checked={checked}
            ref={(input) => {
              if (input) input.indeterminate = partial;
            }}
            onChange={(event) => onToggle(node, event.target.checked)}
            className="shrink-0"
          />
          <ConfigIcon primary={node.type} className="h-4 max-w-4 shrink-0 text-muted-foreground" />
          <span className="min-w-0 flex-1 truncate">{node.name || node.id}</span>
        </label>
        {node.type && <span className="max-w-[16rem] truncate font-mono text-xs text-muted-foreground">{node.type}</span>}
        {children.length > 0 && <Badge size="xxs">{children.length}</Badge>}
        {canExpandChildren && (
          <button
            type="button"
            disabled={disabled}
            onClick={() => onExpandChildren(node)}
            className="inline-flex h-6 shrink-0 items-center gap-1 rounded border border-border px-1.5 text-xs font-medium text-foreground hover:bg-background disabled:cursor-not-allowed disabled:opacity-50"
          >
            <Icon name="lucide:git-branch-plus" className="text-[13px]" />
            <span>Expand Children</span>
          </button>
        )}
      </div>
      {children.map((child) => (
        <ReportTreeNode
          key={child.id}
          node={child}
          selectedIds={selectedIds}
          onToggle={onToggle}
          onExpandChildren={onExpandChildren}
          expandedChildrenIds={expandedChildrenIds}
          disabled={disabled}
          depth={depth + 1}
        />
      ))}
    </div>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs font-medium text-muted-foreground">{label}</span>
      {children}
    </label>
  );
}

function OptionGroup({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div>
      <div className="mb-2 text-xs font-medium text-muted-foreground">{title}</div>
      <div className="grid grid-cols-2 gap-2">{children}</div>
    </div>
  );
}

function CheckOption({ label, checked, onChange }: { label: string; checked: boolean; onChange: (checked: boolean) => void }) {
  return (
    <label className="inline-flex items-center gap-2 rounded-md border border-border px-2 py-1.5 text-sm">
      <input type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} />
      <span className="truncate">{label}</span>
    </label>
  );
}

function NumberField({ label, value, onChange }: { label: string; value: number; onChange: (value: number) => void }) {
  return (
    <Field label={label}>
      <input
        type="number"
        min={0}
        value={value}
        onChange={(event) => onChange(Math.max(0, Number(event.target.value) || 0))}
        className="h-9 w-full rounded-md border border-border bg-background px-2 text-sm outline-none focus:border-primary"
      />
    </Field>
  );
}

export function collectNodeIDs(node: ConfigRelationshipTreeNode): string[] {
  return [node.id, ...(node.children ?? []).flatMap(collectNodeIDs)];
}

export function flattenSelectedIDs(nodes: ConfigRelationshipTreeNode[], selectedIds: Set<string>): string[] {
  const ids: string[] = [];
  const walk = (items: ConfigRelationshipTreeNode[]) => {
    for (const item of items) {
      if (selectedIds.has(item.id)) ids.push(item.id);
      walk(item.children ?? []);
    }
  };
  walk(nodes);
  return ids;
}

function saveBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

function errorMessage(err: unknown) {
  return err instanceof Error ? err.message : String(err);
}
