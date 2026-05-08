import './icon-setup.ts';
import React from 'react';
import { Document, Page, Header, Footer } from '@flanksource/facet';
import type { Application } from './types.ts';
import ApplicationDetails from './components/ApplicationDetails.tsx';
import AccessControlSection from './components/AccessControlSection.tsx';
import IncidentsSection from './components/IncidentsSection.tsx';
import BackupsSection from './components/BackupsSection.tsx';
import FindingsSection from './components/FindingsSection.tsx';
import LocationsSection from './components/LocationsSection.tsx';
import DynamicSection from './components/DynamicSection.tsx';
import CoverPage from './components/CoverPage.tsx';
import PageHeader from './components/PageHeader.tsx';
import PageFooter from './components/PageFooter.tsx';

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
  return (
    <Document pageSize="a4" margins={{ top: 5, bottom: 5, left: 10, right: 10 }}>
      <Header height={10}>
        <PageHeader subtitle="Application Report" />
      </Header>
      <Footer height={10}>
        <PageFooter />
      </Footer>

      <Page type="first" margins={{ top: 10, bottom: 10, left: 10, right: 10 }}>
        <AppCoverPage app={data} />
      </Page>

      <Page>
        <ApplicationDetails app={data} />
        <AccessControlSection accessControl={data.accessControl} />
        <IncidentsSection incidents={data.incidents} />
        {data.backups.length > 0 && (
          <BackupsSection backups={data.backups} />
        )}
        <FindingsSection findings={data.findings} />
        {data.sections.map((section, idx) => (
          <DynamicSection key={idx} section={section} />
        ))}
        <LocationsSection locations={data.locations} />
      </Page>
    </Document>
  );
}
