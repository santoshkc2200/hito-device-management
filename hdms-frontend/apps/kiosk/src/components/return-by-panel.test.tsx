import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { ReturnByPanel } from "./return-by-panel";

const now = () => new Date(2026, 8, 29, 10, 0); // 29 Sep 2026 10:00 local
const iso = (d: Date) => d.toISOString();

function renderPanel(overrides: Partial<React.ComponentProps<typeof ReturnByPanel>> = {}) {
  const props = {
    sessionId: "sess-1",
    loanId: "loan-1",
    dueAt: iso(new Date(2026, 8, 30, 10, 0)),
    latestReturnAt: null,
    onUpdated: vi.fn(),
    onActivity: vi.fn(),
    setDueDate: vi.fn(),
    now,
    ...overrides,
  };
  const utils = render(<ReturnByPanel {...props} />);
  return { ...utils, props };
}

describe("ReturnByPanel", () => {
  it("shows the loan's due date and chips, and passes axe", async () => {
    const { container } = renderPanel();
    expect(screen.getByTestId("return-by-current")).toHaveTextContent(/tomorrow/i);
    expect(screen.getByRole("button", { name: "Today 17:00" })).toBeEnabled();
    expect(await axe(container)).toHaveNoViolations();
  });

  it("disables chips after the latest return and offers a Latest chip when none fit", () => {
    renderPanel({ latestReturnAt: iso(new Date(2026, 8, 29, 13, 0)) });
    expect(screen.getByRole("button", { name: "Today 17:00" })).toBeDisabled();
    expect(screen.getByRole("button", { name: /Latest:/ })).toBeEnabled();
  });

  it("choosing a chip saves it and reports the update", async () => {
    const update = { ok: true as const, loanId: "loan-1", dueAt: iso(new Date(2026, 8, 29, 17)), latestReturnAt: null, sessionExpiresAt: iso(new Date()) };
    const setDueDate = vi.fn().mockResolvedValue(update);
    const { props } = renderPanel({ setDueDate });
    await userEvent.click(screen.getByRole("button", { name: "Today 17:00" }));
    expect(setDueDate).toHaveBeenCalledWith({ sessionId: "sess-1", loanId: "loan-1", dueAt: update.dueAt });
    await waitFor(() => expect(props.onUpdated).toHaveBeenCalledWith(update));
    expect(props.onActivity).toHaveBeenCalled();
  });

  it("a conflict moves the selection to the new latest and explains why", async () => {
    const latest = iso(new Date(2026, 8, 29, 15, 0));
    const setDueDate = vi
      .fn()
      .mockResolvedValueOnce({ ok: false, conflict: true, latestReturnAt: latest })
      .mockResolvedValueOnce({ ok: true, loanId: "loan-1", dueAt: latest, latestReturnAt: latest, sessionExpiresAt: iso(new Date()) });
    const { props } = renderPanel({ setDueDate });
    await userEvent.click(screen.getByRole("button", { name: "Tomorrow 17:00" }));
    await waitFor(() => expect(screen.getByTestId("return-by-message")).toHaveTextContent(/reservation needs this device/i));
    expect(setDueDate).toHaveBeenLastCalledWith({ sessionId: "sess-1", loanId: "loan-1", dueAt: latest });
    await waitFor(() => expect(props.onUpdated).toHaveBeenCalled());
  });

  it("a failure keeps the current date and says so", async () => {
    const setDueDate = vi.fn().mockResolvedValue({ ok: false, conflict: false });
    const { props } = renderPanel({ setDueDate });
    await userEvent.click(screen.getByRole("button", { name: "Today 17:00" }));
    await waitFor(() => expect(screen.getByTestId("return-by-message")).toHaveTextContent(/Could not change/));
    expect(props.onUpdated).not.toHaveBeenCalled();
  });

  it("Other… opens day and time choices bounded by the latest return", async () => {
    renderPanel({ latestReturnAt: iso(new Date(2026, 8, 29, 11, 0)) });
    await userEvent.click(screen.getByRole("button", { name: "Other…" }));
    const times = screen.getByRole("group", { name: "Time" });
    expect(times.querySelectorAll("button")).toHaveLength(4); // 10:15, 10:30, 10:45, 11:00
  });
});
