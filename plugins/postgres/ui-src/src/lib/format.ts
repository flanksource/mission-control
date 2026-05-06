export function formatBytes(n: number | null | undefined): string {
  if (!n) return "—";
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  let v = n;
  let u = 0;
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024;
    u += 1;
  }
  return `${v.toFixed(v >= 100 ? 0 : v >= 10 ? 1 : 2)} ${units[u]}`;
}

export function formatNumber(n: number | null | undefined): string {
  if (n === null || n === undefined) return "—";
  return n.toLocaleString();
}

export function formatDuration(seconds: number | null | undefined): string {
  if (!seconds) return "—";
  if (seconds < 60) return `${seconds.toFixed(0)}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${Math.floor(seconds % 60)}s`;
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  return `${h}h ${m}m`;
}

export function formatPercent(v: number | null | undefined): string {
  if (v === null || v === undefined) return "—";
  return `${v.toFixed(1)}%`;
}
