import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { ja } from "@/i18n/ja";
import { DestinationsTab } from "@/components/backups/destinations-tab";
import { baseConfig, minutesAgo, renderWithClient } from "./backup-fixtures";

const w = ja.backups.wizard;
const root: apiClient.BackupLocationRoot = { path: "/drives/BackupSSD", name: "BackupSSD", hostPath: "/Volumes/BackupSSD", connected: true };
const CHECK_NAMES = ["allowed", "connected", "exists", "writable", "separate_disk", "contents", "space"] as const;

function passAll(path: string): apiClient.BackupLocationCheck {
  return {
    path, ok: true, existingRepo: false, freeBytes: 10_000_000_000, neededBytes: 15_000_000,
    checks: CHECK_NAMES.map((name) => ({ name, status: "pass" as const })),
  };
}

// Serves the drives-only listing, then the given folders under /drives/BackupSSD.
function mockLocations(foldersAt: Record<string, apiClient.BackupFolder[]> = {}) {
  return vi.spyOn(apiClient, "listBackupLocations").mockImplementation((async (opts?: { query?: { path?: string } }) => {
    const path = opts?.query?.path;
    if (!path) return { data: { roots: [root], folders: [] } };
    return { data: { roots: [root], path, parent: path === root.path ? undefined : root.path, folders: foldersAt[path] ?? [] } };
  }) as any);
}

const byText = (text: string) => (_: string, el: Element | null) => el?.textContent?.includes(text) ?? false;

async function openWizard() {
  const user = userEvent.setup();
  renderWithClient(<DestinationsTab />);
  await user.click(await screen.findByRole("button", { name: ja.backups.destinations.add }));
  return { user, dialog: await screen.findByRole("dialog") };
}

// "Use this folder" stays disabled until the folder listing loads; clicking a
// disabled button is silently ignored, so wait for it first.
async function pickCurrentFolder(user: ReturnType<typeof userEvent.setup>, dialog: HTMLElement) {
  const button = await within(dialog).findByRole("button", { name: w.folder.useThis });
  await waitFor(() => expect(button).toBeEnabled());
  await user.click(button);
}

async function goToCheck(user: ReturnType<typeof userEvent.setup>, dialog: HTMLElement) {
  await user.click(await within(dialog).findByRole("button", { name: byText(root.name) }));
  await pickCurrentFolder(user, dialog);
}

describe("Add destination wizard", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig({ drivesHostPath: "/Volumes" }) } as any);
    vi.spyOn(apiClient, "listBackupDestinations").mockResolvedValue({ data: { items: [] } } as any);
  });

  it("blocks a drive that is not connected and says what to ask IT", async () => {
    vi.spyOn(apiClient, "listBackupLocations").mockResolvedValue({
      data: { roots: [{ path: "/drives/usb", name: "usb", connected: false }], folders: [] },
    } as any);
    const { dialog } = await openWizard();
    expect(await within(dialog).findByText(w.where.notConnectedHelp)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: byText("usb") })).toBeDisabled();
  });

  it("says the worker is down instead of showing an empty dialog", async () => {
    vi.spyOn(apiClient, "listBackupLocations").mockResolvedValue({
      error: { type: "https://hdms.local/problems/worker-unavailable", status: 503, title: "Backup worker not responding" },
    } as any);
    const { dialog } = await openWizard();
    expect(await within(dialog).findByText(w.workerDown)).toBeInTheDocument();
  });

  it("walks from drive to a saved, prepared destination", async () => {
    const created: apiClient.BackupFolder = { name: "hdms-backups", path: "/drives/BackupSSD/hdms-backups", hasBackup: false };
    mockLocations();
    const mkdir = vi.spyOn(apiClient, "createBackupLocationFolder").mockResolvedValue({ data: created } as any);
    const check = vi.spyOn(apiClient, "checkBackupLocation").mockResolvedValue({ data: passAll(created.path) } as any);
    const create = vi.spyOn(apiClient, "createBackupDestination").mockResolvedValue({
      data: { id: "d9", name: w.details.defaultName, target: created.path, enabled: true, retentionVersions: 3 },
    } as any);
    const test = vi.spyOn(apiClient, "testBackupDestination").mockResolvedValue({
      data: { id: "r1", kind: "test", status: "pending", requestedAt: minutesAgo(0), destinationId: "d9" },
    } as any);
    vi.spyOn(apiClient, "getBackupRequest").mockResolvedValue({
      data: { id: "r1", kind: "test", status: "done", outcome: "success", requestedAt: minutesAgo(0), destinationId: "d9" },
    } as any);

    const { user, dialog } = await openWizard();
    await user.click(await within(dialog).findByRole("button", { name: byText(root.name) }));

    expect(await within(dialog).findByText(w.folder.empty)).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: w.folder.create }));
    await waitFor(() => expect(mkdir).toHaveBeenCalledWith({ body: { parent: "/drives/BackupSSD", name: "hdms-backups" } }));
    expect(await within(dialog).findByText("/Volumes/BackupSSD/hdms-backups")).toBeInTheDocument();

    await pickCurrentFolder(user, dialog);
    expect(await within(dialog).findByText(w.check.ok)).toBeInTheDocument();
    expect(check).toHaveBeenCalledWith({ body: { path: "/drives/BackupSSD/hdms-backups" } });
    for (const name of CHECK_NAMES) expect(within(dialog).getByText(w.check.pass[name])).toBeInTheDocument();

    await user.click(within(dialog).getByRole("button", { name: w.next }));
    const retention = within(dialog).getByLabelText(w.details.retention);
    await user.clear(retention);
    await user.type(retention, "2");
    expect(within(dialog).getByText(ja.backups.destinations.form.retentionWarning)).toBeInTheDocument();
    await user.clear(retention);
    await user.type(retention, "3");
    await user.click(within(dialog).getByRole("button", { name: w.details.save }));

    await waitFor(() => expect(create).toHaveBeenCalledWith({
      body: { name: w.details.defaultName, target: "/drives/BackupSSD/hdms-backups", retentionVersions: 3, enabled: true },
    }));
    await waitFor(() => expect(test).toHaveBeenCalledWith({ path: { id: "d9" } }));
    expect(await within(dialog).findByText(w.saving.ready)).toBeInTheDocument();
  });

  it("explains a failed check and keeps Next disabled", async () => {
    mockLocations();
    vi.spyOn(apiClient, "checkBackupLocation").mockResolvedValue({
      data: {
        ...passAll("/drives/BackupSSD"), ok: false,
        checks: CHECK_NAMES.map((name) => (name === "separate_disk"
          ? { name, status: "fail" as const, code: "same_disk" as const }
          : { name, status: "pass" as const })),
      },
    } as any);
    const { user, dialog } = await openWizard();
    await goToCheck(user, dialog);

    expect(await within(dialog).findByText(w.check.codes.same_disk)).toBeInTheDocument();
    expect(within(dialog).getByText(w.check.help.same_disk)).toBeInTheDocument();
    expect(within(dialog).getByText(w.check.failed)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: w.next })).toBeDisabled();
  });

  it("turns the destination off when preparing the drive fails", async () => {
    mockLocations();
    vi.spyOn(apiClient, "checkBackupLocation").mockResolvedValue({ data: passAll("/drives/BackupSSD") } as any);
    vi.spyOn(apiClient, "createBackupDestination").mockResolvedValue({
      data: { id: "d9", name: w.details.defaultName, target: "/drives/BackupSSD", enabled: true, retentionVersions: 3 },
    } as any);
    vi.spyOn(apiClient, "testBackupDestination").mockResolvedValue({
      data: { id: "r1", kind: "test", status: "pending", requestedAt: minutesAgo(0), destinationId: "d9" },
    } as any);
    vi.spyOn(apiClient, "getBackupRequest").mockResolvedValue({
      data: { id: "r1", kind: "test", status: "done", outcome: "failure", requestedAt: minutesAgo(0), destinationId: "d9" },
    } as any);
    const update = vi.spyOn(apiClient, "updateBackupDestination").mockResolvedValue({ data: {} } as any);

    const { user, dialog } = await openWizard();
    await goToCheck(user, dialog);
    await user.click(await within(dialog).findByRole("button", { name: w.next }));
    await user.click(within(dialog).getByRole("button", { name: w.details.save }));

    expect(await within(dialog).findByText(w.saving.failed)).toBeInTheDocument();
    await waitFor(() => expect(update).toHaveBeenCalledWith({
      path: { id: "d9" }, body: { name: w.details.defaultName, enabled: false, retentionVersions: 3 },
    }));
    expect(update).toHaveBeenCalledTimes(1);
  });

  it("explains a folder name the server refused", async () => {
    mockLocations();
    vi.spyOn(apiClient, "createBackupLocationFolder").mockResolvedValue({
      error: { type: "https://hdms.local/problems/folder-exists", status: 409, title: "exists" },
    } as any);
    const { user, dialog } = await openWizard();
    await user.click(await within(dialog).findByRole("button", { name: byText(root.name) }));
    await user.click(await within(dialog).findByRole("button", { name: w.folder.create }));
    expect(await within(dialog).findByText(w.folder.exists)).toBeInTheDocument();
  });

  it("names each drive and shows where it really is on the server", async () => {
    mockLocations();
    const { dialog } = await openWizard();
    const card = await within(dialog).findByRole("button", { name: byText("BackupSSD") });
    expect(card).toHaveTextContent("/Volumes/BackupSSD");
    expect(card).not.toHaveTextContent("/drives/BackupSSD");
  });

  it("shows the worker path when the host location is unknown", async () => {
    vi.spyOn(apiClient, "listBackupLocations").mockResolvedValue({
      data: { roots: [{ path: "/drives/usb", name: "usb", connected: true }], folders: [] },
    } as any);
    const { dialog } = await openWizard();
    expect(await within(dialog).findByRole("button", { name: byText("usb") })).toHaveTextContent("/drives/usb");
  });

  it("shows the current folder's real location", async () => {
    mockLocations();
    const { user, dialog } = await openWizard();
    await user.click(await within(dialog).findByRole("button", { name: byText(root.name) }));
    expect(await within(dialog).findByText("/Volumes/BackupSSD")).toBeInTheDocument();
  });
});
