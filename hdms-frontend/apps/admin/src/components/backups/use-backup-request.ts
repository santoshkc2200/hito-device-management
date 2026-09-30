import { getBackupRequest, type BackupRequest } from "@hdms/api-client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

// Polls a queued backup request until the worker finishes it, then refreshes
// every backup query so the page shows the new state.
export function useBackupRequest(id: string | null) {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["backup", "request", id],
    enabled: Boolean(id),
    queryFn: async () => {
      const res = await getBackupRequest({ path: { id: id as string } });
      if (res.error) throw res.error;
      return res.data as BackupRequest;
    },
    refetchInterval: (q) => (q.state.data?.status === "done" ? false : 3000),
  });
  const done = query.data?.status === "done";
  useEffect(() => {
    if (done) void queryClient.invalidateQueries({ queryKey: ["backup"], predicate: (q) => q.queryKey[1] !== "request" });
  }, [done, queryClient]);
  return { request: query.data, isRunning: Boolean(id) && !done };
}
