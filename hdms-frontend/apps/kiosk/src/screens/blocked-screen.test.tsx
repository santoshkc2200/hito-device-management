import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { LocaleProvider } from "@hdms/i18n";
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

  it("words a keyed held-by-other refusal in the active locale, not the server English", () => {
    render(
      <LocaleProvider locale="ja">
        <BlockedScreen
          message={{
            title: "Held by someone else",
            detail: "iPad 12 is with Dr. Sato. Please see the equipment desk.",
            tone: "warning",
            key: "device_held_by_other",
            args: { deviceName: "iPad 12", holderName: "佐藤", holderDepartment: "放射線科" },
          }}
          onDismiss={vi.fn()}
        />
      </LocaleProvider>
    );

    expect(screen.getByTestId("blocked-title")).toHaveTextContent("すでに貸出中です");
    expect(screen.getByTestId("blocked-detail")).toHaveTextContent("iPad 12は佐藤（放射線科）が貸出中です");
    expect(screen.getByTestId("blocked-detail")).not.toHaveTextContent(/Held|equipment desk/);
  });

  it("drops the optional clauses of a keyed message whose args are missing", () => {
    render(
      <LocaleProvider locale="en">
        <BlockedScreen
          message={{ title: "x", detail: "x", tone: "warning", key: "device_held_by_other" }}
          onDismiss={vi.fn()}
        />
      </LocaleProvider>
    );

    expect(screen.getByTestId("blocked-detail")).toHaveTextContent(
      "That device is with a colleague. Only they can return it. Please see the equipment desk."
    );
  });

  it("never shows server English for a key the kiosk does not know", () => {
    render(
      <LocaleProvider locale="ja">
        <BlockedScreen
          message={{ title: "Brand new refusal", detail: "English only", tone: "warning", key: "not_in_catalogue" }}
          onDismiss={vi.fn()}
        />
      </LocaleProvider>
    );

    expect(screen.getByTestId("blocked-title")).toHaveTextContent("処理を完了できませんでした");
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
