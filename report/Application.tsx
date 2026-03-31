import React from 'react';
import { Page, PageBreak } from '@flanksource/facet';
import type { Application } from './types.ts';
import ApplicationDetails from './components/ApplicationDetails.tsx';
import AccessControlSection from './components/AccessControlSection.tsx';
import IncidentsSection from './components/IncidentsSection.tsx';
import BackupsSection from './components/BackupsSection.tsx';
import FindingsSection from './components/FindingsSection.tsx';
import LocationsSection from './components/LocationsSection.tsx';
import DynamicSection from './components/DynamicSection.tsx';

function PageHeader({ app }: { app: Application }) {
  return (
    <div className="flex items-center justify-between px-[10mm] py-[2mm] bg-[#1e293b] text-white text-[9pt]">
      <span className="font-semibold">{app.name}</span>
      <span className="text-gray-300">Application Report</span>
    </div>
  );
}

function PageFooter() {
  const date = new Date().toLocaleDateString('en-US', {
    year: 'numeric', month: 'long', day: 'numeric'
  });
  return (
    <div className="flex items-center justify-between px-[10mm] py-[2mm] border-t border-gray-200 text-[8pt] text-gray-400">
      <span>Generated {date}</span>
    </div>
  );
}

function CoverPage({ app }: { app: Application }) {
  const date = new Date().toLocaleDateString('en-US', {
    year: 'numeric', month: 'long', day: 'numeric'
  });
  return (
    <div className="flex flex-col justify-center items-center h-full min-h-[200mm] text-center px-[20mm]">
      <div className="mb-[8mm]">
        <div className="text-[8pt] font-medium text-blue-600 uppercase tracking-widest mb-[3mm]">
          Application Report
        </div>
        <h1 className="text-[36pt] font-bold text-slate-900 leading-tight mb-[4mm]">
          {app.name}
        </h1>
        <div className="text-[14pt] text-gray-500 mb-[8mm]">
          {app.type} · <span className="font-mono text-[12pt]">{app.namespace}</span>
        </div>
        {app.description && (
          <p className="text-[11pt] text-gray-600 max-w-[120mm] mx-auto mb-[8mm]">
            {app.description}
          </p>
        )}
      </div>
      <div
        className="w-[40mm] h-[1px] mb-[8mm]"
        style={{ backgroundColor: '#2563EB' }}
      />
      <div className="text-[10pt] text-gray-400">
        Generated on {date}
      </div>
    </div>
  );
}

interface ApplicationReportProps {
  data: Application;
}

export default function ApplicationReport({ data }: ApplicationReportProps) {
  const header = <PageHeader app={data} />;
  const footer = <PageFooter />;
  const pageProps = {
    pageSize: 'a4' as const,
    margins: { top: 5, bottom: 5, left: 10, right: 10 },
    header,
    headerHeight: 10,
    footer,
    footerHeight: 10,
  };

  return (
    <>
      {/* Cover page — no header/footer */}
      <Page pageSize="a4" margins={{ top: 10, bottom: 10, left: 10, right: 10 }}>
        <CoverPage app={data} />
      </Page>

      <PageBreak />

      {/* Application details + properties */}
      <Page {...pageProps}>
        <ApplicationDetails app={data} />
      </Page>

      <PageBreak />

      {/* Access control */}
      <Page {...pageProps}>
        <AccessControlSection accessControl={data.accessControl} />
      </Page>

      {/* Incidents */}
      {data.incidents.length > 0 && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <IncidentsSection incidents={data.incidents} />
          </Page>
        </>
      )}

      {/* Backups & Restores */}
      {(data.backups.length > 0 || data.restores.length > 0) && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <BackupsSection backups={data.backups} restores={data.restores} />
          </Page>
        </>
      )}

      {/* Security findings */}
      {data.findings.length > 0 && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <FindingsSection findings={data.findings} />
          </Page>
        </>
      )}

      {/* Dynamic sections (view / changes / configs) */}
      {data.sections.map((section, idx) => (
        <React.Fragment key={idx}>
          <PageBreak />
          <Page {...pageProps}>
            <DynamicSection section={section} />
          </Page>
        </React.Fragment>
      ))}

      {/* Locations */}
      {data.locations.length > 0 && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <LocationsSection locations={data.locations} />
          </Page>
        </>
      )}
    </>
  );
}
