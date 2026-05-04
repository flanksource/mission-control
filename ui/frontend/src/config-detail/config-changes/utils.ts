export function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString('en-US', {
    year: 'numeric', month: 'short', day: 'numeric'
  });
}

export function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString('en-US', {
    year: 'numeric', month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit'
  });
}

export function formatRelative(iso: string): string {
  return formatMonthDay(iso);
}

export function formatTime(iso: string): string {
  return new Date(iso).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
}

export function formatMonthDay(iso: string): string {
  const d = new Date(iso);
  const now = new Date();
  if (d.getFullYear() !== now.getFullYear()) {
    return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
  }
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

export type TimeBucketFormat = 'time' | 'monthDay';

export interface TimeBucket {
  key: string;
  label: string;
  dateFormat: TimeBucketFormat;
}

export function getTimeBucket(iso: string): TimeBucket {
  const d = new Date(iso);
  const now = new Date();
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const diffDays = Math.floor((startOfToday.getTime() - new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()) / 86400000);

  if (diffDays <= 0) {
    return { key: 'today', label: formatDayLabel(d), dateFormat: 'time' };
  }
  if (diffDays <= 6) {
    return { key: `day-${diffDays}`, label: formatDayLabel(d), dateFormat: 'time' };
  }
  if (diffDays <= 30) {
    const weekStart = new Date(d);
    weekStart.setDate(d.getDate() - d.getDay() + 1);
    const weekEnd = new Date(weekStart);
    weekEnd.setDate(weekStart.getDate() + 4);
    const fmt = (dt: Date) => dt.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
    return { key: `week-${fmt(weekStart)}`, label: `${fmt(weekStart)} – ${fmt(weekEnd)}`, dateFormat: 'monthDay' };
  }
  const monthLabel = d.toLocaleDateString('en-US', { month: 'long', year: 'numeric' });
  return { key: `month-${d.getFullYear()}-${d.getMonth()}`, label: monthLabel, dateFormat: 'monthDay' };
}

function formatDayLabel(d: Date): string {
  return d.toLocaleDateString('en-US', { weekday: 'long', month: 'long', day: 'numeric' });
}

export function formatEntryDate(iso: string, fmt: TimeBucketFormat): string {
  return fmt === 'time' ? formatTime(iso) : formatMonthDay(iso);
}

export function formatBytes(bytes: number): string {
  if (bytes >= 1e9) return `${(bytes / 1e9).toFixed(1)} GB`;
  if (bytes >= 1e6) return `${(bytes / 1e6).toFixed(1)} MB`;
  if (bytes >= 1e3) return `${(bytes / 1e3).toFixed(1)} KB`;
  return `${bytes} B`;
}

const SIZE_WITH_UNIT_RE = /^\s*(\d+(?:\.\d+)?)\s*([KMGTP]i?B|B|bytes?)\s*$/i;
const UNIT_CANONICAL: Record<string, string> = {
  B: 'B', BYTE: 'B', BYTES: 'B',
  KB: 'KB', MB: 'MB', GB: 'GB', TB: 'TB', PB: 'PB',
  KIB: 'KiB', MIB: 'MiB', GIB: 'GiB', TIB: 'TiB', PIB: 'PiB',
};

export function humanizeSize(value: unknown): string | undefined {
  if (value === null || value === undefined) return undefined;

  if (typeof value === 'number' && Number.isFinite(value)) {
    return formatBytes(value);
  }

  if (typeof value !== 'string') return undefined;
  const trimmed = value.trim();
  if (!trimmed) return undefined;

  if (/^-?\d+(\.\d+)?$/.test(trimmed)) {
    const n = Number(trimmed);
    return Number.isFinite(n) ? formatBytes(n) : undefined;
  }

  const m = trimmed.match(SIZE_WITH_UNIT_RE);
  if (m) {
    const unit = UNIT_CANONICAL[m[2].toUpperCase()] ?? m[2];
    return `${m[1]} ${unit}`;
  }

  return undefined;
}

/**
 * Formats a millicore value for display, accepting both numeric and string inputs.
 *
 * Rules:
 * - 0 → "0" (no unit)
 * - sub-millicore (0 < v < 1) → "1m" (rounded up)
 * - millicores (1–999) → rounded integer with "m" suffix (e.g. "500m")
 * - cores (≥ 1000) → converted to cores, no unit (e.g. "2", "1.5")
 */
export function formatMillicores(value: number | string): string {
  let mc: number;
  if (typeof value === 'string') {
    mc = parseInt(value.replace(/m$/, ''), 10);
    if (isNaN(mc)) return String(value);
  } else if (typeof value === 'number') {
    mc = value;
  } else {
    return String(value);
  }

  if (mc === 0) return '0';
  if (mc > 0 && mc < 1) return '1m';
  if (mc >= 1000) {
    const cores = mc / 1000;
    return cores === Math.round(cores) ? `${Math.round(cores)}` : `${cores.toFixed(1)}`;
  }
  return `${Math.round(mc)}m`;
}

export function formatDurationMs(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  if (ms < 3600000) return `${Math.floor(ms / 60000)}m ${Math.floor((ms % 60000) / 1000)}s`;
  return `${Math.floor(ms / 3600000)}h ${Math.floor((ms % 3600000) / 60000)}m`;
}

export function formatPropertyValue(value?: number, text?: string, unit?: string): string {
  if (text) return text;
  if (value == null) return '-';
  switch (unit) {
    case 'bytes': return formatBytes(value);
    case 'milliseconds': return formatDurationMs(value);
    case 'millicores': return formatMillicores(value);
    case 'epoch': return formatDate(new Date(value * 1000).toISOString());
    default: return String(value);
  }
}

export const SEVERITY_COLORS: Record<string, string> = {
  critical: '#DC2626',
  high: '#EA580C',
  medium: '#D97706',
  low: '#2563EB',
};

export const SEVERITY_BG: Record<string, string> = {
  critical: '#FEE2E2',
  high: '#FFEDD5',
  medium: '#FEF3C7',
  low: '#DBEAFE',
};

export const HEALTH_COLORS: Record<string, string> = {
  healthy: '#16A34A',
  warning: '#D97706',
  unhealthy: '#DC2626',
  unknown: '#6B7280',
};

export const PURPOSE_COLORS: Record<string, string> = {
  primary: '#2563EB',
  backup: '#D97706',
  dr: '#DC2626',
};

/**
 * Formats a numeric value for display with optional unit and precision handling.
 */
export function formatDisplayValue(
  value: number,
  unit?: string,
  precision?: number,
): string {
  if (!unit) {
    return Number(value.toFixed(precision ?? 0)).toString();
  }
  switch (unit) {
    case 'percent':
      return `${Number(value.toFixed(precision ?? 0))}%`;
    case 'bytes':
      return formatBytes(value);
    case 'millicores':
    case 'millicore':
      return formatMillicores(value);
    default: {
      const rounded = Number(value.toFixed(precision ?? 0));
      return `${rounded} ${unit}`;
    }
  }
}

/**
 * Determines the color for a gauge based on percentage and defined thresholds.
 */
export function getGaugeColor(
  percentage: number,
  thresholds: Array<{ percent: number; color: string }>,
): string {
  const sorted = [...thresholds].sort((a, b) => a.percent - b.percent);
  let color = '#3B82F6';
  for (const t of sorted) {
    if (percentage >= t.percent) color = t.color;
  }
  return color;
}
