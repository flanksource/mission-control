import React from 'react';
import { Badge, Markdown, Section, SeverityStatCard } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { ConfigAnalysis, ConfigSeverity, AnalysisType } from '../config-types.ts';
import ConfigLink from './ConfigLink.tsx';
import { formatDate } from './utils.ts';

interface Props {
  analyses?: ConfigAnalysis[];
  details?: boolean;
}

const SEVERITY_ORDER: ConfigSeverity[] = ['critical', 'high', 'medium', 'low', 'info'];
const STATUS_ORDER = ['open', 'silenced', 'resolved'];
const NO_BREAK_STYLE = { pageBreakInside: 'avoid' as const, breakInside: 'avoid' as const };
const SEVERITY_COLOR: Record<string, 'red' | 'orange' | 'yellow' | 'blue'> = {
  critical: 'red',
  high: 'orange',
  medium: 'yellow',
  low: 'blue',
  info: 'blue',
};
const SEVERITY_TEXT: Record<string, string> = {
  critical: 'text-red-700 bg-red-50 border-red-200',
  high: 'text-orange-700 bg-orange-50 border-orange-200',
  medium: 'text-yellow-700 bg-yellow-50 border-yellow-200',
  low: 'text-blue-700 bg-blue-50 border-blue-200',
  info: 'text-gray-600 bg-gray-50 border-gray-200',
};
const STATUS_TEXT: Record<string, string> = {
  open: 'text-red-700 bg-red-50 border-red-200',
  silenced: 'text-yellow-700 bg-yellow-50 border-yellow-200',
  resolved: 'text-green-700 bg-green-50 border-green-200',
};

const ANALYSIS_TYPES: AnalysisType[] = [
  'security', 'compliance', 'cost', 'performance',
  'reliability', 'recommendation', 'availability', 'integration',
];

// AffectedResource is one config an insight was raised against, along with
// every finding of that insight on it.
interface AffectedResource {
  key: string;
  name: string;
  type?: string;
  permalink?: string;
  findings: ConfigAnalysis[];
}

// InsightGroup collects every occurrence of one insight across the configs it
// affects. The identity is the source-native analyzer scoped to its source.
interface InsightGroup {
  key: string;
  analyzer: string;
  source?: string;
  summary?: string;
  severity: ConfigSeverity;
  statuses: string[];
  lastObserved?: string;
  resources: AffectedResource[];
}

function severityOf(analysis: ConfigAnalysis): ConfigSeverity {
  return (analysis.severity ?? 'info') as ConfigSeverity;
}

function sourceURL(analysis: ConfigAnalysis): string | undefined {
  const links = analysis.properties?.flatMap((p) => p.links ?? []);
  return links?.find((l) => l.url)?.url;
}

function groupKey(analysis: ConfigAnalysis): string {
  return `${analysis.source ?? ''}/${analysis.analyzer}`;
}

function resourceKey(analysis: ConfigAnalysis): string {
  return analysis.configID || analysis.configName || '';
}

function bucket<T>(buckets: Map<string, T[]>, key: string, value: T) {
  const existing = buckets.get(key);
  if (existing) {
    existing.push(value);
  } else {
    buckets.set(key, [value]);
  }
}

// repeatsOnAResource reports whether any single config carries more than one of
// these insights.
function repeatsOnAResource(analyses: ConfigAnalysis[]): boolean {
  const seen = new Set<string>();
  for (const analysis of analyses) {
    const key = resourceKey(analysis);
    if (seen.has(key)) return true;
    seen.add(key);
  }
  return false;
}

function buildGroup(key: string, analyses: ConfigAnalysis[]): InsightGroup {
  const [first] = analyses;
  const group: InsightGroup = {
    key,
    analyzer: first.analyzer,
    source: first.source,
    severity: severityOf(first),
    statuses: [],
    resources: [],
  };
  const byResource = new Map<string, AffectedResource>();

  for (const analysis of analyses) {
    if (!group.summary) group.summary = analysis.summary;
    if (SEVERITY_ORDER.indexOf(severityOf(analysis)) < SEVERITY_ORDER.indexOf(group.severity)) {
      group.severity = severityOf(analysis);
    }
    if (analysis.status && !group.statuses.includes(analysis.status)) {
      group.statuses.push(analysis.status);
    }
    if (analysis.lastObserved && (!group.lastObserved || analysis.lastObserved > group.lastObserved)) {
      group.lastObserved = analysis.lastObserved;
    }

    const rKey = resourceKey(analysis);
    const resource = byResource.get(rKey) ?? {
      key: rKey,
      name: analysis.configName || analysis.configID || '-',
      type: analysis.configType,
      permalink: analysis.permalink,
      findings: [],
    };
    resource.findings.push(analysis);
    byResource.set(rKey, resource);
  }

  group.statuses.sort((a, b) => STATUS_ORDER.indexOf(a) - STATUS_ORDER.indexOf(b));
  group.resources = [...byResource.values()].sort((a, b) => a.name.localeCompare(b.name));
  return group;
}

function groupInsights(analyses: ConfigAnalysis[]): InsightGroup[] {
  const byAnalyzer = new Map<string, ConfigAnalysis[]>();
  for (const analysis of analyses) {
    bucket(byAnalyzer, groupKey(analysis), analysis);
  }

  const groups: InsightGroup[] = [];
  for (const [key, members] of byAnalyzer) {
    // An analyzer that fires more than once against the same config names a
    // category rather than a finding — a package ecosystem, say. Fall back to
    // the summary there so unrelated advisories don't collapse into one row.
    // Analyzers that fire once per config stay grouped, so a single check keeps
    // listing every config it affects.
    if (!repeatsOnAResource(members)) {
      groups.push(buildGroup(key, members));
      continue;
    }

    const bySummary = new Map<string, ConfigAnalysis[]>();
    for (const analysis of members) {
      bucket(bySummary, analysis.summary ?? '', analysis);
    }
    for (const [summary, subset] of bySummary) {
      groups.push(buildGroup(`${key}/${summary}`, subset));
    }
  }

  return groups.sort((a, b) => {
    const severityDiff = SEVERITY_ORDER.indexOf(a.severity) - SEVERITY_ORDER.indexOf(b.severity);
    if (severityDiff !== 0) return severityDiff;
    const countDiff = b.resources.length - a.resources.length;
    if (countDiff !== 0) return countDiff;
    return a.analyzer.localeCompare(b.analyzer);
  });
}

function FindingRow({ analysis, details }: { analysis: ConfigAnalysis; details?: boolean }) {
  return (
    <div className="flex-1 min-w-0" style={NO_BREAK_STYLE}>
      <div className="flex items-center gap-[1.5mm] text-xs">
        {analysis.status && (
          <Badge variant="custom" size="xs" shape="rounded" label={analysis.status} className={STATUS_TEXT[analysis.status] ?? STATUS_TEXT.resolved} />
        )}
        {!details && analysis.summary && (
          <span className="text-gray-500 leading-tight flex-1 truncate">{analysis.summary}</span>
        )}
        {analysis.lastObserved && (
          <span className="text-xs text-gray-400 whitespace-nowrap shrink-0 ml-auto">{formatDate(analysis.lastObserved)}</span>
        )}
      </div>
      {details && analysis.message && (
        <Markdown className="text-xs text-gray-600 leading-snug">{analysis.message}</Markdown>
      )}
    </div>
  );
}

function ResourceEntry({ resource, details }: { resource: AffectedResource; details?: boolean }) {
  const config = { name: resource.name, type: resource.type };
  const inline = resource.findings.length === 1 && !details;

  return (
    <div className="pl-[5mm] py-[0.3mm]" style={NO_BREAK_STYLE}>
      <div className={inline ? 'flex items-center gap-[1.5mm] text-xs' : 'flex items-center gap-[1.5mm] text-xs mb-[0.3mm]'}>
        <span className="text-gray-300 shrink-0">·</span>
        {resource.permalink
          ? <a href={resource.permalink} className="text-slate-700 underline shrink-0"><ConfigLink config={config} /></a>
          : <span className="text-slate-700 shrink-0"><ConfigLink config={config} /></span>}
        {inline && <FindingRow analysis={resource.findings[0]} details={details} />}
      </div>
      {!inline && (
        <div className="pl-[3mm] flex flex-col gap-[0.3mm]">
          {resource.findings.map((analysis) => (
            <FindingRow key={analysis.id} analysis={analysis} details={details} />
          ))}
        </div>
      )}
    </div>
  );
}

function InsightGroupEntry({ group, details }: { group: InsightGroup; details?: boolean }) {
  const [firstResource] = group.resources;
  const only = group.resources.length === 1 && firstResource.findings.length === 1
    ? firstResource.findings[0]
    : undefined;
  const url = sourceURL(firstResource.findings[0]);
  const headline = group.summary;

  return (
    <div className="border-b border-gray-50 last:border-b-0 py-[0.3mm]" style={only ? NO_BREAK_STYLE : undefined}>
      <div className="flex items-center gap-[1.5mm] text-xs">
        <span className="w-[3.5mm] h-[3.5mm] shrink-0 flex items-center justify-center">
          <Icon name={firstResource.findings[0].analysisType || group.analyzer} size={10} />
        </span>
        {url
          ? <a href={url} className="font-medium text-slate-800 whitespace-nowrap underline">{group.analyzer}</a>
          : <span className="font-medium text-slate-800 whitespace-nowrap">{group.analyzer}</span>}
        {group.source && (
          <Badge variant="custom" size="xs" shape="rounded" label={group.source} color="bg-gray-50" textColor="text-gray-500" borderColor="border-gray-200" />
        )}
        {only && (
          <Badge variant="custom" size="xs" shape="rounded" label={firstResource.name} color="bg-blue-50" textColor="text-blue-600" borderColor="border-blue-200" />
        )}
        <span className="text-gray-600 leading-tight flex-1 truncate">{headline ?? ''}</span>
        <Badge variant="custom" size="xs" shape="rounded" label={group.severity} className={SEVERITY_TEXT[group.severity] ?? SEVERITY_TEXT.info} />
        {group.statuses.map((status) => (
          <Badge key={status} variant="custom" size="xs" shape="rounded" label={status} className={STATUS_TEXT[status] ?? STATUS_TEXT.resolved} />
        ))}
        {!only && (
          <Badge variant="custom" size="xs" shape="pill" label={group.resources.length === 1 ? '1 resource' : `${group.resources.length} resources`} color="bg-blue-50" textColor="text-blue-600" borderColor="border-blue-200" />
        )}
        {group.lastObserved && (
          <span className="text-xs text-gray-400 whitespace-nowrap shrink-0">{formatDate(group.lastObserved)}</span>
        )}
      </div>
      {only
        ? details && only.message && (
          <div className="pl-[5mm] pt-[0.3mm]">
            <Markdown className="text-xs text-gray-600 leading-snug">{only.message}</Markdown>
          </div>
        )
        : (
          <div className="flex flex-col">
            {group.resources.map((resource) => (
              <ResourceEntry key={resource.key} resource={resource} details={details} />
            ))}
          </div>
        )}
    </div>
  );
}

function AnalysisTypeGroup({ type, analyses, details }: { type: string; analyses: ConfigAnalysis[]; details?: boolean }) {
  if (analyses.length === 0) return null;
  const groups = groupInsights(analyses);

  return (
    <div className="mb-[2mm]">
      <div className="flex items-center gap-[1.5mm] mb-[0.5mm]">
        <span className="text-xs font-semibold text-slate-800 capitalize">{type}</span>
        <Badge variant="custom" size="xs" shape="pill" label={String(groups.length)} color="bg-gray-100" textColor="text-gray-500" borderColor="border-gray-200" />
      </div>
      <div className="flex flex-col">
        {groups.map((group) => <InsightGroupEntry key={group.key} group={group} details={details} />)}
      </div>
    </div>
  );
}

export default function ConfigInsightsSection({ analyses, details }: Props) {
  if (!analyses?.length) return null;
  const bySeverity = Object.fromEntries(
    SEVERITY_ORDER.map((sev) => [sev, analyses.filter((a) => severityOf(a) === sev).length])
  );
  const byType: Record<string, ConfigAnalysis[]> = {};
  for (const a of analyses) {
    const t = a.analysisType && ANALYSIS_TYPES.includes(a.analysisType as AnalysisType) ? a.analysisType : 'other';
    (byType[t] ??= []).push(a);
  }
  const typeOrder = [...ANALYSIS_TYPES.filter((t) => byType[t]?.length), ...(byType['other']?.length ? ['other' as const] : [])];

  return (
    <Section variant="hero" title="Config Insights" size="md">
      <div className="flex flex-wrap gap-[2mm] mb-[2mm]" style={NO_BREAK_STYLE}>
        {SEVERITY_ORDER.map((sev) => (
          <div key={sev} className="flex-1 min-w-[20mm]" style={NO_BREAK_STYLE}>
            <SeverityStatCard
              color={SEVERITY_COLOR[sev]}
              value={bySeverity[sev]}
              label={sev.charAt(0).toUpperCase() + sev.slice(1)}
            />
          </div>
        ))}
      </div>
      {typeOrder.map((type) => (
        <AnalysisTypeGroup key={type} type={type} analyses={byType[type]} details={details} />
      ))}
    </Section>
  );
}
