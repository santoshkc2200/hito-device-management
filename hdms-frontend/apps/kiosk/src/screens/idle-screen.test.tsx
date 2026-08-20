import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { IdleScreen } from "./idle-screen";

describe("IdleScreen", () => {
  it("renders prompt at required size and passes vitest-axe", async () => {
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
    expect(prompt).toHaveTextContent("Scan your ID card or a device barcode");

    // Must be styled with large text class
    expect(prompt.className).toMatch(/text-4xl|text-5xl/);

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

  it("fires camera toggle callback when camera button clicked", async () => {
    const user = userEvent.setup();
    const onToggleCamera = vi.fn();

    render(<IdleScreen onToggleCamera={onToggleCamera} />);

    const cameraButton = screen.getByRole("button", { name: /camera/i });
    await user.click(cameraButton);
    expect(onToggleCamera).toHaveBeenCalledTimes(1);
  });
});
