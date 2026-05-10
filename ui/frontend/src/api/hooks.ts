import { useQuery } from "@tanstack/react-query";
import {
  getAccessForGroups,
  getAccessForGroup,
  getAccessForUser,
  getAccessGroup,
  getAccessGroups,
  getAccessUser,
  getAccessUsers,
  getGroupsForUser,
  getMembersForGroup,
} from "./access";
import {
  getConfig,
  getConfigAccessLogs,
  getConfigAccessSummary,
  getConfigChanges,
  getConfigInsights,
  getConfigParentsByLocation,
  getConfigRelationshipTrees,
} from "./configs";
import {
  getPlaybookRunWithActions,
  getPlaybookRuns,
  getPlaybooks,
  getRunnablePlaybooksForConfig,
  isFinalPlaybookRunStatus,
  type PlaybookRunsOptions,
} from "./playbooks";
import type { ConfigRelationshipsResponse } from "./types";

export function useConfigDetail(id: string) {
  return useQuery({
    queryKey: ["config", id],
    queryFn: () => getConfig(id),
    enabled: !!id,
  });
}

export function useConfigParents(id: string) {
  return useQuery({
    queryKey: ["config", id, "parents"],
    queryFn: () => getConfigParentsByLocation(id),
    enabled: !!id,
  });
}

export function useConfigRelationshipTrees(id: string) {
  return useQuery<ConfigRelationshipsResponse>({
    queryKey: ["config", id, "relationships", "trees"],
    queryFn: () => getConfigRelationshipTrees(id),
    enabled: !!id,
  });
}

export function useConfigChanges(id: string) {
  return useQuery({
    queryKey: ["config", id, "changes"],
    queryFn: () => getConfigChanges(id),
    enabled: !!id,
  });
}

export function useConfigInsights(id: string) {
  return useQuery({
    queryKey: ["config", id, "insights"],
    queryFn: () => getConfigInsights(id),
    enabled: !!id,
  });
}

export function useConfigAccess(id: string) {
  return useQuery({
    queryKey: ["config", id, "access"],
    queryFn: async () => {
      const [summary, logs] = await Promise.all([
        getConfigAccessSummary(id),
        getConfigAccessLogs(id),
      ]);
      return { summary, logs };
    },
    enabled: !!id,
  });
}

export function useAccessUsers() {
  return useQuery({
    queryKey: ["access", "users"],
    queryFn: getAccessUsers,
  });
}

export function useAccessGroups() {
  return useQuery({
    queryKey: ["access", "groups"],
    queryFn: getAccessGroups,
  });
}

export function useAccessUser(id: string) {
  return useQuery({
    queryKey: ["access", "users", id],
    queryFn: () => getAccessUser(id),
    enabled: !!id,
  });
}

export function useAccessGroup(id: string) {
  return useQuery({
    queryKey: ["access", "groups", id],
    queryFn: () => getAccessGroup(id),
    enabled: !!id,
  });
}

export function useAccessUserDetail(id: string) {
  return useQuery({
    queryKey: ["access", "users", id, "detail"],
    queryFn: async () => {
      const [user, groups, directAccess] = await Promise.all([
        getAccessUser(id),
        getGroupsForUser(id),
        getAccessForUser(id),
      ]);
      const activeGroups = groups.filter((group) => !group.deleted_at);
      const groupNames = new Map(
        activeGroups.map((group) => [
          group.external_group_id,
          group.external_groups?.name ?? group.external_group_id,
        ]),
      );
      const indirectAccess = (await getAccessForGroups(activeGroups.map((group) => group.external_group_id)))
        .map((row) => ({
          ...row,
          group_name: row.group_name ?? groupNames.get(row.external_group_id ?? "") ?? null,
        }));
      return {
        user,
        groups,
        access: [...directAccess, ...indirectAccess],
        directAccess,
        indirectAccess,
      };
    },
    enabled: !!id,
  });
}

export function useAccessGroupDetail(id: string) {
  return useQuery({
    queryKey: ["access", "groups", id, "detail"],
    queryFn: async () => {
      const [group, members, access] = await Promise.all([
        getAccessGroup(id),
        getMembersForGroup(id),
        getAccessForGroup(id),
      ]);
      return { group, members, access };
    },
    enabled: !!id,
  });
}

export function usePlaybooks() {
  return useQuery({
    queryKey: ["playbooks"],
    queryFn: getPlaybooks,
  });
}

export function useRunnablePlaybooksForConfig(configId: string) {
  return useQuery({
    queryKey: ["playbooks", "runnable", "config", configId],
    queryFn: () => getRunnablePlaybooksForConfig(configId),
    enabled: !!configId,
  });
}

export function usePlaybookRuns(options: PlaybookRunsOptions = {}) {
  return useQuery({
    queryKey: ["playbook_runs", options],
    queryFn: () => getPlaybookRuns(options),
  });
}

export function usePlaybookRunDetail(runId: string) {
  return useQuery({
    queryKey: ["playbook_run", runId],
    queryFn: () => getPlaybookRunWithActions(runId),
    enabled: !!runId,
    refetchInterval: (query) => {
      const status = query.state.data?.run?.status;
      return status && isFinalPlaybookRunStatus(status) ? false : 2000;
    },
  });
}
