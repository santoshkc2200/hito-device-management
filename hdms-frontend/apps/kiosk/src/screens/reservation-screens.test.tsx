import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { SuccessScreen } from "./success-screen";
import { OfflineScreen } from "./offline-screen";

/**
 * Phase 6.4d. Two things this feature promised the counter:
 *  - a reserver collecting their device gets a confirmation that reads
 *    like their booking worked, not like an ordinary borrow;
 *  - while offline the kiosk does not enforce reservations and SAYS SO,
 *    rather than silently letting a reserved device walk out.
 */

describe("a reserver collecting their reservation", () => {
  it("is confirmed distinctly from an ordinary borrow", () => {
    const { container: collected } = render(
      <SuccessScreen kind="reservation_collected" onDone={vi.fn()} />
    );
    const collectedBadge = collected
      .querySelector('[data-testid="success-kind-badge"]')
      ?.textContent?.trim();

    const { container: borrowed } = render(<SuccessScreen kind="borrowed" onDone={vi.fn()} />);
    const borrowedBadge = borrowed
      .querySelector('[data-testid="success-kind-badge"]')
      ?.textContent?.trim();

    expect(collectedBadge).toBeTruthy();
    expect(collectedBadge).not.toBe(borrowedBadge);
  });

  it("still reads as a success, not as a refusal", () => {
    const { container } = render(
      <SuccessScreen kind="reservation_collected" onDone={vi.fn()} />
    );
    expect(container.querySelector('[data-testid="success-screen"]')).not.toBeNull();
  });

  it("renders no raw key or placeholder at the counter", () => {
    const { container } = render(
      <SuccessScreen kind="reservation_collected" onDone={vi.fn()} />
    );
    const text = container.textContent ?? "";
    expect(text).not.toContain("success.collected");
    expect(text).not.toContain("undefined");
    expect(text).not.toContain("{");
  });
});

describe("offlineReservationBehaviourMatchesTheDocumentedPolicy", () => {
  /**
   * The documented policy (docs/phases/phase-6/6.4-reservations.md, and
   * 5.1's rule that the kiosk only queues what the server can still
   * adjudicate later): reservations are NOT enforced offline, and the
   * screen says so. A conflict cannot be adjudicated offline without
   * telling somebody at the counter something false.
   */
  it("tells the borrower that reservations are not checked while offline", () => {
    const { container } = render(<OfflineScreen />);
    const notice = container.querySelector('[data-testid="offline-reservations-notice"]');

    expect(notice).not.toBeNull();
    expect(notice?.textContent?.trim()).not.toBe("");
  });

  it("points at the paper register rather than simply refusing", () => {
    const { container } = render(<OfflineScreen />);
    const notice = container.querySelector('[data-testid="offline-reservations-notice"]');

    // The overflow lane is named on the same screen — a kiosk that only
    // says "no" sends someone away holding a device they need.
    expect(container.querySelector('[data-testid="offline-fallback-instruction"]')).not.toBeNull();
    expect(notice?.textContent ?? "").not.toContain("{");
  });
});
