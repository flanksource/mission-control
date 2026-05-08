import React from 'react';
import { Section, SeverityStatCard, Badge, CompactTable } from '@flanksource/facet';
import type { ApplicationFinding } from '../types.ts';
import { formatDate } from './utils.ts';

interface Props {
  findings: ApplicationFinding[];
}

const SEVERITY_ORDER = ['critical', 'high', 'medium', 'low'] as const;
const FINDING_TYPES = ['security', 'compliance', 'reliability', 'performance'] as const;
const ACTIVE_STATUSES = new Set(['open', 'in-progress']);
const NO_BREAK_STYLE = { pageBreakInside: 'avoid' as const, breakInside: 'avoid' as const };

const SEVERITY_COLOR: Record<string, 'red' | 'orange' | 'yellow' | 'blue'> = {
  critical: 'red',
  high: 'orange',
  medium: 'yellow',
  low: 'blue',
};

const SEVERITY_BADGE_STATUS: Record<string, 'error' | 'warning' | 'info'> = {
  critical: 'error',
  high: 'warning',
  medium: 'warning',
  low: 'info',
};

function severityBadge(severity: string) {
  return (
    <Badge
      variant="status"
      status={SEVERITY_BADGE_STATUS[severity] ?? 'info'}
      value={severity}
      size="xs"
      shape="rounded"
    />
  );
}

function TypeGroup({ type, findings }: { type: string; findings: ApplicationFinding[] }) {
  if (findings.length === 0) return null;

  const sorted = [...findings].sort((a, b) => {
    const statusOrder = ['open', 'in-progress', 'accepted', 'resolved'];
    const statusDiff = statusOrder.indexOf(a.status) - statusOrder.indexOf(b.status);
    if (statusDiff !== 0) return statusDiff;
    return SEVERITY_ORDER.indexOf(a.severity as any) - SEVERITY_ORDER.indexOf(b.severity as any);
  });

  const hasAnyRemediation = sorted.some((f) => ACTIVE_STATUSES.has(f.status) && !!f.remediation);
  const columns = ['Title', 'Severity', 'Status', 'Last Seen', ...(hasAnyRemediation ? ['Remediation'] : [])];
  const rows = sorted.map((f) => [
    f.title,
    severityBadge(f.severity),
    f.status,
    formatDate(f.lastObserved),
    ...(hasAnyRemediation ? [ACTIVE_STATUSES.has(f.status) ? (f.remediation ?? '') : ''] : []),
  ]);

  return (
    <div className="mb-[4mm]">
      <div className="flex items-center gap-[6px] mb-[2mm]">
        <span className="text-[9pt] font-semibold text-slate-800 capitalize">{type}</span>
        <span className="text-[7.5pt] text-gray-500 bg-gray-100 px-[6px] py-[1px] rounded-full">{findings.length}</span>
      </div>
      <CompactTable variant="reference" columns={columns} data={rows} />
    </div>
  );
}

export default function FindingsSection({ findings }: Props) {
  const bySeverity = Object.fromEntries(
    SEVERITY_ORDER.map((sev) => [sev, findings.filter((f) => f.severity === sev).length])
  );
  const byType = Object.fromEntries(
    FINDING_TYPES.map((type) => [type, findings.filter((f) => f.type === type)])
  );

  return (
    <Section variant="hero" title="Security Findings" size="md">
      <div className="grid grid-cols-4 gap-[3mm] mb-[5mm]" style={NO_BREAK_STYLE}>
        {SEVERITY_ORDER.map((sev) => (
          <div key={sev} style={NO_BREAK_STYLE}>
            <SeverityStatCard
              color={SEVERITY_COLOR[sev]}
              value={bySeverity[sev]}
              label={sev.charAt(0).toUpperCase() + sev.slice(1)}
            />
          </div>
        ))}
      </div>
      {findings.length === 0 ? (
        <p className="text-[9pt] text-gray-500 italic">No findings recorded.</p>
      ) : (
        FINDING_TYPES.map((type) => (
          <TypeGroup key={type} type={type} findings={byType[type]} />
        ))
      )}
    </Section>
  );
}
