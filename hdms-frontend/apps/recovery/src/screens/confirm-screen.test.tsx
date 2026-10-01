import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@hdms/i18n";
import { ConfirmScreen, type ConfirmMode } from "./confirm-screen";
import { fakeWorker } from "@/test/worker";
import { snapshots, view } from "@/test/fixtures";

function renderConfirm(mode: ConfirmMode) {
  const handlers = { onStarted: vi.fn(), onBack: vi.fn(), onSessionLost: vi.fn() };
  render(
    <LocaleProvider locale="en">
      <ConfirmScreen mode={mode} {...handlers} />
    </LocaleProvider>,
  );
  return handlers;
}

describe("ConfirmScreen", () => {
  it("says what will be replaced and starts only once RESTORE is typed", async () => {
    const worker = fakeWorker({ "POST /restore": { status: 202, body: { restore: view() } } });
    const { onStarted } = renderConfirm({ kind: "restore", snapshot: snapshots[0] });
    const user = userEvent.setup();

    expect(screen.getByText(/Everything recorded after .*Sep 30.* will be replaced/)).toBeInTheDocument();
    expect(screen.getByText(/paper register/)).toBeInTheDocument();

    const start = screen.getByRole("button", { name: "Restore" });
    await user.type(screen.getByLabelText("Type RESTORE to confirm"), "restor");
    expect(start).toBeDisabled();
    await user.type(screen.getByLabelText("Type RESTORE to confirm"), "e");
    expect(start).toBeEnabled();
    await user.click(start);

    expect(onStarted).toHaveBeenCalledTimes(1);
    expect(worker.called("POST /restore")[0].body).toEqual({ snapshotId: "snap-new", confirmation: "RESTORE" });
  });

  it("undoes with the same confirmation", async () => {
    const worker = fakeWorker({ "POST /restore/undo": { status: 202, body: { restore: view({ kind: "undo" }) } } });
    const { onStarted } = renderConfirm({ kind: "undo", snapshotTakenAt: "2026-09-29T17:00:00Z" });
    const user = userEvent.setup();
    expect(screen.getByText(/just before the restore from/)).toBeInTheDocument();
    await user.type(screen.getByLabelText("Type RESTORE to confirm"), "RESTORE");
    await user.click(screen.getByRole("button", { name: "Undo the restore" }));
    expect(onStarted).toHaveBeenCalledTimes(1);
    expect(worker.called("POST /restore/undo")[0].body).toEqual({ confirmation: "RESTORE" });
  });

  it("treats a restore that is already running as started", async () => {
    fakeWorker({ "POST /restore": { status: 409, body: { error: "restore_running" } } });
    const { onStarted } = renderConfirm({ kind: "restore", snapshot: snapshots[0] });
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Type RESTORE to confirm"), "RESTORE");
    await user.click(screen.getByRole("button", { name: "Restore" }));
    expect(onStarted).toHaveBeenCalledTimes(1);
  });

  it.each([
    ["database_server_down", "database server is not running"],
    ["snapshot_not_found", "no longer there"],
  ])("explains %s", async (code, text) => {
    fakeWorker({ "POST /restore": { status: 409, body: { error: code } } });
    renderConfirm({ kind: "restore", snapshot: snapshots[0] });
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Type RESTORE to confirm"), "RESTORE");
    await user.click(screen.getByRole("button", { name: "Restore" }));
    expect(await screen.findByTestId("confirm-error")).toHaveTextContent(text);
  });

  it("hands a lost session back to key entry", async () => {
    fakeWorker({ "POST /restore": { status: 401, body: { error: "session_required" } } });
    const { onSessionLost } = renderConfirm({ kind: "restore", snapshot: snapshots[0] });
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Type RESTORE to confirm"), "RESTORE");
    await user.click(screen.getByRole("button", { name: "Restore" }));
    expect(onSessionLost).toHaveBeenCalledTimes(1);
  });
});
