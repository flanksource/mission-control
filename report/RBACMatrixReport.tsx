import React from 'react';
import { Document, Page, Header, Footer } from '@flanksource/facet';
import type { RBACReport, RBACResource } from './rbac-types.ts';
import RBACSummarySection from './components/RBACSummarySection.tsx';
import RBACMatrixSection, { MatrixLegend } from './components/RBACMatrixSection.tsx';
import RBACChangelogSection from './components/RBACChangelogSection.tsx';
import RBACCoverContent from './components/RBACCoverContent.tsx';
import PageHeader from './components/PageHeader.tsx';
import PageFooter from './components/PageFooter.tsx';

function estimateHeight(resource: RBACResource): number {
  const uniqueUsers = new Set((resource.users || []).map((u) => u.userId)).size;
  return 20 + uniqueUsers * 3 + 5;
}

function packResources(resources: RBACResource[], maxHeight: number): RBACResource[][] {
  const pages: RBACResource[][] = [];
  let current: RBACResource[] = [];
  let currentHeight = 0;

  for (const r of resources) {
    const h = estimateHeight(r);
    if (currentHeight + h > maxHeight && current.length > 0) {
      pages.push(current);
      current = [r];
      currentHeight = h;
    } else {
      current.push(r);
      currentHeight += h;
    }
  }
  if (current.length > 0) pages.push(current);
  return pages;
}

interface RBACMatrixReportProps {
  data: RBACReport;
}

export default function RBACMatrixReportPage({ data }: RBACMatrixReportProps) {
  const resourcePages = packResources(data.resources || [], 160);

  return (
    <Document pageSize="a4-landscape" margins={{ top: 1, bottom: 1, left: 5, right: 5 }}>
      <Header height={8}>
        <PageHeader subtitle="RBAC Matrix" />
      </Header>
      <Footer height={14}>
        <PageFooter generatedAt={data.generatedAt}><MatrixLegend /></PageFooter>
      </Footer>

      <Page type="first" margins={{ top: 10, bottom: 10, left: 5, right: 5 }}>
        <RBACCoverContent report={data} subtitle="RBAC Matrix Report" />
      </Page>

      <Page>
        <RBACSummarySection summary={data.summary} />

        {resourcePages.map((group, pageIdx) => (
          <div key={pageIdx} className="flex flex-col gap-[4mm]">
            {group.map((resource, idx) => (
              <RBACMatrixSection key={idx} resource={resource} />
            ))}
          </div>
        ))}

        <RBACChangelogSection changelog={data.changelog} />
      </Page>
    </Document>
  );
}
