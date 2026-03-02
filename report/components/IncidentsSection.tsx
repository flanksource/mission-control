import React from 'react';
import { Section, SeverityStatCard, CompactTable } from '@flanksource/facet';
import type { ApplicationIncident } from '../types.ts';
import { formatDate } from './utils.ts';

interface Props {
  incidents: ApplicationIncident[];
}

const SEVERITY_ORDER = ['critical', 'high', 'medium', 'low'] as const;
const SEVERITY_COLOR: Record<string, 'red' | 'orange' | 'yellow' | 'blue'> = {
  critical: 'red',
  high: 'orange',
  medium: 'yellow',
  low: 'blue',
};

export default function IncidentsSection({ incidents }: Props) {
  const bySeverity = Object.fromEntries(
    SEVERITY_ORDER.map((sev) => [sev, incidents.filter((i) => i.severity === sev).length])
  );

  const rows = incidents.map((i) => [
    formatDate(i.date),
    i.severity,
    i.description,
    i.status,
    i.resolvedDate ? formatDate(i.resolvedDate) : '-',
  ]);

  return (
    <Section variant="hero" title="Incidents" size="md">
      <div className="grid grid-cols-4 gap-[3mm] mb-[4mm]">
        {SEVERITY_ORDER.map((sev) => (
          <SeverityStatCard
            key={sev}
            color={SEVERITY_COLOR[sev]}
            value={bySeverity[sev]}
            label={sev.charAt(0).toUpperCase() + sev.slice(1)}
          />
        ))}
      </div>
      {rows.length > 0 ? (
        <CompactTable
          variant="reference"
          columns={['Date', 'Severity', 'Description', 'Status', 'Resolved']}
          data={rows}
        />
      ) : (
        <p className="text-[9pt] text-gray-500 italic">No incidents recorded.</p>
      )}
    </Section>
  );
}
