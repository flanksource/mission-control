import React from 'react';
import { Section, StatCard } from '@flanksource/facet';
import type { RBACSummary } from '../rbac-types.ts';

interface Props {
  summary: RBACSummary;
}

export default function RBACSummarySection({ summary }: Props) {
  return (
    <Section variant="hero" title="Summary" size="md">
      <div className="grid grid-cols-3 gap-[3mm]">
        <StatCard label="Total Users" value={summary.totalUsers} />
        <StatCard label="Total Resources" value={summary.totalResources} />
        <StatCard label="Direct Assignments" value={summary.directAssignments} />
        <StatCard label="Group Assignments" value={summary.groupAssignments} />
        <StatCard
          label="Stale Access"
          value={summary.staleAccessCount}
          variant={summary.staleAccessCount > 0 ? 'warning' : 'default'}
        />
        <StatCard
          label="Overdue Reviews"
          value={summary.overdueReviews}
          variant={summary.overdueReviews > 0 ? 'warning' : 'default'}
        />
      </div>
    </Section>
  );
}
