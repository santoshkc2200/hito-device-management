import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@hdms/i18n";
import { POLL_MS, ProgressScreen } from "./progress-screen";
import { fakeWorker } from "@/test/worker";
import { view } from "@/test/fixtures";

function renderProgress(pollMs = 10) {
  const handlers = { onUndo: vi.fn(), onRestart: vi.fn(), onSessionLost: vi.fn() };
  render(
    <LocaleProvider locale="en">
      <ProgressScreen pollMs={pollMs} {...handlers} />
    </LocaleProvider>,
  );
  return handlers;
}

describe("ProgressScreen", () => {
  it("polls every 2 seconds by default", () => {
    expect(POLL_MS).toBe(2000);
  });

  it("shows each step as done, in progress or waiting, then the finished restore", async () => {
    const worker = fakeWorker({
      "GET /restore": [
        { body: { restore: view({ step: "validate" }) } },
        { body: { restore: view({ phase: "completed", step: "record", canUndo: true }) } },
      ],
    });
    // Slow enough that the running state is still on screen when checked.
    const { onUndo } = renderProgress(300);

    expect(await screen.findByTestId("step-restore_scratch")).toHaveAttribute("data-state", "done");
    expect(screen.getByTestId("step-validate")).toHaveAttribute("data-state", "current");
    expect(screen.getByTestId("step-swap")).toHaveAttribute("data-state", "pending");
    expect(screen.getByText("Checking the restored data")).toBeInTheDocument();

    expect(await screen.findByTestId("restore-done")).toHaveTextContent("Sign in with the accounts as they were");
    expect(screen.getByRole("link", { name: "Open HDMS admin" })).toHaveAttribute("href", "/admin/");

    const polls = worker.called("GET /restore").length;
    await new Promise((r) => setTimeout(r, 50));
    expect(worker.called("GET /restore").length).toBe(polls);

    await userEvent.setup().click(screen.getByRole("button", { name: "Undo this restore" }));
    expect(onUndo).toHaveBeenCalledWith("2026-09-29T17:00:00Z");
  });

  it.each([
    ["maintenance_off_failed", "may still show"],
    ["record_failed", "could not be written to the audit log"],
  ])("finishes with the %s warning", async (warning, text) => {
    fakeWorker({ "GET /restore": { body: { restore: view({ phase: "completed", step: "record", warning }) } } });
    renderProgress();
    expect(await screen.findByTestId("restore-warning")).toHaveTextContent(text);
  });

  it("says an undo is done in its own words", async () => {
    fakeWorker({ "GET /restore": { body: { restore: view({ kind: "undo", phase: "completed", step: "record" }) } } });
    renderProgress();
    expect(await screen.findByTestId("restore-done")).toHaveTextContent("Restore undone");
  });

  it.each([
    ["no_admins", "no administrator account"],
    ["interrupted", "server restarted"],
    ["copy_forward_failed", "It stopped at: Keeping backup settings and the audit log."],
  ])("explains a failure with %s and offers to pick again", async (error, text) => {
    fakeWorker({ "GET /restore": { body: { restore: view({ phase: "failed", step: "copy_forward", error }) } } });
    const { onRestart } = renderProgress();
    const failed = await screen.findByTestId("restore-failed");
    expect(failed).toHaveTextContent("Nothing was replaced");
    expect(failed).toHaveTextContent(text);
    await userEvent.setup().click(screen.getByRole("button", { name: "Pick a backup again" }));
    expect(onRestart).toHaveBeenCalledTimes(1);
  });

  it("keeps polling through a worker that does not answer", async () => {
    fakeWorker({
      "GET /restore": [
        { body: { restore: view() } },
        { status: 502, body: {} },
        { body: { restore: view({ phase: "completed", step: "record" }) } },
      ],
    });
    renderProgress();
    expect(await screen.findByTestId("restore-done")).toBeInTheDocument();
  });

  it("hands a lost session back to key entry", async () => {
    fakeWorker({ "GET /restore": [{ body: { restore: view() } }, { status: 401, body: { error: "session_required" } }] });
    const { onSessionLost } = renderProgress();
    await vi.waitFor(() => expect(onSessionLost).toHaveBeenCalledTimes(1));
  });
});
