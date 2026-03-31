import React from 'react';
import { Page, PageBreak } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { ViewReportData, MultiViewReportData } from './view-types.ts';
import ViewResultSection from './components/ViewResultSection.tsx';

function PageHeader({ title, icon }: { title: string; icon?: string }) {
  return (
    <div className="flex items-center justify-between px-[10mm] py-[2mm] bg-[#1e293b] text-white text-[9pt]">
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
    <div className="flex items-center justify-between px-[10mm] py-[2mm] border-t border-gray-200 text-[8pt] text-gray-400">
      <span>Generated {date}</span>
    </div>
  );
}

function CoverPage({ data }: { data: ViewReportData }) {
  const date = new Date().toLocaleDateString('en-US', {
    year: 'numeric', month: 'long', day: 'numeric',
  });
  return (
    <div className="flex flex-col justify-center items-center h-full min-h-[200mm] text-center px-[20mm]">
      <div className="mb-[8mm]">
        {data.icon && (
          <div className="mb-[4mm] flex justify-center">
            <Icon name={data.icon} className="w-[16mm] h-[16mm]" />
          </div>
        )}
        <h1 className="text-[36pt] font-bold text-slate-900 leading-tight mb-[4mm]">
          {data.title || data.name}
        </h1>
        {data.namespace && (
          <div className="text-[10pt] text-gray-500 font-mono bg-gray-50 rounded px-[4mm] py-[2mm] mt-[4mm]">
            {data.namespace}/{data.name}
          </div>
        )}
      </div>
      {data.variables && data.variables.length > 0 && (
        <div className="flex flex-wrap justify-center gap-[2mm] mb-[6mm]">
          {data.variables.map((v) => (
            <span key={v.key} className="inline-flex items-center bg-blue-50 text-blue-800 text-[9pt] px-[3mm] py-[1mm] rounded">
              <span className="font-medium mr-[1mm]">{v.label || v.key}:</span>
              {v.default || '-'}
            </span>
          ))}
        </div>
      )}
      <div className="w-[40mm] h-[1px] mb-[8mm]" style={{ backgroundColor: '#2563EB' }} />
      <div className="text-[10pt] text-gray-400">Generated on {date}</div>
    </div>
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
        <CoverPage data={firstView} />
      </Page>

      {viewsList.map((view, idx) => (
        <React.Fragment key={idx}>
          <PageBreak />
          <Page {...pageProps}>
            <div className="text-[7pt]">
              <ViewResultSection data={view} />
            </div>
          </Page>
        </React.Fragment>
      ))}
    </>
  );
}
