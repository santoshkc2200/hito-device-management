import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { SuccessScreen } from "./success-screen";
import { formatHumanDueDate } from "@/lib/date-format";

describe("SuccessScreen", () => {
  it("renders borrowed variant with device details and passes axe", async () => {
    const onDone = vi.fn();
    const { container } = render(
      <SuccessScreen
        kind="borrowed"
        device={{
          id: "dev-1",
          name: "Philips HeartStart XL+",
          assetTag: "MED-0042",
        }}
        dueAt={new Date(Date.now() + 86400000).toISOString()}
        itemCount={1}
        onDone={onDone}
      />
    );

    expect(screen.getByTestId("success-kind-badge")).toHaveTextContent(/Borrow Confirmed/i);
    expect(screen.getByTestId("success-title")).toHaveTextContent("Device Borrowed Successfully");
    expect(screen.getByTestId("success-detail")).toHaveTextContent("Borrow recorded.");
    expect(screen.getByText("Philips HeartStart XL+")).toBeInTheDocument();
    expect(screen.getByText("MED-0042")).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("renders returned variant with distinct icon, badge, and copy", () => {
    const onDone = vi.fn();
    render(
      <SuccessScreen
        kind="returned"
        device={{
          id: "dev-2",
          name: "Welch Allyn Spot Vital Signs",
          assetTag: "MED-0099",
        }}
        itemCount={1}
        onDone={onDone}
      />
    );

    expect(screen.getByTestId("success-kind-badge")).toHaveTextContent(/Return Confirmed/i);
    expect(screen.getByTestId("success-title")).toHaveTextContent("Device Returned Successfully");
    expect(screen.getByTestId("success-detail")).toHaveTextContent("Return confirmed.");
  });

  it("successWithoutDueDateOmitsTheDueLine", () => {
    const onDone = vi.fn();
    render(
      <SuccessScreen
        kind="borrowed"
        device={{
          id: "dev-1",
          name: "ECG Monitor",
          assetTag: "MED-0011",
        }}
        dueAt={null}
        onDone={onDone}
      />
    );

    expect(screen.queryByTestId("success-due-line")).not.toBeInTheDocument();
    expect(screen.queryByText(/no due date/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/due/i)).not.toBeInTheDocument();
  });

  it("renders multi-item context when itemCount > 1", () => {
    const onDone = vi.fn();
    render(
      <SuccessScreen
        kind="borrowed"
        device={{
          id: "dev-3",
          name: "Infusion Pump",
          assetTag: "MED-0033",
        }}
        itemCount={3}
        onDone={onDone}
      />
    );

    const multiBadge = screen.getByTestId("multi-item-badge");
    expect(multiBadge).toBeInTheDocument();
    expect(multiBadge).toHaveTextContent("3 items");
  });

  it("auto-returns after 4 seconds", () => {
    vi.useFakeTimers();
    try {
      const onDone = vi.fn();
      render(
        <SuccessScreen
          kind="borrowed"
          onDone={onDone}
        />
      );

      expect(onDone).not.toHaveBeenCalled();

      act(() => {
        vi.advanceTimersByTime(3999);
      });
      expect(onDone).not.toHaveBeenCalled();

      act(() => {
        vi.advanceTimersByTime(1);
      });
      expect(onDone).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("done button immediately calls onDone", async () => {
    const user = userEvent.setup();
    const onDone = vi.fn();
    render(
      <SuccessScreen
        kind="borrowed"
        onDone={onDone}
      />
    );

    const doneButton = screen.getByTestId("done-success-button");
    await user.click(doneButton);
    expect(onDone).toHaveBeenCalledTimes(1);
  });

  it("formats human relative due date correctly", () => {
    const base = new Date(2026, 7, 20, 8, 0, 0); // 2026-08-20 08:00 local

    // Same day
    const sameDay = new Date(2026, 7, 20, 17, 0, 0).toISOString();
    expect(formatHumanDueDate(sameDay, base)).toMatch(/Please return by today, \d{2}:\d{2}/);

    // Tomorrow
    const tomorrow = new Date(2026, 7, 21, 9, 0, 0).toISOString();
    expect(formatHumanDueDate(tomorrow, base)).toMatch(/Please return by tomorrow, \d{2}:\d{2}/);

    // Later date
    const later = new Date(2026, 7, 25, 14, 0, 0).toISOString();
    expect(formatHumanDueDate(later, base)).toMatch(/Please return by (?:Aug|August) 25, \d{2}:\d{2}/);

    // Null or invalid
    expect(formatHumanDueDate(null, base)).toBeNull();
    expect(formatHumanDueDate(undefined, base)).toBeNull();
    expect(formatHumanDueDate("invalid-date", base)).toBeNull();
  });
});
