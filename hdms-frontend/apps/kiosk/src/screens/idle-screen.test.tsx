import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { IdleScreen } from "./idle-screen";

describe("IdleScreen", () => {
  it("renders scan guidance and passes vitest-axe", async () => {
    const { container } = render(
      <IdleScreen
        kioskName="East Wing Kiosk"
        supportCode="KIOSK-E1"
        scannerReady={true}
        scannerFresh={true}
      />
    );

    const prompt = screen.getByTestId("idle-prompt");
    expect(prompt).toBeInTheDocument();
    expect(prompt).toHaveTextContent(
      "Tap Start, then scan your staff ID card or a device barcode in any order."
    );

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("idleScreenNeverShowsPersonalData", () => {
    const { container } = render(
      <IdleScreen
        kioskName="General Kiosk"
        supportCode="KIOSK-01"
      />
    );

    const textContent = container.textContent ?? "";

    // Asserts no user names, employee IDs, departments, or emails can appear
    expect(textContent).not.toMatch(/Dr\.|Nurse|Department|Cardiology|@hospital|EMP-/i);
    expect(screen.queryByTestId("user-greeting-badge")).not.toBeInTheDocument();
    expect(screen.queryByTestId("open-loans-list")).not.toBeInTheDocument();
    expect(screen.queryByText(/Active Loans/i)).not.toBeInTheDocument();
  });

  it("renders scanner wake hint when scanner is sleeping", () => {
    render(
      <IdleScreen
        kioskName="General Kiosk"
        scannerReady={true}
        scannerFresh={false}
      />
    );

    expect(screen.getByTestId("scanner-wake-hint")).toBeInTheDocument();
    expect(screen.getByTestId("scanner-wake-hint")).toHaveTextContent(
      /press the scanner trigger once to wake it/i
    );
  });

  it("uses header camera fallback after scanning starts, including when HID reports available", async () => {
    const user = userEvent.setup();
    const onStartCamera = vi.fn();

    render(
      <IdleScreen
        isScanning={true}
        scannerReady={true}
        onStartCamera={onStartCamera}
      />
    );

    expect(screen.getByTestId("scan-status")).toHaveTextContent(
      "Scan with the device scanner, or use the camera instead."
    );
    expect(screen.queryByTestId("camera-fallback-card")).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Toggle camera barcode scanner" })
    );
    expect(onStartCamera).toHaveBeenCalledTimes(1);
  });

  it("starts scanner only after start card is tapped", async () => {
    const user = userEvent.setup();
    const onStartScanning = vi.fn();

    render(
      <IdleScreen
        onStartScanning={onStartScanning}
        onStartCamera={vi.fn()}
      />
    );

    expect(screen.getByTestId("start-scanning-button")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Toggle camera barcode scanner" })
    ).not.toBeInTheDocument();
    await user.click(screen.getByTestId("start-scanning-button"));

    expect(onStartScanning).toHaveBeenCalledTimes(1);
  });

  it("keeps Start as the only scan action and groups non-interactive scan choices", () => {
    render(<IdleScreen onStartScanning={vi.fn()} />);

    const startButton = screen.getByRole("button", { name: "Start" });
    const actionStack = screen.getByTestId("idle-action-stack");
    const guidanceCard = screen.getByTestId("scan-guidance-card");
    const staffCard = screen.getByTestId("staff-id-card");
    const deviceCard = screen.getByTestId("device-barcode-card");

    expect(screen.queryByRole("button", { name: /staff id card/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /device barcode/i })).not.toBeInTheDocument();
    expect(guidanceCard).toHaveTextContent(
      "Tap Start, then scan your staff ID card or a device barcode in any order."
    );
    expect(startButton).toHaveClass("min-h-48", "max-w-sm");
    expect(staffCard).toHaveClass("min-h-20");
    expect(deviceCard).toHaveClass("min-h-20");
    expect(actionStack).toHaveClass("flex-col", "min-[700px]:flex-row");
    expect(startButton.compareDocumentPosition(guidanceCard)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    expect(guidanceCard.contains(staffCard)).toBe(true);
    expect(guidanceCard.contains(deviceCard)).toBe(true);
    expect(startButton).toHaveTextContent("Start");
    expect(startButton).not.toHaveTextContent("Start scanning");
  });
});
