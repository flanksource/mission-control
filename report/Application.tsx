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
import CoverPage from './components/CoverPage.tsx';

function PageHeader({ app }: { app: Application }) {
  return (
    <div className="flex items-center justify-between px-[10mm] py-[2mm] bg-[#1e293b] text-white text-sm">
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
    <div className="flex items-center justify-between px-[10mm] py-[2mm] border-t border-gray-200 text-sm text-gray-400">
      <span>Generated {date}</span>
    </div>
  );
}

function AppCoverPage({ app }: { app: Application }) {
  return (
    <CoverPage
      title={app.name}
      subtitle="Application Report"
      query={app.namespace ? `${app.type} · ${app.namespace}` : undefined}
      subjects={app.description ? [{ name: app.name, description: app.description }] : undefined}
    />
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
        <AppCoverPage app={data} />
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
