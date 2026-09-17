import { render, screen, fireEvent, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { axe } from "vitest-axe";
import { OfflineIndicator } from "./offline-indicator";
import { DiagnosticsModal } from "./diagnostics-modal";
import {
  enqueueQueueItem,
  updateQueueItemStatus,
  resetQueueDbForTesting,
  getQueueItems,
  getDeletionAuditLogs,
} from "../lib/offline-queue";
import { setKioskConfig, clearKioskConfig } from "../lib/kiosk-config";
import { LocaleProvider } from "@hdms/i18n";

describe("5.1d Offline UI", () => {
  const KIOSK_ID = "kiosk-ui-test";

  beforeEach(async () => {
    await resetQueueDbForTesting();
    clearKioskConfig();
    localStorage.clear();
    setKioskConfig({
      kioskId: KIOSK_ID,
      kioskName: "Ward 4 Kiosk",
      token: "test-token",
    });
  });

  afterEach(async () => {
    await resetQueueDbForTesting();
    clearKioskConfig();
    localStorage.clear();
  });

  it("component test — indicator renders the count, updates as items drain, hides at zero", async () => {
    // 1. Initial render with 3 pending items
    const { rerender } = render(
      <LocaleProvider locale="en">
        <OfflineIndicator count={3} />
      </LocaleProvider>
    );

    // Indicator must render the live pending count
    expect(screen.getByTestId("offline-indicator")).toBeInTheDocument();
    expect(screen.getByText(/Working offline — 3 transactions pending/i)).toBeInTheDocument();

    // 2. Count drains: 3 -> 2
    rerender(
      <LocaleProvider locale="en">
        <OfflineIndicator count={2} />
      </LocaleProvider>
    );
    expect(screen.getByText(/Working offline — 2 transactions pending/i)).toBeInTheDocument();

    // 3. Count drains: 2 -> 1
    rerender(
      <LocaleProvider locale="en">
        <OfflineIndicator count={1} />
      </LocaleProvider>
    );
    expect(screen.getByText(/Working offline — 1 transaction pending/i)).toBeInTheDocument();

    // 4. Count reaches zero: indicator disappears quietly with no toast or announcement
    rerender(
      <LocaleProvider locale="en">
        <OfflineIndicator count={0} />
      </LocaleProvider>
    );
    expect(screen.queryByTestId("offline-indicator")).not.toBeInTheDocument();
    expect(screen.queryByText(/Working offline/i)).not.toBeInTheDocument();

    // 5. Verify Japanese locale format as well
    rerender(
      <LocaleProvider locale="ja">
        <OfflineIndicator count={2} />
      </LocaleProvider>
    );
    expect(screen.getByTestId("offline-indicator")).toBeInTheDocument();
    expect(screen.getByText(/オフライン動作中 — 保留中のトランザクション 2 件/i)).toBeInTheDocument();
  });

  it("component test — a quarantined item shows its reason and cannot be dismissed accidentally", async () => {
    // Enqueue an item and transition it to quarantined with reason
    const item = await enqueueQueueItem({
      kioskId: KIOSK_ID,
      request: {
        method: "POST",
        url: "/v1/sessions/borrow",
        body: JSON.stringify({ barcode: "HD-D-QUARANTINE-1", action: "borrow" }),
      },
    });

    const failureReason = "Device already on loan to another staff member";
    await updateQueueItemStatus(item.sequence, "quarantined", failureReason, {
      title: "Custody Conflict",
      detail: failureReason,
      status: 422,
    });

    // Also enqueue a successful replayed item so attendant can see outcome
    const doneItem = await enqueueQueueItem({
      kioskId: KIOSK_ID,
      request: {
        method: "POST",
        url: "/v1/sessions/return",
        body: JSON.stringify({ barcode: "HD-D-RETURNED-1", action: "return" }),
      },
    });
    await updateQueueItemStatus(doneItem.sequence, "done", undefined, {
      status: "returned",
      loanId: "loan-closed-456",
    });

    render(
      <LocaleProvider locale="en">
        <DiagnosticsModal isOpen={true} onClose={vi.fn()} />
      </LocaleProvider>
    );

    // Unlock diagnostics modal with PIN
    const pinInput = screen.getByLabelText(/Attendant PIN/i);
    const unlockBtn = screen.getByRole("button", { name: /Unlock/i });
    act(() => {
      fireEvent.change(pinInput, { target: { value: "1234" } });
    });
    await act(async () => {
      fireEvent.click(unlockBtn);
    });

    // Verify Offline Queue Telemetry section is present
    expect(await screen.findByText(/Offline Queue/i)).toBeInTheDocument();

    // Switch to Quarantine tab/list
    const quarantineTab = await screen.findByRole("tab", { name: /Quarantine/i });
    await act(async () => {
      fireEvent.click(quarantineTab);
    });

    // Quarantined item shows its reason
    expect(await screen.findByText(new RegExp(failureReason, "i"))).toBeInTheDocument();
    expect(screen.getByText(/HD-D-QUARANTINE-1/i)).toBeInTheDocument();

    // Initial dismiss click must NOT immediately delete the item (accidental dismissal prevention)
    const dismissButton = screen.getByRole("button", { name: /^Dismiss$/i });
    await act(async () => {
      fireEvent.click(dismissButton);
    });

    // Item must still exist in quarantine
    let quarantinedInDb = await getQueueItems(KIOSK_ID, "quarantined");
    expect(quarantinedInDb).toHaveLength(1);

    // Confirmation prompt/button must appear
    const confirmButton = screen.getByRole("button", { name: /Confirm Dismiss/i });
    expect(confirmButton).toBeInTheDocument();

    // Cancelling dismissal preserves the item
    const cancelButton = screen.getByRole("button", { name: /Cancel/i });
    await act(async () => {
      fireEvent.click(cancelButton);
    });

    quarantinedInDb = await getQueueItems(KIOSK_ID, "quarantined");
    expect(quarantinedInDb).toHaveLength(1);

    // Clicking Dismiss again and confirming deletes the item and records audit log
    const dismissBtn2 = screen.getByRole("button", { name: /^Dismiss$/i });
    await act(async () => {
      fireEvent.click(dismissBtn2);
    });

    const confirmBtn2 = screen.getByRole("button", { name: /Confirm Dismiss/i });
    await act(async () => {
      fireEvent.click(confirmBtn2);
    });

    // Now item is removed from quarantine
    quarantinedInDb = await getQueueItems(KIOSK_ID, "quarantined");
    expect(quarantinedInDb).toHaveLength(0);

    // Reason was recorded in audit store
    const auditLogs = await getDeletionAuditLogs(KIOSK_ID);
    expect(auditLogs).toHaveLength(1);
    expect(auditLogs[0].sequence).toBe(item.sequence);
    expect(auditLogs[0].reason).toContain("dismissed by attendant");

    // Attendant can also inspect replayed outcomes tab
    const replayedTab = screen.getByRole("tab", { name: /Replayed/i });
    await act(async () => {
      fireEvent.click(replayedTab);
    });
    expect(screen.getByText(/HD-D-RETURNED-1/i)).toBeInTheDocument();
    expect(screen.getByText(/loan-closed-456/i)).toBeInTheDocument();
  });

  it("an axe accessibility assertion at kiosk contrast levels", async () => {
    const { container } = render(
      <LocaleProvider locale="en">
        <OfflineIndicator count={5} />
      </LocaleProvider>
    );

    // Accessibility assertion: WCAG AAA/AA contrast, semantic roles, aria labels
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
