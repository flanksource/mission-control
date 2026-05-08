import { afterEach, describe, expect, it, vi } from "vitest";
import { getAccessForGroup } from "./access";

describe("access API helpers", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("loads every permissions page for a group detail permissions tab", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = new URL(String(input), "http://incident-commander.local");
      const offset = Number(url.searchParams.get("offset") ?? "0");
      const page = offset === 0
        ? Array.from({ length: 500 }, (_, index) => ({
          config_id: `cfg-${index}`,
          config_name: `Config ${index}`,
          role: "reader",
        }))
        : [{ config_id: "cfg-500", config_name: "Config 500", role: "writer" }];

      return new Response(JSON.stringify(page), {
        status: 200,
        headers: {
          "Content-Type": "application/json",
          "Content-Range": `${offset}-${offset + page.length - 1}/501`,
        },
      });
    });

    const rows = await getAccessForGroup("group-1");

    expect(rows).toHaveLength(501);
    expect(rows.at(-1)).toMatchObject({ config_id: "cfg-500", role: "writer" });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    const paths = fetchMock.mock.calls.map(([path]) => new URL(String(path), "http://incident-commander.local"));
    expect(paths[0].searchParams.get("external_group_id")).toBe("eq.group-1");
    expect(paths.map((path) => path.searchParams.get("offset"))).toEqual(["0", "500"]);
  });
});
