// Result-set exporters used by the Console tab: JSON + CSV plus a small
// download helper.

export interface ExportColumn {
  key: string;
  label?: string;
}

export function toJson(rows: Record<string, unknown>[]): string {
  return JSON.stringify(rows, null, 2);
}

export function toCsv(rows: Record<string, unknown>[], columns: ExportColumn[]): string {
  const header = columns.map((c) => escapeCsv(c.label ?? c.key)).join(",");
  const body = rows
    .map((row) => columns.map((c) => escapeCsv(row[c.key])).join(","))
    .join("\n");
  return body ? `${header}\n${body}\n` : `${header}\n`;
}

function escapeCsv(value: unknown): string {
  const s = formatScalar(value);
  if (/[",\n\r]/.test(s)) return `"${s.replace(/"/g, '""')}"`;
  return s;
}

export function formatScalar(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

// downloadBlob triggers a client-side file save. Returns false when the
// browser disallows it (e.g. iframe sandbox without allow-downloads).
export function downloadBlob(filename: string, mime: string, content: string): boolean {
  try {
    const blob = new Blob([content], { type: mime });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
    return true;
  } catch {
    return false;
  }
}

export function slugify(raw: string, fallback = "results"): string {
  const cleaned = raw
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return cleaned || fallback;
}
