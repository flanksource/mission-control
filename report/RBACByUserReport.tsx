import React from 'react';
import { Document, Page, Header, Footer } from '@flanksource/facet';
import type { RBACReport } from './rbac-types.ts';
import RBACSummarySection from './components/RBACSummarySection.tsx';
import RBACUserSection from './components/RBACUserSection.tsx';
import RBACChangelogSection from './components/RBACChangelogSection.tsx';
import RBACCoverContent from './components/RBACCoverContent.tsx';
import { MatrixLegend } from './components/RBACMatrixSection.tsx';
import PageHeader from './components/PageHeader.tsx';
import PageFooter from './components/PageFooter.tsx';

interface Props {
  data: RBACReport;
}

export default function RBACByUserReportPage({ data }: Props) {
  const users = data.users || [];

  return (
    <Document pageSize="a4-landscape" margins={{ top: 1, bottom: 1, left: 5, right: 5 }}>
      <Header height={8}>
        <PageHeader subtitle="RBAC Report (By User)" />
      </Header>
      <Footer height={14}>
        <PageFooter generatedAt={data.generatedAt}><MatrixLegend /></PageFooter>
      </Footer>

      <Page type="first" margins={{ top: 10, bottom: 10, left: 5, right: 5 }}>
        <RBACCoverContent report={data} subtitle="RBAC Report - By User" />
      </Page>

      <Page>
        <RBACSummarySection summary={data.summary} />

        {users.map((user, idx) => (
          <RBACUserSection key={idx} user={user} />
        ))}

        <RBACChangelogSection changelog={data.changelog} />
      </Page>
    </Document>
  );
}
