const MINUTE = 60;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const WEEK = 7 * DAY;

export function formatRelativeTime(input: string | number | Date | null | undefined, now: Date = new Date()): string {
  if (!input) return "";
  const date = input instanceof Date ? input : new Date(input);
  const ms = date.getTime();
  if (Number.isNaN(ms)) return "";

  const diffSeconds = Math.round((now.getTime() - ms) / 1000);
  if (diffSeconds < 5) return "just now";
  if (diffSeconds < MINUTE) return `${diffSeconds}s ago`;
  if (diffSeconds < HOUR) return `${Math.floor(diffSeconds / MINUTE)}m ago`;
  if (diffSeconds < DAY) return `${Math.floor(diffSeconds / HOUR)}h ago`;
  if (diffSeconds < WEEK) return `${Math.floor(diffSeconds / DAY)}d ago`;
  return date.toLocaleDateString();
}
