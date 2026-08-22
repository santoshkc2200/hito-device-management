import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { IdleScreen } from "./idle-screen";
import { AwaitingUserScreen } from "./awaiting-user-screen";
import { AwaitingDeviceScreen } from "./awaiting-device-screen";
import { SuccessScreen } from "./success-screen";
import { BlockedScreen } from "./blocked-screen";
import { OfflineScreen } from "./offline-screen";

describe("Touch Target Audit (WCAG 2.2 / iPad Kiosk Sizing)", () => {
  const checkInteractiveElementsTouchTargets = (container: HTMLElement) => {
    const interactiveElements = container.querySelectorAll<HTMLElement>(
      'button, a, input, select, textarea, [role="button"]'
    );

    expect(interactiveElements.length).toBeGreaterThan(0);

    for (const el of Array.from(interactiveElements)) {
      const classNames = el.className;
      // In our design system:
      // Minimum interactive target is 48px: min-h-12 (48px), min-w-12 (48px), h-12, size-12, or higher
      // Primary targets are >= 64px: min-h-16 (64px)
      const has48pxMinimum =
        classNames.includes("min-h-12") ||
        classNames.includes("min-h-14") ||
        classNames.includes("min-h-16") ||
        classNames.includes("size-12") ||
        classNames.includes("h-12") ||
        classNames.includes("h-14") ||
        classNames.includes("h-16") ||
        classNames.includes("p-3") ||
        classNames.includes("p-4");

      expect(
        has48pxMinimum,
        `Interactive element <${el.tagName.toLowerCase()} ${el.getAttribute(
          "aria-label"
        ) || el.textContent?.trim()}> must satisfy minimum 48px touch target rule. Class: ${classNames}`
      ).toBe(true);
    }
  };

  it("IdleScreen satisfies all touch-target requirements (all >= 48px)", () => {
    const { container } = render(
      <IdleScreen
        onStartScanning={vi.fn()}
        onOpenDiagnostics={vi.fn()}
      />
    );
    checkInteractiveElementsTouchTargets(container);
  });

  it("AwaitingUserScreen satisfies touch-target requirements (Cancel >= 64px)", () => {
    const { container } = render(
      <AwaitingUserScreen
        pendingDevice={{
          id: "dev-1",
          assetTag: "DEV-01",
          name: "Tablet",
        }}
        expiresAt={new Date(Date.now() + 45000).toISOString()}
        onCancel={vi.fn()}
        onToggleCamera={vi.fn()}
        onOpenDiagnostics={vi.fn()}
      />
    );

    checkInteractiveElementsTouchTargets(container);

    const cancelButton = container.querySelector('[data-testid="cancel-session-button"]');
    expect(cancelButton?.className).toContain("min-h-16"); // >= 64px
  });

  it("AwaitingDeviceScreen satisfies touch-target requirements (Done >= 64px, Return >= 48px)", () => {
    const { container } = render(
      <AwaitingDeviceScreen
        user={{
          id: "user-1",
          fullName: "Dr. Alice",
          department: "ICU",
          openLoanCount: 1,
        }}
        openLoans={[
          {
            id: "loan-1",
            deviceId: "dev-1",
            assetTag: "DEV-01",
            deviceName: "Device 1",
            borrowedAt: new Date().toISOString(),
          },
        ]}
        expiresAt={new Date(Date.now() + 25000).toISOString()}
        onReturnLoan={vi.fn()}
        onClose={vi.fn()}
        onToggleCamera={vi.fn()}
        onOpenDiagnostics={vi.fn()}
      />
    );

    checkInteractiveElementsTouchTargets(container);

    const doneButton = container.querySelector('[data-testid="done-session-button"]');
    expect(doneButton?.className).toContain("min-h-16"); // >= 64px

    const returnButton = container.querySelector('[data-testid="return-button-loan-1"]');
    expect(returnButton?.className).toContain("min-h-12"); // >= 48px
  });

  it("SuccessScreen satisfies touch-target requirements (Done and Scan Another >= 64px)", () => {
    const { container } = render(
      <SuccessScreen
        kind="borrowed"
        onDone={vi.fn()}
        onScanAnother={vi.fn()}
        onToggleCamera={vi.fn()}
        onOpenDiagnostics={vi.fn()}
      />
    );

    checkInteractiveElementsTouchTargets(container);

    const doneButton = container.querySelector('[data-testid="done-success-button"]');
    expect(doneButton?.className).toContain("min-h-16");

    const scanAnother = container.querySelector('[data-testid="scan-another-button"]');
    expect(scanAnother?.className).toContain("min-h-16");
  });

  it("BlockedScreen satisfies touch-target requirements (OK >= 64px)", () => {
    const { container } = render(
      <BlockedScreen
        onDismiss={vi.fn()}
        onToggleCamera={vi.fn()}
        onOpenDiagnostics={vi.fn()}
      />
    );

    checkInteractiveElementsTouchTargets(container);

    const okButton = container.querySelector('[data-testid="ok-blocked-button"]');
    expect(okButton?.className).toContain("min-h-16");
  });

  it("OfflineScreen satisfies touch-target requirements", () => {
    const { container } = render(
      <OfflineScreen
        onToggleCamera={vi.fn()}
        onOpenDiagnostics={vi.fn()}
      />
    );

    checkInteractiveElementsTouchTargets(container);
  });
});
