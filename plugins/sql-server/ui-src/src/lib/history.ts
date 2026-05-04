// localStorage-backed history of executed SQL statements:
// 50-entry cap, deduped (re-running an old query bumps it to the top),
// safe to call from the iframe (silently no-ops if storage is denied).

const STORAGE_KEY = "sql-server-console-history";
export const MAX_HISTORY = 50;

export interface HistoryEntry {
  query: string;
  timestamp: number;
}

export function loadHistory(): HistoryEntry[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? (parsed as HistoryEntry[]) : [];
  } catch {
    return [];
  }
}

export function saveToHistory(query: string): HistoryEntry[] {
  const trimmed = query.trim();
  if (!trimmed) return loadHistory();
  const deduped = loadHistory().filter((h) => h.query !== trimmed);
  const next: HistoryEntry[] = [{ query: trimmed, timestamp: Date.now() }, ...deduped].slice(
    0,
    MAX_HISTORY,
  );
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
  } catch {
    // ignore — host disabled storage
  }
  return next;
}

export function clearHistory(): void {
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    // ignore
  }
}
