import React from 'react';
import { Section, CompactTable } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type {
  ViewReportData, ViewColumnDef, ViewColumnType,
  RowAttributes, CellAttributes, GaugeConfig, BadgeConfig, PanelResult,
} from '../view-types.ts';
import {
  formatDate,
  formatBytes,
  formatMillicores,
  formatDurationMs,
  HEALTH_COLORS,
} from './utils.ts';

interface Props {
  data: ViewReportData;
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

const STATUS_COLORS: Record<string, { bg: string; fg: string }> = {
  running:    { bg: '#DCFCE7', fg: '#166534' },
  active:     { bg: '#DCFCE7', fg: '#166534' },
  healthy:    { bg: '#DCFCE7', fg: '#166534' },
  ready:      { bg: '#DCFCE7', fg: '#166534' },
  succeeded:  { bg: '#DCFCE7', fg: '#166534' },
  warning:    { bg: '#FEF3C7', fg: '#92400E' },
  degraded:   { bg: '#FEF3C7', fg: '#92400E' },
  pending:    { bg: '#DBEAFE', fg: '#1E40AF' },
  stopped:    { bg: '#F3F4F6', fg: '#374151' },
  terminated: { bg: '#FEE2E2', fg: '#991B1B' },
  failed:     { bg: '#FEE2E2', fg: '#991B1B' },
  error:      { bg: '#FEE2E2', fg: '#991B1B' },
  unhealthy:  { bg: '#FEE2E2', fg: '#991B1B' },
};

function StatusBadge({ value }: { value: string }) {
  const colors = STATUS_COLORS[value.toLowerCase()] ?? { bg: '#F3F4F6', fg: '#374151' };
  return (
    <span
      className="inline-flex px-[1.5mm] py-[0.3mm] rounded text-[6pt] font-semibold"
      style={{ backgroundColor: colors.bg, color: colors.fg }}
    >
      {value}
    </span>
  );
}

function BadgeCell({ value, config }: { value: string; config?: BadgeConfig }) {
  let bg = '#F3F4F6';
  let fg = '#374151';

  if (config?.color?.map) {
    const mapped = config.color.map[value] || config.color.map[value.toLowerCase()];
    if (mapped) { bg = mapped; fg = '#FFFFFF'; }
  } else if (config?.color?.auto) {
    const auto = STATUS_COLORS[value.toLowerCase()];
    if (auto) { bg = auto.bg; fg = auto.fg; }
  }

  return (
    <span
      className="inline-flex px-[1.5mm] py-[0.3mm] rounded text-[6pt] font-semibold"
      style={{ backgroundColor: bg, color: fg }}
    >
      {value}
    </span>
  );
}

function GaugeCell({ value, gauge, attrs }: { value: number; gauge?: GaugeConfig; attrs?: CellAttributes }) {
  const max = attrs?.max ?? 100;
  const min = attrs?.min ?? 0;
  const range = max - min;
  const pct = range > 0 ? Math.min(100, Math.max(0, ((value - min) / range) * 100)) : 0;

  let color = '#3B82F6';
  if (gauge?.thresholds) {
    const sorted = [...gauge.thresholds].sort((a, b) => a.percent - b.percent);
    for (const t of sorted) {
      if (pct >= t.percent) color = t.color;
    }
  }

  const precision = gauge?.precision ?? 1;

  return (
    <span className="inline-flex items-center gap-[1mm] w-full">
      <span className="flex-1 h-[1.5mm] rounded-full overflow-hidden" style={{ backgroundColor: '#E5E7EB' }}>
        <span className="block h-full rounded-full" style={{ width: `${pct}%`, backgroundColor: color }} />
      </span>
      <span className="text-[6pt] text-gray-600 min-w-[6mm] text-right">{value.toFixed(precision)}%</span>
    </span>
  );
}

function ConfigItemCell({ value, attrs }: { value: any; attrs?: CellAttributes }) {
  const name = typeof value === 'object' && value !== null ? (value as any).name ?? String(value) : String(value);
  const health = attrs?.config?.health;
  const configType = attrs?.config?.type;

  return (
    <span className="inline-flex items-center gap-[0.5mm]">
      {configType
        ? <Icon name={configType} className="w-[3mm] h-[3mm]" />
        : health && (
          <span
            className="inline-block w-[1.5mm] h-[1.5mm] rounded-full"
            style={{ backgroundColor: HEALTH_COLORS[health.toLowerCase()] ?? '#6B7280' }}
          />
        )}
      {name}
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

function LinkWrapper({ url, children }: { url?: string; children: React.ReactNode }) {
  if (!url) return <>{children}</>;
  return <a href={url} className="text-blue-600 underline">{children}</a>;
}

function formatCellValue(
  value: any,
  col: ViewColumnDef,
  attrs?: CellAttributes,
): React.ReactNode {
  if (value == null) return '-';

  const type = col.type;

  let node: React.ReactNode;
  switch (type) {
    case 'datetime':
      node = formatDate(String(value));
      break;
    case 'boolean':
      node = value ? 'Yes' : 'No';
      break;
    case 'number':
      node = typeof value === 'number'
        ? (col.unit ? `${value.toLocaleString()} ${col.unit}` : value.toLocaleString())
        : String(value);
      break;
    case 'duration':
      node = typeof value === 'number' ? formatDurationMs(value / 1_000_000) : String(value);
      break;
    case 'bytes':
      node = typeof value === 'number' ? formatBytes(value) : String(value);
      break;
    case 'decimal':
      node = typeof value === 'number'
        ? (col.unit ? `${value.toFixed(2)} ${col.unit}` : value.toFixed(2))
        : String(value);
      break;
    case 'millicore':
      node = formatMillicores(value);
      break;
    case 'gauge':
      node = typeof value === 'number'
        ? <GaugeCell value={value} gauge={col.gauge} attrs={attrs} />
        : String(value);
      break;
    case 'health':
      node = <HealthDot health={String(value)} />;
      break;
    case 'status':
      node = <StatusBadge value={String(value)} />;
      break;
    case 'config_item':
      node = <ConfigItemCell value={value} attrs={attrs} />;
      break;
    case 'labels':
      node = isTagLike(value) ? <TagBadges value={value} /> : String(value);
      break;
    default:
      if (col.badge) {
        node = <BadgeCell value={String(value)} config={col.badge} />;
      } else if (isTagLike(value)) {
        node = <TagBadges value={value} />;
      } else {
        node = String(value);
      }
  }

  if (attrs?.icon) {
    const isUrl = typeof attrs.icon === 'string' && (attrs.icon.startsWith('http:') || attrs.icon.startsWith('https://'));
    node = (
      <span className="inline-flex items-center gap-[0.5mm]">
        {isUrl
          ? <img src={attrs.icon} className="w-[3mm] h-[3mm]" />
          : <Icon name={attrs.icon} className="w-[3mm] h-[3mm]" />}
        {node}
      </span>
    );
  }

  return <LinkWrapper url={attrs?.url}>{node}</LinkWrapper>;
}

function getRowAttributes(row: any[], columns: ViewColumnDef[]): RowAttributes | undefined {
  const attrIdx = columns.findIndex((c) => c.type === 'row_attributes');
  if (attrIdx === -1 || attrIdx >= row.length) return undefined;
  const val = row[attrIdx];
  if (typeof val === 'object' && val !== null && !Array.isArray(val)) return val as RowAttributes;
  if (typeof val === 'string') {
    try { return JSON.parse(val); } catch { return undefined; }
  }
  return undefined;
}

function PieChartPanel({ panel }: { panel: PanelResult }) {
  const entries = panel.rows.map((r) => {
    const keys = Object.keys(r);
    const labelKey = keys.find((k) => typeof r[k] === 'string') ?? keys[0];
    const valueKey = keys.find((k) => typeof r[k] === 'number') ?? keys[1];
    return { label: String(r[labelKey]), value: Number(r[valueKey]) };
  });
  const total = entries.reduce((sum, e) => sum + e.value, 0);
  const colors = panel.piechart?.colors ?? {};

  return (
    <div>
      <div className="flex flex-col gap-[1mm]">
        {entries.map((e) => {
          const pct = total > 0 ? (e.value / total) * 100 : 0;
          const color = colors[e.label] ?? colors[e.label.toLowerCase()] ?? '#94A3B8';
          return (
            <div key={e.label} className="flex items-center gap-[2mm]">
              <span className="text-[6pt] text-gray-600 w-[18mm] text-right">{e.label}</span>
              <span className="flex-1 h-[2mm] rounded-full overflow-hidden" style={{ backgroundColor: '#E5E7EB' }}>
                <span className="block h-full rounded-full" style={{ width: `${pct}%`, backgroundColor: color }} />
              </span>
              <span className="text-[6pt] text-gray-600 w-[8mm]">{e.value} ({pct.toFixed(0)}%)</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function NumberPanel({ panel }: { panel: PanelResult }) {
  const value = panel.rows[0]?.value ?? 0;
  const unit = panel.number?.unit ?? '';
  return (
    <div className="flex items-baseline gap-[1mm]">
      <span className="text-[18pt] font-bold text-slate-900">{value}</span>
      {unit && <span className="text-[8pt] text-gray-500">{unit}</span>}
    </div>
  );
}

function GaugePanel({ panel }: { panel: PanelResult }) {
  const value = Number(panel.rows[0]?.value ?? 0);
  const unit = panel.gauge?.unit ?? '';
  const thresholds = panel.gauge?.thresholds;
  let color = '#3B82F6';
  if (thresholds) {
    const sorted = [...thresholds].sort((a, b) => a.percent - b.percent);
    for (const t of sorted) {
      if (value >= t.percent) color = t.color;
    }
  }
  const pct = Math.min(100, Math.max(0, value));

  return (
    <div>
      <div className="flex items-baseline gap-[1mm] mb-[1mm]">
        <span className="text-[14pt] font-bold text-slate-900">{value.toFixed(1)}{unit}</span>
      </div>
      <span className="block w-full h-[2mm] rounded-full overflow-hidden" style={{ backgroundColor: '#E5E7EB' }}>
        <span className="block h-full rounded-full" style={{ width: `${pct}%`, backgroundColor: color }} />
      </span>
    </div>
  );
}

function PropertiesPanel({ panel }: { panel: PanelResult }) {
  return (
    <div className="flex flex-col gap-[0.5mm]">
      {panel.rows.map((r, i) => (
        <div key={i} className="flex justify-between border-b border-gray-100 py-[0.5mm]">
          <span className="text-[7pt] text-gray-600">{r.label}</span>
          <span className="text-[7pt] font-medium text-slate-900">{r.value}</span>
        </div>
      ))}
    </div>
  );
}

function PanelCard({ panel }: { panel: PanelResult }) {
  let content: React.ReactNode;
  switch (panel.type) {
    case 'piechart': content = <PieChartPanel panel={panel} />; break;
    case 'number': content = <NumberPanel panel={panel} />; break;
    case 'gauge': content = <GaugePanel panel={panel} />; break;
    case 'properties': content = <PropertiesPanel panel={panel} />; break;
    default: content = <span className="text-[7pt] text-gray-400">Unsupported panel type: {panel.type}</span>;
  }

  return (
    <div className="border border-gray-200 rounded p-[2mm]">
      <div className="text-[7pt] font-semibold text-slate-800 mb-[1mm]">{panel.name}</div>
      {panel.description && <div className="text-[6pt] text-gray-500 mb-[1mm]">{panel.description}</div>}
      {content}
    </div>
  );
}

function PanelsGrid({ panels }: { panels: PanelResult[] }) {
  return (
    <div className="grid grid-cols-2 gap-[2mm] mt-[3mm] mb-[3mm]">
      {panels.map((p, i) => <PanelCard key={i} panel={p} />)}
    </div>
  );
}

export default function ViewResultSection({ data }: Props) {
  const columns = data.columns ?? [];
  const rows = data.rows ?? [];

  const visibleCols = columns.filter((c) => !c.hidden && c.type !== 'row_attributes' && c.type !== 'grants');
  const headers = visibleCols.map((c) => c.name.replace(/_/g, ' ').replace(/\b\w/g, (l) => l.toUpperCase()));

  const tableRows = rows.map((row) => {
    const colMap = new Map(columns.map((c, i) => [c.name, { value: row[i], col: c }]));
    const rowAttrs = getRowAttributes(row, columns);

    return visibleCols.map((col) => {
      const entry = colMap.get(col.name);
      if (!entry) return '-';
      const cellAttrs = rowAttrs?.[col.name];
      return formatCellValue(entry.value, col, cellAttrs);
    });
  });

  const SectionIcon = data.icon
    ? (props: { className?: string }) => <Icon name={data.icon!} className={props.className ?? 'w-[4mm] h-[4mm]'} />
    : undefined;

  return (
    <Section variant="hero" title={data.title || data.name} icon={SectionIcon} size="md">
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

      {data.panels && data.panels.length > 0 && (
        <PanelsGrid panels={data.panels} />
      )}

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
