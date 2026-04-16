import React from 'react';
import { Page } from '@flanksource/facet';
import type { KitchenSinkData } from './kitchen-sink/KitchenSinkTypes.ts';
import CoverPage from './components/CoverPage.tsx';
import PageHeaderComponent from './components/PageHeader.tsx';
import PageFooterComponent from './components/PageFooter.tsx';
import LayoutComponentsPage from './kitchen-sink/LayoutComponentsPage.tsx';
import ConfigComponentsPage from './kitchen-sink/ConfigComponentsPage.tsx';
import ChangesPage from './kitchen-sink/ChangesPage.tsx';
import DynamicSectionsPage from './kitchen-sink/DynamicSectionsPage.tsx';
import InsightsAndGraphPage from './kitchen-sink/InsightsAndGraphPage.tsx';
import ApplicationPage from './kitchen-sink/ApplicationPage.tsx';
import RBACPage from './kitchen-sink/RBACPage.tsx';
import CatalogPage from './kitchen-sink/CatalogPage.tsx';
import ViewPage from './kitchen-sink/ViewPage.tsx';

interface KitchenSinkProps {
  data: KitchenSinkData;
}

export default function KitchenSink({ data }: KitchenSinkProps) {
  const generatedAt = new Date().toISOString();

  const header = <PageHeaderComponent subtitle="Kitchen Sink" />;
  const footer = <PageFooterComponent generatedAt={generatedAt} />;
  const pageProps = {
    pageSize: 'a4' as const,
    margins: { top: 5, bottom: 5, left: 5, right: 5 },
    header,
    headerHeight: 10,
    footer,
    footerHeight: 10,
  };

  return (
    <>
      <Page pageSize="a4" margins={{ top: 10, bottom: 10, left: 10, right: 10 }}>
        <CoverPage
          title="Config Components"
          subtitle="Component Showcase"
          stats={[
            { label: 'changes', value: data.changes.length },
            { label: 'insights', value: data.analyses.length },
            { label: 'relationships', value: data.relationships.length },
          ]}
          generatedAt={generatedAt}
        >
          <p className="text-sm text-gray-600 max-w-[120mm] mx-auto mt-[4mm]">
            PDF-compatible components for rendering config items, changes, insights, and relationships.
          </p>
        </CoverPage>
      </Page>

      <LayoutComponentsPage data={data} pageProps={pageProps} />
      <ConfigComponentsPage data={data} pageProps={pageProps} />
      <ChangesPage data={data} pageProps={pageProps} />
      <DynamicSectionsPage data={data} pageProps={pageProps} />
      <InsightsAndGraphPage data={data} pageProps={pageProps} />
      <ApplicationPage data={data} pageProps={pageProps} />
      <RBACPage data={data} pageProps={pageProps} />
      <CatalogPage data={data} pageProps={pageProps} />
      <ViewPage data={data} pageProps={pageProps} />
    </>
  );
}
