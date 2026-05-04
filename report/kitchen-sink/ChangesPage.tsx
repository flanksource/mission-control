import React from 'react';
import { Page, Section } from '@flanksource/facet';
import type { KitchenSinkData } from './KitchenSinkTypes.ts';
import ConfigChangesSection from '../components/ConfigChangesSection.tsx';
import ConfigChangesExamples from '../components/ConfigChangesExamples.tsx';
import { defaultConfigChangesExtensions } from '../components/config-changes-builtin-extensions.tsx';

interface Props {
  data: KitchenSinkData;
  pageProps: any;
}

export default function ChangesPage({ data, pageProps }: Props) {
  const allChanges = data.changes ?? [];
  const schemaExampleChanges = allChanges.filter((change) => change.source === 'schema-examples');
  const demoChanges = allChanges.filter((change) => change.source !== 'schema-examples');

  return (
    <Page {...pageProps}>
      <ConfigChangesExamples changes={demoChanges} />

      {schemaExampleChanges.length > 0 && (
        <Section variant="hero" title="Schema Example Coverage" size="md">
          <div className="text-xs text-gray-500 mb-[2mm]">
            Every standalone example in the duty handwritten change-types schema is generated into this report and rendered once here in schema order.
          </div>
          <ConfigChangesSection changes={schemaExampleChanges} hideConfigName />
        </Section>
      )}

      <Section variant="hero" title="Auto-Categorized Changes (Extensions)" size="md">
        <div className="text-xs text-gray-500 mb-[2mm]">
          A single changes array routed through the ConfigChangesSection extension API. Built-in extensions ({defaultConfigChangesExtensions.map((e) => e.key).join(', ')}) claim matching changes (drop: true) so each change appears in exactly one section.
        </div>
        <ConfigChangesSection changes={demoChanges} extensions={defaultConfigChangesExtensions} />
      </Section>

      <ConfigChangesSection changes={demoChanges} />
    </Page>
  );
}
