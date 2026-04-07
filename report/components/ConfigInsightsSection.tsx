import React from 'react';
import { Section, SeverityStatCard } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { ConfigAnalysis, ConfigSeverity, AnalysisType } from '../config-types.ts';
import { formatDate } from './utils.ts';

interface Props {
  analyses?: ConfigAnalysis[];
}

const SEVERITY_ORDER: ConfigSeverity[] = ['critical', 'high', 'medium', 'low', 'info'];
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

function InsightEntry({ analysis }: { analysis: ConfigAnalysis }) {
  const sev = analysis.severity ?? 'info';
  return (
    <div className="flex items-center gap-[1.5mm] py-[0.3mm] border-b border-gray-50 last:border-b-0 text-xs">
      <span className="w-[3.5mm] h-[3.5mm] shrink-0 flex items-center justify-center">
        <Icon name={analysis.analysisType || analysis.analyzer} size={10} />
      </span>
      <span className="font-medium text-slate-800 whitespace-nowrap">{analysis.analyzer}</span>
      {analysis.configName && (
        <span className="text-xs text-blue-600 bg-blue-50 px-[0.5mm] rounded whitespace-nowrap shrink-0">{analysis.configName}</span>
      )}
      <span className="text-gray-600 leading-tight flex-1 truncate">{analysis.message || analysis.summary || '-'}</span>
      <span className={`text-xs leading-none px-[0.5mm] py-[0.15mm] rounded border font-semibold whitespace-nowrap shrink-0 ${SEVERITY_TEXT[sev] ?? SEVERITY_TEXT.info}`}>
        {sev}
      </span>
      {analysis.status && (
        <span className={`text-xs leading-none px-[0.5mm] py-[0.15mm] rounded border font-semibold whitespace-nowrap shrink-0 ${STATUS_TEXT[analysis.status] ?? STATUS_TEXT.resolved}`}>
          {analysis.status}
        </span>
      )}
      {analysis.lastObserved && (
        <span className="text-xs text-gray-400 whitespace-nowrap shrink-0">{formatDate(analysis.lastObserved)}</span>
      )}
    </div>
  );
}

function AnalysisTypeGroup({ type, analyses }: { type: string; analyses: ConfigAnalysis[] }) {
  if (analyses.length === 0) return null;

  const sorted = [...analyses].sort((a, b) => {
    const statusOrder = ['open', 'silenced', 'resolved'];
    const statusDiff = statusOrder.indexOf(a.status ?? '') - statusOrder.indexOf(b.status ?? '');
    if (statusDiff !== 0) return statusDiff;
    return SEVERITY_ORDER.indexOf(a.severity as ConfigSeverity) - SEVERITY_ORDER.indexOf(b.severity as ConfigSeverity);
  });

  return (
    <div className="mb-[2mm]">
      <div className="flex items-center gap-[1.5mm] mb-[0.5mm]">
        <span className="text-xs font-semibold text-slate-800 capitalize">{type}</span>
        <span className="text-xs text-gray-500 bg-gray-100 px-[1mm] rounded-full">
          {analyses.length}
        </span>
      </div>
      <div className="flex flex-col">
        {sorted.map((a) => <InsightEntry key={a.id} analysis={a} />)}
      </div>
    </div>
  );
}

export default function ConfigInsightsSection({ analyses }: Props) {
  if (!analyses?.length) return null;
  const bySeverity = Object.fromEntries(
    SEVERITY_ORDER.map((sev) => [sev, analyses.filter((a) => (a.severity ?? 'info') === sev).length])
  );
  const byType = Object.fromEntries(
    ANALYSIS_TYPES.map((t) => [t, analyses.filter((a) => a.analysisType === t)])
  );

  return (
    <Section variant="hero" title="Config Insights" size="md">
      <div className="flex gap-[2mm] mb-[2mm]">
        {SEVERITY_ORDER.map((sev) => (
          <SeverityStatCard
            key={sev}
            color={SEVERITY_COLOR[sev]}
            value={bySeverity[sev]}
            label={sev.charAt(0).toUpperCase() + sev.slice(1)}
          />
        ))}
      </div>
      {ANALYSIS_TYPES.map((type) => (
        <AnalysisTypeGroup key={type} type={type} analyses={byType[type]} />
      ))}
    </Section>
  );
}
