import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const STORAGE_KEY = "mc.recents.v1";

beforeAll(() => {
  const store = new Map<string, string>();
  const localStorage: Storage = {
    get length() {
      return store.size;
    },
    clear: () => store.clear(),
    getItem: (key) => (store.has(key) ? store.get(key)! : null),
    key: (index) => Array.from(store.keys())[index] ?? null,
    removeItem: (key) => {
      store.delete(key);
    },
    setItem: (key, value) => {
      store.set(key, String(value));
    },
  };
  const listeners = new Map<string, Set<EventListener>>();
  const w = {
    localStorage,
    addEventListener: (type: string, listener: EventListener) => {
      const set = listeners.get(type) ?? new Set();
      set.add(listener);
      listeners.set(type, set);
    },
    removeEventListener: (type: string, listener: EventListener) => {
      listeners.get(type)?.delete(listener);
    },
    dispatchEvent: (event: Event) => {
      listeners.get(event.type)?.forEach((listener) => listener(event));
      return true;
    },
  } as unknown as Window & typeof globalThis;
  (globalThis as Record<string, unknown>).window = w;
  (globalThis as Record<string, unknown>).localStorage = localStorage;
  if (typeof CustomEvent === "undefined") {
    (globalThis as Record<string, unknown>).CustomEvent = class<T> extends Event {
      detail: T | undefined;
      constructor(type: string, init?: CustomEventInit<T>) {
        super(type, init);
        this.detail = init?.detail;
      }
    };
  }
});

// Import after globals are installed so the module can reference `window`.
const { addRecent, clearRecents, getRecents } = await import("./recents");

describe("recents", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns empty list when storage is empty", () => {
    expect(getRecents()).toEqual([]);
  });

  it("adds a new item with a lastUsed timestamp", () => {
    addRecent({ kind: "config", id: "c-1", name: "EC2 i-abc", type: "AWS::EC2::Instance", href: "/ui/item/c-1" });
    const recents = getRecents();
    expect(recents).toHaveLength(1);
    expect(recents[0]).toMatchObject({
      kind: "config",
      id: "c-1",
      name: "EC2 i-abc",
      type: "AWS::EC2::Instance",
      href: "/ui/item/c-1",
    });
    expect(typeof recents[0].lastUsed).toBe("string");
    expect(Date.parse(recents[0].lastUsed)).not.toBeNaN();
  });

  it("dedupes by kind:id and bumps the existing item to the top", () => {
    addRecent({ kind: "config", id: "c-1", name: "First", href: "/ui/item/c-1" });
    addRecent({ kind: "config", id: "c-2", name: "Second", href: "/ui/item/c-2" });
    addRecent({ kind: "config", id: "c-1", name: "First (renamed)", href: "/ui/item/c-1" });
    const recents = getRecents();
    expect(recents).toHaveLength(2);
    expect(recents[0].id).toBe("c-1");
    expect(recents[0].name).toBe("First (renamed)");
    expect(recents[1].id).toBe("c-2");
  });

  it("treats config and playbook kinds as separate keys", () => {
    addRecent({ kind: "config", id: "x", name: "Config X", href: "/ui/item/x" });
    addRecent({ kind: "playbook", id: "x", name: "Playbook X", href: "/ui/playbooks/runs/x" });
    expect(getRecents()).toHaveLength(2);
  });

  it("caps the list at 25 items, dropping the oldest", () => {
    for (let i = 0; i < 30; i++) {
      addRecent({ kind: "config", id: `c-${i}`, name: `Item ${i}`, href: `/ui/item/c-${i}` });
    }
    const recents = getRecents();
    expect(recents).toHaveLength(25);
    expect(recents[0].id).toBe("c-29");
    expect(recents[recents.length - 1].id).toBe("c-5");
  });

  it("ignores entries with empty id or name", () => {
    addRecent({ kind: "config", id: "", name: "Bad", href: "/ui/item/" });
    addRecent({ kind: "config", id: "ok", name: "", href: "/ui/item/ok" });
    expect(getRecents()).toEqual([]);
  });

  it("clearRecents empties the list", () => {
    addRecent({ kind: "config", id: "c-1", name: "One", href: "/ui/item/c-1" });
    clearRecents();
    expect(getRecents()).toEqual([]);
  });

  it("returns empty list when storage holds malformed JSON", () => {
    window.localStorage.setItem(STORAGE_KEY, "{not json");
    expect(getRecents()).toEqual([]);
  });

  it("filters out entries that do not match the schema", () => {
    window.localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify([
        { kind: "config", id: "good", name: "Good", href: "/ui/item/good", lastUsed: new Date().toISOString() },
        { kind: "unknown", id: "bad", name: "Bad", href: "/x", lastUsed: new Date().toISOString() },
        { id: "missing-kind", name: "X", href: "/x" },
      ]),
    );
    const recents = getRecents();
    expect(recents).toHaveLength(1);
    expect(recents[0].id).toBe("good");
  });
});
