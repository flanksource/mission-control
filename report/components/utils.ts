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

export function formatMillicores(value: number | string): string {
  const mc = typeof value === 'string' ? parseInt(value.replace(/m$/, ''), 10) : value;
  if (isNaN(mc)) return String(value);
  return mc >= 1000 ? `${(mc / 1000).toFixed(2)} cores` : `${mc}m`;
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
