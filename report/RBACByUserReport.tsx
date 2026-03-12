import React from 'react';
import { Page, PageBreak } from '@flanksource/facet';
import type { RBACReport } from './rbac-types.ts';
import RBACSummarySection from './components/RBACSummarySection.tsx';
import RBACUserSection from './components/RBACUserSection.tsx';
import RBACChangelogSection from './components/RBACChangelogSection.tsx';

function PageHeader({ title }: { title: string }) {
  return (
    <div className="flex items-center justify-between px-[10mm] py-[2mm] bg-[#1e293b] text-white text-[9pt]">
      <span className="font-semibold">{title}</span>
      <span className="text-gray-300">RBAC Report (By User)</span>
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

function CoverPage({ title, query }: { title: string; query?: string }) {
  const date = new Date().toLocaleDateString('en-US', {
    year: 'numeric', month: 'long', day: 'numeric',
  });
  return (
    <div className="flex flex-col justify-center items-center h-full min-h-[200mm] text-center px-[20mm]">
      <div className="mb-[8mm]">
        <div className="text-[8pt] font-medium text-blue-600 uppercase tracking-widest mb-[3mm]">
          RBAC Report - By User
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
      <div className="text-[10pt] text-gray-400">Generated on {date}</div>
    </div>
  );
}

interface Props {
  data: RBACReport;
}

export default function RBACByUserReportPage({ data }: Props) {
  const header = <PageHeader title={data.title} />;
  const footer = <PageFooter />;
  const pageProps = {
    pageSize: 'a4' as const,
    margins: { top: 3, bottom: 3, left: 3, right: 3 },
    header,
    headerHeight: 10,
    footer,
    footerHeight: 10,
  };

  const users = data.users || [];

  return (
    <>
      <Page pageSize="a4" margins={{ top: 10, bottom: 10, left: 10, right: 10 }}>
        <CoverPage title={data.title} query={data.query} />
      </Page>

      <PageBreak />

      <Page {...pageProps}>
        <RBACSummarySection summary={data.summary} />
      </Page>

      {users.map((user, idx) => (
        <React.Fragment key={idx}>
          <PageBreak />
          <Page {...pageProps}>
            <div className="text-[7pt]">
              <RBACUserSection user={user} />
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
