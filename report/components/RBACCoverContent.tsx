import React from 'react';
import type { RBACReport } from '../rbac-types.ts';
import CoverPage from './CoverPage.tsx';

interface Props {
  report: RBACReport;
  subtitle: string;
}

export default function RBACCoverContent({ report, subtitle }: Props) {
  const { subject, parents } = report;
  const subjects = subject ? [subject] : undefined;

  return (
    <CoverPage
      title={report.title}
      subtitle={subtitle}
      query={report.query}
      breadcrumbs={parents}
      subjects={subjects}
      stats={[
        { label: 'resources', value: report.summary?.totalResources ?? 0 },
        { label: 'users', value: report.summary?.totalUsers ?? 0 },
        { label: 'stale', value: report.summary?.staleAccessCount ?? 0 },
      ]}
    />
  );
}
