import React from 'react';
import { Page, PageBreak } from '@flanksource/facet';
import type { RBACReport, RBACResource } from './rbac-types.ts';
import RBACSummarySection from './components/RBACSummarySection.tsx';
import RBACMatrixSection, { MatrixLegend } from './components/RBACMatrixSection.tsx';
import RBACChangelogSection from './components/RBACChangelogSection.tsx';
import RBACCoverContent from './components/RBACCoverContent.tsx';

function PageHeader({ title }: { title: string }) {
  return (
    <div className="flex items-center justify-between px-[5mm] py-[1mm] bg-[#1e293b] text-white text-[7pt]">
      <span className="font-semibold">{title}</span>
      <span className="text-gray-300">RBAC Matrix</span>
    </div>
  );
}

function PageFooter() {
  const now = new Date().toISOString().replace('T', ' ').replace(/\.\d+Z$/, ' UTC');
  return (
    <div className="px-[5mm] py-[1mm] border-t border-gray-200 text-[6pt] text-gray-400">
      <MatrixLegend />
      <div className="flex items-center justify-between mt-[1mm]">
        <span>Generated {now}</span>
      </div>
    </div>
  );
}

function estimateHeight(resource: RBACResource): number {
  const uniqueUsers = new Set(resource.users.map((u) => u.userId)).size;
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
  const header = <PageHeader title={data.title} />;
  const footer = <PageFooter />;
  const pageProps = {
    pageSize: 'a4-landscape' as const,
    margins: { top: 1, bottom: 1, left: 5, right: 5 },
    header,
    headerHeight: 8,
    footer,
    footerHeight: 14,
  };

  const resourcePages = packResources(data.resources, 160);

  return (
    <>
      <Page pageSize="a4-landscape" margins={{ top: 10, bottom: 10, left: 5, right: 5 }}>
        <RBACCoverContent report={data} subtitle="RBAC Matrix Report" />
      </Page>

      <PageBreak />

      <Page {...pageProps}>
        <RBACSummarySection summary={data.summary} />
      </Page>

      {resourcePages.map((group, pageIdx) => (
        <React.Fragment key={pageIdx}>
          <PageBreak />
          <Page {...pageProps}>
            <div className="flex flex-col gap-[4mm]">
              {group.map((resource, idx) => (
                <RBACMatrixSection key={idx} resource={resource} />
              ))}
            </div>
          </Page>
        </React.Fragment>
      ))}

      {data.changelog.length > 0 && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <RBACChangelogSection changelog={data.changelog} />
          </Page>
        </>
      )}
    </>
  );
}
