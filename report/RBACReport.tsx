import React from 'react';
import { Page, PageBreak } from '@flanksource/facet';
import type { RBACReport } from './rbac-types.ts';
import RBACSummarySection from './components/RBACSummarySection.tsx';
import RBACResourceSection from './components/RBACResourceSection.tsx';
import RBACChangelogSection from './components/RBACChangelogSection.tsx';

function PageHeader({ title }: { title: string }) {
  return (
    <div className="flex items-center justify-between px-[5mm] py-[1mm] bg-[#1e293b] text-white text-[7pt]">
      <span className="font-semibold">{title}</span>
      <span className="text-gray-300">RBAC Report</span>
    </div>
  );
}

function PageFooter() {
  const now = new Date().toISOString().replace('T', ' ').replace(/\.\d+Z$/, ' UTC');
  return (
    <div className="flex items-center justify-between px-[5mm] py-[1mm] border-t border-gray-200 text-[6pt] text-gray-400">
      <span>Generated {now}</span>
    </div>
  );
}

function CoverPage({ title, query }: { title: string; query?: string }) {
  const now = new Date().toISOString().replace('T', ' ').replace(/\.\d+Z$/, ' UTC');
  return (
    <div className="flex flex-col justify-center items-center h-full min-h-[140mm] text-center px-[20mm]">
      <div className="mb-[8mm]">
        <div className="text-[8pt] font-medium text-blue-600 uppercase tracking-widest mb-[3mm]">
          RBAC Report
        </div>
        <h1 className="text-[36pt] font-bold text-slate-900 leading-tight mb-[4mm]">
          {title}
        </h1>
        {query && (
          <div className="text-[10pt] text-gray-500 font-mono bg-gray-50 rounded px-[4mm] py-[2mm] mt-[4mm]">
            {query}
          </div>
        )}
      </div>
      <div className="w-[40mm] h-[1px] mb-[8mm]" style={{ backgroundColor: '#2563EB' }} />
      <div className="text-[10pt] text-gray-400">Generated {now}</div>
    </div>
  );
}

interface RBACReportProps {
  data: RBACReport;
}

export default function RBACReportPage({ data }: RBACReportProps) {
  const header = <PageHeader title={data.title} />;
  const footer = <PageFooter />;
  const pageProps = {
    pageSize: 'a4-landscape' as const,
    margins: { top: 1, bottom: 1, left: 0, right: 0 },
    header,
    headerHeight: 8,
    footer,
    footerHeight: 8,
  };

  return (
    <>
      <style>{`@page { size: 297mm 210mm; }`}</style>
      <Page pageSize="a4-landscape" margins={{ top: 10, bottom: 10, left: 0, right: 0 }}>
        <CoverPage title={data.title} query={data.query} />
      </Page>

      <PageBreak />

      <Page {...pageProps}>
        <RBACSummarySection summary={data.summary} />
      </Page>

      {data.resources.map((resource, idx) => (
        <React.Fragment key={idx}>
          <PageBreak />
          <Page {...pageProps}>
            <RBACResourceSection resource={resource} />
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
