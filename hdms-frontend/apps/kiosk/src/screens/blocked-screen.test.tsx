import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { BlockedScreen } from "./blocked-screen";

describe("BlockedScreen", () => {
  it("blockedScreenRendersServerGuidanceVerbatim for unbound card", async () => {
    const onDismiss = vi.fn();
    const { container } = render(
      <BlockedScreen
        message={{
          title: "Card Not Assigned",
          detail: "This badge is not currently assigned to any staff member. Please visit the equipment administrator or use the paper register.",
          tone: "warning",
        }}
        onDismiss={onDismiss}
      />
    );

    expect(screen.getByTestId("blocked-title")).toHaveTextContent("Card Not Assigned");
    expect(screen.getByTestId("blocked-detail")).toHaveTextContent(
      "This badge is not currently assigned to any staff member. Please visit the equipment administrator or use the paper register."
    );

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("blockedScreenRendersServerGuidanceVerbatim for unknown card", () => {
    const onDismiss = vi.fn();
    render(
      <BlockedScreen
        message={{
          title: "Card Not Recognised",
          detail: "This card was not recognised by the hospital database. Please see equipment administrator or record on the paper register.",
          tone: "warning",
        }}
        onDismiss={onDismiss}
      />
    );

    expect(screen.getByTestId("blocked-title")).toHaveTextContent("Card Not Recognised");
    expect(screen.getByTestId("blocked-detail")).toHaveTextContent(
      "This card was not recognised by the hospital database. Please see equipment administrator or record on the paper register."
    );
  });

  it("blockedScreenRendersServerGuidanceVerbatim for revoked card", () => {
    const onDismiss = vi.fn();
    render(
      <BlockedScreen
        message={{
          title: "Card Inactive",
          detail: "This card credential has been deactivated. Please contact the equipment administrator.",
          tone: "warning",
        }}
        onDismiss={onDismiss}
      />
    );

    expect(screen.getByTestId("blocked-title")).toHaveTextContent("Card Inactive");
    expect(screen.getByTestId("blocked-detail")).toHaveTextContent(
      "This card credential has been deactivated. Please contact the equipment administrator."
    );
  });

  it("unknownOutcomeKindFallsBackToSafeMessage", () => {
    const onDismiss = vi.fn();
    render(
      <BlockedScreen
        message={null}
        problem={null}
        onDismiss={onDismiss}
      />
    );

    expect(screen.getByTestId("blocked-title")).toHaveTextContent("Action Not Completed");
    expect(screen.getByTestId("blocked-detail")).toHaveTextContent(
      /paper register|equipment administrator/i
    );
  });

  it("auto-dismisses after 8 seconds", () => {
    vi.useFakeTimers();
    try {
      const onDismiss = vi.fn();
      render(
        <BlockedScreen
          message={{
            title: "Device Already on Loan",
            detail: "This defibrillator is currently checked out to another user.",
            tone: "warning",
          }}
          onDismiss={onDismiss}
        />
      );

      expect(onDismiss).not.toHaveBeenCalled();

      act(() => {
        vi.advanceTimersByTime(7999);
      });
      expect(onDismiss).not.toHaveBeenCalled();

      act(() => {
        vi.advanceTimersByTime(1);
      });
      expect(onDismiss).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("ok button immediately calls onDismiss", async () => {
    const user = userEvent.setup();
    const onDismiss = vi.fn();
    render(
      <BlockedScreen
        message={{
          title: "Device Unavailable",
          detail: "This item is marked for maintenance.",
          tone: "warning",
        }}
        onDismiss={onDismiss}
      />
    );

    const okButton = screen.getByTestId("ok-blocked-button");
    await user.click(okButton);
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("renders non-alarming amber styling for warning tone", () => {
    const onDismiss = vi.fn();
    render(
      <BlockedScreen
        message={{
          title: "Device Unavailable",
          detail: "This item is marked for maintenance.",
          tone: "warning",
        }}
        onDismiss={onDismiss}
      />
    );

    const iconContainer = screen.getByTestId("blocked-icon-container");
    expect(iconContainer.className).toMatch(/amber/);
    expect(iconContainer.className).not.toMatch(/bg-destructive/);
  });
});
