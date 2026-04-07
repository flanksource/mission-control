import React from 'react';
import { StatCard } from '@flanksource/facet';
import type { ApplicationChange } from '../types.ts';
import { formatEntryDate, getTimeBucket, type TimeBucketFormat } from './utils.ts';
import {
  classifyDeploymentChange,
  filterDeploymentChanges,
  getChangeActor,
} from './change-section-utils.ts';

interface Props {
  changes: ApplicationChange[];
}

const CATEGORY_STYLES: Record<'scale' | 'policy' | 'spec', string> = {
  scale: 'bg-blue-50 text-blue-700 border-blue-200',
  policy: 'bg-orange-50 text-orange-700 border-orange-200',
  spec: 'bg-slate-50 text-slate-700 border-slate-200',
};

interface BucketGroup {
  key: string;
  label: string;
  dateFormat: TimeBucketFormat;
  changes: ApplicationChange[];
}

function groupByTimeBucket(changes: ApplicationChange[]): BucketGroup[] {
  const groups: BucketGroup[] = [];
  const groupMap = new Map<string, BucketGroup>();

  for (const change of changes) {
    const bucket = getTimeBucket(change.date);
    let group = groupMap.get(bucket.key);
    if (!group) {
      group = { key: bucket.key, label: bucket.label, dateFormat: bucket.dateFormat, changes: [] };
      groupMap.set(bucket.key, group);
      groups.push(group);
    }
    group.changes.push(change);
  }

  return groups;
}

export default function DeploymentChanges({ changes }: Props) {
  const relevant = filterDeploymentChanges(changes).sort((a, b) => (
    new Date(b.date).getTime() - new Date(a.date).getTime()
  ));

  if (!relevant.length) {
    return null;
  }

  const counts = {
    scale: relevant.filter((change) => classifyDeploymentChange(change) === 'scale').length,
    policy: relevant.filter((change) => classifyDeploymentChange(change) === 'policy').length,
    spec: relevant.filter((change) => classifyDeploymentChange(change) === 'spec').length,
  };

  const groups = groupByTimeBucket(relevant);

  return (
    <>
      <div className="grid grid-cols-4 gap-[3mm] mb-[4mm]">
        <StatCard label="Relevant Changes" value={String(relevant.length)} variant="bordered" size="sm" />
        <StatCard label="Spec Updates" value={String(counts.spec)} variant="bordered" size="sm" />
        <StatCard label="Scaling Events" value={String(counts.scale)} variant="bordered" size="sm" />
        <StatCard label="Policy Updates" value={String(counts.policy)} variant="bordered" size="sm" />
      </div>

      {groups.map((group) => (
        <div key={group.key} className="mb-[3mm]">
          <div className="text-[8pt] font-semibold text-gray-500 border-b border-gray-200 pb-[0.8mm] mb-[1mm]">
            {group.label}
            <span className="font-normal text-gray-400 ml-[1mm]">({group.changes.length})</span>
          </div>
          <div className="flex flex-col">
            {group.changes.map((change) => {
              const category = classifyDeploymentChange(change) ?? 'spec';
              return (
                <div key={change.id} className="flex items-center gap-[1.5mm] py-[0.6mm] border-b border-gray-50 last:border-b-0 text-[8pt]">
                  <span className="text-gray-400 font-mono whitespace-nowrap w-[14mm] text-right shrink-0">
                    {formatEntryDate(change.date, group.dateFormat)}
                  </span>
                  <span className={`leading-none px-[1mm] py-[0.35mm] rounded border font-semibold whitespace-nowrap shrink-0 ${CATEGORY_STYLES[category]}`}>
                    {category}
                  </span>
                  <span className="text-slate-800 font-medium whitespace-nowrap shrink-0">{change.changeType ?? '-'}</span>
                  <span className="text-gray-400 whitespace-nowrap shrink-0">{getChangeActor(change)}</span>
                  <span className="text-gray-600 leading-tight flex-1">{change.description}</span>
                </div>
              );
            })}
          </div>
        </div>
      ))}
    </>
  );
}
