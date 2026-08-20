import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { CountdownRing } from "./countdown-timer";

describe("CountdownRing Component", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("countdownRingHiddenAboveEightSeconds", () => {
    const futureExpiry = new Date(Date.now() + 25000).toISOString();
    const { container } = render(<CountdownRing expiresAt={futureExpiry} />);

    expect(container.firstChild).toBeNull();
    expect(screen.queryByTestId("countdown-ring")).not.toBeInTheDocument();
  });

  it("countdownRingAppearsInFinalEightSeconds", () => {
    const baseNow = Date.now();
    const expiryDate = new Date(baseNow + 20000).toISOString(); // 20s remaining

    const { rerender } = render(<CountdownRing expiresAt={expiryDate} />);
    expect(screen.queryByTestId("countdown-ring")).not.toBeInTheDocument();

    // Advance by 12 seconds -> 8s remaining
    act(() => {
      vi.advanceTimersByTime(12000);
    });

    rerender(<CountdownRing expiresAt={expiryDate} />);

    const ring = screen.getByTestId("countdown-ring");
    expect(ring).toBeInTheDocument();
    expect(ring).toHaveAttribute("data-seconds-remaining", "8");
    expect(ring).toHaveTextContent("8s");

    // Advance by 3 more seconds -> 5s remaining
    act(() => {
      vi.advanceTimersByTime(3000);
    });

    rerender(<CountdownRing expiresAt={expiryDate} />);
    expect(screen.getByTestId("countdown-ring")).toHaveAttribute("data-seconds-remaining", "5");
    expect(screen.getByTestId("countdown-ring")).toHaveTextContent("5s");
  });

  it("accessibleLabelPresent", () => {
    const expiryDate = new Date(Date.now() + 6000).toISOString();
    render(<CountdownRing expiresAt={expiryDate} />);

    const ring = screen.getByRole("timer");
    expect(ring).toBeInTheDocument();
    expect(ring).toHaveAttribute("aria-label", "Session expires in 6 seconds");
    expect(ring).toHaveAttribute("aria-live", "polite");
    expect(ring).toHaveAttribute("aria-atomic", "true");
  });

  it("returnsNullWhenExpiresAtIsNull", () => {
    const { container } = render(<CountdownRing expiresAt={null} />);
    expect(container.firstChild).toBeNull();
  });

  it("hidesWhenExpired", () => {
    const expiryDate = new Date(Date.now() + 2000).toISOString();
    const { rerender } = render(<CountdownRing expiresAt={expiryDate} />);

    expect(screen.getByTestId("countdown-ring")).toBeInTheDocument();

    // Advance past expiry
    act(() => {
      vi.advanceTimersByTime(3000);
    });

    rerender(<CountdownRing expiresAt={expiryDate} />);
    expect(screen.queryByTestId("countdown-ring")).not.toBeInTheDocument();
  });
});
