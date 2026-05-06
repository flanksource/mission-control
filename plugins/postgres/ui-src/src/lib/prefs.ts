// Tiny typed wrapper around localStorage for plugin-wide UI preferences.
// Returns the fallback when storage is unavailable (sandboxed iframes) or
// the stored value isn't one of the allowed enum entries.

export function readPref<T extends string>(key: string, allowed: readonly T[], fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    if (raw && (allowed as readonly string[]).includes(raw)) return raw as T;
  } catch {
    // ignore
  }
  return fallback;
}

export function writePref(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // ignore
  }
}
