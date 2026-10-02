import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { ja } from "@/i18n/ja";
import { RecoveryKeyCard } from "@/components/backups/recovery-key-card";
import { renderWithClient } from "./backup-fixtures";

const rk = ja.backups.recoveryKey;
const KEY = "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01-2345";

function renderCard(status: apiClient.BackupRecoveryKeyState["status"]) {
  return renderWithClient(<RecoveryKeyCard state={{ status }} localPath="/var/backups/hdms/repo" />);
}

async function createKey(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("button", { name: rk.create }));
  const dialog = await screen.findByRole("dialog");
  await user.type(within(dialog).getByLabelText(rk.password), "correct horse battery staple");
  await user.type(within(dialog).getByLabelText(rk.totpCode), "123456");
  await user.click(within(dialog).getByRole("button", { name: rk.continue }));
  return dialog;
}

describe("Recovery key card", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(apiClient, "listBackupDestinations").mockResolvedValue({
      data: { items: [{ id: "d1", name: "Ward NAS", target: "/drives/WardNAS/hdms", enabled: true, retentionVersions: 3 }] },
    } as any);
  });

  it("shows each status with its help text", () => {
    const { unmount } = renderCard("missing");
    expect(screen.getByText(rk.status.missing)).toBeInTheDocument();
    expect(screen.getByText(rk.missingHelp)).toBeInTheDocument();
    unmount();
    renderCard("outdated");
    expect(screen.getByText(rk.outdatedHelp)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: rk.replace })).toBeInTheDocument();
  });

  it("creates a key, shows the sheet, and confirms only with the right last group", async () => {
    const create = vi.spyOn(apiClient, "createBackupRecoveryKey").mockResolvedValue({
      data: { key: KEY, createdAt: new Date().toISOString() },
    } as any);
    const confirm = vi.spyOn(apiClient, "confirmBackupRecoveryKey").mockResolvedValue({ data: { status: "ready" } } as any);
    const user = userEvent.setup();
    renderCard("missing");

    const dialog = await createKey(user);
    await waitFor(() => expect(create).toHaveBeenCalledWith({ body: { password: "correct horse battery staple", totpCode: "123456" } }));
    expect(await within(dialog).findByText(KEY)).toBeInTheDocument();
    expect(within(dialog).getByText(rk.sheetOnce)).toBeInTheDocument();
    expect(within(dialog).getByText(/Ward NAS/)).toBeInTheDocument();

    const lastGroup = within(dialog).getByLabelText(rk.confirmLabel);
    await user.type(lastGroup, "9999");
    await user.click(within(dialog).getByRole("button", { name: rk.confirm }));
    expect(within(dialog).getByText(rk.confirmMismatch)).toBeInTheDocument();
    expect(confirm).not.toHaveBeenCalled();

    await user.clear(lastGroup);
    await user.type(lastGroup, "2345");
    await user.click(within(dialog).getByRole("button", { name: rk.confirm }));
    await waitFor(() => expect(confirm).toHaveBeenCalledTimes(1));
  });

  it("keeps the dialog open and says why when the password is wrong", async () => {
    vi.spyOn(apiClient, "createBackupRecoveryKey").mockResolvedValue({
      error: { type: "https://hdms.local/problems/reauth-failed", status: 422, title: "x" },
    } as any);
    const user = userEvent.setup();
    renderCard("missing");
    const dialog = await createKey(user);
    expect(await within(dialog).findByText(rk.reauthFailed)).toBeInTheDocument();
  });
});
