import React from 'react';
import { Page, Section } from '@flanksource/facet';
import type { KitchenSinkData } from './KitchenSinkTypes.ts';
import ViewResultSection from '../components/ViewResultSection.tsx';

interface Props {
  data: KitchenSinkData;
  pageProps: any;
}

export default function ViewPage({ data, pageProps }: Props) {
  const viewReport = data.viewReport;
  if (!viewReport) return null;

  return (
    <Page {...pageProps}>
      <Section variant="hero" title="ViewResultSection" size="md">
        <div className="text-xs text-gray-500 mb-[2mm]">
          Renders view data with typed columns (string, number, boolean, datetime, duration,
          health, status, gauge, bytes, millicore, config_item, labels) and panels
          (number, gauge, bargauge, piechart, table, text).
        </div>
      </Section>
      <ViewResultSection data={viewReport} />
    </Page>
  );
}
