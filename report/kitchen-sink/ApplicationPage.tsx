import React from 'react';
import { Page, Section } from '@flanksource/facet';
import type { KitchenSinkData } from './KitchenSinkTypes.ts';
import ApplicationDetails from '../components/ApplicationDetails.tsx';
import AccessControlSection from '../components/AccessControlSection.tsx';
import IncidentsSection from '../components/IncidentsSection.tsx';
import BackupsSection from '../components/BackupsSection.tsx';
import FindingsSection from '../components/FindingsSection.tsx';
import LocationsSection from '../components/LocationsSection.tsx';

interface Props {
  data: KitchenSinkData;
  pageProps: any;
}

export default function ApplicationPage({ data, pageProps }: Props) {
  const app = data.application;
  if (!app) return null;

  return (
    <Page {...pageProps}>
      <Section variant="hero" title="Application Domain Components" size="md">
        <div className="text-xs text-gray-500">
          Components used in the Application report: details, access control, incidents, backups, findings, and locations.
        </div>
      </Section>

      <ApplicationDetails app={app} />
      <AccessControlSection accessControl={app.accessControl} />
      <IncidentsSection incidents={app.incidents} />
      <BackupsSection backups={app.backups} />
      <FindingsSection findings={app.findings} />
      <LocationsSection locations={app.locations} />
    </Page>
  );
}
