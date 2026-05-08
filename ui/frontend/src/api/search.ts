import { fetchPostgrest } from "./http";
import type { ConfigItem, ExternalGroup, ExternalUser } from "./types";

export type GlobalSearchKind = "config" | "external_user" | "external_group";

export type GlobalSearchResult = {
  id: string;
  kind: GlobalSearchKind;
  title: string;
  subtitle?: string;
  meta?: string;
  href: string;
};

const searchLimit = 8;

export async function globalSearch(query: string): Promise<GlobalSearchResult[]> {
  const q = query.trim();
  if (q.length < 2) return [];

  const [configs, users, groups] = await Promise.all([
    searchConfigs(q),
    searchExternalUsers(q),
    searchExternalGroups(q),
  ]);

  return [
    ...configs.map(configToResult),
    ...users.map(userToResult),
    ...groups.map(groupToResult),
  ];
}

async function searchConfigs(query: string): Promise<ConfigItem[]> {
  const params = new URLSearchParams({
    select: "id,name,type,config_class,status,health,path,updated_at",
    order: "updated_at.desc.nullslast,name.asc",
    limit: String(searchLimit),
  });
  const or = orFilter([
    `name.ilike.${ilike(query)}`,
    `type.ilike.${ilike(query)}`,
    `path.ilike.${ilike(query)}`,
    exactUUIDFilter(query),
  ]);
  if (or) params.set("or", or);

  const result = await fetchPostgrest<ConfigItem[]>(`/db/config_detail?${params.toString()}`);
  return result.data ?? [];
}

async function searchExternalUsers(query: string): Promise<ExternalUser[]> {
  const params = new URLSearchParams({
    select: "id,name,email,user_type,account_id,aliases,created_at,updated_at",
    deleted_at: "is.null",
    order: "name.asc",
    limit: String(searchLimit),
  });
  const or = orFilter([
    `name.ilike.${ilike(query)}`,
    `email.ilike.${ilike(query)}`,
    `user_type.ilike.${ilike(query)}`,
    exactUUIDFilter(query),
  ]);
  if (or) params.set("or", or);

  const result = await fetchPostgrest<ExternalUser[]>(`/db/external_users?${params.toString()}`);
  return result.data ?? [];
}

async function searchExternalGroups(query: string): Promise<ExternalGroup[]> {
  const params = new URLSearchParams({
    select: "id,name,group_type,account_id,aliases,created_at,updated_at",
    deleted_at: "is.null",
    order: "name.asc",
    limit: String(searchLimit),
  });
  const or = orFilter([
    `name.ilike.${ilike(query)}`,
    `group_type.ilike.${ilike(query)}`,
    exactUUIDFilter(query),
  ]);
  if (or) params.set("or", or);

  const result = await fetchPostgrest<ExternalGroup[]>(`/db/external_groups?${params.toString()}`);
  return result.data ?? [];
}

function configToResult(config: ConfigItem): GlobalSearchResult {
  return {
    id: `config:${config.id}`,
    kind: "config",
    title: config.name || config.id,
    subtitle: config.type,
    meta: config.status || config.health || config.config_class || undefined,
    href: `/ui/item/${encodeURIComponent(config.id)}`,
  };
}

function userToResult(user: ExternalUser): GlobalSearchResult {
  return {
    id: `external_user:${user.id}`,
    kind: "external_user",
    title: user.name || user.email || user.id,
    subtitle: user.email || user.user_type || undefined,
    meta: user.account_id || user.user_type || undefined,
    href: `/ui/access/users/${encodeURIComponent(user.id)}`,
  };
}

function groupToResult(group: ExternalGroup): GlobalSearchResult {
  return {
    id: `external_group:${group.id}`,
    kind: "external_group",
    title: group.name || group.id,
    subtitle: group.group_type || undefined,
    meta: group.account_id || undefined,
    href: `/ui/access/groups/${encodeURIComponent(group.id)}`,
  };
}

function ilike(query: string) {
  return `*${sanitizePattern(query)}*`;
}

function exactUUIDFilter(query: string): string | undefined {
  const id = query.trim();
  if (!/^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/.test(id)) {
    return undefined;
  }
  return `id.eq.${id}`;
}

function orFilter(parts: Array<string | undefined>) {
  const filtered = parts.filter((part): part is string => Boolean(part));
  return filtered.length > 0 ? `(${filtered.join(",")})` : undefined;
}

function sanitizePattern(query: string) {
  return query.trim().replace(/[(),*]/g, " ").replace(/\s+/g, " ");
}
