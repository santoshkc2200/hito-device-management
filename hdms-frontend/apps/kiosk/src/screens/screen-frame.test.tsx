import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { ScreenFrame } from "./screen-frame";

describe("ScreenFrame", () => {
  it("renders persistent chrome header, footer, and children", async () => {
    const { container } = render(
      <ScreenFrame
        kioskName="West Wing Station"
        supportCode="KIOSK-W2"
        scannerReady={true}
        scannerFresh={true}
      >
        <div data-testid="test-content">Screen Body Content</div>
      </ScreenFrame>
    );

    expect(screen.getByText("West Wing Station")).toBeInTheDocument();
    expect(screen.getByText("Scanner Ready")).toBeInTheDocument();
    expect(screen.getByTestId("kiosk-support-code")).toHaveTextContent("KIOSK-W2");
    expect(screen.getByTestId("test-content")).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("handles scanner idle state with wake hint", () => {
    render(
      <ScreenFrame
        kioskName="Station A"
        scannerReady={true}
        scannerFresh={false}
      >
        <div>Content</div>
      </ScreenFrame>
    );

    expect(
      screen.getByText("Scanner Idle — press trigger once to wake")
    ).toBeInTheDocument();
  });

  it("triggers camera and diagnostics callbacks", async () => {
    const user = userEvent.setup();
    const onToggleCamera = vi.fn();
    const onOpenDiagnostics = vi.fn();

    render(
      <ScreenFrame
        onToggleCamera={onToggleCamera}
        onOpenDiagnostics={onOpenDiagnostics}
      >
        <div>Content</div>
      </ScreenFrame>
    );

    const cameraButton = screen.getByRole("button", { name: /camera/i });
    await user.click(cameraButton);
    expect(onToggleCamera).toHaveBeenCalledTimes(1);

    const diagButton = screen.getByRole("button", { name: /diagnostics/i });
    await user.click(diagButton);
    expect(onOpenDiagnostics).toHaveBeenCalledTimes(1);
  });
});
