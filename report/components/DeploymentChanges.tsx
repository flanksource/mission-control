import React from 'react';
import { ListTable, StatCard } from '@flanksource/facet';
import type { ApplicationChange } from '../types.ts';
import {
  classifyDeploymentChange,
  filterDeploymentChanges,
  getChangeActor,
} from './change-section-utils.ts';

interface Props {
  changes: ApplicationChange[];
}

const COUNT_VALUE_CLASS = 'text-[16pt] leading-[18pt]';
const NO_BREAK_STYLE = { pageBreakInside: 'avoid' as const, breakInside: 'avoid' as const };
const CATEGORY_STYLES: Record<'scale' | 'policy' | 'spec', string> = {
  scale: 'bg-blue-50 text-blue-700 border-blue-200',
  policy: 'bg-orange-50 text-orange-700 border-orange-200',
  spec: 'bg-slate-50 text-slate-700 border-slate-200',
};
const CATEGORY_LABELS: Record<'scale' | 'policy' | 'spec', string> = {
  scale: 'Scale',
  policy: 'Policy',
  spec: 'Spec',
};
const CATEGORY_TAG_MAPPING = (key: string, value: unknown): string => {
  if (key !== 'category') {
    return '';
  }

  const normalized = String(value).toLowerCase();
  if (normalized === 'scale') {
    return CATEGORY_STYLES.scale;
  }
  if (normalized === 'policy') {
    return CATEGORY_STYLES.policy;
  }
  return CATEGORY_STYLES.spec;
};

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

  return (
    <>
      <div className="flex flex-wrap items-stretch gap-[3mm] mb-[4mm]" style={NO_BREAK_STYLE}>
        <div className="flex-1 min-w-[28mm]" style={NO_BREAK_STYLE}>
          <StatCard
            label="Relevant Changes"
            value={String(relevant.length)}
            variant="summary"
            size="sm"
            color="gray"
            shrink
            valueClassName={COUNT_VALUE_CLASS}
          />
        </div>
        <div className="flex-1 min-w-[28mm]" style={NO_BREAK_STYLE}>
          <StatCard
            label="Spec Updates"
            value={String(counts.spec)}
            variant="summary"
            size="sm"
            color="gray"
            shrink
            valueClassName={COUNT_VALUE_CLASS}
          />
        </div>
        <div className="flex-1 min-w-[28mm]" style={NO_BREAK_STYLE}>
          <StatCard
            label="Scaling Events"
            value={String(counts.scale)}
            variant="summary"
            size="sm"
            color="blue"
            shrink
            valueClassName={COUNT_VALUE_CLASS}
          />
        </div>
        <div className="flex-1 min-w-[28mm]" style={NO_BREAK_STYLE}>
          <StatCard
            label="Policy Updates"
            value={String(counts.policy)}
            variant="summary"
            size="sm"
            color="orange"
            shrink
            valueClassName={COUNT_VALUE_CLASS}
          />
        </div>
      </div>
      <ListTable
        rows={relevant.map((change) => {
          const category = classifyDeploymentChange(change) ?? 'spec';
          return {
            id: change.id,
            date: change.date,
            subject: change.description,
            subtitle: change.changeType ?? '-',
            category: CATEGORY_LABELS[category],
            actor: getChangeActor(change),
          };
        })}
        subject="subject"
        subtitle="subtitle"
        date="date"
        primaryTags={['category']}
        keys={['actor']}
        tagMapping={CATEGORY_TAG_MAPPING}
        groups={[{ by: 'date' }]}
        size="xs"
        density="compact"
        wrap
        cellClassName="text-[8pt]"
      />
    </>
  );
}
