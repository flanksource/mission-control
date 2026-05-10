import React from 'react';
import { Section, CompactTable } from '@flanksource/facet';
import type { ConfigItem, ConfigRelationship } from '../config-types.ts';
import { HEALTH_COLORS } from './utils.ts';
import ConfigLink from './ConfigLink.tsx';

interface Props {
  centralConfig: ConfigItem;
  relationships?: ConfigRelationship[];
  relatedConfigs?: ConfigItem[];
}

function HealthDot({ health }: { health: string }) {
  const color = HEALTH_COLORS[health.toLowerCase()] ?? '#6B7280';
  return (
    <span className="inline-flex items-center gap-[0.5mm]">
      <span className="inline-block w-[2mm] h-[2mm] rounded-full" style={{ backgroundColor: color }} />
      {health}
    </span>
  );
}

function RelationshipGroup({ title, relationships, configLookup }: {
  title: string;
  relationships: ConfigRelationship[];
  configLookup: Map<string, ConfigItem>;
}) {
  if (relationships.length === 0) return null;

  const rows = relationships.map((rel) => {
    const targetID = rel.direction === 'incoming' ? rel.configID : rel.relatedID;
    const config = configLookup.get(targetID);
    return [
      config ? <ConfigLink config={config} /> : targetID,
      config?.type ?? '-',
      rel.relation,
      config?.health ? <HealthDot health={config.health} /> : '-',
    ];
  });

  return (
    <div className="mb-[4mm]">
      <div className="flex items-center gap-[6px] mb-[2mm]">
        <span className="text-[9pt] font-semibold text-slate-800">{title}</span>
        <span className="text-[7.5pt] text-gray-500 bg-gray-100 px-[6px] py-[1px] rounded-full">
          {relationships.length}
        </span>
      </div>
      <div className="border-l-2 ml-[4mm] pl-[3mm]" style={{ borderColor: '#D1D5DB' }}>
        <CompactTable variant="reference" columns={['Config', 'Type', 'Relation', 'Health']} data={rows} />
      </div>
    </div>
  );
}

export default function ConfigRelationshipGraph({ centralConfig, relationships, relatedConfigs }: Props) {
  if (!relatedConfigs?.length) return null;
  const configLookup = new Map(relatedConfigs.map((c) => [c.id, c]));

  const rels = relationships || [];
  const incoming = rels.filter((r) => r.direction === 'incoming');
  const outgoing = rels.filter((r) => r.direction === 'outgoing');

  return (
    <Section variant="hero" title="Config Relationships" size="md">
      <div
        className="flex items-center gap-[2mm] p-[3mm] rounded mb-[4mm]"
        style={{ backgroundColor: '#F0F9FF', border: '1px solid #BAE6FD' }}
      >
        <ConfigLink config={centralConfig} showHealth />
        {centralConfig.status && (
          <span className="text-[7pt] text-gray-500 ml-[2mm]">({centralConfig.status})</span>
        )}
      </div>
      <RelationshipGroup title="Depends On" relationships={outgoing} configLookup={configLookup} />
      <RelationshipGroup title="Depended On By" relationships={incoming} configLookup={configLookup} />
    </Section>
  );
}
