import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@hdms/i18n";
import { SnapshotsScreen } from "./snapshots-screen";
import { fakeWorker } from "@/test/worker";
import { snapshots, view } from "@/test/fixtures";

function renderSnapshots() {
  const handlers = { onPick: vi.fn(), onUndo: vi.fn(), onRunning: vi.fn(), onSessionLost: vi.fn() };
  render(
    <LocaleProvider locale="en">
      <SnapshotsScreen {...handlers} />
    </LocaleProvider>,
  );
  return handlers;
}

describe("SnapshotsScreen", () => {
  it("lists backups newest first, highlights the newest and shows plain dates and sizes", async () => {
    fakeWorker({ "GET /restore": { body: { restore: null } }, "GET /snapshots": { body: { snapshots } } });
    const { onPick } = renderSnapshots();

    const rows = await screen.findAllByTestId("snapshot");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveAttribute("data-newest", "true");
    expect(rows[0]).toHaveTextContent("Newest");
    expect(rows[0]).toHaveTextContent("Sep 30");
    expect(rows[0]).toHaveTextContent("02:00");
    expect(rows[0]).toHaveTextContent("Size 1.5 MB");
    expect(rows[1]).not.toHaveAttribute("data-newest");

    await userEvent.setup().click(screen.getAllByRole("button", { name: "Restore this backup" })[0]);
    expect(onPick).toHaveBeenCalledWith(snapshots[0]);
  });

  it("goes straight to progress when a restore is already running", async () => {
    const worker = fakeWorker({ "GET /restore": { body: { restore: view() } }, "GET /snapshots": { body: { snapshots } } });
    const { onRunning } = renderSnapshots();
    await vi.waitFor(() => expect(onRunning).toHaveBeenCalledTimes(1));
    expect(worker.called("GET /snapshots")).toHaveLength(0);
  });

  it("offers to undo the last restore while the previous database is kept", async () => {
    fakeWorker({
      "GET /restore": { body: { restore: view({ phase: "completed", step: "record", canUndo: true }) } },
      "GET /snapshots": { body: { snapshots } },
    });
    const { onUndo } = renderSnapshots();
    expect(await screen.findByTestId("last-restore")).toHaveTextContent("you can undo it");
    await userEvent.setup().click(screen.getByRole("button", { name: "Undo this restore" }));
    expect(onUndo).toHaveBeenCalledWith("2026-09-29T17:00:00Z");
  });

  it("hands a lost session back to key entry", async () => {
    fakeWorker({ "GET /restore": { status: 401, body: { error: "session_required" } } });
    const { onSessionLost } = renderSnapshots();
    await vi.waitFor(() => expect(onSessionLost).toHaveBeenCalledTimes(1));
  });

  it("says when the backups cannot be read, and when there are none", async () => {
    fakeWorker({
      "GET /restore": { body: { restore: null } },
      "GET /snapshots": [{ status: 502, body: { error: "repository_unreadable" } }, { body: { snapshots: [] } }],
    });
    renderSnapshots();
    expect(await screen.findByTestId("snapshots-error")).toHaveTextContent("cannot be read");
    await userEvent.setup().click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("There are no backups in this place.")).toBeInTheDocument();
  });
});
