import { fetchAllPostgrest, fetchPostgrest } from "./http";
import type {
  ConfigAccessSummary,
  ExternalGroup,
  ExternalUser,
  ExternalUserGroupMembership,
} from "./types";

const accessLimit = 500;
const accessSummarySelect = "config_id,config_name,config_type,external_group_id,external_user_id,user,email,role,user_type,created_at,last_signed_in_at,last_reviewed_at";

export async function getAccessUsers(): Promise<ExternalUser[]> {
  const params = new URLSearchParams({
    select: "id,name,email,user_type,account_id,aliases,created_at,updated_at",
    deleted_at: "is.null",
    order: "name.asc",
    limit: String(accessLimit),
  });
  const result = await fetchPostgrest<ExternalUser[]>(`/db/external_users?${params.toString()}`);
  return result.data ?? [];
}

export async function getAccessGroups(): Promise<ExternalGroup[]> {
  const params = new URLSearchParams({
    select: "id,name,group_type,account_id,aliases,created_at,updated_at,members_count,permissions_count",
    deleted_at: "is.null",
    order: "name.asc",
    limit: String(accessLimit),
  });
  const result = await fetchPostgrest<ExternalGroup[]>(`/db/external_group_summary?${params.toString()}`);
  return result.data ?? [];
}

export async function getAccessUser(id: string): Promise<ExternalUser | null> {
  const params = new URLSearchParams({
    select: "id,name,email,user_type,account_id,aliases,created_at,updated_at",
    id: `eq.${id}`,
    deleted_at: "is.null",
    limit: "1",
  });
  const result = await fetchPostgrest<ExternalUser[]>(`/db/external_users?${params.toString()}`);
  return result.data[0] ?? null;
}

export async function getAccessGroup(id: string): Promise<ExternalGroup | null> {
  const params = new URLSearchParams({
    select: "id,name,group_type,account_id,aliases,created_at,updated_at",
    id: `eq.${id}`,
    deleted_at: "is.null",
    limit: "1",
  });
  const result = await fetchPostgrest<ExternalGroup[]>(`/db/external_groups?${params.toString()}`);
  return result.data[0] ?? null;
}

export async function getGroupsForUser(userID: string): Promise<ExternalUserGroupMembership[]> {
  const params = new URLSearchParams({
    select: "external_user_id,external_group_id,created_at,deleted_at,external_groups(id,name,group_type,account_id)",
    external_user_id: `eq.${userID}`,
    order: "deleted_at.asc.nullsfirst,created_at.desc",
  });
  const result = await fetchAllPostgrest<ExternalUserGroupMembership>(`/db/external_user_groups?${params.toString()}`, accessLimit);
  return result.data ?? [];
}

export async function getMembersForGroup(groupID: string): Promise<ExternalUserGroupMembership[]> {
  const params = new URLSearchParams({
    select: "external_user_id,external_group_id,created_at,deleted_at,external_users(id,name,email,user_type,account_id)",
    external_group_id: `eq.${groupID}`,
    order: "deleted_at.asc.nullsfirst,created_at.desc",
  });
  const result = await fetchAllPostgrest<ExternalUserGroupMembership>(`/db/external_user_groups?${params.toString()}`, accessLimit);
  return result.data ?? [];
}

export async function getAccessForUser(userID: string): Promise<ConfigAccessSummary[]> {
  const params = new URLSearchParams({
    select: accessSummarySelect,
    external_user_id: `eq.${userID}`,
    order: "config_name.asc",
  });
  const result = await fetchAllPostgrest<ConfigAccessSummary>(`/db/config_access_summary?${params.toString()}`, accessLimit);
  return result.data ?? [];
}

export async function getAccessForGroup(groupID: string): Promise<ConfigAccessSummary[]> {
  const params = new URLSearchParams({
    select: accessSummarySelect,
    external_group_id: `eq.${groupID}`,
    order: "config_name.asc",
  });
  const result = await fetchAllPostgrest<ConfigAccessSummary>(`/db/config_access_summary?${params.toString()}`, accessLimit);
  return result.data ?? [];
}

export async function getAccessForGroups(groupIDs: string[]): Promise<ConfigAccessSummary[]> {
  const ids = Array.from(new Set(groupIDs.filter(Boolean)));
  if (ids.length === 0) return [];

  const params = new URLSearchParams({
    select: accessSummarySelect,
    external_group_id: `in.(${ids.join(",")})`,
    order: "config_name.asc",
  });
  const result = await fetchAllPostgrest<ConfigAccessSummary>(`/db/config_access_summary?${params.toString()}`, accessLimit);
  return result.data ?? [];
}
