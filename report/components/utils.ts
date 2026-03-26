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
  const diff = Date.now() - new Date(iso).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export function formatBytes(bytes: number): string {
  if (bytes >= 1e9) return `${(bytes / 1e9).toFixed(1)} GB`;
  if (bytes >= 1e6) return `${(bytes / 1e6).toFixed(1)} MB`;
  if (bytes >= 1e3) return `${(bytes / 1e3).toFixed(1)} KB`;
  return `${bytes} B`;
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
