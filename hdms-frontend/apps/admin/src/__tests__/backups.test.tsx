import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { ja } from "@/i18n/ja";
import { BackupsPage } from "@/routes/backups";
import { formatBytes } from "@/components/backups/format";
import { baseConfig, renderWithClient } from "./backup-fixtures";

vi.mock("@/lib/use-role", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/use-role")>();
  return { ...actual, RoleGate: ({ children }: { children: React.ReactNode }) => <>{children}</> };
});

describe("Backups page", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(apiClient, "getBackupConfig").mockResolvedValue({ data: baseConfig() } as any);
    vi.spyOn(apiClient, "listBackupDestinations").mockResolvedValue({ data: { items: [] } } as any);
    vi.spyOn(apiClient, "listBackupSnapshots").mockResolvedValue({ data: { items: [] } } as any);
    vi.spyOn(apiClient, "listBackupRuns").mockResolvedValue({ data: { items: [] } } as any);
  });

  it("shows the four tabs and opens on the overview", async () => {
    renderWithClient(<BackupsPage />);
    expect(await screen.findByRole("tab", { name: ja.backups.tabs.overview })).toHaveAttribute("aria-selected", "true");
    for (const name of [ja.backups.tabs.destinations, ja.backups.tabs.snapshots, ja.backups.tabs.history]) {
      expect(screen.getByRole("tab", { name })).toBeInTheDocument();
    }
  });

  it("switches tabs", async () => {
    const user = userEvent.setup();
    renderWithClient(<BackupsPage />);
    await user.click(await screen.findByRole("tab", { name: ja.backups.tabs.history }));
    await waitFor(() => expect(apiClient.listBackupRuns).toHaveBeenCalled());
  });
});

describe("formatBytes", () => {
  it("uses binary units", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1536)).toBe("1.5 KiB");
    expect(formatBytes(5_242_880)).toBe("5.0 MiB");
  });
});
