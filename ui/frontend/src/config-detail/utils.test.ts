import { describe, expect, it } from "vitest";
import type { ConfigAccessSummary, ConfigAnalysis, ConfigChange } from "../api/types";
import {
  NIL_UUID,
  accessSummaryToRBACUserRole,
  buildGroupedRBACMatrix,
  buildRBACMatrix,
  buildRBACResource,
  dedupeTagEntries,
  groupChangesByDay,
  groupInsightsByType,
  isIndirectAccess,
  isKnownHealth,
  isNilUUID,
  severityCounts,
} from "./utils";

describe("config detail utilities", () => {
  it("adapts access summary rows to report-style RBAC user roles", () => {
    expect(accessSummaryToRBACUserRole({
      external_user_id: "u-1",
      user: "Alice",
      email: "alice@example.com",
      role: "admin",
      user_type: "human",
    })).toMatchObject({
      userId: "u-1",
      userName: "Alice",
      role: "admin",
      roleSource: "direct",
      sourceSystem: "human",
      isStale: false,
      isReviewOverdue: false,
    });

    expect(accessSummaryToRBACUserRole({
      external_user_id: NIL_UUID,
      external_group_id: "g-1",
      user: "Platform",
      role: "reader",
      user_type: "group",
    }).roleSource).toBe("group:Platform");
  });

  it("builds an RBAC matrix by principal and role", () => {
    const rows: ConfigAccessSummary[] = [
      {
        config_id: "c-1",
        external_user_id: "u-1",
        user: "Alice",
        email: "alice@example.com",
        role: "reader",
      },
      {
        config_id: "c-1",
        external_user_id: "u-1",
        user: "Alice",
        email: "alice@example.com",
        role: "admin",
      },
      {
        config_id: "c-1",
        external_user_id: NIL_UUID,
        external_group_id: "g-1",
        user: "Platform",
        role: "reader",
      },
    ];

    const resource = buildRBACResource({ id: "c-1", name: "app", type: "Kubernetes::Deployment" }, rows);
    const matrix = buildRBACMatrix(resource);

    expect(matrix.roles).toEqual(["admin", "reader"]);
    expect(matrix.users.map((user) => user.name)).toEqual(["Alice", "Platform"]);
    expect(matrix.users[0].roles.get("admin")?.email).toBe("alice@example.com");
    expect(matrix.users[1].groupId).toBe("g-1");
    expect(isIndirectAccess(resource.users[2])).toBe(true);
  });

  it("prefers direct access when duplicate principal-role rows exist", () => {
    const resource = buildRBACResource({ id: "c-1", name: "app" }, [
      {
        external_user_id: "u-1",
        external_group_id: "g-1",
        user: "Alice",
        role: "reader",
      },
      {
        external_user_id: "u-1",
        user: "Alice",
        role: "reader",
      },
    ]);

    const matrix = buildRBACMatrix(resource);

    expect(matrix.users[0].roles.get("reader")?.roleSource).toBe("direct");
  });

  it("groups indirect users under their source group", () => {
    const resource = buildRBACResource({ id: "c-1", name: "app" }, [
      {
        external_user_id: "u-1",
        external_group_id: "g-1",
        group_name: "Platform",
        user: "Alice",
        email: "alice@example.com",
        role: "reader",
      },
      {
        external_user_id: "u-2",
        external_group_id: "g-1",
        group_name: "Platform",
        user: "Bob",
        role: "admin",
      },
    ]);

    const matrix = buildGroupedRBACMatrix(resource);

    expect(matrix.directUsers).toHaveLength(0);
    expect(matrix.roles).toEqual(["admin", "reader"]);
    expect(matrix.groups.map((group) => group.groupName)).toEqual(["Platform"]);
    expect(matrix.groups[0].users.map((user) => user.name)).toEqual(["Alice", "Bob"]);
    expect(matrix.groups[0].users[0].roles.get("reader")?.userId).toBe("u-1");
  });

  it("keeps direct rows direct while retaining indirect group children", () => {
    const resource = buildRBACResource({ id: "c-1", name: "app" }, [
      {
        external_user_id: "u-1",
        external_group_id: "g-1",
        group_name: "Platform",
        user: "Alice",
        role: "reader",
      },
      {
        external_user_id: "u-1",
        user: "Alice",
        role: "reader",
      },
    ]);

    const matrix = buildGroupedRBACMatrix(resource);

    expect(matrix.directUsers).toHaveLength(1);
    expect(matrix.directUsers[0].roles.get("reader")?.roleSource).toBe("direct");
    expect(matrix.groups).toHaveLength(1);
    expect(matrix.groups[0].users[0].roles.get("reader")?.roleSource).toBe("group:Platform");
  });

  it("keeps group-only nil UUID grants on the group row", () => {
    const resource = buildRBACResource({ id: "c-1", name: "app" }, [
      {
        external_user_id: NIL_UUID,
        external_group_id: "g-1",
        group_name: "Platform",
        user: "Platform",
        role: "reader",
      },
    ]);

    const matrix = buildGroupedRBACMatrix(resource);

    expect(matrix.groups).toHaveLength(1);
    expect(matrix.groups[0].groupId).toBe("g-1");
    expect(matrix.groups[0].roles.get("reader")?.groupId).toBe("g-1");
    expect(matrix.groups[0].users).toHaveLength(0);
  });

  it("separates identical indirect roles granted by different groups", () => {
    const resource = buildRBACResource({ id: "c-1", name: "app" }, [
      {
        external_user_id: "u-1",
        external_group_id: "g-1",
        group_name: "Platform",
        user: "Alice",
        role: "reader",
      },
      {
        external_user_id: "u-1",
        external_group_id: "g-2",
        group_name: "Auditors",
        user: "Alice",
        role: "reader",
      },
    ]);

    const matrix = buildGroupedRBACMatrix(resource);

    expect(matrix.groups.map((group) => group.groupName)).toEqual(["Auditors", "Platform"]);
    expect(matrix.groups.every((group) => group.users[0].name === "Alice")).toBe(true);
  });

  it("treats unknown health and nil UUIDs as empty display values", () => {
    expect(isKnownHealth("healthy")).toBe(true);
    expect(isKnownHealth("unknown")).toBe(false);
    expect(isKnownHealth(null)).toBe(false);
    expect(isNilUUID(NIL_UUID)).toBe(true);
    expect(isNilUUID(null)).toBe(true);
    expect(isNilUUID("agent-1")).toBe(false);
  });

  it("dedupes tag entries against labels", () => {
    expect(
      dedupeTagEntries(
        [
          ["env", "prod"],
          ["team", "platform"],
          ["ENV", "prod"],
        ],
        [["env", "prod"]],
      ),
    ).toEqual([["team", "platform"]]);
  });

  it("counts severities with info as the fallback", () => {
    const items = [
      { severity: "critical" },
      { severity: "critical" },
      { severity: "medium" },
      {},
    ];

    expect(severityCounts(items)).toEqual({
      critical: 2,
      medium: 1,
      info: 1,
    });
  });

  it("groups changes into date buckets", () => {
    const changes: ConfigChange[] = [
      { id: "1", config_id: "c", change_type: "Updated", created_at: "2026-04-20T12:00:00Z" },
      { id: "2", config_id: "c", change_type: "Updated", created_at: "2026-04-20T13:00:00Z" },
    ];

    const groups = groupChangesByDay(changes);

    expect(groups).toHaveLength(1);
    expect(groups[0].items).toHaveLength(2);
  });

  it("groups insights by analysis type", () => {
    const insights: ConfigAnalysis[] = [
      { id: "1", config_id: "c", analysis_type: "security" },
      { id: "2", config_id: "c", analysis_type: "cost" },
      { id: "3", config_id: "c" },
    ];

    expect(groupInsightsByType(insights).map(([type]) => type)).toEqual(["cost", "other", "security"]);
  });
});
