import React from 'react';
import { Section, CompactTable } from '@flanksource/facet';
import type { ViewReportData, ViewColumnType } from '../view-types.ts';
import {
  formatDate,
  formatBytes,
  formatMillicores,
  formatDurationMs,
} from './utils.ts';

interface Props {
  data: ViewReportData;
}

const HEALTH_CLASSES: Record<string, string> = {
  healthy:   'bg-green-500',
  warning:   'bg-yellow-500',
  unhealthy: 'bg-red-500',
  unknown:   'bg-gray-400',
};

function HealthDot({ health }: { health: string }) {
  const dotClass = HEALTH_CLASSES[health.toLowerCase()] ?? 'bg-gray-400';
  return (
    <span className="inline-flex items-center gap-1">
      <span className={`inline-block w-[2mm] h-[2mm] rounded-full ${dotClass}`} />
      {health}
    </span>
  );
}

function TagBadges({ value }: { value: Record<string, string> }) {
  return (
    <span className="inline-flex flex-wrap gap-[0.5mm]">
      {Object.entries(value).map(([k, v]) => (
        <span key={k} className="inline-flex items-center border border-blue-200 rounded overflow-hidden text-[6pt]" style={{ whiteSpace: 'nowrap' }}>
          <span className="px-[1.5mm] py-[0.3mm] font-medium" style={{ backgroundColor: '#DBEAFE', color: '#475569' }}>{k}</span>
          <span className="px-[1.5mm] py-[0.3mm]" style={{ backgroundColor: '#FFFFFF', color: '#0F172A' }}>{v}</span>
        </span>
      ))}
    </span>
  );
}

function isTagLike(value: any): value is Record<string, string> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false;
  return Object.values(value).every((v) => typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean');
}

function formatCellValue(value: any, type: ViewColumnType): React.ReactNode {
  if (value == null) return '-';
  switch (type) {
    case 'datetime': return formatDate(String(value));
    case 'boolean': return value ? 'Yes' : 'No';
    case 'number': return typeof value === 'number' ? value.toLocaleString() : String(value);
    case 'duration': return typeof value === 'number' ? formatDurationMs(value / 1_000_000) : String(value);
    case 'bytes': return typeof value === 'number' ? formatBytes(value) : String(value);
    case 'decimal': return typeof value === 'number' ? value.toFixed(2) : String(value);
    case 'millicore': return formatMillicores(value);
    case 'gauge': return typeof value === 'number' ? `${value.toFixed(1)}%` : String(value);
    case 'health': return <HealthDot health={String(value)} />;
    case 'config_item':
      return typeof value === 'object' && value !== null ? (value as any).name ?? String(value) : String(value);
    case 'labels':
      if (isTagLike(value)) return <TagBadges value={value} />;
      return String(value);
    default:
      if (isTagLike(value)) return <TagBadges value={value} />;
      return String(value);
  }
}

export default function ViewResultSection({ data }: Props) {
  const columns = data.columns ?? [];
  const rows = data.rows ?? [];

  const visibleCols = columns.filter((c) => !c.hidden && c.type !== 'row_attributes' && c.type !== 'grants');
  const headers = visibleCols.map((c) => c.name.replace(/_/g, ' ').replace(/\b\w/g, (l) => l.toUpperCase()));

  const tableRows = rows.map((row) => {
    const colMap = new Map(columns.map((c, i) => [c.name, { value: row[i], col: c }]));
    return visibleCols.map(({ name, type }) => {
      const entry = colMap.get(name);
      if (!entry) return '-';
      return formatCellValue(entry.value, type);
    });
  });

  return (
    <Section variant="hero" title={data.title || data.name} size="md">
      {data.variables && data.variables.length > 0 && (
        <div className="flex flex-wrap gap-[2mm] mb-[2mm]">
          {data.variables.map((v) => (
            <span key={v.key} className="inline-flex items-center bg-blue-50 text-blue-800 text-[7pt] px-[2mm] py-[0.5mm] rounded">
              <span className="font-medium mr-[1mm]">{v.label || v.key}:</span>
              {v.default || '-'}
            </span>
          ))}
        </div>
      )}
      <CompactTable variant="reference" columns={headers} data={tableRows} />

      {data.sectionResults && data.sectionResults.length > 0 && (
        data.sectionResults.map((section, idx) => (
          section.view ? (
            <ViewResultSection key={idx} data={section.view} />
          ) : null
        ))
      )}
    </Section>
  );
}
