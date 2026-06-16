import { useEffect, useState } from "react";

export type RecentKind = "config" | "playbook";

export type RecentItem = {
  kind: RecentKind;
  id: string;
  name: string;
  type?: string;
  icon?: string;
  href: string;
  lastUsed: string;
};

const STORAGE_KEY = "mc.recents.v1";
const MAX_ITEMS = 25;

function readStorage(): RecentItem[] {
  if (typeof window === "undefined") return [];
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(isRecentItem);
  } catch {
    return [];
  }
}

function writeStorage(items: RecentItem[]) {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(items));
    window.dispatchEvent(new CustomEvent(STORAGE_KEY));
  } catch {
    // Quota exceeded or storage disabled — best-effort, do nothing.
  }
}

function isRecentItem(value: unknown): value is RecentItem {
  if (!value || typeof value !== "object") return false;
  const v = value as Record<string, unknown>;
  return (
    (v.kind === "config" || v.kind === "playbook") &&
    typeof v.id === "string" &&
    typeof v.name === "string" &&
    typeof v.href === "string" &&
    typeof v.lastUsed === "string"
  );
}

export function getRecents(): RecentItem[] {
  return readStorage();
}

export function addRecent(input: Omit<RecentItem, "lastUsed">): void {
  if (!input.id || !input.name) return;
  const now = new Date().toISOString();
  const dedupKey = `${input.kind}:${input.id}`;
  const existing = readStorage().filter((item) => `${item.kind}:${item.id}` !== dedupKey);
  const next: RecentItem[] = [{ ...input, lastUsed: now }, ...existing].slice(0, MAX_ITEMS);
  writeStorage(next);
}

export function clearRecents(): void {
  writeStorage([]);
}

export function useRecents(): RecentItem[] {
  const [items, setItems] = useState<RecentItem[]>(() => readStorage());

  useEffect(() => {
    const refresh = () => setItems(readStorage());
    const onStorage = (event: StorageEvent) => {
      if (event.key === STORAGE_KEY || event.key === null) refresh();
    };
    window.addEventListener(STORAGE_KEY, refresh);
    window.addEventListener("storage", onStorage);
    return () => {
      window.removeEventListener(STORAGE_KEY, refresh);
      window.removeEventListener("storage", onStorage);
    };
  }, []);

  return items;
}
