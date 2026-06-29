export type SearchResourceType =
  | "configs"
  | "canaries"
  | "checks"
  | "config_changes"
  | "playbooks"
  | "connections";

export type SearchedResource = {
  id: string;
  name: string;
  type: string;
  namespace: string;
  agent: string;
  labels: Record<string, string>;
  icon?: string;
  tags?: Record<string, string>;
  summary?: string;
  change_type?: string;
  config_id?: string;
};

export type SearchResourcesRequest = {
  limit?: number;
  checks?: SearchSelector[];
  canaries?: SearchSelector[];
  configs?: SearchSelector[];
  config_changes?: SearchSelector[];
  playbooks?: SearchSelector[];
  connections?: SearchSelector[];
};

// `search` is the raw user-typed string — the server runs it through the
// PEG grammar in duty/query/grammar, so syntax like
// `type=pod prometheus` or `tag.cluster=beta-cluster` is parsed
// correctly. `agent: "all"` disables the implicit local-agent filter.
export type SearchSelector = {
  search?: string;
  agent?: string;
  tagSelector?: string;
  labelSelector?: string;
  fieldSelector?: string;
  id?: string;
  name?: string;
  namespace?: string;
  types?: string[];
  statuses?: string[];
  health?: string;
};

export type SelectedResources = {
  configs: SearchedResource[];
  checks: SearchedResource[];
  canaries: SearchedResource[];
  config_changes: SearchedResource[];
  playbooks: SearchedResource[];
  connections: SearchedResource[];
};

const DEFAULT_LIMIT = 25;

export async function searchResources(
  input: SearchResourcesRequest,
): Promise<SelectedResources | null> {
  const response = await fetch("/resources/search", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: JSON.stringify(input),
  });

  if (!response.ok) {
    throw new Error(
      `POST /resources/search failed with ${response.status}: ${await response.text()}`,
    );
  }

  return (await response.json()) as SelectedResources | null;
}

export function emptySelectedResources(): SelectedResources {
  return {
    configs: [],
    checks: [],
    canaries: [],
    config_changes: [],
    playbooks: [],
    connections: [],
  };
}

export const SEARCH_DEFAULT_LIMIT = DEFAULT_LIMIT;
