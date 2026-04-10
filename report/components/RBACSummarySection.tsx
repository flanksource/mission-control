import React from 'react';
import { Section, StatCard } from '@flanksource/facet';
import type { RBACSummary } from '../rbac-types.ts';

interface Props {
  summary: RBACSummary;
}

const NO_BREAK_STYLE = { pageBreakInside: 'avoid' as const, breakInside: 'avoid' as const };

export default function RBACSummarySection({ summary }: Props) {
  return (
    <Section variant="hero" title="Summary" size="md">
      <div className="grid grid-cols-3 gap-[3mm]" style={NO_BREAK_STYLE}>
        <div style={NO_BREAK_STYLE}>
          <StatCard label="Total Users" value={summary.totalUsers} />
        </div>
        <div style={NO_BREAK_STYLE}>
          <StatCard label="Total Resources" value={summary.totalResources} />
        </div>
        <div style={NO_BREAK_STYLE}>
          <StatCard label="Direct Assignments" value={summary.directAssignments} />
        </div>
        <div style={NO_BREAK_STYLE}>
          <StatCard label="Group Assignments" value={summary.groupAssignments} />
        </div>
        <div style={NO_BREAK_STYLE}>
          <StatCard
            label="Stale Access"
            value={summary.staleAccessCount}
            color={summary.staleAccessCount > 0 ? 'orange' : undefined}
          />
        </div>
        <div style={NO_BREAK_STYLE}>
          <StatCard
            label="Overdue Reviews"
            value={summary.overdueReviews}
            color={summary.overdueReviews > 0 ? 'red' : undefined}
          />
        </div>
      </div>
    </Section>
  );
}
