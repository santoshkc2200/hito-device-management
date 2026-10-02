import { getBackupConfig, type BackupConfig } from "@hdms/api-client";
import { useQuery } from "@tanstack/react-query";

export function useBackupConfig() {
  return useQuery({
    queryKey: ["backup", "config"],
    queryFn: async () => {
      const res = await getBackupConfig();
      if (res.error) throw res.error;
      return res.data as BackupConfig;
    },
    refetchInterval: 30_000,
  });
}
