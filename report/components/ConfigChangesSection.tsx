import React from 'react';
import { Section, SeverityStatCard } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { ConfigChange, ConfigSeverity } from '../config-types.ts';
import { formatRelative } from './utils.ts';

interface Props {
  changes: ConfigChange[];
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

function ChangeEntry({ change }: { change: ConfigChange }) {
  const sev = change.severity ?? 'info';
  const author = change.createdBy || change.externalCreatedBy || change.source || '';
  return (
    <div className="flex items-center gap-[1.5mm] py-[0.3mm] border-b border-gray-50 last:border-b-0 text-[6pt]">
      <span className="text-gray-400 font-mono whitespace-nowrap w-[12mm] text-right shrink-0">
        {change.createdAt ? formatRelative(change.createdAt) : '-'}
      </span>
      <span className="w-[3.5mm] h-[3.5mm] shrink-0 flex items-center justify-center">
        <Icon name={change.changeType} size={10} />
      </span>
      <span className="font-medium text-slate-800 whitespace-nowrap">{change.changeType}</span>
      <span className="text-gray-600 leading-tight flex-1 truncate">{change.summary ?? '-'}</span>
      <span className={`text-[4.5pt] leading-none px-[0.5mm] py-[0.15mm] rounded border font-semibold whitespace-nowrap shrink-0 ${SEVERITY_TEXT[sev] ?? SEVERITY_TEXT.info}`}>
        {sev}
      </span>
      {author && <span className="text-[5pt] text-gray-400 whitespace-nowrap shrink-0">{author}</span>}
      {(change.count ?? 0) > 1 && (
        <span className="text-[4.5pt] text-gray-400 bg-gray-100 px-[0.7mm] rounded shrink-0">×{change.count}</span>
      )}
    </div>
  );
}

export default function ConfigChangesSection({ changes }: Props) {
  const bySeverity = Object.fromEntries(
    SEVERITY_ORDER.map((sev) => [sev, changes.filter((c) => (c.severity ?? 'info') === sev).length])
  );

  return (
    <Section variant="hero" title="Config Changes" size="md">
      <div className="flex gap-[2mm] mb-[2mm]">
        {SEVERITY_ORDER.map((sev) => (
          <SeverityStatCard
            key={sev}
            color={SEVERITY_COLOR[sev]}
            value={bySeverity[sev]}
            label={sev.charAt(0).toUpperCase() + sev.slice(1)}
            size="xs"
          />
        ))}
      </div>
      {changes.length === 0 ? (
        <p className="text-[8pt] text-gray-500 italic">No changes recorded.</p>
      ) : (
        <div className="flex flex-col">
          {changes.map((c) => <ChangeEntry key={c.id} change={c} />)}
        </div>
      )}
    </Section>
  );
}
