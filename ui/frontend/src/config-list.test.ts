import { afterEach, describe, expect, it, vi } from "vitest";
import {
  buildConfigListQuery,
  buildLabelAndTagClause,
  filterConfigItems,
  searchConfigItems,
  groupConfigItems,
  parseConfigListFilters,
  parseTriStateParam,
  serializeTriStateParam,
  triStateToFilterExpression,
} from "./config-list";
import type { ConfigItem } from "./api/types";

describe("config-list filters", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("parses URL filters using the route config type", () => {
    const filters = parseConfigListFilters(
      new URLSearchParams(
        "search=api&labels=env____prod:1,team____core:-1&status=Ready:1&health=healthy:1&groupBy=changed,cluster__tag&showDeletedConfigs=true",
      ),
      "Kubernetes::Deployment",
    );

    expect(filters).toMatchObject({
      configType: "Kubernetes::Deployment",
      search: "api",
      labels: { env____prod: "include", team____core: "exclude" },
      status: { Ready: "include" },
      health: { healthy: "include" },
      groupBy: ["changed", "cluster__tag"],
      showDeleted: true,
    });
  });

  it("serializes tri-state params and filter expressions", () => {
    const value = parseTriStateParam("healthy:1,unknown:-1");

    expect(value).toEqual({ healthy: "include", unknown: "exclude" });
    expect(serializeTriStateParam(value)).toBe("healthy:1,unknown:-1");
    expect(triStateToFilterExpression(value)).toBe("healthy,!unknown");
  });

  it("builds a PostgREST config query from active filters", () => {
    const query = new URLSearchParams(
      buildConfigListQuery({
        configType: "Kubernetes::Deployment",
        search: "api",
        labels: { env____prod: "include" },
        status: { Ready: "include", Deleted: "exclude" },
        health: { healthy: "include" },
        groupBy: [],
        showDeleted: false,
        limit: 50,
      }),
    );

    expect(query.get("type")).toBe("eq.Kubernetes::Deployment");
    expect(query.get("deleted_at")).toBe("is.null");
    expect(query.get("status.filter")).toBe("Ready,!Deleted");
    expect(query.get("health.filter")).toBe("healthy");
    expect(query.get("or")).toContain("name.ilike.*api*");
    expect(query.get("and")).toBe("(or(labels->>env.eq.prod,tags->>env.eq.prod))");
  });

  it("builds include and exclude clauses for combined label/tag filters", () => {
    expect(buildLabelAndTagClause({ env____prod: "include", team____core: "exclude" })).toBe(
      "(or(labels->>env.eq.prod,tags->>env.eq.prod),labels->>team.neq.core,tags->>team.neq.core)",
    );
  });

  it("groups rows by fields and tag keys", () => {
    const rows = [
      config({ id: "1", name: "api", changes: 2, tags: { cluster: "prod" } }),
      config({ id: "2", name: "worker", changes: 0, tags: { cluster: "prod" } }),
      config({ id: "3", name: "batch", changes: 0, tags: { cluster: "dev" } }),
    ];

    const groups = groupConfigItems(rows, ["changed", "cluster__tag"]);

    expect(groups.map((group) => [group.label, group.rows.map((row) => row.id)])).toEqual([
      ["Changed / prod", ["1"]],
      ["No changes / dev", ["3"]],
      ["No changes / prod", ["2"]],
    ]);
  });

  it("searches configs via the authenticated resources API", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({
      configs: [
        { id: "1", name: "AcmExpiryNotifier", type: "AWS::Lambda::Function", tags: { account: "123456789012" }, health: "unknown" },
      ],
    }), { status: 200 }));

    const rows = await searchConfigItems({
      configType: "AWS::Lambda::Function",
      search: "",
      labels: {},
      status: {},
      health: {},
      groupBy: [],
      showDeleted: false,
      limit: 500,
    });

    expect(rows).toEqual([
      expect.objectContaining({
        id: "1",
        name: "AcmExpiryNotifier",
        type: "AWS::Lambda::Function",
        tags: { account: "123456789012" },
      }),
    ]);
    expect(fetchMock).toHaveBeenCalledWith("/resources/search", expect.objectContaining({
      credentials: "same-origin",
      method: "POST",
      body: JSON.stringify({ limit: 500, configs: [{ types: ["AWS::Lambda::Function"] }] }),
    }));
  });

  it("filters searched configs client-side", () => {
    const rows = [
      config({ id: "1", name: "api", status: "Ready", health: "healthy", tags: { env: "prod" } }),
      config({ id: "2", name: "worker", status: "Ready", health: "unknown", tags: { env: "dev" } }),
    ];

    expect(filterConfigItems(rows, {
      configType: "Kubernetes::Deployment",
      search: "api",
      labels: { env____prod: "include" },
      status: { Ready: "include" },
      health: { unknown: "exclude" },
      groupBy: [],
      showDeleted: false,
      limit: 500,
    }).map((row) => row.id)).toEqual(["1"]);
  });
});

function config(overrides: Partial<ConfigItem>): ConfigItem {
  return {
    id: "",
    name: "",
    type: "Kubernetes::Deployment",
    ...overrides,
  };
}
