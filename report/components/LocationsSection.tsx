import React from 'react';
import { Section } from '@flanksource/facet';
import type { ApplicationLocation } from '../types.ts';

interface Props {
  locations: ApplicationLocation[];
}

const PURPOSE_BADGE_CLASSES: Record<string, string> = {
  primary: 'bg-blue-100 text-blue-700',
  backup:  'bg-yellow-100 text-yellow-700',
  dr:      'bg-red-100 text-red-700',
};

function LocationCard({ loc }: { loc: ApplicationLocation }) {
  const badgeClass = PURPOSE_BADGE_CLASSES[loc.purpose] ?? 'bg-gray-100 text-gray-600';

  return (
    <div className="border border-gray-200 rounded-[2mm] p-[3mm]">
      <div className="flex items-center justify-between mb-[2mm]">
        <span className="text-[11pt] font-semibold text-slate-800">{loc.name}</span>
        <span className={`text-[7pt] font-medium px-[2mm] py-[0.5mm] rounded-full ${badgeClass}`}>
          {loc.purpose}
        </span>
      </div>
      <div className="text-[9pt] text-gray-600 space-y-[1mm]">
        <div><span className="font-medium">Provider:</span> {loc.provider}</div>
        <div><span className="font-medium">Region:</span> {loc.region}</div>
        <div><span className="font-medium">Account:</span> {loc.account}</div>
        <div><span className="font-medium">Type:</span> {loc.type}</div>
        <div><span className="font-medium">Resources:</span> {loc.resourceCount}</div>
      </div>
    </div>
  );
}

export default function LocationsSection({ locations }: Props) {
  return (
    <Section variant="hero" title="Deployment Locations" size="md">
      <div className="grid grid-cols-3 gap-[4mm]">
        {locations.map((loc) => (
          <LocationCard key={`${loc.account}-${loc.name}`} loc={loc} />
        ))}
      </div>
    </Section>
  );
}
