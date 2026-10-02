import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type * as apiClient from "@hdms/api-client";

// Shared by the backups tests. Not a test file itself: importing a *.test.tsx
// from another test file would register its tests twice.
export const minutesAgo = (m: number) => new Date(Date.now() - m * 60_000).toISOString();

export function baseConfig(overrides: Partial<apiClient.BackupConfig> = {}): apiClient.BackupConfig {
  return {
    schedule: { enabled: true, mode: "daily", intervalMinutes: 360, timeLocal: "02:00", weekday: 0 },
    nextRunAt: new Date(Date.now() + 3_600_000).toISOString(),
    lastRun: { id: 1, job: "backup", startedAt: minutesAgo(600), finishedAt: minutesAgo(599), outcome: "success", detail: {} },
    lastSuccessAt: minutesAgo(600),
    workerSeenAt: minutesAgo(1),
    local: { path: "/var/backups/hdms/repo", snapshotCount: 3, latestSizeBytes: 5_242_880 },
    drivesDir: "/drives",
    recoveryKey: { status: "ready", createdAt: minutesAgo(10_000), confirmedAt: minutesAgo(9_990) },
    ...overrides,
  };
}

export function renderWithClient(ui: React.ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}
