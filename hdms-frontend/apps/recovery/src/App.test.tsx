import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { App } from "./App";
import { fakeWorker } from "@/test/worker";
import { localSource, snapshots, view, workingStatus } from "@/test/fixtures";

const KEY = "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01-2345";

async function unlock(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByTestId("source-local"));
  await user.type(screen.getByLabelText("Recovery key"), KEY);
  await user.click(screen.getByRole("button", { name: "Unlock" }));
}

describe("recovery flow", () => {
  it("walks from the status page to a finished restore", async () => {
    const worker = fakeWorker({
      "GET /status": { body: workingStatus },
      "GET /sources": { body: { sources: [localSource] } },
      "POST /unlock": { body: { keysMatch: true } },
      "GET /snapshots": { body: { snapshots } },
      "POST /restore": { status: 202, body: { restore: view() } },
      "GET /restore": [
        { body: { restore: null } },
        { body: { restore: view() } },
        { body: { restore: view({ phase: "completed", step: "record", canUndo: true }) } },
      ],
    });
    const user = userEvent.setup();
    render(<App initialLocale="en" pollMs={10} />);

    await unlock(user);
    await user.click((await screen.findAllByRole("button", { name: "Restore this backup" }))[0]);
    await user.type(screen.getByLabelText("Type RESTORE to confirm"), "RESTORE");
    await user.click(screen.getByRole("button", { name: "Restore" }));

    expect(await screen.findByTestId("restore-done")).toHaveTextContent("Restored");
    expect(worker.called("POST /restore")[0].body).toEqual({ snapshotId: "snap-new", confirmation: "RESTORE" });
  });

  it("returns to key entry when the session is lost mid-restore and picks the restore up again", async () => {
    const worker = fakeWorker({
      "GET /status": { body: { ...workingStatus, restoreRunning: true } },
      "GET /sources": { body: { sources: [localSource] } },
      "POST /unlock": { body: { keysMatch: true } },
      "GET /snapshots": { body: { snapshots } },
      "GET /restore": [
        { body: { restore: view() } },
        { status: 401, body: { error: "session_required" } },
        { body: { restore: view({ step: "swap" }) } },
        { body: { restore: view({ phase: "completed", step: "record" }) } },
      ],
    });
    const user = userEvent.setup();
    render(<App initialLocale="en" pollMs={10} />);

    await unlock(user);
    expect(await screen.findByText(/recovery session ended/)).toBeInTheDocument();

    await user.type(screen.getByLabelText("Recovery key"), KEY);
    await user.click(screen.getByRole("button", { name: "Unlock" }));

    expect(await screen.findByTestId("restore-done")).toBeInTheDocument();
    expect(worker.called("POST /restore")).toHaveLength(0);
    expect(worker.called("GET /snapshots")).toHaveLength(0);
  });

  it("stops at a keys mismatch and goes back to the start", async () => {
    fakeWorker({
      "GET /status": { body: workingStatus },
      "GET /sources": { body: { sources: [localSource] } },
      "POST /unlock": { status: 409, body: { error: "keys_mismatch" } },
    });
    const user = userEvent.setup();
    render(<App initialLocale="en" />);

    await unlock(user);
    expect(await screen.findByTestId("keys-mismatch")).toHaveTextContent("install.sh --restore");
    await user.click(screen.getByRole("button", { name: "Back" }));
    expect(await screen.findByTestId("source-local")).toBeInTheDocument();
  });

  it("opens in Japanese and switches to English", async () => {
    fakeWorker({ "GET /status": { body: workingStatus }, "GET /sources": { body: { sources: [localSource] } } });
    const user = userEvent.setup();
    render(<App />);

    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("HDMS 復旧");
    await user.click(screen.getByTestId("language-toggle"));
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("HDMS recovery");
  });
});
