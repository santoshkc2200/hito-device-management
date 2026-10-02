import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { ja } from "@/i18n/ja";
import { DestinationsTab } from "@/components/backups/destinations-tab";
import { cloudPoll } from "@/components/backups/use-cloud-accounts";
import { baseConfig, renderWithClient } from "./backup-fixtures";

const c = ja.backups.cloud;
const w = ja.backups.wizard;

const connected: apiClient.BackupCloudAccount = {
  id: "acct-1", provider: "google_drive", name: "病院ドライブ", clientId: "cid", tenant: "common",
  status: "connected", accountEmail: "drive-owner@example.test",
};
const signIn: apiClient.BackupCloudSignIn = {
  id: "acct-1", userCode: "ABCD-EFGH", verificationUri: "https://example.test/device", expiresAt: new Date(Date.now() + 600_000).toISOString(),
};
const byText = (text: string) => (_: string, el: Element | null) => {
  if (!el?.textContent?.includes(text)) return false;
  if (el.tagName.toLowerCase() === "button") return true;
  return Array.from(el.children).every((child) => !child.textContent?.includes(text));
};

describe("cloud backup", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    cloudPoll.ms = 5;
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig({ drivesHostPath: "/Volumes" }) } as any);
    vi.spyOn(apiClient, "listBackupDestinations").mockResolvedValue({ data: { items: [] } } as any);
    vi.spyOn(apiClient, "listBackupLocations").mockResolvedValue({ data: { roots: [], folders: [] } } as any);
  });

  it("connects an account with the device code and lists it", async () => {
    let accounts: apiClient.BackupCloudAccount[] = [];
    vi.spyOn(apiClient, "listBackupCloudAccounts").mockImplementation((async () => ({ data: { items: accounts } })) as any);
    const create = vi.spyOn(apiClient, "createBackupCloudAccount").mockResolvedValue({ data: signIn } as any);
    const polls = [{ ...connected, status: "pending" as const }, connected];
    vi.spyOn(apiClient, "getBackupCloudAccount").mockImplementation((async () => {
      const next = polls.length > 1 ? polls.shift()! : polls[0];
      if (next.status === "connected") accounts = [connected];
      return { data: next };
    }) as any);

    const user = userEvent.setup();
    renderWithClient(<DestinationsTab />);
    await user.click(await screen.findByRole("button", { name: c.add }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText(c.connect.name), "病院ドライブ");
    await user.type(within(dialog).getByLabelText(c.connect.clientId), "cid");
    await user.type(within(dialog).getByLabelText(c.connect.clientSecret), "s3cret");
    await user.click(within(dialog).getByRole("button", { name: c.connect.start }));

    expect(await within(dialog).findByText("ABCD-EFGH")).toBeInTheDocument();
    expect(create).toHaveBeenCalledWith({
      body: { provider: "google_drive", name: "病院ドライブ", clientId: "cid", clientSecret: "s3cret" },
    });
    expect(await within(dialog).findByText(byText("drive-owner@example.test"), undefined, { timeout: 3000 })).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: c.connect.done }));
    expect(await screen.findByText("病院ドライブ")).toBeInTheDocument();
  });

  it("walks the wizard from a connected account to a saved cloud destination", async () => {
    vi.spyOn(apiClient, "listBackupCloudAccounts").mockResolvedValue({ data: { items: [connected] } } as any);
    const create = vi.spyOn(apiClient, "createBackupDestination").mockResolvedValue({
      data: { id: "d-1", name: w.cloud.defaultName, target: "cloud:acct-1/hdms-backups", provider: "google_drive", enabled: true, retentionVersions: 3 },
    } as any);
    const test = vi.spyOn(apiClient, "testBackupDestination").mockResolvedValue({ data: { id: "req-1", kind: "test", status: "pending", requestedAt: new Date().toISOString() } } as any);
    vi.spyOn(apiClient, "getBackupRequest").mockResolvedValue({
      data: { id: "req-1", kind: "test", status: "done", outcome: "success", requestedAt: new Date().toISOString() },
    } as any);

    const user = userEvent.setup();
    renderWithClient(<DestinationsTab />);
    await user.click(await screen.findByRole("button", { name: ja.backups.destinations.add }));
    const dialog = await screen.findByRole("dialog");
    await user.click(await within(dialog).findByRole("button", { name: byText(w.where.cloud) }));
    await user.click(await within(dialog).findByRole("button", { name: byText("病院ドライブ") }));
    const folder = await within(dialog).findByLabelText(w.cloud.folderName);
    await user.clear(folder);
    await user.type(folder, "a:b");
    expect(within(dialog).getByText(w.cloud.invalidFolder)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: w.cloud.useThis })).toBeDisabled();
    await user.clear(folder);
    await user.type(folder, "hdms-backups");
    await user.click(within(dialog).getByRole("button", { name: w.cloud.useThis }));
    await user.click(await within(dialog).findByRole("button", { name: w.details.save }));

    await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
    expect(create).toHaveBeenCalledWith({
      body: { name: w.cloud.defaultName, cloudAccountId: "acct-1", folder: "hdms-backups", retentionVersions: 3, enabled: true },
    });
    await waitFor(() => expect(test).toHaveBeenCalledWith({ path: { id: "d-1" } }));
    expect(await within(dialog).findByText(w.saving.ready)).toBeInTheDocument();
  });

  it("explains why an account in use cannot be disconnected", async () => {
    vi.spyOn(apiClient, "listBackupCloudAccounts").mockResolvedValue({ data: { items: [connected] } } as any);
    vi.spyOn(apiClient, "deleteBackupCloudAccount").mockResolvedValue({
      error: { type: "https://hdms.local/problems/cloud-account-in-use", status: 409, title: "in use" },
    } as any);
    const user = userEvent.setup();
    renderWithClient(<DestinationsTab />);
    await user.click(await screen.findByRole("button", { name: c.disconnect }));
    const confirm = await screen.findByRole("alertdialog");
    await user.click(within(confirm).getByRole("button", { name: c.disconnect }));
    expect(await screen.findByText(c.inUse)).toBeInTheDocument();
  });
});
