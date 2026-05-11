import type { ConfigAccessSummary, ConfigAnalysis, ConfigChange, ConfigItem, ConfigSeverity } from "../api/types";

export const NIL_UUID = "00000000-0000-0000-0000-000000000000";

export type RBACRoleSource = "direct" | `group:${string}`;

export type RBACUserRole = {
  userId: string;
  userName: string;
  email: string;
  role: string;
  roleSource: RBACRoleSource;
  groupId?: string | null;
  groupName?: string | null;
  sourceSystem: string;
  createdAt?: string;
  lastSignedInAt?: string | null;
  lastReviewedAt?: string | null;
  isStale: boolean;
  isReviewOverdue: boolean;
};

export type RBACResource = {
  configId: string;
  configName: string;
  configType: string;
  configClass?: string;
  path?: string;
  tags?: Record<string, string> | null;
  labels?: Record<string, string> | null;
  users: RBACUserRole[];
};

export type RBACMatrixUser = {
  key: string;
  name: string;
  email?: string;
  userId: string;
  groupId?: string | null;
  groupName?: string | null;
  roleSource?: RBACRoleSource;
  roles: Map<string, RBACUserRole>;
};

export type RBACMatrixGroup = {
  key: string;
  groupId?: string | null;
  groupName: string;
  roles: Map<string, RBACUserRole>;
  users: RBACMatrixUser[];
};

export function buildRBACResource(config: ConfigItem, rows: ConfigAccessSummary[]): RBACResource {
  return {
    configId: config.id,
    configName: config.name || rows[0]?.config_name || config.id,
    configType: config.type || rows[0]?.config_type || "",
    configClass: config.config_class,
    path: config.path,
    tags: config.tags,
    labels: config.labels,
    users: rows.map(accessSummaryToRBACUserRole),
  };
}

export function accessSummaryToRBACUserRole(row: ConfigAccessSummary): RBACUserRole {
  const userName = row.user || row.email || "Unknown";
  const groupName = row.group_name || (row.external_user_id === NIL_UUID ? userName : row.external_group_id);
  return {
    userId: row.external_user_id || NIL_UUID,
    userName,
    email: row.email || "",
    role: row.role || "unknown",
    roleSource: row.external_group_id ? `group:${groupName || userName}` : "direct",
    groupId: row.external_group_id,
    groupName,
    sourceSystem: row.user_type || "",
    createdAt: row.created_at,
    lastSignedInAt: row.last_signed_in_at,
    lastReviewedAt: row.last_reviewed_at,
    isStale: false,
    isReviewOverdue: false,
  };
}

export function buildRBACMatrix(resource: RBACResource) {
  const roleSet = new Set<string>();
  const userMap = new Map<string, RBACMatrixUser>();

  for (const row of resource.users) {
    const role = row.role || "unknown";
    roleSet.add(role);

    const key = principalKey(row);
    const groupId = row.roleSource !== "direct" ? row.groupId ?? null : null;
    const existing = userMap.get(key);
    if (existing) {
      existing.roles.set(role, mergeAccessRows(existing.roles.get(role), row));
      if (existing.roleSource !== "direct" && row.roleSource === "direct") {
        existing.roleSource = row.roleSource;
        existing.groupId = null;
      } else if (existing.roleSource !== "direct" && !existing.groupId && groupId) {
        existing.groupId = groupId;
      }
      continue;
    }

    userMap.set(key, {
      key,
      userId: row.userId,
      groupId,
      name: row.userName || row.email || "Unknown",
      email: row.email,
      roleSource: row.roleSource,
      roles: new Map([[role, row]]),
    });
  }

  return {
    roles: Array.from(roleSet).sort((a, b) => a.localeCompare(b)),
    users: Array.from(userMap.values()).sort((a, b) => a.name.localeCompare(b.name)),
  };
}

export function buildGroupedRBACMatrix(resource: RBACResource) {
  const roleSet = new Set<string>();
  const directUsers = new Map<string, RBACMatrixUser>();
  const groups = new Map<string, RBACMatrixGroup>();

  for (const row of resource.users) {
    const role = row.role || "unknown";
    roleSet.add(role);

    if (!isIndirectAccess(row)) {
      const user = upsertMatrixUser(directUsers, principalKey(row), row, null);
      user.roles.set(role, mergeAccessRows(user.roles.get(role), row));
      continue;
    }

    const groupKey = row.groupId ? `group:${row.groupId}` : row.roleSource;
    const groupName = row.groupName || row.roleSource.replace(/^group:/, "") || row.groupId || "Unknown group";
    let group = groups.get(groupKey);
    if (!group) {
      group = {
        key: groupKey,
        groupId: row.groupId,
        groupName,
        roles: new Map(),
        users: [],
      };
      groups.set(groupKey, group);
    }
    group.roles.set(role, mergeAccessRows(group.roles.get(role), row));

    if (row.userId && row.userId !== NIL_UUID) {
      const users = new Map(group.users.map((user) => [user.key, user]));
      const user = upsertMatrixUser(users, principalKey(row), row, row.groupId ?? null);
      user.roles.set(role, mergeAccessRows(user.roles.get(role), row));
      group.users = Array.from(users.values()).sort((a, b) => a.name.localeCompare(b.name));
    }
  }

  return {
    roles: Array.from(roleSet).sort((a, b) => a.localeCompare(b)),
    directUsers: Array.from(directUsers.values()).sort((a, b) => a.name.localeCompare(b.name)),
    groups: Array.from(groups.values()).sort((a, b) => a.groupName.localeCompare(b.groupName)),
  };
}

function upsertMatrixUser(
  users: Map<string, RBACMatrixUser>,
  key: string,
  row: RBACUserRole,
  groupId: string | null,
) {
  let user = users.get(key);
  if (!user) {
    user = {
      key,
      userId: row.userId,
      groupId,
      groupName: row.groupName,
      name: row.userName || row.email || "Unknown",
      email: row.email,
      roleSource: row.roleSource,
      roles: new Map(),
    };
    users.set(key, user);
  }
  return user;
}

export function principalKey(row: RBACUserRole) {
  if (row.userId && row.userId !== NIL_UUID) {
    return row.userId;
  }
  if (row.roleSource.startsWith("group:")) {
    return row.roleSource;
  }
  return `name:${row.userName || row.email || "unknown"}`;
}

export function isIndirectAccess(row: RBACUserRole | ConfigAccessSummary) {
  if ("roleSource" in row) {
    return row.roleSource !== "direct";
  }
  return !!row.external_group_id || row.external_user_id === NIL_UUID;
}

export function isNilUUID(value?: string | null) {
  return !value || value === NIL_UUID;
}

function mergeAccessRows(existing: RBACUserRole | undefined, next: RBACUserRole) {
  if (!existing) return next;
  if (isIndirectAccess(existing) && !isIndirectAccess(next)) return next;
  if (!existing.lastReviewedAt && next.lastReviewedAt) return next;
  if (!existing.lastSignedInAt && next.lastSignedInAt) return next;
  if (!existing.createdAt && next.createdAt) return next;
  return existing;
}

export function groupChangesByDay(changes: ConfigChange[]) {
  const groups = new Map<string, ConfigChange[]>();
  for (const change of changes) {
    const key = dateBucket(change.created_at);
    groups.set(key, [...(groups.get(key) ?? []), change]);
  }
  return Array.from(groups.entries()).map(([label, items]) => ({ label, items }));
}

export function groupInsightsByType(insights: ConfigAnalysis[]) {
  const groups = new Map<string, ConfigAnalysis[]>();
  for (const insight of insights) {
    const key = insight.analysis_type || "other";
    groups.set(key, [...(groups.get(key) ?? []), insight]);
  }
  return Array.from(groups.entries()).sort(([a], [b]) => a.localeCompare(b));
}

export function severityCounts(items: Array<{ severity?: ConfigSeverity | null }>) {
  const counts: Record<string, number> = {};
  for (const item of items) {
    const severity = item.severity || "info";
    counts[severity] = (counts[severity] ?? 0) + 1;
  }
  return counts;
}

export function dateBucket(value?: string | null) {
  if (!value) return "Unknown";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Unknown";

  const today = new Date();
  const startToday = new Date(today.getFullYear(), today.getMonth(), today.getDate()).getTime();
  const startDate = new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
  const diffDays = Math.round((startToday - startDate) / 86_400_000);

  if (diffDays === 0) return "Today";
  if (diffDays === 1) return "Yesterday";
  if (diffDays > 1 && diffDays < 7) return `${diffDays} days ago`;

  return date.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}

export function formatDate(value?: string | null) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function timeAgo(value?: string | null) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const diffMs = Date.now() - date.getTime();
  const abs = Math.abs(diffMs);
  const suffix = diffMs >= 0 ? "ago" : "from now";
  const units: Array<[number, string]> = [
    [365 * 86_400_000, "y"],
    [30 * 86_400_000, "mo"],
    [86_400_000, "d"],
    [3_600_000, "h"],
    [60_000, "m"],
  ];
  for (const [size, unit] of units) {
    if (abs >= size) return `${Math.floor(abs / size)}${unit} ${suffix}`;
  }
  return "just now";
}

export function severityTone(severity?: string | null) {
  switch ((severity || "").toLowerCase()) {
    case "critical":
    case "high":
      return "danger" as const;
    case "medium":
      return "warning" as const;
    case "low":
    case "info":
      return "info" as const;
    default:
      return "neutral" as const;
  }
}

export function healthStatus(health?: string | null) {
  switch ((health || "").toLowerCase()) {
    case "healthy":
      return "success" as const;
    case "unhealthy":
      return "error" as const;
    case "warning":
      return "warning" as const;
    default:
      return "info" as const;
  }
}

export function isKnownHealth(health?: string | null) {
  return !!health && health.toLowerCase() !== "unknown";
}

export function tagEntries(tags?: Record<string, string> | null) {
  return dedupeTagEntries(Object.entries(tags ?? {}).filter(([, value]) => value !== ""));
}

export function dedupeTagEntries(entries: Array<[string, string]>, existing: Array<[string, string]> = []) {
  const seen = new Set(existing.map(([key, value]) => tagKey(key, value)));
  const deduped: Array<[string, string]> = [];
  for (const [key, value] of entries) {
    const normalized = tagKey(key, value);
    if (seen.has(normalized)) continue;
    seen.add(normalized);
    deduped.push([key, value]);
  }
  return deduped;
}

function tagKey(key: string, value: string) {
  return `${key.trim().toLowerCase()}=${String(value).trim()}`;
}

export function costItems(config: ConfigItem) {
  return [
    ["Per minute", config.cost_per_minute],
    ["1 day", config.cost_total_1d],
    ["7 days", config.cost_total_7d],
    ["30 days", config.cost_total_30d],
  ].filter(([, value]) => typeof value === "number" && Number(value) > 0) as Array<[string, number]>;
}

export function stringifyValue(value: unknown) {
  if (value === null || value === undefined || value === "") return "-";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value);
}
