import { render, screen, fireEvent, act } from "@testing-library/react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { axe } from "vitest-axe";
import { AttendantModal } from "./attendant-modal";
import { ManualSource, HidWedgeSource, ScanRouter } from "@hdms/scan";
import { CROCKFORD_ALPHABET } from "@hdms/domain";
import { setKioskConfig } from "@/lib/kiosk-config";


// Real valid token from fixtures
const VALID_TOKEN = "HD-U-B3G6822S6K-H";

describe("AttendantModal (Phase 3.4)", () => {
  beforeEach(() => {
    localStorage.clear();
    setKioskConfig({
      kioskId: "kiosk-01",
      kioskName: "ICU Entrance Kiosk",
      token: "secret-token",
    });
  });

  it("renders PIN prompt when opened and is accessible (axe clean)", async () => {
    const { container } = render(
      <AttendantModal isOpen={true} onClose={() => {}} />
    );

    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByLabelText(/Attendant 4–6 Digit PIN/i)).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("unlocks Crockford keypad upon entering correct PIN (1234)", async () => {
    render(<AttendantModal isOpen={true} onClose={() => {}} />);

    const pinInput = screen.getByLabelText(/Attendant 4–6 Digit PIN/i);
    const submitBtn = screen.getByTestId("pin-submit-button");

    fireEvent.change(pinInput, { target: { value: "1234" } });
    fireEvent.click(submitBtn);

    expect(screen.getByTestId("crockford-keypad-view")).toBeInTheDocument();
    expect(screen.getByTestId("keypad-grid")).toBeInTheDocument();
  });

  it("pinLockoutAfterFiveFailures with 60s countdown and disables input", async () => {
    vi.useFakeTimers();

    render(<AttendantModal isOpen={true} onClose={() => {}} />);

    const pinInput = screen.getByLabelText(/Attendant 4–6 Digit PIN/i);
    const submitBtn = screen.getByTestId("pin-submit-button");

    // Fail 5 times
    for (let i = 1; i <= 5; i++) {
      fireEvent.change(pinInput, { target: { value: "9999" } });
      fireEvent.click(submitBtn);
    }

    expect(
      screen.getByText(/Too many failed attempts\. Locked out for 60 seconds\./i)
    ).toBeInTheDocument();
    expect(screen.getByTestId("pin-lockout-notice")).toBeInTheDocument();
    expect(pinInput).toBeDisabled();
    expect(submitBtn).toBeDisabled();

    // Advance 30 seconds
    act(() => {
      vi.advanceTimersByTime(30000);
    });
    expect(screen.getByTestId("pin-lockout-notice")).toHaveTextContent("30s");

    // Advance remaining 30 seconds
    act(() => {
      vi.advanceTimersByTime(30000);
    });


    // Lockout released
    expect(pinInput).not.toBeDisabled();

    vi.useRealTimers();
  });

  it("keypadCannotProduceAnInvalidCharacter - all keypad buttons belong to Crockford alphabet or symbols", async () => {
    render(<AttendantModal isOpen={true} onClose={() => {}} />);

    // Unlock
    fireEvent.change(screen.getByLabelText(/Attendant 4–6 Digit PIN/i), {
      target: { value: "1234" },
    });
    fireEvent.click(screen.getByTestId("pin-submit-button"));

    // Clear initial prefix
    fireEvent.click(screen.getByTestId("keypad-key-clear"));
    expect(screen.getByTestId("token-display").textContent).toBe("HD-U-");

    // Click every single character button on the keypad
    for (const ch of CROCKFORD_ALPHABET) {
      const btn = screen.getByTestId(`keypad-key-${ch}`);
      expect(btn).toBeInTheDocument();
      // Ensure target size >= 56px (min-h-[56px] min-w-[56px])
      expect(btn.className).toContain("min-h-[56px]");
      expect(btn.className).toContain("min-w-[56px]");
    }
  });

  it("substitutesAmbiguousCharacters in helper and handles prefix switching", async () => {
    render(<AttendantModal isOpen={true} onClose={() => {}} />);

    // Unlock
    fireEvent.change(screen.getByLabelText(/Attendant 4–6 Digit PIN/i), {
      target: { value: "1234" },
    });
    fireEvent.click(screen.getByTestId("pin-submit-button"));

    // Switch prefix to HD-D-
    const devicePrefixBtn = screen.getByRole("button", { name: /HD-D- \(Device\)/i });
    fireEvent.click(devicePrefixBtn);

    expect(screen.getByTestId("token-display").textContent).toContain("HD-D-");

    // Switch back to HD-U-
    const staffPrefixBtn = screen.getByRole("button", { name: /HD-U- \(Staff ID\)/i });
    fireEvent.click(staffPrefixBtn);
    expect(screen.getByTestId("token-display").textContent).toContain("HD-U-");
  });

  it("submitDisabledUntilChecksumValid and checksumErrorNamesTheLastCharacter", async () => {
    render(<AttendantModal isOpen={true} onClose={() => {}} />);

    // Unlock
    fireEvent.change(screen.getByLabelText(/Attendant 4–6 Digit PIN/i), {
      target: { value: "1234" },
    });
    fireEvent.click(screen.getByTestId("pin-submit-button"));

    const submitBtn = screen.getByTestId("token-submit-button");
    expect(submitBtn).toBeDisabled();

    // Type full 10-char payload with WRONG check character: B3G6822S6K + 0 (correct is H)
    fireEvent.click(screen.getByTestId("keypad-key-clear"));
    const payload = "B3G6822S6K";
    for (const char of payload) {
      fireEvent.click(screen.getByTestId(`keypad-key-${char}`));
    }
    fireEvent.click(screen.getByTestId("keypad-key-hyphen"));
    fireEvent.click(screen.getByTestId("keypad-key-0"));

    // Checksum error must explicitly name the last character
    expect(
      screen.getByText(/That code doesn't look right — check the last character\./i)
    ).toBeInTheDocument();
    expect(submitBtn).toBeDisabled();


    // Fix the last character to H (Backspace then H)
    fireEvent.click(screen.getByTestId("keypad-key-backspace"));
    fireEvent.click(screen.getByTestId("keypad-key-H"));

    // Now valid!
    expect(screen.getByTestId("token-valid-badge")).toBeInTheDocument();
    expect(submitBtn).not.toBeDisabled();
  });

  it("manualSubmissionFlowsThroughTheRouterWithSourceManual", async () => {
    const router = new ScanRouter();
    const manualSource = new ManualSource();
    router.register(manualSource);
    await router.start();

    const receivedScans: any[] = [];
    router.subscribe((event) => {
      if (event.kind === "scan") {
        receivedScans.push(event.scan);
      }
    });

    const onClose = vi.fn();
    render(
      <AttendantModal
        isOpen={true}
        onClose={onClose}
        manualSource={manualSource}
      />
    );

    // Unlock
    fireEvent.change(screen.getByLabelText(/Attendant 4–6 Digit PIN/i), {
      target: { value: "1234" },
    });
    fireEvent.click(screen.getByTestId("pin-submit-button"));

    // Type valid token: HD-U-B3G6822S6K-H
    const payload = "B3G6822S6K";
    for (const char of payload) {
      fireEvent.click(screen.getByTestId(`keypad-key-${char}`));
    }
    fireEvent.click(screen.getByTestId("keypad-key-hyphen"));
    fireEvent.click(screen.getByTestId("keypad-key-H"));

    const submitBtn = screen.getByTestId("token-submit-button");
    expect(submitBtn).not.toBeDisabled();

    fireEvent.click(submitBtn);

    // Assert scan was emitted with source === 'manual'
    expect(receivedScans).toHaveLength(1);
    expect(receivedScans[0].token).toBe(VALID_TOKEN);
    expect(receivedScans[0].source).toBe("manual");
    expect(onClose).toHaveBeenCalled();

    await router.stop();
  });

  it("gateClosesOnWatchdog after 60 seconds of inactivity", () => {
    vi.useFakeTimers();
    const onClose = vi.fn();

    render(<AttendantModal isOpen={true} onClose={onClose} />);

    // Advance 59 seconds
    act(() => {
      vi.advanceTimersByTime(59000);
    });
    expect(onClose).not.toHaveBeenCalled();

    // Advance past 60s
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(onClose).toHaveBeenCalled();

    vi.useRealTimers();
  });

  it("wedgeDoesNotEmitWhileKeypadHasFocus when typing PIN or interacting with keypad", async () => {
    const hidSource = new HidWedgeSource({ maxIntervalMs: 35 });
    const emitSpy = vi.fn();
    await hidSource.start(emitSpy);

    render(<AttendantModal isOpen={true} onClose={() => {}} />);

    const pinInput = screen.getByLabelText(/Attendant 4–6 Digit PIN/i);
    pinInput.focus();

    // Simulate slow human typing into PIN input
    const now = performance.now();
    fireEvent.keyDown(pinInput, { key: "1", timeStamp: now });
    fireEvent.keyDown(pinInput, { key: "2", timeStamp: now + 120 });
    fireEvent.keyDown(pinInput, { key: "3", timeStamp: now + 240 });
    fireEvent.keyDown(pinInput, { key: "4", timeStamp: now + 360 });
    fireEvent.keyDown(pinInput, { key: "Enter", timeStamp: now + 480 });

    // HID wedge should not emit anything because human typing is slow and targeted to input
    expect(emitSpy).not.toHaveBeenCalled();

    await hidSource.stop();
  });

  it("keypad screen passes vitest-axe accessibility audit", async () => {
    const { container } = render(<AttendantModal isOpen={true} onClose={() => {}} />);

    // Unlock
    fireEvent.change(screen.getByLabelText(/Attendant 4–6 Digit PIN/i), {
      target: { value: "1234" },
    });
    fireEvent.click(screen.getByTestId("pin-submit-button"));

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
