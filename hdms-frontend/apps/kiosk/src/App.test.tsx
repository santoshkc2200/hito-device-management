import { render, screen } from "@testing-library/react";
import { describe, expect, it, beforeEach } from "vitest";
import { axe } from "vitest-axe";
import { App } from "./App";
import { clearKioskConfig, setKioskConfig } from "./lib/kiosk-config";

describe("App (Kiosk Entrypoint)", () => {
  beforeEach(() => {
    clearKioskConfig();
  });

  it("renders PairingScreen when iPad is unpaired and passes axe a11y audit", async () => {
    const { container } = render(<App />);

    expect(
      await screen.findByText(/This iPad is not yet paired/i)
    ).toBeInTheDocument();
    expect(screen.getByTestId("pairing-keypad")).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("renders the primary IdleScreen when paired and passes axe a11y audit", async () => {
    setKioskConfig({
      kioskId: "kiosk-01",
      kioskName: "HDMS Kiosk",
      token: "mock-token-xyz",
    });

    const { container } = render(<App />);

    expect(
      await screen.findByText(
        /Tap Start, then scan your staff ID card or a device barcode in any order\./i
      )
    ).toBeInTheDocument();
    expect(screen.getByText("HDMS Kiosk")).toBeInTheDocument();
    expect(screen.getByText(/Scanner Ready/i)).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
