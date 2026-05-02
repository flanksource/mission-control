import { fetchJSON, fetchPostgrest, type PaginatedResult } from "./http";
import type {
  PlaybookListItem,
  PlaybookParameter,
  PlaybookRun,
  PlaybookRunAction,
  PlaybookRunStatus,
  PlaybookRunSubmitRequest,
  PlaybookRunSubmitResponse,
  PlaybookRunTarget,
  PlaybookRunWithActions,
} from "./types";

export const PLAYBOOK_RUN_FINAL_STATUSES = new Set<PlaybookRunStatus>([
  "cancelled",
  "completed",
  "failed",
  "timed_out",
]);

export type PlaybookRunsOptions = {
  configId?: string;
  componentId?: string;
  checkId?: string;
  playbookId?: string;
  status?: string;
  limit?: number;
  offset?: number;
};

export function isFinalPlaybookRunStatus(status?: string | null) {
  return !!status && PLAYBOOK_RUN_FINAL_STATUSES.has(status);
}

export async function getPlaybooks(): Promise<PlaybookListItem[]> {
  return fetchJSON<PlaybookListItem[]>("/playbook/list");
}

export async function getRunnablePlaybooksForConfig(configId: string): Promise<PlaybookListItem[]> {
  return fetchJSON<PlaybookListItem[]>(`/playbook/list?config_id=${encodeURIComponent(configId)}`);
}

export async function getPlaybookParams(
  playbookId: string,
  target: PlaybookRunTarget = {},
): Promise<PlaybookParameter[]> {
  const result = await fetchJSON<{ params?: PlaybookParameter[] }>(
    `/playbook/${encodeURIComponent(playbookId)}/params`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id: playbookId, ...target }),
    },
  );
  return result.params ?? [];
}

export async function submitPlaybookRun(
  request: PlaybookRunSubmitRequest,
): Promise<PlaybookRunSubmitResponse> {
  return fetchJSON<PlaybookRunSubmitResponse>("/playbook/run", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });
}

export async function approvePlaybookRun(runId: string): Promise<void> {
  await fetchJSON<unknown>(`/playbook/run/approve/${encodeURIComponent(runId)}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
  });
}

export async function cancelPlaybookRun(runId: string): Promise<void> {
  await fetchJSON<unknown>(`/playbook/run/cancel/${encodeURIComponent(runId)}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
  });
}

export type PlaybookUpdateRequest = {
  id: string;
  namespace: string;
  name: string;
  title?: string;
  icon?: string;
  description?: string;
  source?: string;
  category?: string;
  spec?: PlaybookListItem["spec"];
};

export async function updatePlaybook(request: PlaybookUpdateRequest): Promise<PlaybookListItem[]> {
  const { id, ...body } = request;
  return fetchJSON<PlaybookListItem[]>(`/db/playbooks?id=eq.${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export async function deletePlaybook(playbookId: string): Promise<void> {
  await fetchJSON<unknown[]>(`/db/playbooks?id=eq.${encodeURIComponent(playbookId)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ deleted_at: new Date().toISOString() }),
  });
}

export async function getPlaybookRuns(options: PlaybookRunsOptions = {}): Promise<PaginatedResult<PlaybookRun[]>> {
  return fetchPostgrest<PlaybookRun[]>(buildPlaybookRunsPath(options));
}

export async function getPlaybookRunWithActions(runId: string): Promise<PlaybookRunWithActions> {
  const [runsResult, actionsResult] = await Promise.all([
    fetchPostgrest<PlaybookRun[]>(buildPlaybookRunsByIdPath(runId)),
    fetchPostgrest<PlaybookRunAction[]>(`/db/rpc/get_playbook_run_actions?run_id=${encodeURIComponent(runId)}`),
  ]);

  const runs = runsResult.data ?? [];
  const run = runs.find((candidate) => candidate.id === runId) ?? null;
  const actions = actionsResult.data ?? [];
  const detailResult = await fetchActionDetails(runs.map((run) => run.id));
  const mergedActions = mergeActionDetails(actions, detailResult.details);
  const response: PlaybookRunWithActions = {
    run,
    childRuns: runs.filter((run) => run.parent_id === runId),
    actions: run ? mergePlaybookRunSpecActions(runs, mergedActions) : mergedActions,
  };
  if (detailResult.error) {
    response.actionDetailsError = detailResult.error;
  }

  return response;
}

type ActionDetailResult = Pick<PlaybookRunAction, "id" | "error" | "result" | "artifacts">;
type PlaybookActionSpec = Record<string, unknown> | string | number | boolean | null;

async function fetchActionDetails(runIds: string[]): Promise<{ details: ActionDetailResult[]; error?: string }> {
  if (runIds.length === 0) return { details: [] };
  const ids = runIds.map((id) => encodeURIComponent(id)).join(",");
  try {
    const result = await fetchPostgrest<ActionDetailResult[]>(
      `/db/playbook_run_actions?select=id,error,result,artifacts:artifacts(*)&playbook_run_id=in.(${ids})`,
    );
    return { details: result.data ?? [] };
  } catch (err) {
    return {
      details: [],
      error: err instanceof Error ? err.message : String(err),
    };
  }
}

function mergeActionDetails(actions: PlaybookRunAction[], details: ActionDetailResult[]): PlaybookRunAction[] {
  if (details.length === 0) return actions;
  const byID = new Map(details.map((detail) => [detail.id, detail]));
  return actions.map((action) => {
    const detail = byID.get(action.id);
    if (!detail) return action;
    return {
      ...action,
      error: detail.error ?? action.error,
      result: detail.result ?? action.result,
      artifacts: detail.artifacts ?? action.artifacts,
    };
  });
}

export function mergePlaybookRunSpecActions(
  runs: PlaybookRun[],
  actions: PlaybookRunAction[],
): PlaybookRunAction[] {
  if (runs.length === 0 || (actions.length === 0 && runs.every((run) => playbookActionSpecs(run.spec).length === 0))) {
    return actions;
  }

  const actionsByRunID = new Map<string, PlaybookRunAction[]>();
  for (const action of actions) {
    actionsByRunID.set(action.playbook_run_id, [...(actionsByRunID.get(action.playbook_run_id) ?? []), action]);
  }

  const merged: PlaybookRunAction[] = [];
  const seenActionIDs = new Set<string>();
  for (const run of runs) {
    const runActions = actionsByRunID.get(run.id) ?? [];
    const specs = playbookActionSpecs(run.spec);
    if (specs.length === 0) {
      for (const action of runActions) {
        merged.push(action);
        seenActionIDs.add(action.id);
      }
      continue;
    }

    const usedRunActionIDs = new Set<string>();
    for (let index = 0; index < specs.length; index += 1) {
      const spec = specs[index];
      const specName = playbookActionSpecName(spec, index);
      const matchingAction = findMatchingAction(runActions, usedRunActionIDs, specName, index, hasPlaybookActionSpecName(spec));
      if (matchingAction) {
        merged.push(matchingAction);
        usedRunActionIDs.add(matchingAction.id);
        seenActionIDs.add(matchingAction.id);
        continue;
      }
      merged.push(syntheticPlaybookRunAction(run, specName, index));
    }

    for (const action of runActions) {
      if (usedRunActionIDs.has(action.id)) continue;
      merged.push(action);
      seenActionIDs.add(action.id);
    }
  }

  for (const action of actions) {
    if (!seenActionIDs.has(action.id)) {
      merged.push(action);
    }
  }
  return merged;
}

function playbookActionSpecs(spec?: Record<string, unknown> | null): PlaybookActionSpec[] {
  const actions = spec?.actions;
  return Array.isArray(actions) ? actions as PlaybookActionSpec[] : [];
}

function playbookActionSpecName(spec: PlaybookActionSpec, index: number) {
  if (typeof spec === "string" && spec.trim()) return spec.trim();
  if (spec && typeof spec === "object" && !Array.isArray(spec)) {
    const record = spec as Record<string, unknown>;
    const named = firstNonEmptyString(record.name, record.title);
    if (named) return named;
    const actionType = Object.keys(record).find((key) => !["if", "delay", "timeout", "retry", "templates", "parameters", "params"].includes(key));
    if (actionType) return titleCase(actionType);
  }
  return `Step ${index + 1}`;
}

function hasPlaybookActionSpecName(spec: PlaybookActionSpec) {
  return (typeof spec === "string" && spec.trim() !== "")
    || Boolean(spec && typeof spec === "object" && !Array.isArray(spec) && firstNonEmptyString((spec as Record<string, unknown>).name, (spec as Record<string, unknown>).title));
}

function findMatchingAction(
  actions: PlaybookRunAction[],
  usedActionIDs: Set<string>,
  specName: string,
  specIndex: number,
  namedSpec: boolean,
) {
  const exact = actions.find((action) => !usedActionIDs.has(action.id) && action.name === specName);
  if (exact) return exact;
  if (namedSpec) return undefined;
  return actions.find((action, index) => index === specIndex && !usedActionIDs.has(action.id));
}

function syntheticPlaybookRunAction(run: PlaybookRun, name: string, index: number): PlaybookRunAction {
  return {
    id: `spec:${run.id}:${index}`,
    name,
    playbook_run_id: run.id,
    synthetic: true,
    spec_index: index,
  };
}

function firstNonEmptyString(...values: unknown[]) {
  for (const value of values) {
    if (typeof value === "string" && value.trim()) return value.trim();
  }
  return null;
}

function titleCase(value: string) {
  return value.replace(/[_-]+/g, " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}

export function buildPlaybookRunsPath(options: PlaybookRunsOptions = {}) {
  const params = playbookRunBaseParams();
  params.push("parent_id=is.null");
  if (options.configId) params.push(`config_id=eq.${encodeURIComponent(options.configId)}`);
  if (options.componentId) params.push(`component_id=eq.${encodeURIComponent(options.componentId)}`);
  if (options.checkId) params.push(`check_id=eq.${encodeURIComponent(options.checkId)}`);
  if (options.playbookId) params.push(`playbook_id=eq.${encodeURIComponent(options.playbookId)}`);
  if (options.status) params.push(`status=eq.${encodeURIComponent(options.status)}`);
  params.push("order=created_at.desc");
  params.push(`limit=${options.limit ?? 50}`);
  if (options.offset) params.push(`offset=${options.offset}`);
  return `/db/playbook_runs?${params.join("&")}`;
}

export function buildPlaybookRunsByIdPath(runId: string) {
  const params = playbookRunBaseParams();
  params.push(`or=(id.eq.${encodeURIComponent(runId)},parent_id.eq.${encodeURIComponent(runId)})`);
  params.push("order=created_at.asc");
  return `/db/playbook_runs?${params.join("&")}`;
}

function playbookRunBaseParams() {
  return [
    "select=*,playbooks(id,name,title,icon,category),component:components(id,name,icon),check:checks(id,name,icon),config:config_items(id,name,type,config_class)",
  ];
}
