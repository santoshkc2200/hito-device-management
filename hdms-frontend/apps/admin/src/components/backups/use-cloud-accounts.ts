import { listBackupCloudAccounts } from "@hdms/api-client";
import { useQuery } from "@tanstack/react-query";

export const CLOUD_ACCOUNTS_KEY = ["backup", "cloud-accounts"] as const;

/**
 * Milliseconds between sign-in checks while the code is on screen. A test
 * seam; the server already refuses to ask the provider more often than the
 * provider allows, so a short interval is harmless.
 */
export const cloudPoll = { ms: 5000 };

export function useCloudAccounts() {
  return useQuery({
    queryKey: CLOUD_ACCOUNTS_KEY,
    queryFn: async () => {
      const res = await listBackupCloudAccounts();
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
  });
}
