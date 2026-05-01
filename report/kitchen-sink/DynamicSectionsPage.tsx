import React from 'react';
import { Page, Section } from '@flanksource/facet';
import type { KitchenSinkData } from './KitchenSinkTypes.ts';
import DynamicSection from '../components/DynamicSection.tsx';

interface Props {
  data: KitchenSinkData;
  pageProps: any;
}

export default function DynamicSectionsPage({ data, pageProps }: Props) {
  const dynamicSections = data.dynamicSections ?? [];

  return (
    <Page {...pageProps}>
      <Section variant="hero" title="DynamicSection Auto-routing" size="md">
        <div className="text-xs text-gray-500">
          DynamicSection chooses the specialized renderer from the section title and change type mix, including grouped RBAC sections with date buckets and resource icons.
        </div>
      </Section>

      {dynamicSections.map((section, index) => (
        <DynamicSection key={`${section.title}-${index}`} section={section} />
      ))}

      {data.genericChangesSection && (
        <>
          <Section variant="hero" title="DynamicSection - Generic Fallback" size="md">
            <div className="text-xs text-gray-500">
              When changes don't match RBAC/backup/deployment patterns, falls back to a generic table.
            </div>
          </Section>
          <DynamicSection section={data.genericChangesSection} />
        </>
      )}

      {data.dynamicViewSection && (
        <>
          <Section variant="hero" title="DynamicSection - View" size="md">
            <div className="text-xs text-gray-500">
              DynamicSection with type='view' renders a table with typed columns.
            </div>
          </Section>
          <DynamicSection section={data.dynamicViewSection} />
        </>
      )}

      {data.dynamicConfigsSection && (
        <>
          <Section variant="hero" title="DynamicSection - Configs" size="md">
            <div className="text-xs text-gray-500">
              DynamicSection with type='configs' renders a config list table.
            </div>
          </Section>
          <DynamicSection section={data.dynamicConfigsSection} />
        </>
      )}
    </Page>
  );
}
