import React from 'react';
import { Page, Section } from '@flanksource/facet';
import type { KitchenSinkData } from './KitchenSinkTypes.ts';
import RBACCoverContent from '../components/RBACCoverContent.tsx';
import RBACSummarySection from '../components/RBACSummarySection.tsx';
import RBACMatrixSection from '../components/RBACMatrixSection.tsx';
import RBACUserSection from '../components/RBACUserSection.tsx';
import RBACChangelogSection from '../components/RBACChangelogSection.tsx';

interface Props {
  data: KitchenSinkData;
  pageProps: any;
}

export default function RBACPage({ data, pageProps }: Props) {
  const report = data.rbacReport;
  if (!report) return null;

  return (
    <Page {...pageProps}>
      <Section variant="hero" title="RBAC Domain Components" size="md">
        <div className="text-xs text-gray-500">
          Components used in the RBAC reports: cover, summary, matrix, per-user view, and changelog.
        </div>
      </Section>

      <Section variant="hero" title="RBACCoverContent" size="md">
        <div className="text-xs text-gray-500 mb-[2mm]">
          RBAC-specific cover page with subject, breadcrumbs, and summary stats.
        </div>
        <div className="border border-gray-200 rounded-[2mm] overflow-hidden" style={{ height: '100mm' }}>
          <RBACCoverContent report={report} subtitle="Access Matrix Report" />
        </div>
      </Section>

      <RBACSummarySection summary={report.summary} />

      {(report.resources || []).map((resource) => (
        <RBACMatrixSection key={resource.configId} resource={resource} />
      ))}

      {(report.users || []).map((user) => (
        <Section key={user.userId} variant="hero" title={`RBACUserSection - ${user.userName}`} size="md">
          <RBACUserSection user={user} />
        </Section>
      ))}

      <RBACChangelogSection changelog={report.changelog} />
    </Page>
  );
}
