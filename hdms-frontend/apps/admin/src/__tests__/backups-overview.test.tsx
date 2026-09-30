import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { ja } from "@/i18n/ja";
import { OverviewTab } from "@/components/backups/overview-tab";
import { baseConfig, minutesAgo, renderWithClient } from "./backup-fixtures";

describe("Backups overview", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("shows last and next backup and the local copy", async () => {
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig() } as any);
    renderWithClient(<OverviewTab />);
    expect(await screen.findByText(ja.backups.outcome.success)).toBeInTheDocument();
    expect(screen.getByText("/var/backups/hdms/repo")).toBeInTheDocument();
    expect(screen.getByText("5.0 MiB")).toBeInTheDocument();
    expect(screen.queryByText(ja.backups.overview.workerDown)).not.toBeInTheDocument();
  });

  it("shows worker-down warning when heartbeat is stale", async () => {
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig({ workerSeenAt: minutesAgo(10) }) } as any);
    renderWithClient(<OverviewTab />);
    expect(await screen.findByText(ja.backups.overview.workerDown)).toBeInTheDocument();
  });

  it("shows worker-down warning when the worker never reported", async () => {
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig({ workerSeenAt: undefined }) } as any);
    renderWithClient(<OverviewTab />);
    expect(await screen.findByText(ja.backups.overview.workerDown)).toBeInTheDocument();
  });

  it("shows Off when schedule disabled", async () => {
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({
      data: baseConfig({ schedule: { enabled: false, mode: "daily", intervalMinutes: 360, timeLocal: "02:00", weekday: 0 }, nextRunAt: undefined }),
    } as any);
    renderWithClient(<OverviewTab />);
    expect(await screen.findByText(ja.backups.overview.scheduleOff)).toBeInTheDocument();
  });

  it("marks a degraded last run as partial, not success", async () => {
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({
      data: baseConfig({ lastRun: { id: 2, job: "backup", startedAt: minutesAgo(30), outcome: "degraded", detail: {} } }),
    } as any);
    renderWithClient(<OverviewTab />);
    expect(await screen.findByText(ja.backups.outcome.degraded)).toBeInTheDocument();
  });

  it("queues a backup and reports when it finishes", async () => {
    const user = userEvent.setup();
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig() } as any);
    vi.spyOn(apiClient, "runBackupNow").mockResolvedValue({
      data: { id: "r1", kind: "run", status: "pending", requestedAt: minutesAgo(0) },
    } as any);
    vi.spyOn(apiClient, "getBackupRequest").mockResolvedValue({
      data: { id: "r1", kind: "run", status: "done", outcome: "success", requestedAt: minutesAgo(0) },
    } as any);
    renderWithClient(<OverviewTab />);
    await user.click(await screen.findByRole("button", { name: ja.backups.overview.backUpNow }));
    await waitFor(() => expect(apiClient.runBackupNow).toHaveBeenCalledTimes(1));
    expect(await screen.findByText(ja.backups.overview.finished.replace("{outcome}", ja.backups.outcome.success))).toBeInTheDocument();
  });

  it("saves the schedule with the edited fields", async () => {
    const user = userEvent.setup();
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig() } as any);
    const save = vi.spyOn(apiClient, "updateBackupSchedule").mockResolvedValue({ data: baseConfig() } as any);
    renderWithClient(<OverviewTab />);
    const time = await screen.findByLabelText(ja.backups.overview.timeLocal);
    await user.clear(time);
    await user.type(time, "03:30");
    await user.click(screen.getByRole("button", { name: ja.backups.overview.save }));
    await waitFor(() =>
      expect(save).toHaveBeenCalledWith({
        body: { enabled: true, mode: "daily", intervalMinutes: 360, timeLocal: "03:30", weekday: 0 },
      })
    );
  });

  it("refuses an invalid time without calling the API", async () => {
    const user = userEvent.setup();
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig() } as any);
    const save = vi.spyOn(apiClient, "updateBackupSchedule");
    renderWithClient(<OverviewTab />);
    const time = await screen.findByLabelText(ja.backups.overview.timeLocal);
    await user.clear(time);
    await user.type(time, "25:00");
    await user.click(screen.getByRole("button", { name: ja.backups.overview.save }));
    expect(await screen.findByText(ja.backups.validation.timeFormat)).toBeInTheDocument();
    expect(save).not.toHaveBeenCalled();
  });
});
