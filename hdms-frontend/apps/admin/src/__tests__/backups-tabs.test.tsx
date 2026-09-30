import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { ja } from "@/i18n/ja";
import { DestinationsTab } from "@/components/backups/destinations-tab";
import { HistoryTab } from "@/components/backups/history-tab";
import { SnapshotsTab } from "@/components/backups/snapshots-tab";
import { baseConfig, minutesAgo, renderWithClient } from "./backup-fixtures";

const nas: apiClient.BackupDestination = {
  id: "d1", name: "Ward NAS", target: "/mnt/nas/hdms", enabled: true, retentionVersions: 2,
  lastError: "backup: stat destination path \"/mnt/nas/hdms\": no such file or directory",
};

describe("Destinations tab", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig() } as any);
    vi.spyOn(apiClient, "listBackupDestinations").mockResolvedValue({ data: { items: [nas] } } as any);
  });

  it("lists destinations and shows the last error", async () => {
    renderWithClient(<DestinationsTab />);
    expect(await screen.findByText("Ward NAS")).toBeInTheDocument();
    expect(screen.getByText(/no such file or directory/)).toBeInTheDocument();
  });

  it("adds a destination, warning when fewer than 3 versions", async () => {
    const user = userEvent.setup();
    const create = vi.spyOn(apiClient, "createBackupDestination").mockResolvedValue({ data: nas } as any);
    renderWithClient(<DestinationsTab />);
    await user.click(await screen.findByRole("button", { name: ja.backups.destinations.add }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText(ja.backups.destinations.form.name), "Ward NAS");
    await user.type(within(dialog).getByLabelText(ja.backups.destinations.form.target), "/mnt/nas/hdms");
    const retention = within(dialog).getByLabelText(ja.backups.destinations.form.retention);
    await user.clear(retention);
    await user.type(retention, "2");
    expect(within(dialog).getByText(ja.backups.destinations.form.retentionWarning)).toBeInTheDocument();
    expect(within(dialog).getByText(/\/var\/backups, \/mnt\/nas/)).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: ja.backups.destinations.form.save }));
    await waitFor(() =>
      expect(create).toHaveBeenCalledWith({ body: { name: "Ward NAS", target: "/mnt/nas/hdms", retentionVersions: 2, enabled: true } })
    );
  });

  it("rejects a relative path without calling the API", async () => {
    const user = userEvent.setup();
    const create = vi.spyOn(apiClient, "createBackupDestination");
    renderWithClient(<DestinationsTab />);
    await user.click(await screen.findByRole("button", { name: ja.backups.destinations.add }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText(ja.backups.destinations.form.name), "X");
    await user.type(within(dialog).getByLabelText(ja.backups.destinations.form.target), "mnt/nas");
    await user.click(within(dialog).getByRole("button", { name: ja.backups.destinations.form.save }));
    expect(await within(dialog).findByText(ja.backups.validation.targetAbsolute)).toBeInTheDocument();
    expect(create).not.toHaveBeenCalled();
  });

  it("queues a test for a row", async () => {
    const user = userEvent.setup();
    const test = vi.spyOn(apiClient, "testBackupDestination").mockResolvedValue({
      data: { id: "r9", kind: "test", status: "pending", requestedAt: minutesAgo(0), destinationId: "d1" },
    } as any);
    vi.spyOn(apiClient, "getBackupRequest").mockResolvedValue({
      data: { id: "r9", kind: "test", status: "pending", requestedAt: minutesAgo(0) },
    } as any);
    renderWithClient(<DestinationsTab />);
    await user.click(await screen.findByRole("button", { name: ja.backups.destinations.test }));
    await waitFor(() => expect(test).toHaveBeenCalledWith({ path: { id: "d1" } }));
  });

  it("deletes after confirmation", async () => {
    const user = userEvent.setup();
    const del = vi.spyOn(apiClient, "deleteBackupDestination").mockResolvedValue({ data: undefined } as any);
    renderWithClient(<DestinationsTab />);
    await user.click(await screen.findByRole("button", { name: ja.backups.destinations.delete }));
    const confirm = await screen.findByRole("alertdialog");
    await user.click(within(confirm).getByRole("button", { name: ja.backups.destinations.delete }));
    await waitFor(() => expect(del).toHaveBeenCalledWith({ path: { id: "d1" } }));
  });
});

describe("Backups (snapshots) tab", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(apiClient, "listBackupDestinations").mockResolvedValue({ data: { items: [nas] } } as any);
    vi.spyOn(apiClient, "listBackupSnapshots").mockResolvedValue({
      data: { items: [{ snapshotId: "abcd1234", takenAt: minutesAgo(60), sizeBytes: 2048, verifiedAt: minutesAgo(30) }] },
    } as any);
  });

  it("lists local snapshots and verifies", async () => {
    const user = userEvent.setup();
    const verify = vi.spyOn(apiClient, "verifyBackups").mockResolvedValue({
      data: { id: "v1", kind: "verify", status: "pending", requestedAt: minutesAgo(0) },
    } as any);
    vi.spyOn(apiClient, "getBackupRequest").mockResolvedValue({
      data: { id: "v1", kind: "verify", status: "pending", requestedAt: minutesAgo(0) },
    } as any);
    renderWithClient(<SnapshotsTab />);
    expect(await screen.findByText("2.0 KiB")).toBeInTheDocument();
    expect(apiClient.listBackupSnapshots).toHaveBeenCalledWith({ query: { repo: "local" } });
    await user.click(screen.getByRole("button", { name: ja.backups.snapshots.verifyNow }));
    await waitFor(() => expect(verify).toHaveBeenCalledWith({ body: {} }));
  });
});

describe("History tab", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("shows outcomes and expands per-destination detail", async () => {
    const user = userEvent.setup();
    vi.spyOn(apiClient, "listBackupRuns").mockResolvedValue({
      data: {
        items: [
          {
            id: 7, job: "backup", startedAt: minutesAgo(60), outcome: "degraded",
            detail: { destinations: [{ name: "Ward NAS", outcome: "failure", error: "no route to host" }] },
          },
        ],
      },
    } as any);
    renderWithClient(<HistoryTab />);
    expect(await screen.findByText(ja.backups.outcome.degraded)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: ja.backups.history.showDetails }));
    expect(await screen.findByText(/no route to host/)).toBeInTheDocument();
  });
});
