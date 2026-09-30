import { listBackupRuns } from "@hdms/api-client";
import { useQuery } from "@tanstack/react-query";

export function HistoryTab() {
  useQuery({ queryKey: ["backup", "runs"], queryFn: () => listBackupRuns({ query: { limit: 20 } }) });
  return null;
}
