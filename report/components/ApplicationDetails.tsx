import React from 'react';
import { Section, StatCard } from '@flanksource/facet';
import type { Application } from '../types.ts';
import { formatPropertyValue } from './utils.ts';

interface Props {
  app: Application;
}

const NO_BREAK_STYLE = { pageBreakInside: 'avoid' as const, breakInside: 'avoid' as const };

export default function ApplicationDetails({ app }: Props) {
  const sortedProps = [...(app.properties ?? [])].sort(
    (a, b) => (a.order ?? 0) - (b.order ?? 0)
  );

  return (
    <Section variant="hero" title={app.name} subtitle={`${app.type} · ${app.namespace}`}>
      {app.description && (
        <p className="text-[10pt] text-gray-600 mt-[2mm] mb-[4mm]">{app.description}</p>
      )}
      {sortedProps.length > 0 && (
        <div className="grid grid-cols-4 gap-[3mm] mt-[4mm]" style={NO_BREAK_STYLE}>
          {sortedProps.map((prop) => (
            <div key={prop.name} style={NO_BREAK_STYLE}>
              <StatCard
                label={prop.label ?? prop.name ?? ''}
                value={formatPropertyValue(prop.value, prop.text, prop.unit)}
                variant="bordered"
                size="sm"
              />
            </div>
          ))}
        </div>
      )}
    </Section>
  );
}
