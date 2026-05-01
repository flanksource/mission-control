import React from 'react';
import { Page, Section } from '@flanksource/facet';
import type { KitchenSinkData } from './KitchenSinkTypes.ts';
import ConfigLink from '../components/ConfigLink.tsx';
import ConfigItemCard from '../components/ConfigItemCard.tsx';
import ScraperCard from '../components/ScraperCard.tsx';

interface Props {
  data: KitchenSinkData;
  pageProps: any;
}

export default function ConfigComponentsPage({ data, pageProps }: Props) {
  const sampleConfigs = [data.configItem, ...data.relatedConfigs.slice(0, 5)];
  const scrapers = data.scrapers ?? [];

  return (
    <Page {...pageProps}>
      <Section variant="hero" title="ConfigLink" size="md">
        <div className="text-xs text-gray-500 mb-[2mm]">
          Renders a config item as Icon + Name with optional health indicator.
        </div>
        <div className="flex flex-col gap-[3mm]">
          {sampleConfigs.map((config) => (
            <div key={config.id} className="flex items-center gap-[4mm] py-[1mm] border-b border-gray-100">
              <div className="w-[60mm]">
                <ConfigLink config={config} />
              </div>
              <div className="w-[60mm]">
                <ConfigLink config={config} showHealth />
              </div>
              <span className="text-[7pt] text-gray-400">{config.type}</span>
            </div>
          ))}
        </div>
      </Section>

      <Section variant="hero" title="ConfigItemCard" size="md">
        <div className="text-xs text-gray-500 mb-[2mm]">
          Renders a config item with icon, name, tags, and metadata.
        </div>
        <div className="flex flex-col gap-[3mm]">
          {sampleConfigs.map((config) => (
            <div key={config.id} className="py-[1mm] border-b border-gray-100">
              <ConfigItemCard config={{ ...config, id: config.id, created_at: data.configItem.createdAt, updated_at: data.configItem.updatedAt }} />
            </div>
          ))}
        </div>
      </Section>

      <Section variant="hero" title="ScraperCard" size="md">
        <div className="text-xs text-gray-500 mb-[2mm]">
          Renders a scraper with type icons, source badge, spec hash, created by, dates, and GitOps provenance.
        </div>
        <div className="flex flex-col gap-[3mm]">
          {scrapers.map((scraper) => (
            <ScraperCard key={scraper.id} scraper={scraper} />
          ))}
        </div>
      </Section>
    </Page>
  );
}
