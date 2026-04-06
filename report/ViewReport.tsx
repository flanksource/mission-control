import React from 'react';
import { Page, PageBreak } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { ViewReportData, MultiViewReportData } from './view-types.ts';
import ViewResultSection from './components/ViewResultSection.tsx';
import CoverPage from './components/CoverPage.tsx';

function PageHeader({ title, icon }: { title: string; icon?: string }) {
  return (
    <div className="flex items-center justify-between px-[10mm] py-[2mm] bg-[#1e293b] text-white text-sm">
      <span className="font-semibold inline-flex items-center gap-[1mm]">
        {icon && <Icon name={icon} className="w-[3mm] h-[3mm]" />}
        {title}
      </span>
      <span className="text-gray-300">View Report</span>
    </div>
  );
}

function PageFooter() {
  const date = new Date().toLocaleDateString('en-US', {
    year: 'numeric', month: 'long', day: 'numeric',
  });
  return (
    <div className="flex items-center justify-between px-[10mm] py-[2mm] border-t border-gray-200 text-sm text-gray-400">
      <span>Generated {date}</span>
    </div>
  );
}

function ViewCoverPage({ data }: { data: ViewReportData }) {
  const variableTags = (data.variables || []).reduce((acc, v) => {
    acc[v.label || v.key] = v.default || '-';
    return acc;
  }, {} as Record<string, string>);

  return (
    <CoverPage
      title={data.title || data.name}
      icon={data.icon}
      query={data.namespace ? `${data.namespace}/${data.name}` : undefined}
      tags={Object.keys(variableTags).length > 0 ? variableTags : undefined}
    />
  );
}

function isMultiView(data: any): data is MultiViewReportData {
  return data && Array.isArray(data.views);
}

interface ViewReportProps {
  data: ViewReportData | MultiViewReportData;
}

export default function ViewReportPage({ data }: ViewReportProps) {
  const viewsList = isMultiView(data) ? data.views : [data];
  const firstView = viewsList[0];

  const header = <PageHeader title={firstView.title || firstView.name} icon={firstView.icon} />;
  const footer = <PageFooter />;
  const pageProps = {
    pageSize: 'a4' as const,
    margins: { top: 3, bottom: 3, left: 10, right: 10 },
    header,
    headerHeight: 10,
    footer,
    footerHeight: 10,
  };

  return (
    <>
      <Page pageSize="a4" margins={{ top: 10, bottom: 10, left: 10, right: 10 }}>
        <ViewCoverPage data={firstView} />
      </Page>

      {viewsList.map((view, idx) => (
        <React.Fragment key={idx}>
          <PageBreak />
          <Page {...pageProps}>
            <div className="text-xs">
              <ViewResultSection data={view} />
            </div>
          </Page>
        </React.Fragment>
      ))}
    </>
  );
}
