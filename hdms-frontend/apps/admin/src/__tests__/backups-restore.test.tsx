import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { ja } from "@/i18n/ja";
import { RestoreBanner } from "@/components/backups/restore-banner";
import { SnapshotsTab } from "@/components/backups/snapshots-tab";
import { renderWithClient } from "./backup-fixtures";

const r = ja.backups.restore;
const STEPS = ["safety_backup", "restore_scratch", "migrate_scratch", "validate", "maintenance_on", "copy_forward", "swap", "maintenance_off", "record"];

function view(over: Partial<apiClient.BackupRestore> = {}): apiClient.BackupRestore {
  return {
    kind: "restore", phase: "running", step: "validate", steps: STEPS, sourceKind: "local",
    snapshotTakenAt: "2026-09-29T02:00:00Z", startedAt: "2026-10-01T09:00:00Z",
    canUndo: false, canDiscard: false, ...over,
  };
}

function status(restore?: apiClient.BackupRestore, maintenance = false) {
  return { data: { maintenance, workerAvailable: true, restore } } as any;
}

describe("Console restore", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(apiClient, "listBackupDestinations").mockResolvedValue({ data: { items: [] } } as any);
    vi.spyOn(apiClient, "listBackupSnapshots").mockResolvedValue({
      data: { items: [{ snapshotId: "snap1", takenAt: "2026-09-29T02:00:00Z", sizeBytes: 1024 }] },
    } as any);
    vi.spyOn(apiClient, "getBackupRestore").mockResolvedValue(status());
  });

  it("restores a snapshot only after RESTORE, password and code", async () => {
    const start = vi.spyOn(apiClient, "startBackupRestore").mockResolvedValue(status(view({ step: "safety_backup" })));
    const user = userEvent.setup();
    renderWithClient(<SnapshotsTab />);

    await user.click(await screen.findByRole("button", { name: r.button }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(r.restoreTitle)).toBeInTheDocument();
    const submit = within(dialog).getByRole("button", { name: r.restoreSubmit });
    expect(submit).toBeDisabled();

    await user.type(within(dialog).getByLabelText(r.typeLabel.replace("{word}", "RESTORE")), "restore");
    await user.type(within(dialog).getByLabelText(r.password), "correct horse battery staple");
    await user.type(within(dialog).getByLabelText(r.totpCode), "123456");
    expect(submit).toBeDisabled(); // lowercase is not the word

    const word = within(dialog).getByLabelText(r.typeLabel.replace("{word}", "RESTORE"));
    await user.clear(word);
    await user.type(word, "RESTORE");
    await user.click(submit);
    await waitFor(() =>
      expect(start).toHaveBeenCalledWith({
        body: { repo: "local", snapshotId: "snap1", confirmation: "RESTORE", password: "correct horse battery staple", totpCode: "123456" },
      })
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("keeps the dialog open and explains a wrong password", async () => {
    vi.spyOn(apiClient, "startBackupRestore").mockResolvedValue({
      error: { type: "https://hdms.local/problems/reauth-failed", status: 422, title: "x" },
    } as any);
    const user = userEvent.setup();
    renderWithClient(<SnapshotsTab />);
    await user.click(await screen.findByRole("button", { name: r.button }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText(r.typeLabel.replace("{word}", "RESTORE")), "RESTORE");
    await user.type(within(dialog).getByLabelText(r.password), "wrong");
    await user.type(within(dialog).getByLabelText(r.totpCode), "123456");
    await user.click(within(dialog).getByRole("button", { name: r.restoreSubmit }));
    expect(await within(dialog).findByText(r.problems.reauthFailed)).toBeInTheDocument();
  });

  it("disables Restore while a restore runs", async () => {
    vi.spyOn(apiClient, "getBackupRestore").mockResolvedValue(status(view()));
    renderWithClient(<SnapshotsTab />);
    await waitFor(() => expect(screen.getByRole("button", { name: r.button })).toBeDisabled());
  });

  it("shows the step list while running", async () => {
    vi.spyOn(apiClient, "getBackupRestore").mockResolvedValue(status(view()));
    renderWithClient(<RestoreBanner />);
    const list = await screen.findByRole("list", { name: r.stepsLabel });
    expect(within(list).getByText(r.steps.validate).closest("li")).toHaveAttribute("data-state", "current");
    expect(within(list).getByText(r.steps.safety_backup).closest("li")).toHaveAttribute("data-state", "done");
    expect(within(list).getByText(r.steps.swap).closest("li")).toHaveAttribute("data-state", "pending");
  });

  it("keeps the last step list when a poll fails", async () => {
    const get = vi.spyOn(apiClient, "getBackupRestore").mockResolvedValue(status(view()));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={client}><RestoreBanner /></QueryClientProvider>);
    await screen.findByRole("list", { name: r.stepsLabel });
    // At the swap the API drops its connections and the session may be gone.
    get.mockResolvedValue({ error: { status: 401, title: "Unauthorized" } } as any);
    await client.refetchQueries({ queryKey: ["backup", "restore"] });
    expect(screen.getByRole("list", { name: r.stepsLabel })).toBeInTheDocument();
  });

  it("offers roll back and discard after a restore, and discards on confirm", async () => {
    vi.spyOn(apiClient, "getBackupRestore").mockResolvedValue(
      status(view({ phase: "completed", step: "record", finishedAt: "2026-10-01T09:05:00Z", canUndo: true, canDiscard: true }))
    );
    const discard = vi.spyOn(apiClient, "discardBackupRestore").mockResolvedValue(
      status(view({ phase: "completed", step: "record", canDiscard: false, discardedAt: "2026-10-01T10:00:00Z" }))
    );
    const user = userEvent.setup();
    renderWithClient(<RestoreBanner />);
    expect(await screen.findByRole("button", { name: r.rollBack })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: r.discard }));
    const alert = await screen.findByRole("alertdialog");
    await user.click(within(alert).getByRole("button", { name: r.discardConfirm }));
    await waitFor(() => expect(discard).toHaveBeenCalledTimes(1));
  });

  it("explains a failed restore", async () => {
    vi.spyOn(apiClient, "getBackupRestore").mockResolvedValue(
      status(view({ phase: "failed", error: "no_admins", finishedAt: new Date().toISOString() }))
    );
    renderWithClient(<RestoreBanner />);
    expect(await screen.findByText(r.errors.no_admins)).toBeInTheDocument();
    expect(screen.getByText(r.failed)).toBeInTheDocument();
  });

  it("ends a stuck maintenance mode", async () => {
    vi.spyOn(apiClient, "getBackupRestore").mockResolvedValue(status(undefined, true));
    const end = vi.spyOn(apiClient, "endBackupMaintenance").mockResolvedValue(status(undefined, false));
    const user = userEvent.setup();
    renderWithClient(<RestoreBanner />);
    await user.click(await screen.findByRole("button", { name: r.endMaintenance }));
    await waitFor(() => expect(end).toHaveBeenCalledTimes(1));
  });
});
