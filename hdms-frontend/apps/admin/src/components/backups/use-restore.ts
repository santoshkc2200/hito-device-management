import { getBackupRestore, type BackupRestoreStatus } from "@hdms/api-client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";

export const restoreQueryKey = ["backup", "restore"] as const;

// Polls the restore state every 2 seconds while one runs. A failed poll
// keeps the last answer on screen: the API drops its connections at the
// swap, and the admin may have to sign in again before polling resumes.
export function useRestore() {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: restoreQueryKey,
    queryFn: async () => {
      const res = await getBackupRestore();
      if (res.error) throw res.error;
      return res.data as BackupRestoreStatus;
    },
    refetchInterval: (q) => (q.state.data?.restore?.phase === "running" ? 2000 : false),
  });
  const running = query.data?.restore?.phase === "running";
  const wasRunning = useRef(false);
  useEffect(() => {
    // A finished restore changed the snapshots, the config and the history.
    if (wasRunning.current && !running) {
      void queryClient.invalidateQueries({ queryKey: ["backup"], predicate: (q) => q.queryKey[1] !== "restore" });
    }
    wasRunning.current = running;
  }, [running, queryClient]);
  return { status: query.data, running };
}
