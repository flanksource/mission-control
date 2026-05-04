import React from "react";
import { Document, Page, Header, Footer, SeverityStatCard, ListTable, Badge as FacetBadge, PageNo } from "@flanksource/facet";
import { OUTCOME_ICONS, CATEGORY_ICONS, KILL_CHAIN_ICONS, IDENTITY_ICONS, ENDPOINT_ICONS, RESOURCE_ICONS, APP_ICONS, type IconDef } from "./icons";
import { Sqlserver, K8S, Aws, Azure, MissionControl, MissionControlLogo } from "@flanksource/icons/mi";
import { Icon } from "@flanksource/icons/icon";
import vscodeIcons from "@iconify-json/vscode-icons/icons.json";

function VscodeIcon({ name, size = 20 }: { name: string; size?: number }) {
  const iconName = name.replace("vscode-icons:", "");
  const iconData = (vscodeIcons as any).icons[iconName];
  if (!iconData) return null;
  const w = iconData.width || (vscodeIcons as any).width || 32;
  const h = iconData.height || (vscodeIcons as any).height || 32;
  return <svg viewBox={`0 0 ${w} ${h}`} width={size} height={size} dangerouslySetInnerHTML={{ __html: iconData.body }} />;
}


function SvgIcon({ icon, size = 14 }: { icon: IconDef; size?: number }) {
  return <svg viewBox={icon.viewBox} width={size} height={size} dangerouslySetInnerHTML={{ __html: icon.body }} />;
}

function svgIconComponent(icon: IconDef): React.ComponentType<{ className?: string }> {
  return ({ className }: { className?: string }) => (
    <svg viewBox={icon.viewBox} width="14" height="14" className={className} dangerouslySetInnerHTML={{ __html: icon.body }} />
  );
}

type Severity = "critical" | "high" | "medium" | "low" | "info";
type Platform = "sql-server" | "kubernetes" | "aws" | "azure" | "mission-control";
type Outcome = "safety-switch" | "page-oncall" | "high-ticket" | "low-ticket" | "informational";
interface Identity { name: string; type: string; displayName?: string }
interface Endpoint { ip?: string; hostname?: string; type?: string; network?: string; tags?: string[] }
interface AppRef { name: string; type?: string; tags?: string[] }
interface Resource { name: string; type: string; scope?: string; tags?: string[] }
interface Actor { identity?: Identity; endpoint?: Endpoint; app?: AppRef; resource?: Resource }
interface AuditSample { timestamp: string; action: string; detail?: string; succeeded?: boolean; src?: Actor; dst?: Actor }
interface FileInfo {
  name?: string; size?: string; created?: string; modified?: string;
  location?: string; host?: string;
}
interface DataSource {
  type?: string; categories?: string[]; connection?: string; path?: string; query?: string;
  timeRange?: { start: string; end: string; durationSeconds?: number };
  git?: { sha?: string; repo?: string; file?: string; lineNo?: number; branch?: string; tag?: string };
  contentSha?: string;
  app?: { name?: string; version?: string; icon?: string };
  file?: FileInfo;
}
interface AuditFinding {
  title: string; severity: Severity; platform: Platform; category: string; outcome: Outcome;
  detection: { pattern: string; threshold?: string };
  dataSource?: DataSource;
  evidence: {
    summary: string;
    timeRange?: { start: string; end: string; durationSeconds?: number };
    metrics?: Record<string, number>;
    samples?: AuditSample[];
  };
  recommendation: { action: string; mitigations?: string[]; references?: string[] };
  context?: {
    killChainPhase?: string; mitreTechnique?: string; compliance?: string[];
    relatedFindings?: string[];
    baseline?: { normalValue?: number; observedValue?: number; deviationFactor?: number; baselinePeriod?: string };
  };
  provenance?: { generatedAt?: string; generatedBy?: string; version?: string; runId?: string; model?: string };
}

const SEVERITY_STYLES: Record<Severity, { className: string; dot: string; border: string; order: number; color: "red" | "orange" | "yellow" | "blue" | "gray" }> = {
  critical: { className: "bg-red-50 text-red-700", dot: "bg-red-500", border: "border-red-200", order: 0, color: "red" },
  high: { className: "bg-orange-50 text-orange-700", dot: "bg-orange-500", border: "border-orange-200", order: 1, color: "orange" },
  medium: { className: "bg-yellow-50 text-yellow-700", dot: "bg-yellow-500", border: "border-yellow-200", order: 2, color: "yellow" },
  low: { className: "bg-blue-50 text-blue-700", dot: "bg-blue-400", border: "border-blue-200", order: 3, color: "blue" },
  info: { className: "bg-gray-50 text-gray-600", dot: "bg-gray-400", border: "border-gray-200", order: 4, color: "gray" },
};

const OUTCOME_STYLES: Record<Outcome, { className: string; icon: React.ComponentType<{ className?: string }>; label: string }> = {
  "safety-switch": { className: "bg-red-900/10 text-red-900 border border-red-900/20", icon: svgIconComponent(OUTCOME_ICONS["safety-switch"]), label: "Kill Switch" },
  "page-oncall": { className: "bg-red-50 text-red-500 border border-red-200", icon: svgIconComponent(OUTCOME_ICONS["page-oncall"]), label: "Page On-Call" },
  "high-ticket": { className: "bg-orange-50 text-orange-600 border border-orange-200", icon: svgIconComponent(OUTCOME_ICONS["high-ticket"]), label: "High Priority" },
  "low-ticket": { className: "bg-yellow-50 text-yellow-600 border border-yellow-200", icon: svgIconComponent(OUTCOME_ICONS["low-ticket"]), label: "Track Issue" },
  "informational": { className: "bg-gray-50 text-gray-500 border border-gray-200", icon: svgIconComponent(OUTCOME_ICONS["informational"]), label: "Log Only" },
};
const PLATFORM_LABELS: Record<Platform, string> = {
  "sql-server": "SQL Server", kubernetes: "Kubernetes", aws: "AWS", azure: "Azure", "mission-control": "Mission Control",
};
const PLATFORM_ICONS: Record<Platform, React.ComponentType<{ className?: string }>> = {
  "sql-server": Sqlserver, kubernetes: K8S, aws: Aws, azure: Azure, "mission-control": MissionControl,
};

function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString("en-US", { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}
function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.round(seconds / 3600)}h`;
  return `${Math.round(seconds / 86400)}d`;
}
function formatKey(key: string): string {
  return key.replace(/([A-Z])/g, " $1").replace(/^./, (s) => s.toUpperCase());
}

interface BadgeProps { label?: string; icon?: React.ComponentType<{ className?: string }>; className?: string; size?: "xs" | "sm" }

function Badge({ label, icon: Icon, className = "bg-gray-100 text-gray-500", size = "sm" }: BadgeProps) {
  return (
    <span className={`inline-flex items-center font-medium whitespace-nowrap border rounded-full text-xs ${size === "xs" ? "px-1 gap-1" : "px-2 py-0.5 gap-1"} ${className}`}>
      {Icon && <Icon className="w-3 h-3" />}
      {label && <span>{label}</span>}
    </span>
  );
}

function severityBadge(s: Severity) {
  return { label: s, className: SEVERITY_STYLES[s].className, dot: SEVERITY_STYLES[s].dot };
}

const CATEGORY_ICON_COMPONENTS: Record<string, React.ComponentType<{ className?: string }>> = Object.fromEntries(
  Object.entries(CATEGORY_ICONS).map(([k, v]) => [k, svgIconComponent(v)])
);

function findingSubtitleTags(f: AuditFinding): BadgeProps[] {
  const tags: BadgeProps[] = [
    { label: f.category, className: "bg-gray-100 text-gray-500", icon: CATEGORY_ICON_COMPONENTS[f.category] },
    { label: PLATFORM_LABELS[f.platform], className: "bg-gray-100 text-gray-500", icon: PLATFORM_ICONS[f.platform] },
  ];
  if (f.context?.killChainPhase) {
    const kcIcon = KILL_CHAIN_ICONS[f.context.killChainPhase];
    tags.push({ label: f.context.killChainPhase, className: "bg-purple-50 text-purple-600", icon: kcIcon ? svgIconComponent(kcIcon) : undefined });
  }
  if (f.context?.mitreTechnique) tags.push({ label: `MITRE ${f.context.mitreTechnique}`, className: "bg-purple-50 text-purple-600" });
  return tags;
}

function findingComplianceTags(f: AuditFinding): BadgeProps[] {
  return [
    ...(f.context?.compliance?.map((c) => ({ label: c, className: "bg-gray-50 text-gray-500" })) || []),
    ...(f.context?.relatedFindings?.map((r) => ({ label: `→ ${r}`, className: "font-mono bg-gray-50 text-gray-500" })) || []),
  ];
}

type EntityEntry = { name: string; type: string; scope?: string; className?: string; icon?: React.ComponentType<{ className?: string }> };

function iconFor(map: Record<string, IconDef>, key: string): React.ComponentType<{ className?: string }> | undefined {
  const def = map[key];
  return def ? svgIconComponent(def) : undefined;
}
function findingEntities(f: AuditFinding): EntityEntry[] {
  const seen = new Set<string>();
  const entities: EntityEntry[] = [];
  for (const s of f.evidence.samples || []) {
    for (const actor of [s.src, s.dst].filter(Boolean) as Actor[]) {
      if (actor.identity && !seen.has(actor.identity.name)) {
        seen.add(actor.identity.name);
        entities.push({ name: actor.identity.displayName || actor.identity.name, type: actor.identity.type, className: "font-mono bg-gray-100 text-gray-700", icon: iconFor(IDENTITY_ICONS, actor.identity.type) });
      }
      if (actor.endpoint?.ip && !seen.has(actor.endpoint.ip)) {
        seen.add(actor.endpoint.ip);
        entities.push({ name: actor.endpoint.ip, type: actor.endpoint.type || "ip", className: "font-mono bg-gray-100 text-gray-700", icon: iconFor(ENDPOINT_ICONS, actor.endpoint.type || "ip") });
      }
      if (actor.app && !seen.has(actor.app.name)) {
        seen.add(actor.app.name);
        entities.push({ name: actor.app.name, type: actor.app.type || "app", className: "bg-indigo-50 text-indigo-700", icon: iconFor(APP_ICONS, actor.app.type || "default") });
      }
      if (actor.resource && !seen.has(actor.resource.name)) {
        seen.add(actor.resource.name);
        entities.push({ name: actor.resource.name, type: actor.resource.type, scope: actor.resource.scope, className: "bg-blue-50 text-blue-700", icon: iconFor(RESOURCE_ICONS, actor.resource.type) });
      }
    }
  }
  return entities;
}

function findingMetrics(f: AuditFinding): Record<string, string | number> | undefined {
  const m: Record<string, string | number> = { ...f.evidence.metrics };
  if (f.context?.baseline?.deviationFactor) m["Deviation"] = `${f.context.baseline.deviationFactor}x`;
  return Object.keys(m).length > 0 ? m : undefined;
}

function formatShortDateTime(iso: string): string {
  return new Date(iso).toLocaleString("en-US", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

function ActorPart({ icon, text, cls }: { icon?: React.ComponentType<{ className?: string }>; text: string; cls: string }) {
  return <span className={`inline-flex items-center gap-0.5 ${cls}`}>{icon && React.createElement(icon, { className: "w-3 h-3" })}{text}</span>;
}
function ActorCell({ actor }: { actor?: Actor }) {
  if (!actor) return null;
  const parts: React.ReactNode[] = [];
  if (actor.identity) parts.push(<ActorPart key="id" icon={iconFor(IDENTITY_ICONS, actor.identity.type)} text={actor.identity.name} cls="font-mono" />);
  if (actor.endpoint) parts.push(<ActorPart key="ep" icon={iconFor(ENDPOINT_ICONS, actor.endpoint.type || "ip")} text={actor.endpoint.hostname || actor.endpoint.ip || ""} cls="text-gray-400 font-mono" />);
  if (actor.app) parts.push(<ActorPart key="app" icon={iconFor(APP_ICONS, actor.app.type || "default")} text={actor.app.name} cls="text-indigo-500" />);
  if (actor.resource) parts.push(<ActorPart key="res" icon={iconFor(RESOURCE_ICONS, actor.resource.type)} text={actor.resource.name} cls="text-blue-600 font-mono" />);
  return <>{parts.map((p, i) => <React.Fragment key={i}>{i > 0 && <span className="text-gray-300 mx-0.5">·</span>}{p}</React.Fragment>)}</>;
}

function EvidenceRows({ samples }: { samples: AuditSample[] }) {
  const hasSrc = samples.some((s) => s.src);
  const hasDst = samples.some((s) => s.dst);
  const hasOk = samples.some((s) => s.succeeded != null);
  const colCount = 2 + (hasSrc ? 1 : 0) + (hasDst ? 1 : 0) + (hasOk ? 1 : 0);
  const th = "text-left pr-2 py-0.5 font-semibold";
  return (
    <table className="w-full text-xs border-collapse">
      <thead>
        <tr className="text-[8pt] text-gray-400 uppercase tracking-wide">
          <th className={th}>Time</th>
          {hasSrc && <th className={th}>Source</th>}
          <th className={th}>Action</th>
          {hasDst && <th className={th}>Destination</th>}
          {hasOk && <th className={`${th} pr-0`}>OK</th>}
        </tr>
      </thead>
      <tbody>
        {samples.map((s, i) => (
          <React.Fragment key={i}>
            <tr className="border-b border-gray-50">
              <td className="text-gray-400 font-mono pr-2 py-0.5 whitespace-nowrap">{formatShortDateTime(s.timestamp)}</td>
              {hasSrc && <td className="pr-2 py-0.5"><ActorCell actor={s.src} /></td>}
              <td className="text-gray-500 pr-2 py-0.5">{s.action}</td>
              {hasDst && <td className="pr-2 py-0.5"><ActorCell actor={s.dst} /></td>}
              {hasOk && <td className={`py-0.5 ${s.succeeded ? "text-green-600" : "text-red-500"}`}>{s.succeeded != null ? (s.succeeded ? "✓" : "✗") : ""}</td>}
            </tr>
            {s.detail && (
              <tr className="border-b border-gray-50">
                <td colSpan={colCount} className="text-gray-400 font-mono py-0.5 break-all">{s.detail}</td>
              </tr>
            )}
          </React.Fragment>
        ))}
      </tbody>
    </table>
  );
}

interface FindingProps {
  id: string; title: string; summary: string; className?: string;
  severity: { label: string; className?: string; dot?: string };
  subtitleTags?: BadgeProps[]; complianceTags?: BadgeProps[];
  timeRange?: { start: string; end: string; durationSeconds?: number };
  metrics?: Record<string, string | number>; entities?: EntityEntry[];
  samples?: AuditSample[]; recommendation?: string; mitigations?: string[];
}

function Finding({ id, title, summary, severity, subtitleTags, complianceTags, className,
  timeRange, metrics, entities, samples, recommendation, mitigations }: FindingProps) {
  return (
    <section aria-label={title} className={`mb-3 break-inside-avoid rounded-md p-3 border ${className || "border-gray-200"}`}>
      <div className="flex items-center gap-2 pb-0.5 flex-nowrap">
        <span className="inline-flex items-center font-mono text-xs bg-gray-50 text-gray-500 rounded-xs px-1 shrink-0 border border-gray-200">{id}</span>
        <h3 className="text-base font-semibold text-gray-900 flex-1 min-w-0">{title}</h3>
        <Badge label={severity.label} className={severity.className} />
      </div>
      {subtitleTags && subtitleTags.length > 0 && (
        <div className="flex flex-wrap gap-1 mb-1.5">
          {subtitleTags.map((t, i) => <Badge key={i} {...t} size="xs" />)}
        </div>
      )}
      <p className="text-xs text-gray-700 leading-relaxed mb-1.5">{summary}</p>
      {entities && entities.length > 0 && (
        <div className="mb-1.5">
          <span className="text-[8pt] font-semibold text-gray-400 uppercase tracking-wide">Affected Assets</span>
          <div className="flex flex-wrap gap-1 mt-0.5">
            {entities.map((e, i) => (
              <FacetBadge key={i} variant="label" size="xs" shape="rounded" icon={e.icon} label={e.type || ""} value={e.name} color={e.className || "bg-gray-100 text-gray-700"} />
            ))}
          </div>
        </div>
      )}
      {(samples?.length || timeRange || metrics) && (
        <div>
          <div className="flex items-center flex-wrap gap-2">
            <span className="text-[8pt] font-semibold text-gray-400 uppercase tracking-wide">Evidence</span>
            {timeRange && (
              <span className="text-xs text-gray-400">
                {formatDateTime(timeRange.start)} — {formatDateTime(timeRange.end)}
                {timeRange.durationSeconds != null && ` (${formatDuration(timeRange.durationSeconds)})`}
              </span>
            )}
            {metrics && Object.entries(metrics).filter(([, v]) => v != null).map(([key, val]) => (
              <span key={key} className="text-xs text-gray-400">{formatKey(key)} <span className="text-gray-600">{typeof val === "number" ? val.toLocaleString() : String(val)}</span></span>
            ))}
          </div>
          {samples && samples.length > 0 && <EvidenceRows samples={samples} />}
        </div>
      )}
      {recommendation && (
        <div className="bg-gray-50 border-l-2 border-l-amber-500 border-y border-r border-gray-200 rounded-r px-3 py-2">
          <span className="text-[8pt] font-bold text-gray-700 uppercase tracking-wide">Recommended Action</span>
          <p className="text-xs text-gray-900 mt-0.5 font-medium">{recommendation}</p>
          {mitigations && mitigations.length > 0 && (
            <ol className="mt-0.5 ml-4 list-decimal">
              {mitigations.map((m, i) => <li key={i} className="text-xs text-gray-700 leading-tight pl-0.5">{m}</li>)}
            </ol>
          )}
        </div>
      )}
      {complianceTags && complianceTags.length > 0 && (
        <div className="flex flex-wrap gap-1 mt-1.5">
          {complianceTags.map((t, i) => <Badge key={i} {...t} size="xs" />)}
        </div>
      )}
    </section>
  );
}


function countBy(items: AuditFinding[], key: (f: AuditFinding) => string): { name: string; count: number }[] {
  const map = new Map<string, number>();
  for (const f of items) { const v = key(f); map.set(v, (map.get(v) || 0) + 1); }
  return [...map.entries()].sort((a, b) => b[1] - a[1]).map(([name, count]) => ({ name, count }));
}

function BreakdownTable({ title, rows, iconMap }: {
  title: string;
  rows: { name: string; count: number }[];
  iconMap?: (value: unknown) => React.ReactNode;
}) {
  return (
    <ListTable
      title={title}
      rows={rows}
      subject="name"
      icon={iconMap ? "name" : undefined}
      iconMap={iconMap}
      keys={["count"]}
      size="xs"
    />
  );
}

function dedupDataSources(findings: AuditFinding[]): DataSource[] {
  const seen = new Set<string>();
  return findings.filter((f) => {
    if (!f.dataSource) return false;
    const key = f.dataSource.connection || f.dataSource.path || "";
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  }).map((f) => f.dataSource!);
}

function repoIcon(repo?: string): string {
  if (!repo) return "git";
  if (repo.includes("github")) return "github";
  if (repo.includes("azure") || repo.includes("dev.azure")) return "azure-devops";
  if (repo.includes("gitlab")) return "gitlab";
  if (repo.includes("bitbucket")) return "bitbucket";
  return "git";
}
function gitFileUrl(git: NonNullable<DataSource["git"]>): string | undefined {
  if (!git.repo || !git.file) return undefined;
  const sha = git.sha || git.branch || "main";
  if (git.repo.includes("github")) return `https://${git.repo}/blob/${sha}/${git.file}${git.lineNo ? `#L${git.lineNo}` : ""}`;
  if (git.repo.includes("dev.azure")) return `https://${git.repo}?path=/${git.file}&version=GC${sha}${git.lineNo ? `&line=${git.lineNo}` : ""}`;
  if (git.repo.includes("gitlab")) return `https://${git.repo}/-/blob/${sha}/${git.file}${git.lineNo ? `#L${git.lineNo}` : ""}`;
  return undefined;
}
function fileTypeIcon(path?: string): string {
  if (!path) return "vscode-icons:default-file";
  const ext = path.split(".").pop()?.toLowerCase();
  if (ext === "xlsx" || ext === "xls") return "vscode-icons:file-type-excel";
  if (ext === "csv") return "vscode-icons:file-type-excel2";
  if (ext === "json") return "vscode-icons:file-type-json";
  if (ext === "xml") return "vscode-icons:file-type-xml";
  if (ext === "parquet") return "vscode-icons:file-type-sql";
  if (ext === "yaml" || ext === "yml") return "vscode-icons:file-type-yaml";
  if (ext === "sql" || ext === "sqlaudit") return "vscode-icons:file-type-sql";
  if (ext === "log") return "vscode-icons:file-type-log";
  if (ext === "pdf") return "vscode-icons:file-type-pdf2";
  if (ext === "sqlite" || ext === "db") return "vscode-icons:file-type-sqlite";
  return "vscode-icons:default-file";
}
function locationIcon(loc?: string): string {
  if (!loc) return "server";
  if (loc === "sharepoint") return "sharepoint";
  if (loc === "google-drive") return "google-drive";
  if (loc === "onedrive") return "onedrive";
  if (loc === "network-share") return "server";
  return "server";
}
const CATEGORY_BADGE: Record<string, string> = {
  "ai": "bg-purple-50 text-purple-600 border-purple-200",
  "users": "bg-blue-50 text-blue-600 border-blue-200",
  "groups": "bg-blue-50 text-blue-600 border-blue-200",
  "roles": "bg-blue-50 text-blue-600 border-blue-200",
  "access-logs": "bg-amber-50 text-amber-600 border-amber-200",
  "audit-logs": "bg-amber-50 text-amber-600 border-amber-200",
  "flow-logs": "bg-cyan-50 text-cyan-600 border-cyan-200",
  "configuration": "bg-gray-100 text-gray-600 border-gray-200",
};
function DataSourceCard({ ds }: { ds: DataSource }) {
  const isFile = ds.type === "file" || ds.file;
  const fileName = ds.file?.name || ds.path?.split("/").pop();
  const typeIcon = isFile ? fileTypeIcon(ds.file?.name || ds.path) : (ds.type || "database");
  const url = ds.git ? gitFileUrl(ds.git) : undefined;
  const fullGitPath = ds.git ? [ds.git.repo, ds.git.file].filter(Boolean).join("/") + (ds.git.lineNo ? `:${ds.git.lineNo}` : "") : undefined;
  const DsTypeIcon = typeIcon.startsWith("vscode-icons:")
    ? <VscodeIcon name={typeIcon} size={20} />
    : <Icon name={typeIcon} className="w-5 h-5 text-gray-600 shrink-0" />;
  return (
    <div className="border border-gray-200 rounded-lg p-3 bg-gray-50 mb-2">
      <div className="flex items-center gap-2 mb-1">
        {DsTypeIcon}
        {isFile && fileName
          ? <span className="text-sm font-semibold text-gray-800">{fileName}</span>
          : <span className="text-sm font-semibold text-gray-800">{ds.type || "file"}</span>}
        {ds.connection && <span className="text-xs text-gray-500 font-mono">{ds.connection}</span>}
        {ds.categories?.map((c) => (
          <FacetBadge key={c} variant="custom" size="sm" shape="pill" label={c} className={CATEGORY_BADGE[c] || "bg-gray-100 text-gray-500 border-gray-200"} />
        ))}
        <span className="flex-1" />
        {ds.app && (
          <span className="flex items-center gap-1 text-xs text-gray-500">
            {ds.app.icon && <Icon name={ds.app.icon} className="w-3.5 h-3.5" />}
            {ds.app.name}{ds.app.version && ` v${ds.app.version}`}
          </span>
        )}
      </div>
      {ds.path && <div className="text-xs text-gray-500 font-mono">{ds.path}</div>}
      {ds.file && (
        <div className="flex items-center flex-wrap gap-2 mt-1 text-xs text-gray-500">
          {ds.file.location && (
            <span className="flex items-center gap-0.5"><Icon name={locationIcon(ds.file.location)} className="w-3 h-3" />{ds.file.host || ds.file.location}</span>
          )}
          {ds.file.size && <span>{ds.file.size}</span>}
          {ds.file.created && <span>Created {formatDateTime(ds.file.created)}</span>}
          {ds.file.modified && <span>Modified {formatDateTime(ds.file.modified)}</span>}
        </div>
      )}
      {ds.git && (
        <div className="flex items-center flex-wrap gap-1.5 mt-1.5">
          <Icon name={repoIcon(ds.git.repo)} className="w-3.5 h-3.5 text-gray-500" />
          {fullGitPath && (url
            ? <a href={url} className="text-xs font-mono text-blue-600 underline">{fullGitPath}</a>
            : <span className="text-xs font-mono text-gray-600">{fullGitPath}</span>
          )}
          {ds.git.sha && <span className="text-xs font-mono bg-amber-50 border border-amber-200 rounded px-1 py-px text-amber-700">{ds.git.sha}</span>}
          {ds.git.tag && <span className="text-xs font-mono bg-blue-50 border border-blue-200 rounded px-1 py-px text-blue-600">{ds.git.tag}</span>}
          {ds.git.branch && <span className="text-xs font-mono bg-green-50 border border-green-200 rounded px-1 py-px text-green-600">{ds.git.branch}</span>}
        </div>
      )}
      {ds.contentSha && (
        <div className="flex items-center gap-1 mt-1 text-xs font-mono text-gray-400">
          <Icon name="shield-lock" className="w-3 h-3 shrink-0" />
          <span>sha256:{ds.contentSha}</span>
        </div>
      )}
      {ds.query && <div className="text-xs font-mono text-gray-500 bg-white border border-gray-200 rounded p-1.5 mt-1.5 break-all leading-tight">{ds.query}</div>}
    </div>
  );
}
function DataSourcesList({ findings }: { findings: AuditFinding[] }) {
  const sources = dedupDataSources(findings);
  const p = findings.find((f) => f.provenance)?.provenance;
  const aiVendorIcon = (model?: string): string => {
    if (!model) return "brain";
    const m = model.toLowerCase();
    if (m.includes("claude") || m.includes("anthropic")) return "claude";
    if (m.includes("gemini")) return "gemini";
    if (m.includes("gpt") || m.includes("openai") || m.includes("chatgpt")) return "openai";
    if (m.includes("ollama")) return "ollama";
    if (m.includes("mistral")) return "mistral";
    return "brain";
  };
  const aiCard: DataSource | null = p?.model ? {
    type: aiVendorIcon(p.model), categories: ["ai"],
    app: { name: p.generatedBy || "audit-log-analyzer", version: p.version, icon: aiVendorIcon(p.model) },
    connection: p.model,
    path: p.runId ? `Run: ${p.runId}` : undefined,
  } : null;
  return (
    <div className="flex flex-col">
      {aiCard && <DataSourceCard ds={aiCard} />}
      {sources.map((ds, i) => <DataSourceCard key={i} ds={ds} />)}
    </div>
  );
}

function SummaryContent({ findings }: { findings: AuditFinding[] }) {
  const criticalCount = findings.filter((f) => f.severity === "critical").length;
  const highCount = findings.filter((f) => f.severity === "high").length;
  const mediumCount = findings.filter((f) => f.severity === "medium").length;

  const outcomeCounts = countBy(findings, (f) => OUTCOME_STYLES[f.outcome].label);
  const platformCounts = countBy(findings, (f) => PLATFORM_LABELS[f.platform]);
  const categoryCounts = countBy(findings, (f) => f.category);

  return (
    <>
      <div className="mb-6">
        <h1 className="text-2xl font-bold text-gray-900">Audit Findings Report</h1>
        <p className="text-sm text-gray-500 mt-1">
          Generated {new Date().toLocaleDateString("en-ZA", { dateStyle: "long" })} · {findings.length} findings
        </p>
      </div>

      <div className="grid grid-cols-4 gap-4 mb-8">
        <SeverityStatCard color="red" value={criticalCount} label="Critical" />
        <SeverityStatCard color="orange" value={highCount} label="High" />
        <SeverityStatCard color="yellow" value={mediumCount} label="Medium" />
        <SeverityStatCard color="gray" value={findings.length} label="Total" />
      </div>

      <div className="grid grid-cols-2 gap-6 mb-8">
        <BreakdownTable title="By Outcome" rows={outcomeCounts} iconMap={(v) => {
          const o = Object.entries(OUTCOME_STYLES).find(([, s]) => s.label === v);
          return o ? <SvgIcon icon={OUTCOME_ICONS[o[0]]} /> : null;
        }} />
        <BreakdownTable title="By Platform" rows={platformCounts} />
        <BreakdownTable title="By Category" rows={categoryCounts} iconMap={(v) => {
          const icon = CATEGORY_ICONS[v as string];
          return icon ? <SvgIcon icon={icon} /> : null;
        }} />
      </div>

    </>
  );
}

export default function FindingsReport(props: Record<string, unknown>) {
  const data = (props.data ?? props) as Record<string, unknown>;
  const findings: AuditFinding[] = Array.isArray(data.findings) ? data.findings
    : Array.isArray(data) ? data : [];
  const sorted = [...findings].sort((a, b) => SEVERITY_STYLES[a.severity].order - SEVERITY_STYLES[b.severity].order);

  const reportDate = new Date().toLocaleDateString("en-ZA", { dateStyle: "long" });
  return (
    <Document title={`Audit Findings Report - ${reportDate}`} margins={{ top: 5, bottom: 5, left: 5, right: 5 }}>
      <Header type="default" height={12}>
        <div className="flex items-center justify-between px-3 h-full bg-[#1e293b]">
          <MissionControlLogo className="filter grayscale brightness-[250] contrast-100 mix-blend-screen h-[5mm] w-auto" />
          <span className="text-[9pt] text-white/80">Audit Findings Report</span>
        </div>
      </Header>
      <Footer type="default" height={8}>
        <div className="flex items-center justify-between px-4 h-full border-t border-gray-200 text-[7pt] text-gray-400">
          <span className="uppercase tracking-wide font-semibold">Confidential</span>
          <span>{reportDate}</span>
          <PageNo format="Page ${page} of ${total}" />
        </div>
      </Footer>
      <Page >
        <SummaryContent findings={findings} />
      </Page>
      <Page title="Table of Contents">
        {sorted.map((f, i) => (
          <div key={i} className="flex items-center gap-2 text-xs py-0.5 border-b border-gray-50">
            <span className="text-gray-400 font-mono shrink-0">#{i + 1}</span>
            <Badge label={f.severity} className={SEVERITY_STYLES[f.severity].className} size="xs" />
            <span className="text-gray-800 flex-1">{f.title}</span>
            <span className="text-gray-400">{PLATFORM_LABELS[f.platform]}</span>
          </div>
        ))}
      </Page>
      <Page title="Data Sources">
        <DataSourcesList findings={findings} />
      </Page>
      <Page title="Detailed Findings">
        {sorted.map((f, i) => (
          <Finding key={i} id={`#${i + 1}`} title={f.title} summary={f.evidence.summary}
            className={SEVERITY_STYLES[f.severity].border} severity={severityBadge(f.severity)}
            subtitleTags={findingSubtitleTags(f)}
            complianceTags={findingComplianceTags(f)} timeRange={f.evidence.timeRange}
            metrics={findingMetrics(f)} entities={findingEntities(f)} samples={f.evidence.samples}
            recommendation={f.recommendation.action} mitigations={f.recommendation.mitigations} />
        ))}
      </Page>
    </Document>
  );
}
