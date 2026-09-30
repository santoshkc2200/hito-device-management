import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { AwaitingUserScreen } from "./awaiting-user-screen";

describe("AwaitingUserScreen", () => {
  const mockPendingDevice = {
    id: "dev-ultra-1",
    assetTag: "SCANNER-99",
    name: "Ultrasound Probe",
    category: "Diagnostic Tool",
    status: "available" as const,
  };

  it("renders prompt at required size and passes vitest-axe", async () => {
    const { container } = render(
      <AwaitingUserScreen
        pendingDevice={mockPendingDevice}
        expiresAt={new Date(Date.now() + 45000).toISOString()}
        onCancel={vi.fn()}
      />
    );

    const prompt = screen.getByTestId("awaiting-user-prompt");
    expect(prompt).toBeInTheDocument();
    expect(prompt).toHaveTextContent("Now scan your ID card");
    expect(prompt.className).toMatch(/text-kiosk-prompt|text-4xl|text-5xl/);

    expect(screen.getByTestId("pending-device-asset-tag")).toHaveTextContent(
      "SCANNER-99"
    );
    expect(screen.getByText("Ultrasound Probe")).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("tells the person an on-loan device is already out before they scan a card", () => {
    render(
      <AwaitingUserScreen
        pendingDevice={{ id: "d1", assetTag: "LAPTOP-01", name: "Clinical Tablet", status: "on_loan" }}
        expiresAt={new Date(Date.now() + 45000).toISOString()}
        onCancel={vi.fn()}
      />
    );

    expect(screen.getByTestId("awaiting-user-prompt")).toHaveTextContent("This device is already on loan");
    expect(screen.getByTestId("awaiting-user-hint")).toHaveTextContent(/Only the person who borrowed it/);
  });

  it("awaitingUserShowsDeviceWithoutHolderName", () => {
    const onLoanDevice = {
      id: "dev-laptop-01",
      assetTag: "LAPTOP-01",
      name: "Clinical Tablet",
      status: "on_loan" as const,
    };

    const { container } = render(
      <AwaitingUserScreen
        pendingDevice={onLoanDevice}
        expiresAt={new Date(Date.now() + 45000).toISOString()}
        onCancel={vi.fn()}
      />
    );

    const badge = screen.getByTestId("device-status-badge");
    expect(badge).toHaveTextContent(/Currently on loan/i);

    // Assert that no person name or holder information is shown
    const text = container.textContent ?? "";
    expect(text).not.toMatch(/Dr\.|Nurse|Holder|Borrower|Cardiology|user-/i);
  });

  it("cancel button triggers onCancel callback", async () => {
    const user = userEvent.setup();
    const onCancel = vi.fn();

    render(
      <AwaitingUserScreen
        pendingDevice={mockPendingDevice}
        expiresAt={new Date(Date.now() + 45000).toISOString()}
        onCancel={onCancel}
      />
    );

    const cancelButton = screen.getByTestId("cancel-session-button");
    await user.click(cancelButton);
    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});
