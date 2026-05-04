// Shared cache of "ONLINE databases on this instance" so multiple tabs
// (Console, Trace, Processes) share one fetch and one cache entry. The
// underlying `databases-list` op short-circuits to a single entry when
// the catalog item is bound to an MSSQL::Database, so the picker
// naturally locks to that DB without per-tab logic.

import { useQuery } from "@tanstack/react-query";
import { callOp } from "./api";

export function useDatabases(configID: string) {
  return useQuery({
    queryKey: ["databases", configID],
    queryFn: () => callOp<string[]>("databases-list", configID, {}),
    staleTime: 60_000,
    enabled: !!configID,
  });
}
