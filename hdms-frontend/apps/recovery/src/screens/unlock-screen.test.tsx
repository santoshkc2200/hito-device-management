import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { LocaleProvider } from "@hdms/i18n";
import { UnlockScreen } from "./unlock-screen";
import { fakeWorker } from "@/test/worker";
import { localSource, nasSource } from "@/test/fixtures";

const KEY = "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01-2345";

function renderUnlock(props: Partial<Parameters<typeof UnlockScreen>[0]> = {}) {
  const handlers = { onUnlocked: vi.fn(), onMismatch: vi.fn(), onBack: vi.fn() };
  const view = render(
    <LocaleProvider locale="en">
      <UnlockScreen source={localSource} sessionEnded={false} {...handlers} {...props} />
    </LocaleProvider>,
  );
  return { ...handlers, container: view.container };
}

async function typeKeyAndSubmit(key = KEY) {
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Recovery key"), key);
  await user.click(screen.getByRole("button", { name: "Unlock" }));
  return user;
}

describe("UnlockScreen", () => {
  it("unlocks the chosen place with the typed key", async () => {
    const worker = fakeWorker({ "POST /unlock": { body: { keysMatch: true } } });
    const { onUnlocked, container } = renderUnlock({ source: nasSource });
    expect(screen.getByText("Backups: Ward NAS")).toBeInTheDocument();
    expect(await axe(container)).toHaveNoViolations();

    await typeKeyAndSubmit();

    expect(onUnlocked).toHaveBeenCalledTimes(1);
    expect(worker.called("POST /unlock")[0].body).toEqual({ source: nasSource.id, key: KEY });
  });

  it("keeps Unlock disabled until something is typed", () => {
    fakeWorker({});
    renderUnlock();
    expect(screen.getByRole("button", { name: "Unlock" })).toBeDisabled();
  });

  it.each([
    [422, "key_typo", "typing mistake"],
    [422, "key_format", "28 letters and numbers in 7 groups"],
    [401, "key_wrong", "does not open the backups"],
    [404, "no_bundle", "No recovery key is stored"],
    [422, "bundle_damaged", "damaged"],
    [404, "source_not_found", "no longer available"],
    [500, "internal", "Something went wrong"],
  ])("explains %i %s in plain words and keeps the key for correction", async (status, code, text) => {
    fakeWorker({ "POST /unlock": { status, body: { error: code } } });
    const { onUnlocked } = renderUnlock();
    await typeKeyAndSubmit();

    expect(await screen.findByTestId("unlock-error")).toHaveTextContent(text);
    expect(screen.getByLabelText("Recovery key")).toHaveValue(KEY);
    expect(onUnlocked).not.toHaveBeenCalled();
  });

  it("says how long to wait after too many attempts", async () => {
    fakeWorker({ "POST /unlock": { status: 429, body: { error: "too_many_attempts" }, headers: { "Retry-After": "90" } } });
    renderUnlock();
    await typeKeyAndSubmit();
    expect(await screen.findByTestId("unlock-error")).toHaveTextContent("Wait 2 min");
  });

  it("hands a keys mismatch to the mismatch screen and forgets the key", async () => {
    fakeWorker({ "POST /unlock": { status: 409, body: { error: "keys_mismatch" } } });
    const { onMismatch } = renderUnlock();
    await typeKeyAndSubmit();
    expect(onMismatch).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText("Recovery key")).toHaveValue("");
  });

  it("explains why the key is asked for again after a lost session", () => {
    fakeWorker({});
    renderUnlock({ sessionEnded: true });
    expect(screen.getByText(/recovery session ended/)).toBeInTheDocument();
  });
});
