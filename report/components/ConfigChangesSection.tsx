import React from 'react';
import { Section, SeverityStatCard } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { ConfigChange, ConfigSeverity } from '../config-types.ts';
import { getTimeBucket, formatEntryDate, type TimeBucketFormat } from './utils.ts';

interface Props {
  changes?: ConfigChange[];
  hideConfigName?: boolean;
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

function ChangeEntry({ change, dateFormat, hideConfigName }: { change: ConfigChange; dateFormat: TimeBucketFormat; hideConfigName?: boolean }) {
  const sev = change.severity ?? 'info';
  const author = change.createdBy || change.externalCreatedBy || change.source || '';
  const artifactCount = (change.artifacts || []).length;
  return (
    <div className="flex items-center gap-[1.5mm] py-[0.3mm] border-b border-gray-50 last:border-b-0 text-xs">
      <span className="text-gray-400 font-mono whitespace-nowrap w-[12mm] text-right shrink-0">
        {change.createdAt ? formatEntryDate(change.createdAt, dateFormat) : '-'}
      </span>
      <span className="w-[3.5mm] h-[3.5mm] shrink-0 flex items-center justify-center">
        <Icon name={change.changeType} size={10} />
      </span>
      <span className="font-medium text-slate-800 whitespace-nowrap">{change.changeType}</span>
      {!hideConfigName && change.configName && (
        <span className="text-xs text-blue-600 bg-blue-50 px-[0.5mm] rounded whitespace-nowrap shrink-0">{change.configName}</span>
      )}
      <span className="text-gray-600 leading-tight flex-1 truncate">{change.summary ?? '-'}</span>
      {sev !== 'info' && (
        <span className={`text-xs leading-none px-[0.5mm] py-[0.15mm] rounded border font-semibold whitespace-nowrap shrink-0 ${SEVERITY_TEXT[sev] ?? SEVERITY_TEXT.info}`}>
          {sev}
        </span>
      )}
      {author && <span className="text-xs text-gray-400 whitespace-nowrap shrink-0">{author}</span>}
      {(change.count ?? 0) > 1 && (
        <span className="text-xs text-gray-400 bg-gray-100 px-[0.7mm] rounded shrink-0">×{change.count}</span>
      )}
      {artifactCount > 0 && (
        <a href={`#artifact-${change.id}`} className="text-xs text-purple-600 bg-purple-50 border border-purple-200 px-[0.7mm] rounded shrink-0" style={{ textDecoration: 'none' }}>
          {artifactCount} screenshot{artifactCount > 1 ? 's' : ''} →
        </a>
      )}
    </div>
  );
}

interface BucketGroup {
  key: string;
  label: string;
  dateFormat: TimeBucketFormat;
  changes: ConfigChange[];
}

function groupByTimeBucket(changes: ConfigChange[]): BucketGroup[] {
  const sorted = [...changes].sort((a, b) => {
    const ta = a.createdAt ? new Date(a.createdAt).getTime() : 0;
    const tb = b.createdAt ? new Date(b.createdAt).getTime() : 0;
    return tb - ta;
  });

  const groups: BucketGroup[] = [];
  const groupMap = new Map<string, BucketGroup>();

  for (const c of sorted) {
    const bucket = c.createdAt ? getTimeBucket(c.createdAt) : { key: 'unknown', label: 'Unknown', dateFormat: 'monthDay' as TimeBucketFormat };
    let group = groupMap.get(bucket.key);
    if (!group) {
      group = { key: bucket.key, label: bucket.label, dateFormat: bucket.dateFormat, changes: [] };
      groupMap.set(bucket.key, group);
      groups.push(group);
    }
    group.changes.push(c);
  }

  return groups;
}

export default function ConfigChangesSection({ changes, hideConfigName: hideConfigNameProp }: Props) {
  if (!changes?.length) return null;
  const uniqueConfigs = new Set(changes.map((c) => c.configID || c.configName).filter(Boolean));
  const hideConfigName = hideConfigNameProp || uniqueConfigs.size <= 1;
  const bySeverity = Object.fromEntries(
    SEVERITY_ORDER.map((sev) => [sev, changes.filter((c) => (c.severity ?? 'info') === sev).length])
  );

  const groups = groupByTimeBucket(changes);

  return (
    <Section variant="hero" title="Config Changes" size="md">
      <div className="flex gap-[2mm] mb-[2mm]">
        {SEVERITY_ORDER.filter((sev) => bySeverity[sev] > 0).map((sev) => (
          <SeverityStatCard
            key={sev}
            color={SEVERITY_COLOR[sev]}
            value={bySeverity[sev]}
            label={sev.charAt(0).toUpperCase() + sev.slice(1)}
          />
        ))}
      </div>
      {groups.map((group) => (
        <div key={group.key} className="mb-[2mm]">
          <div className="text-xs font-semibold text-gray-500 border-b border-gray-200 pb-[0.3mm] mb-[0.5mm]">
            {group.label}
            <span className="font-normal text-gray-400 ml-[1mm]">({group.changes.length})</span>
          </div>
          <div className="flex flex-col">
            {group.changes.map((c) => <ChangeEntry key={c.id} change={c} dateFormat={group.dateFormat} hideConfigName={hideConfigName} />)}
          </div>
        </div>
      ))}
    </Section>
  );
}
