import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { DiagnosticsModal } from "./diagnostics-modal";
import { HidWedgeSource } from "@hdms/scan";

describe("DiagnosticsModal", () => {
  let source: HidWedgeSource;

  beforeEach(() => {
    localStorage.clear();
    source = new HidWedgeSource();
  });

  it("renders PIN prompt when opened and unlocks with correct PIN", () => {
    const handleClose = vi.fn();
    render(
      <DiagnosticsModal isOpen={true} onClose={handleClose} scannerSource={source} />
    );

    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByLabelText(/Attendant PIN/i)).toBeInTheDocument();

    const pinInput = screen.getByLabelText(/Attendant PIN/i);
    const unlockBtn = screen.getByRole("button", { name: /Unlock/i });

    // Enter incorrect PIN
    fireEvent.change(pinInput, { target: { value: "0000" } });
    fireEvent.click(unlockBtn);
    expect(screen.getByText(/Incorrect PIN/i)).toBeInTheDocument();

    // Enter correct PIN (1234)
    fireEvent.change(pinInput, { target: { value: "1234" } });
    fireEvent.click(unlockBtn);

    expect(screen.getByText(/Hardware Diagnostics/i)).toBeInTheDocument();
    expect(screen.getByText(/Last 10 Raw Sequences/i)).toBeInTheDocument();
  });

  it("does not render when isOpen is false", () => {
    render(<DiagnosticsModal isOpen={false} onClose={vi.fn()} scannerSource={source} />);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});
