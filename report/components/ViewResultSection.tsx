import React from 'react';
import { Section, CompactTable } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type {
  ViewReportData, ViewColumnDef,
  RowAttributes, CellAttributes, GaugeConfig, BadgeConfig, PanelResult, HeatmapVariant,
} from '../view-types';
import {
  formatDate,
  formatBytes,
  formatMillicores,
  formatDurationMs,
  HEALTH_COLORS,
} from './utils';

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

type CalendarStatus = 'success' | 'failed' | 'none';

type HeatmapValue = {
  date: string;
  successful: number;
  failed: number;
  count: number;
  size?: string;
};

const DAY_HEADERS = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'] as const;
const DATE_ONLY_REGEX = /^\d{4}-\d{2}-\d{2}$/;

const SUCCESS_STATUSES = new Set(['success', 'successful', 'completed']);
const FAILURE_STATUSES = new Set(['failed', 'failure', 'error', 'errored', 'cancelled', 'canceled']);

const CALENDAR_CELL_CLASSES: Record<CalendarStatus, string> = {
  success: 'bg-green-50 border border-green-600',
  failed: 'bg-red-50 border border-red-600',
  none: 'bg-gray-100 border border-gray-200',
};

const CALENDAR_TEXT_CLASSES: Record<CalendarStatus, string> = {
  success: 'text-green-700',
  failed: 'text-red-600',
  none: 'text-gray-400',
};

const COMPACT_CELL_STYLES: Record<'success' | 'mixed' | 'failed' | 'none', { bg: string; border: string }> = {
  success: { bg: '#BBF7D0', border: '#86EFAC' },
  mixed: { bg: '#FED7AA', border: '#FDBA74' },
  failed: { bg: '#FECACA', border: '#FCA5A5' },
  none: { bg: '#F3F4F6', border: '#E5E7EB' },
};

function toNumber(value: unknown): number {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function formatDateKey(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
}

function toDateKey(value: unknown): string | null {
  if (typeof value === 'string') {
    const normalized = value.trim();
    if (!normalized) return null;

    if (DATE_ONLY_REGEX.test(normalized)) {
      return normalized;
    }

    const parsed = new Date(normalized);
    if (Number.isNaN(parsed.getTime())) return null;
    return formatDateKey(parsed);
  }

  if (value instanceof Date && !Number.isNaN(value.getTime())) {
    return formatDateKey(value);
  }

  return null;
}

function toSize(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined;
  const size = value.trim();
  return size ? size : undefined;
}

function toStatus(value: unknown): CalendarStatus | undefined {
  if (typeof value !== 'string') return undefined;
  const normalized = value.toLowerCase().trim();
  if (SUCCESS_STATUSES.has(normalized)) return 'success';
  if (FAILURE_STATUSES.has(normalized)) return 'failed';
  return undefined;
}

function resolveHeatmapVariant(panel: PanelResult): HeatmapVariant {
  return panel.heatmap?.mode === 'compact' ? 'compact' : 'calendar';
}

function buildHeatmapValues(rows?: Array<Record<string, unknown>>): HeatmapValue[] {
  const valuesByDate = new Map<string, HeatmapValue>();

  for (const row of rows ?? []) {
    const date = toDateKey(row.date ?? row.day ?? row.timestamp);
    if (!date) continue;

    const status = toStatus(row.status);
    let successful = toNumber(row.successful);
    let failed = toNumber(row.failed);
    let count = toNumber(row.count);

    if (count <= 0) {
      count = successful + failed;
    }

    if (successful <= 0 && failed <= 0) {
      if (status === 'success') {
        successful = count > 0 ? count : 1;
      } else if (status === 'failed') {
        failed = count > 0 ? count : 1;
      } else if (count > 0) {
        successful = count;
      }
    }

    if (count <= 0) {
      count = successful + failed;
    }

    const size = toSize(row.size);

    const existing = valuesByDate.get(date);
    if (existing) {
      existing.successful += successful;
      existing.failed += failed;
      existing.count += count;
      if (size) existing.size = size;
      continue;
    }

    valuesByDate.set(date, { date, successful, failed, count, size });
  }

  return Array.from(valuesByDate.values()).sort((a, b) => a.date.localeCompare(b.date));
}

function getCalendarStatus(value: HeatmapValue | undefined): CalendarStatus {
  if (!value) return 'none';

  const total = value.count > 0 ? value.count : value.successful + value.failed;
  if (total <= 0) return 'none';
  if (value.failed > 0) return 'failed';
  if (value.successful > 0) return 'success';
  return 'none';
}

function getCellLabel(value: HeatmapValue | undefined): string | undefined {
  if (!value) return undefined;
  if (value.size) return value.size;

  const total = value.count > 0 ? value.count : value.successful + value.failed;
  if (total <= 0) return undefined;
  return `${total}`;
}

function getTooltipText(date: string, value: HeatmapValue | undefined): string {
  if (!value) return `${date}: No backups`;

  const total = value.count > 0 ? value.count : value.successful + value.failed;
  return `${date}: ${value.successful} successful, ${value.failed} failed (${total} total)`;
}

function groupValuesByMonth(values: HeatmapValue[]): Array<{ key: string; values: HeatmapValue[] }> {
  const buckets = new Map<string, HeatmapValue[]>();

  for (const value of values) {
    const key = value.date.slice(0, 7);
    if (!buckets.has(key)) {
      buckets.set(key, []);
    }
    buckets.get(key)!.push(value);
  }

  return Array.from(buckets.entries())
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([key, monthValues]) => ({
      key,
      values: monthValues.sort((a, b) => a.date.localeCompare(b.date)),
    }));
}

function buildCompactWeeks(values: HeatmapValue[]): {
  weeks: Array<Array<{ date: string; inRange: boolean; value?: HeatmapValue }>>;
  startDate: Date;
  endDate: Date;
} {
  const valuesByDate = new Map(values.map((value) => [value.date, value]));

  let startDate: Date;
  let endDate: Date;

  if (values.length === 0) {
    endDate = new Date();
    startDate = new Date();
    startDate.setDate(endDate.getDate() - 120);
  } else {
    startDate = new Date(`${values[0].date}T00:00:00`);
    endDate = new Date(`${values[values.length - 1].date}T00:00:00`);

    if (Number.isNaN(startDate.getTime()) || Number.isNaN(endDate.getTime())) {
      endDate = new Date();
      startDate = new Date();
      startDate.setDate(endDate.getDate() - 120);
    }
  }

  const startAligned = new Date(startDate);
  startAligned.setDate(startAligned.getDate() - startAligned.getDay());

  const endAligned = new Date(endDate);
  endAligned.setDate(endAligned.getDate() + (6 - endAligned.getDay()));

  const weeks: Array<Array<{ date: string; inRange: boolean; value?: HeatmapValue }>> = [];
  const cursor = new Date(startAligned);

  while (cursor <= endAligned) {
    const week: Array<{ date: string; inRange: boolean; value?: HeatmapValue }> = [];

    for (let dayIndex = 0; dayIndex < 7; dayIndex++) {
      const date = formatDateKey(cursor);
      const inRange = cursor >= startDate && cursor <= endDate;
      week.push({ date, inRange, value: valuesByDate.get(date) });
      cursor.setDate(cursor.getDate() + 1);
    }

    weeks.push(week);
  }

  return { weeks, startDate, endDate };
}

function getCompactCellKind(value?: HeatmapValue): 'success' | 'mixed' | 'failed' | 'none' {
  if (!value) return 'none';

  const total = value.count > 0 ? value.count : value.successful + value.failed;
  if (total <= 0) return 'none';

  if (value.failed <= 0) return 'success';

  const successRatio = value.successful / total;
  if (successRatio >= 0.5) return 'mixed';
  return 'failed';
}

function CalendarMonthHeatmap({ values, monthKey }: { values: HeatmapValue[]; monthKey?: string }) {
  const valuesByDate = new Map(values.map((value) => [value.date, value]));

  let year: number;
  let month: number;

  if (monthKey) {
    const [y, m] = monthKey.split('-').map((n) => Number(n));
    year = Number.isFinite(y) ? y : new Date().getFullYear();
    month = Number.isFinite(m) ? m - 1 : new Date().getMonth();
  } else if (values.length > 0) {
    const latest = new Date(`${values[values.length - 1].date}T00:00:00`);
    if (!Number.isNaN(latest.getTime())) {
      year = latest.getFullYear();
      month = latest.getMonth();
    } else {
      year = new Date().getFullYear();
      month = new Date().getMonth();
    }
  } else {
    year = new Date().getFullYear();
    month = new Date().getMonth();
  }

  const daysInMonth = new Date(year, month + 1, 0).getDate();
  const firstDow = new Date(year, month, 1).getDay();

  const monthLabel = new Date(year, month, 1).toLocaleString('default', {
    month: 'long',
    year: 'numeric',
  });

  const cells: Array<number | null> = [
    ...Array.from({ length: firstDow }, () => null),
    ...Array.from({ length: daysInMonth }, (_, index) => index + 1),
  ];

  return (
    <div>
      <p className="text-[8pt] font-semibold text-slate-700 mb-[1.5mm]">{monthLabel}</p>
      <div className="grid grid-cols-7 gap-[0.8mm]">
        {DAY_HEADERS.map((day) => (
          <div key={day} className="text-center text-[6pt] text-gray-500 pb-[0.5mm]">{day}</div>
        ))}
        {cells.map((day, index) => {
          if (day === null) return <div key={`empty-${index}`} />;

          const key = `${year}-${String(month + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
          const value = valuesByDate.get(key);
          const status = getCalendarStatus(value);
          const label = getCellLabel(value);

          return (
            <div
              key={key}
              title={getTooltipText(key, value)}
              className={`${CALENDAR_CELL_CLASSES[status]} rounded-[1mm] p-[0.7mm] min-h-[8.5mm] flex flex-col justify-between`}
            >
              <span className="text-[6pt] text-gray-700 font-semibold leading-none">{day}</span>
              {label ? (
                <span className={`text-[5.5pt] leading-none ${CALENDAR_TEXT_CLASSES[status]}`}>
                  {label}
                </span>
              ) : null}
            </div>
          );
        })}
      </div>
      <div className="flex flex-wrap gap-[2mm] mt-[1.5mm] text-[6pt] text-gray-600">
        <span className="flex items-center gap-[0.7mm]">
          <span className="inline-block w-[2.5mm] h-[2.5mm] bg-green-50 border border-green-600 rounded-[0.5mm]" />
          Success
        </span>
        <span className="flex items-center gap-[0.7mm]">
          <span className="inline-block w-[2.5mm] h-[2.5mm] bg-red-50 border border-red-600 rounded-[0.5mm]" />
          Failed
        </span>
        <span className="flex items-center gap-[0.7mm]">
          <span className="inline-block w-[2.5mm] h-[2.5mm] bg-gray-100 border border-gray-200 rounded-[0.5mm]" />
          No backup
        </span>
      </div>
    </div>
  );
}

function CompactHeatmap({ values }: { values: HeatmapValue[] }) {
  const { weeks, startDate, endDate } = buildCompactWeeks(values);

  const weekCount = Math.max(weeks.length, 1);
  const columnGapMm = 0.3;
  const maxAvailableWidthMm = 82;
  const calculatedCellMm = (maxAvailableWidthMm - (weekCount - 1) * columnGapMm) / weekCount;
  const cellSizeMm = Math.max(0.9, Math.min(2.2, calculatedCellMm));

  return (
    <div>
      <div className="text-[6pt] text-gray-500 mb-[1mm]">
        {formatDateKey(startDate)} to {formatDateKey(endDate)}
      </div>

      <div className="flex" style={{ columnGap: `${columnGapMm}mm` }}>
        {weeks.map((week, weekIndex) => (
          <div key={weekIndex} className="flex flex-col" style={{ rowGap: `${columnGapMm}mm` }}>
            {week.map((day) => {
              if (!day.inRange) {
                return (
                  <span
                    key={day.date}
                    className="inline-block rounded-[0.4mm] border border-transparent"
                    style={{
                      width: `${cellSizeMm}mm`,
                      height: `${cellSizeMm}mm`,
                      backgroundColor: 'transparent',
                    }}
                  />
                );
              }

              const kind = getCompactCellKind(day.value);
              const cellStyle = COMPACT_CELL_STYLES[kind];

              return (
                <span
                  key={day.date}
                  title={getTooltipText(day.date, day.value)}
                  className="inline-block rounded-[0.4mm] border"
                  style={{
                    width: `${cellSizeMm}mm`,
                    height: `${cellSizeMm}mm`,
                    backgroundColor: cellStyle.bg,
                    borderColor: cellStyle.border,
                  }}
                />
              );
            })}
          </div>
        ))}
      </div>

      <div className="flex flex-wrap items-center gap-[2mm] mt-[1.5mm] text-[6pt] text-gray-600">
        <span className="flex items-center gap-[0.7mm]">
          <span className="inline-block w-[2.5mm] h-[2.5mm] rounded-[0.5mm] border" style={{ backgroundColor: COMPACT_CELL_STYLES.success.bg, borderColor: COMPACT_CELL_STYLES.success.border }} />
          Success
        </span>
        <span className="flex items-center gap-[0.7mm]">
          <span className="inline-block w-[2.5mm] h-[2.5mm] rounded-[0.5mm] border" style={{ backgroundColor: COMPACT_CELL_STYLES.mixed.bg, borderColor: COMPACT_CELL_STYLES.mixed.border }} />
          Mixed
        </span>
        <span className="flex items-center gap-[0.7mm]">
          <span className="inline-block w-[2.5mm] h-[2.5mm] rounded-[0.5mm] border" style={{ backgroundColor: COMPACT_CELL_STYLES.failed.bg, borderColor: COMPACT_CELL_STYLES.failed.border }} />
          Failed
        </span>
      </div>
    </div>
  );
}

function HeatmapPanel({ panel }: { panel: PanelResult }) {
  const values = buildHeatmapValues(panel.rows as Array<Record<string, unknown>> | undefined);
  const variant = resolveHeatmapVariant(panel);

  if (variant === 'compact') {
    return <CompactHeatmap values={values} />;
  }

  const monthGroups = groupValuesByMonth(values);

  if (monthGroups.length === 0) {
    return <CalendarMonthHeatmap values={[]} />;
  }

  return (
    <div className="flex flex-col gap-[3mm]">
      {monthGroups.map((group) => (
        <div key={group.key}>
          <CalendarMonthHeatmap values={group.values} monthKey={group.key} />
        </div>
      ))}
    </div>
  );
}

function PieChartPanel({ panel }: { panel: PanelResult }) {
  const rows = panel.rows ?? [];
  const entries = rows.map((r) => {
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
  const value = panel.rows?.[0]?.value ?? 0;
  const unit = panel.number?.unit ?? '';
  return (
    <div className="flex items-baseline gap-[1mm]">
      <span className="text-[18pt] font-bold text-slate-900">{value}</span>
      {unit && <span className="text-[8pt] text-gray-500">{unit}</span>}
    </div>
  );
}

function GaugePanel({ panel }: { panel: PanelResult }) {
  const value = Number(panel.rows?.[0]?.value ?? 0);
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
      {(panel.rows ?? []).map((r, i) => (
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
    case 'heatmap': content = <HeatmapPanel panel={panel} />; break;
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
      {panels.map((p, i) => (
        <div key={i}>
          <PanelCard panel={p} />
        </div>
      ))}
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
      {visibleCols.length > 0 && (
        <CompactTable variant="reference" columns={headers} data={tableRows} />
      )}

      {data.panels && data.panels.length > 0 && (
        <PanelsGrid panels={data.panels} />
      )}

      {data.sectionResults && data.sectionResults.length > 0 && (
        data.sectionResults.map((section, idx) => (
          section.view ? (
            <div key={idx}>
              <ViewResultSection data={section.view} />
            </div>
          ) : null
        ))
      )}
    </Section>
  );
}
