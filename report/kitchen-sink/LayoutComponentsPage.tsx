import React from 'react';
import { Page, Section } from '@flanksource/facet';
import type { KitchenSinkData } from './KitchenSinkTypes.ts';
import CoverPage from '../components/CoverPage.tsx';
import PageHeaderComponent from '../components/PageHeader.tsx';
import PageFooterComponent from '../components/PageFooter.tsx';

interface Props {
  data: KitchenSinkData;
  pageProps: any;
}

export default function LayoutComponentsPage({ data, pageProps }: Props) {
  const config = data.configItem;

  return (
    <Page {...pageProps}>
      <Section variant="hero" title="CoverPage (reusable)" size="md">
        <div className="text-xs text-gray-500 mb-[2mm]">
          Reusable cover page component with title, icon, breadcrumbs, subjects, tags, stats, and date range.
        </div>
        <div className="border border-gray-200 rounded-[2mm] overflow-hidden" style={{ height: '120mm' }}>
          <CoverPage
            title="Catalog Report"
            subtitle="Infrastructure Audit"
            icon={config.type}
            query="type=AWS::EKS::Cluster AND labels.env=production"
            breadcrumbs={[
              { id: 'root', name: 'AWS', type: 'AWS' },
              { id: 'region', name: 'us-east-1', type: 'AWS::Region' },
            ]}
            subjects={[{
              name: config.name,
              type: config.type,
              status: config.status,
              health: config.health,
              description: config.description,
              tags: config.labels,
            }]}
            stats={[
              { label: 'changes', value: data.changes.length },
              { label: 'insights', value: data.analyses.length },
              { label: 'relationships', value: data.relationships.length },
            ]}
            dateRange={{ from: '2026-03-01T00:00:00Z', to: '2026-03-30T23:59:59Z' }}
            generatedAt="2026-03-30T12:00:00Z"
          />
        </div>
      </Section>

      <Section variant="hero" title="PageHeader" size="md">
        <div className="text-xs text-gray-500 mb-[2mm]">
          Logo-based header bar with optional subtitle.
        </div>
        <div className="flex flex-col gap-[2mm]">
          <div className="border border-gray-200 rounded-[1mm] overflow-hidden">
            <PageHeaderComponent />
          </div>
          <div className="border border-gray-200 rounded-[1mm] overflow-hidden">
            <PageHeaderComponent subtitle="Infrastructure Audit — March 2026" />
          </div>
        </div>
      </Section>

      <Section variant="hero" title="PageFooter" size="md">
        <div className="text-xs text-gray-500 mb-[2mm]">
          Footer with generation timestamp and optional public URL.
        </div>
        <div className="flex flex-col gap-[2mm]">
          <div className="border border-gray-200 rounded-[1mm] overflow-hidden">
            <PageFooterComponent generatedAt="2026-03-30T12:00:00Z" />
          </div>
          <div className="border border-gray-200 rounded-[1mm] overflow-hidden">
            <PageFooterComponent generatedAt="2026-03-30T12:00:00Z" publicURL="https://app.flanksource.com/catalog/cfg-eks-001" />
          </div>
        </div>
      </Section>
    </Page>
  );
}
