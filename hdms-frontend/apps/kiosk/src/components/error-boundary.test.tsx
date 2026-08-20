import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { ErrorBoundary } from "./error-boundary";

function ThrowingComponent({ shouldThrow }: { shouldThrow: boolean }) {
  if (shouldThrow) {
    throw new Error("Deliberate render crash test");
  }
  return <div data-testid="healthy-child">Healthy Component</div>;
}

describe("ErrorBoundary", () => {
  beforeEach(() => {
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders healthy children normally when no error occurs", () => {
    render(
      <ErrorBoundary kioskName="Test Kiosk">
        <ThrowingComponent shouldThrow={false} />
      </ErrorBoundary>
    );

    expect(screen.getByTestId("healthy-child")).toBeInTheDocument();
    expect(screen.queryByTestId("error-boundary-screen")).not.toBeInTheDocument();
  });

  it("errorBoundaryCatchesAndShowsSupportCode and passes axe", async () => {
    const { container } = render(
      <ErrorBoundary kioskName="KIOSK-NORTH-1">
        <ThrowingComponent shouldThrow={true} />
      </ErrorBoundary>
    );

    expect(screen.getByTestId("error-boundary-screen")).toBeInTheDocument();
    expect(screen.getByTestId("error-boundary-title")).toHaveTextContent("Temporary Display Issue");

    const code = screen.getByTestId("error-boundary-support-code");
    expect(code).toBeInTheDocument();
    expect(code.textContent).toMatch(/KIOSK-NORTH-1-[A-Z0-9]+/);

    // Verify paper fallback is communicated
    expect(screen.getByText(/Hospital Paper Register/i)).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("auto-reloads after 15 seconds", () => {
    vi.useFakeTimers();
    try {
      const onReload = vi.fn();
      render(
        <ErrorBoundary kioskName="KIOSK-1" onReload={onReload}>
          <ThrowingComponent shouldThrow={true} />
        </ErrorBoundary>
      );

      expect(onReload).not.toHaveBeenCalled();

      // Advance 14 seconds
      act(() => {
        vi.advanceTimersByTime(14000);
      });
      expect(onReload).not.toHaveBeenCalled();

      // Advance 1 more second (15s total)
      act(() => {
        vi.advanceTimersByTime(1000);
      });
      expect(onReload).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("fires reload callback when restart button is clicked", () => {
    const onReload = vi.fn();
    render(
      <ErrorBoundary kioskName="KIOSK-1" onReload={onReload}>
        <ThrowingComponent shouldThrow={true} />
      </ErrorBoundary>
    );

    const reloadButton = screen.getByTestId("error-boundary-reload-button");
    reloadButton.click();
    expect(onReload).toHaveBeenCalledTimes(1);
  });
});
