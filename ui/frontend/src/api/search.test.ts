import { afterEach, describe, expect, it, vi } from "vitest";
import { globalSearch } from "./search";

describe("globalSearch", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("forwards the raw query to /resources/search so grammar is parsed server-side", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockImplementation(async (input, init) => {
        const url = new URL(String(input), "http://incident-commander.local");

        if (url.pathname === "/resources/search") {
          const body = JSON.parse(String(init?.body ?? "{}"));
          expect(body).toEqual({
            limit: 8,
            configs: [{ search: "type=pod prometheus", agent: "all" }],
          });

          return new Response(
            JSON.stringify({
              configs: [
                {
                  id: "cfg-1",
                  name: "prometheus-pod",
                  type: "Kubernetes::Pod",
                  status: "Running",
                  health: "healthy",
                  icon: "config",
                },
              ],
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }

        if (url.pathname === "/db/external_users") {
          return new Response(JSON.stringify([]), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        }

        if (url.pathname === "/db/external_groups") {
          return new Response(JSON.stringify([]), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        }

        throw new Error(`unexpected request: ${url.pathname}`);
      });

    const results = await globalSearch("type=pod prometheus");

    expect(results).toEqual([
      {
        id: "config:cfg-1",
        kind: "config",
        title: "prometheus-pod",
        subtitle: "Kubernetes::Pod",
        meta: "Running",
        href: "/ui/item/cfg-1",
      },
    ]);

    const configCall = fetchMock.mock.calls.find(([path]) => {
      const url = new URL(String(path), "http://incident-commander.local");
      return url.pathname === "/db/config_detail";
    });
    expect(configCall).toBeUndefined();

    const resourceSearchCall = fetchMock.mock.calls.find(([path]) => {
      const url = new URL(String(path), "http://incident-commander.local");
      return url.pathname === "/resources/search";
    });
    expect(resourceSearchCall).toBeDefined();
  });

  it("returns no results and skips network calls for queries shorter than 2 chars", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify([]), { status: 200 }),
    );

    const results = await globalSearch("a");

    expect(results).toEqual([]);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("surfaces /resources/search errors", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = new URL(String(input), "http://incident-commander.local");
      if (url.pathname === "/resources/search") {
        return new Response("upstream blew up", { status: 500 });
      }
      return new Response(JSON.stringify([]), { status: 200 });
    });

    await expect(globalSearch("type=pod")).rejects.toThrow(/resources\/search/);
  });
});
