import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { AwaitingDeviceScreen } from "./awaiting-device-screen";

describe("AwaitingDeviceScreen", () => {
  const mockUser = {
    id: "user-101",
    fullName: "Dr. Meredith Grey",
    department: "General Surgery",
    openLoanCount: 2,
  };

  const mockOpenLoans = [
    {
      id: "loan-normal-1",
      deviceId: "dev-tablet-01",
      assetTag: "TAB-001",
      deviceName: "iPad Clinical 1",
      borrowedAt: new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString(),
      dueAt: new Date(Date.now() + 48 * 60 * 60 * 1000).toISOString(),
    },
    {
      id: "loan-overdue-2",
      deviceId: "dev-pump-02",
      assetTag: "PUMP-900",
      deviceName: "Infusion Pump",
      borrowedAt: new Date(Date.now() - 5 * 24 * 60 * 60 * 1000).toISOString(),
      dueAt: new Date(Date.now() - 4 * 24 * 60 * 60 * 1000).toISOString(), // 4 days overdue
    },
  ];

  it("renders prompt at required size and passes vitest-axe", async () => {
    const { container } = render(
      <AwaitingDeviceScreen
        user={mockUser}
        openLoans={mockOpenLoans}
        expiresAt={new Date(Date.now() + 25000).toISOString()}
        onReturnLoan={vi.fn()}
        onClose={vi.fn()}
      />
    );

    const prompt = screen.getByTestId("awaiting-device-prompt");
    expect(prompt).toBeInTheDocument();
    expect(prompt).toHaveTextContent("Scan a device to borrow or return");
    expect(prompt.className).toMatch(/text-kiosk-prompt|text-4xl|text-5xl/);

    expect(screen.getByTestId("user-greeting-badge")).toHaveTextContent(
      "Dr. Meredith Grey"
    );
    expect(screen.getByTestId("user-greeting-badge")).toHaveTextContent(
      "General Surgery"
    );

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("returnRequiresTwoTaps and firstTapAloneFiresNothing", async () => {
    const user = userEvent.setup();
    const onReturnLoan = vi.fn();

    render(
      <AwaitingDeviceScreen
        user={mockUser}
        openLoans={mockOpenLoans}
        expiresAt={new Date(Date.now() + 25000).toISOString()}
        onReturnLoan={onReturnLoan}
        onClose={vi.fn()}
      />
    );

    const returnBtn = screen.getByTestId("return-button-loan-normal-1");
    expect(returnBtn).toHaveTextContent("RETURN");

    // 1st Tap: Enters confirmation mode
    await user.click(returnBtn);

    expect(returnBtn).toHaveTextContent("Confirm Return");
    expect(onReturnLoan).not.toHaveBeenCalled(); // First tap alone fires nothing!

    // 2nd Tap: Confirms and executes return
    await user.click(returnBtn);

    expect(onReturnLoan).toHaveBeenCalledTimes(1);
    expect(onReturnLoan).toHaveBeenCalledWith("loan-normal-1");
  });

  it("overdueRowIsFlaggedWithIconAndText (not color alone)", () => {
    render(
      <AwaitingDeviceScreen
        user={mockUser}
        openLoans={mockOpenLoans}
        expiresAt={new Date(Date.now() + 25000).toISOString()}
        onReturnLoan={vi.fn()}
        onClose={vi.fn()}
      />
    );

    const overdueFlag = screen.getByTestId("overdue-flag-loan-overdue-2");
    expect(overdueFlag).toBeInTheDocument();

    // Must contain both explicit text and an icon element
    expect(overdueFlag).toHaveTextContent(/overdue/i);
    expect(overdueFlag.querySelector("svg")).toBeInTheDocument();
  });

  it("openLoansEmptyStateIsNotAnError", () => {
    const { container } = render(
      <AwaitingDeviceScreen
        user={mockUser}
        openLoans={[]}
        expiresAt={new Date(Date.now() + 25000).toISOString()}
        onReturnLoan={vi.fn()}
        onClose={vi.fn()}
      />
    );

    const emptyState = screen.getByTestId("empty-loans-container");
    expect(emptyState).toBeInTheDocument();
    expect(emptyState).toHaveTextContent("No devices currently borrowed");

    // Assert that empty state is not styled as an error or destructive alert
    expect(emptyState.className).not.toMatch(/destructive|bg-red|text-red/);
    expect(container.textContent).not.toMatch(/Error|Failed|Problem/i);
  });

  it("doneClosesTheSession", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();

    render(
      <AwaitingDeviceScreen
        user={mockUser}
        openLoans={mockOpenLoans}
        expiresAt={new Date(Date.now() + 25000).toISOString()}
        onReturnLoan={vi.fn()}
        onClose={onClose}
      />
    );

    const doneButton = screen.getByTestId("done-session-button");
    await user.click(doneButton);
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
