import { afterEach, describe, expect, it, vi } from "vitest";
import { fetchAllPostgrest } from "./http";

describe("PostgREST API helpers", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("fetches all pages until the content-range total is reached", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = new URL(String(input), "http://incident-commander.local");
      const offset = Number(url.searchParams.get("offset") ?? "0");
      const page = offset === 0
        ? [{ id: "p-1" }, { id: "p-2" }]
        : offset === 2
          ? [{ id: "p-3" }, { id: "p-4" }]
          : [{ id: "p-5" }];
      return new Response(JSON.stringify(page), {
        status: 200,
        headers: {
          "Content-Type": "application/json",
          "Content-Range": `${offset}-${offset + page.length - 1}/5`,
        },
      });
    });

    const result = await fetchAllPostgrest<{ id: string }>("/db/config_access_summary?select=id&limit=1", 2);

    expect(result).toEqual({
      data: [{ id: "p-1" }, { id: "p-2" }, { id: "p-3" }, { id: "p-4" }, { id: "p-5" }],
      total: 5,
    });
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(fetchMock.mock.calls.map(([path]) => String(path))).toEqual([
      "/db/config_access_summary?select=id&limit=2&offset=0",
      "/db/config_access_summary?select=id&limit=2&offset=2",
      "/db/config_access_summary?select=id&limit=2&offset=4",
    ]);
  });

  it("stops on a short page when the total is not reported", async () => {
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(new Response(JSON.stringify([{ id: "p-1" }, { id: "p-2" }]), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify([{ id: "p-3" }]), { status: 200 }));

    await expect(fetchAllPostgrest<{ id: string }>("/db/config_access_summary?select=id", 2))
      .resolves.toEqual({
        data: [{ id: "p-1" }, { id: "p-2" }, { id: "p-3" }],
        total: 3,
      });
  });
});
