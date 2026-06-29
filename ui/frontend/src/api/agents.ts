import { fetchPostgrest } from "./http";

// The "local" agent (resources without an assigned agent) is represented
// by UUID.Nil throughout the duty models. The global search uses this to
// decide whether to render an agent badge for a result.
export const LocalAgentId = "00000000-0000-0000-0000-000000000000";

export function isLocalAgent(agentId?: string | null): boolean {
  return !agentId || agentId === LocalAgentId;
}

export type AgentItem = {
  id: string;
  name: string;
  description?: string | null;
};

export async function getAgentByIDs(ids: string[]): Promise<AgentItem[]> {
  const unique = Array.from(
    new Set(
      ids
        .map((id) => id.trim())
        .filter((id) => id && id !== LocalAgentId),
    ),
  );

  if (unique.length === 0) {
    return [];
  }

  const params = new URLSearchParams({
    select: "id,name,description",
    id: `in.(${unique.map(encodeURIComponent).join(",")})`,
  });
  const result = await fetchPostgrest<AgentItem[]>(`/db/agents?${params.toString()}`);
  return result.data ?? [];
}
