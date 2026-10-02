import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { LocaleProvider } from "@hdms/i18n";
import { StartScreen } from "./start-screen";
import { fakeWorker } from "@/test/worker";
import { localSource, localWithoutKey, nasSource, usbSource, workingStatus } from "@/test/fixtures";

function renderStart() {
  const onChoose = vi.fn();
  const view = render(
    <LocaleProvider locale="en">
      <StartScreen onChoose={onChoose} />
    </LocaleProvider>,
  );
  return { onChoose, container: view.container };
}

describe("StartScreen", () => {
  it.each([
    ["working", "HDMS database is working"],
    ["empty", "HDMS database is empty"],
    ["damaged", "HDMS database is damaged"],
  ])("says what the worker sees when the database is %s", async (database, text) => {
    fakeWorker({
      "GET /status": { body: { ...workingStatus, database } },
      "GET /sources": { body: { sources: [localSource] } },
    });
    renderStart();
    expect(await screen.findByTestId("database-status")).toHaveTextContent(text);
  });

  it("lists every place with backups and refuses one without a recovery key", async () => {
    fakeWorker({
      "GET /status": { body: workingStatus },
      "GET /sources": { body: { sources: [localWithoutKey, nasSource, usbSource] } },
    });
    const user = userEvent.setup();
    const { onChoose, container } = renderStart();

    const local = await screen.findByTestId("source-local");
    expect(local).toHaveTextContent("This server");
    expect(local).toHaveTextContent("No recovery key is stored here yet");
    expect(local).toBeDisabled();
    expect(screen.getByTestId(`source-${nasSource.id}`)).toHaveTextContent("Ward NAS");
    expect(screen.getByTestId(`source-${usbSource.id}`)).toHaveTextContent("Network drive or external disk");
    expect(screen.getByTestId(`source-${usbSource.id}`)).toHaveTextContent("/drives/usb/hdms");

    await user.click(screen.getByTestId(`source-${usbSource.id}`));
    expect(onChoose).toHaveBeenCalledWith(usbSource);
    expect(await axe(container)).toHaveNoViolations();
  });

  it("offers no source while the database server is down, and checks again on request", async () => {
    const worker = fakeWorker({
      "GET /status": { body: { ...workingStatus, database: "server_down" } },
      "GET /sources": { body: { sources: [localSource] } },
    });
    const user = userEvent.setup();
    renderStart();

    expect(await screen.findByTestId("database-status")).toHaveTextContent("The database server is not running");
    expect(screen.queryByTestId("source-local")).toBeNull();
    expect(worker.called("GET /sources")).toHaveLength(0);

    await user.click(screen.getByRole("button", { name: "Check again" }));
    await screen.findByTestId("database-status");
    expect(worker.called("GET /status").length).toBeGreaterThanOrEqual(2);
  });

  it("says when a restore is already running", async () => {
    fakeWorker({
      "GET /status": { body: { ...workingStatus, restoreRunning: true } },
      "GET /sources": { body: { sources: [localSource] } },
    });
    renderStart();
    expect(await screen.findByTestId("restore-running")).toHaveTextContent("A restore is running");
  });

  it("says plainly when the worker does not answer", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new TypeError("Failed to fetch"));
    renderStart();
    expect(await screen.findByTestId("start-error")).toHaveTextContent("not responding");
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });
});
