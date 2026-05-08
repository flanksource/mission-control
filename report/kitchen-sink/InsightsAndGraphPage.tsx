import React from 'react';
import { Page, Section, MatrixTable, Dot } from '@flanksource/facet';
import type { KitchenSinkData } from './KitchenSinkTypes.ts';
import ConfigInsightsSection from '../components/ConfigInsightsSection.tsx';
import ConfigRelationshipGraph from '../components/ConfigRelationshipGraph.tsx';

interface Props {
  data: KitchenSinkData;
  pageProps: any;
}

export default function InsightsAndGraphPage({ data, pageProps }: Props) {
  return (
    <Page {...pageProps}>
      <ConfigInsightsSection analyses={data.analyses} />

      <ConfigRelationshipGraph
        centralConfig={data.configItem}
        relationships={data.relationships}
        relatedConfigs={data.relatedConfigs}
      />

      <Section variant="hero" title="MatrixTable" size="md">
        <div className="text-xs text-gray-500 mb-[3mm]">
          Rotated column headers using CSS-Tricks translate+rotate pattern.
        </div>
        <MatrixTable
          columnWidth={10} headerHeight={12}
          columns={['Read', 'Write', 'Execute', 'Admin', 'Delete', 'Audit']}
          rows={[
            { label: <span className="font-medium">alice@example.com</span>, cells: [<Dot color="#2563EB" />, <Dot color="#2563EB" />, <Dot color="#2563EB" />, <Dot color="#2563EB" />, null, null] },
            { label: <span className="font-medium">bob@example.com</span>, cells: [<Dot color="#2563EB" />, <Dot color="#2563EB" />, null, null, null, null] },
            { label: <span className="font-medium">charlie@example.com</span>, cells: [<Dot color="#2563EB" />, null, null, null, null, <Dot color="#7C3AED" />] },
            { label: <span className="font-medium">deploy-bot</span>, cells: [<Dot color="#2563EB" />, <Dot color="#2563EB" />, <Dot color="#2563EB" />, null, null, null] },
            { label: <span className="font-medium">monitoring-svc</span>, cells: [<Dot color="#EA580C" />, null, null, null, null, <Dot color="#EA580C" />] },
          ]}
        />
        <div className="mt-[6mm] text-xs text-gray-500 mb-[3mm]">
          With longer column names and more rows.
        </div>
        <MatrixTable
          columnWidth={10} headerHeight={25}
          columns={['db_datareader', 'db_datawriter', 'db_owner', 'db_securityadmin', 'db_backupoperator', 'db_ddladmin', 'db_accessadmin']}
          rows={[
            { label: <span className="font-medium">design-studio-pas</span>, cells: [null, null, <Dot color="#2563EB" />, null, null, null, null] },
            { label: <span className="font-medium">monitoring_ro</span>, cells: [<Dot color="#2563EB" />, null, null, null, null, null, null] },
            { label: <span className="font-medium">workflow-qa-bot</span>, cells: [null, null, <Dot color="#EA580C" />, null, null, null, null] },
            { label: <span className="font-medium">demo-sa</span>, cells: [null, null, <Dot color="#2563EB" />, null, null, null, null] },
            { label: <span className="font-medium">SG-ACME Shared Dev DB</span>, cells: [null, <Dot color="#7C3AED" />, null, null, <Dot color="#7C3AED" />, null, null] },
            { label: <span className="font-medium">SG-ACME Shared RO</span>, cells: [<Dot color="#7C3AED" />, null, null, null, null, null, <Dot color="#7C3AED" />] },
            { label: <span className="font-medium">svc_mission_control</span>, cells: [<Dot color="#2563EB" />, <Dot color="#2563EB" />, null, null, null, null, null] },
          ]}
        />
      </Section>
    </Page>
  );
}
