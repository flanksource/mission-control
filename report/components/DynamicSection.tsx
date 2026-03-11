import React from 'react';
import { Section, CompactTable } from '@flanksource/facet';
import type { ApplicationSection, ViewColumnType } from '../types.ts';
import {
  formatDate,
  formatRelative,
  formatBytes,
  formatMillicores,
  formatDurationMs,
  SEVERITY_COLORS,
  SEVERITY_BG,
} from './utils.ts';

interface Props {
  section: ApplicationSection;
}

const HEALTH_CLASSES: Record<string, string> = {
  healthy:   'bg-green-500',
  warning:   'bg-yellow-500',
  unhealthy: 'bg-red-500',
  unknown:   'bg-gray-400',
};

const REFRESH_CLASSES: Record<string, string> = {
  fresh: 'bg-green-100 text-green-800',
  cache: 'bg-yellow-100 text-yellow-800',
};

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
    case 'status': return <SeverityBadge severity={String(value)} />;
    case 'gauge': return typeof value === 'number' ? `${value.toFixed(1)}%` : String(value);
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

function HealthDot({ health }: { health: string }) {
  const dotClass = HEALTH_CLASSES[health.toLowerCase()] ?? 'bg-gray-400';
  return (
    <span className="inline-flex items-center gap-1">
      <span className={`inline-block w-[2mm] h-[2mm] rounded-full ${dotClass}`} />
      {health}
    </span>
  );
}

function SeverityBadge({ severity }: { severity: string }) {
  const key = severity?.toLowerCase();
  const color = SEVERITY_COLORS[key] ?? '#6B7280';
  const bg = SEVERITY_BG[key] ?? '#F3F4F6';
  return (
    <span style={{ color, backgroundColor: bg }} className="px-[1.5mm] py-[0.3mm] rounded text-[7pt] font-medium">
      {severity}
    </span>
  );
}

function ViewSection({ section }: { section: ApplicationSection }) {
  const { view } = section;
  if (!view?.columns || !view?.rows) return null;

  const visibleCols = view.columns.filter((c) => !c.hidden);
  const headers = visibleCols.map((c) => c.name.replace(/_/g, ' ').replace(/\b\w/g, (l) => l.toUpperCase()));

  const refreshClass = view.refreshStatus
    ? (REFRESH_CLASSES[view.refreshStatus] ?? 'bg-red-100 text-red-800')
    : null;

  const rows = view.rows.map((row) => {
    const colMap = new Map(view.columns!.map((c, i) => [c.name, { value: row[i], col: c }]));
    return visibleCols.map(({ name, type }) => {
      const entry = colMap.get(name);
      if (!entry) return '-';
      if (type === 'health') return <HealthDot health={String(entry.value)} />;
      return formatCellValue(entry.value, type);
    });
  });

  return (
    <div>
      <div className="flex items-center mb-[2mm]">
        {view.refreshStatus && (
          <span className={`text-[7pt] px-[1.5mm] py-[0.5mm] rounded font-medium ${refreshClass}`}>
            {view.refreshStatus}
          </span>
        )}
        {view.lastRefreshedAt && (
          <span className="text-[8pt] text-gray-400 ml-[2mm]">
            Updated {formatRelative(view.lastRefreshedAt)}
          </span>
        )}
      </div>
      <CompactTable variant="reference" columns={headers} data={rows} />
    </div>
  );
}

function ChangesSection({ section }: { section: ApplicationSection }) {
  const rows = (section.changes ?? []).map((c) => [
    formatRelative(c.date),
    c.changeType ?? '-',
    <SeverityBadge severity={c.status} />,
    c.createdBy || c.source || '-',
    c.description,
  ]);
  return <CompactTable variant="reference" columns={['Age', 'Type', 'Severity', 'Source', 'Description']} data={rows} />;
}

function ConfigsSection({ section }: { section: ApplicationSection }) {
  const rows = (section.configs ?? []).map((c) => [
    c.name,
    c.type ?? '-',
    c.status ?? '-',
    c.health ? <HealthDot health={c.health} /> : '-',
    c.labels ? <TagBadges value={c.labels} /> : '-',
  ]);
  return <CompactTable variant="reference" columns={['Name', 'Type', 'Status', 'Health', 'Labels']} data={rows} />;
}

export default function DynamicSection({ section }: Props) {
  return (
    <Section variant="hero" title={section.title} size="md">
      {section.type === 'view' && <ViewSection section={section} />}
      {section.type === 'changes' && <ChangesSection section={section} />}
      {section.type === 'configs' && <ConfigsSection section={section} />}
    </Section>
  );
}
