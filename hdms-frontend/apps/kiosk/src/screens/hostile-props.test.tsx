import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { IdleScreen } from "./idle-screen";
import { AwaitingUserScreen } from "./awaiting-user-screen";
import { AwaitingDeviceScreen } from "./awaiting-device-screen";
import { SuccessScreen } from "./success-screen";
import { BlockedScreen } from "./blocked-screen";
import { OfflineScreen } from "./offline-screen";

describe("Hostile Props Safety Guard (All Kiosk Screens)", () => {
  const assertSafeHumanRender = (container: HTMLElement) => {
    const text = container.textContent ?? "";

    // 1. Never empty state
    expect(text.trim().length).toBeGreaterThan(15);

    // 2. Never raw JavaScript objects or unhandled primitives
    expect(text).not.toMatch(/\[object Object\]/);
    expect(text).not.toMatch(/\bundefined\b/);
    expect(text).not.toMatch(/\bNaN\b/);

    // 3. Never raw exception / stack trace text
    expect(text).not.toMatch(/Error:\s+/i);
    expect(text).not.toMatch(/at (?:HTMLUnknownElement|renderWithHooks)/i);

    // 4. Never raw URLs or HTTP status dumps
    expect(text).not.toMatch(/https?:\/\//i);
    expect(text).not.toMatch(/500 Internal Server Error/i);
  };

  it("IdleScreen renders safely with hostile/missing props", () => {
    const { container } = render(
      <IdleScreen
        kioskName={undefined as any}
        supportCode={null as any}
        scannerReady={false}
        scannerFresh={false}
      />
    );
    assertSafeHumanRender(container);
  });

  it("AwaitingUserScreen renders safely with hostile/corrupt device and dates", () => {
    const { container } = render(
      <AwaitingUserScreen
        kioskName={undefined as any}
        supportCode={null as any}
        pendingDevice={null}
        expiresAt={"corrupted-iso-timestamp" as any}
        onCancel={vi.fn()}
      />
    );
    assertSafeHumanRender(container);
  });

  it("AwaitingDeviceScreen renders safely with null user and corrupted loan items", () => {
    const { container } = render(
      <AwaitingDeviceScreen
        user={null}
        openLoans={[
          {
            id: "loan-bad-1",
            deviceId: "dev-1",
            deviceName: "Hostile Device Name",
            assetTag: "BAD-001",
            borrowedAt: "invalid-date",
            dueAt: "corrupt-due-date",
          },
          {
            id: "loan-bad-2",
            deviceId: "dev-2",
            deviceName: "No Dates Device",
            assetTag: "TAG-999",
            borrowedAt: undefined as any,
            dueAt: undefined as any,
          },
        ]}
        expiresAt={null}
        onReturnLoan={vi.fn()}
        onClose={vi.fn()}
      />
    );
    assertSafeHumanRender(container);
  });

  it("SuccessScreen renders safely with unknown kind, null device, and corrupted dates", () => {
    const { container } = render(
      <SuccessScreen
        kind={"some_future_unknown_kind" as any}
        device={null}
        dueAt={"invalid-due-date"}
        itemCount={0}
        onDone={vi.fn()}
      />
    );
    assertSafeHumanRender(container);
  });

  it("BlockedScreen renders safely with null message, unknown tone, and raw problem error", () => {
    const { container } = render(
      <BlockedScreen
        message={null}
        problem={{
          kind: "unknown-problem",
          type: "about:blank",
          title: "",
          status: 500,
          supportCode: "TEST-CODE",
          raw: new Error("raw backend error"),
        }}
        onDismiss={vi.fn()}
      />
    );
    assertSafeHumanRender(container);
  });

  it("OfflineScreen renders safely with empty/null props", () => {
    const { container } = render(
      <OfflineScreen
        kioskName={undefined as any}
        supportCode={null as any}
      />
    );
    assertSafeHumanRender(container);
  });
});
