import React from 'react';
import { Page, Section } from '@flanksource/facet';
import type { KitchenSinkData } from './KitchenSinkTypes.ts';
import ConfigTreeSection from '../components/ConfigTreeSection.tsx';
import CatalogAccessSection from '../components/CatalogAccessSection.tsx';
import CatalogAccessLogsSection from '../components/CatalogAccessLogsSection.tsx';
import CatalogList from '../components/CatalogList.tsx';
import ArtifactAppendix from '../components/ArtifactAppendix.tsx';

interface Props {
  data: KitchenSinkData;
  pageProps: any;
}

export default function CatalogPage({ data, pageProps }: Props) {
  const catalog = data.catalogReport;
  if (!catalog) return null;

  return (
    <Page {...pageProps}>
      <Section variant="hero" title="Catalog Domain Components" size="md">
        <div className="text-xs text-gray-500">
          Components used in the Catalog report: config tree, access control, access logs, catalog list, and artifact appendix.
        </div>
      </Section>

      {catalog.relationshipTree && (
        <ConfigTreeSection tree={catalog.relationshipTree} />
      )}

      <CatalogAccessSection access={catalog.access} />
      <CatalogAccessLogsSection logs={catalog.accessLogs} />
      <CatalogList entries={catalog.entries} />
      <ArtifactAppendix changes={catalog.changes} />
    </Page>
  );
}
