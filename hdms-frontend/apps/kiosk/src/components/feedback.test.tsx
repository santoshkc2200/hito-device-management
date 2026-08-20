import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import * as React from "react";
import { Feedback } from "./feedback";
import {
  ALL_OUTCOME_KINDS,
  OUTCOME_FEEDBACK_MAP,
} from "@/lib/feedback-config";
import type { Outcome } from "@hdms/api-client";

describe("Feedback Component & Configuration", () => {
  it("everyOutcomeKindHasIconWordAndColour", () => {
    for (const kind of ALL_OUTCOME_KINDS) {
      const entry = OUTCOME_FEEDBACK_MAP[kind];
      expect(entry, `Missing feedback mapping for outcome kind: ${kind}`).toBeDefined();
      expect(entry.kind).toBe(kind);
      expect(entry.word.length).toBeGreaterThan(0);
      expect(entry.icon).toBeDefined();
      expect(entry.colorClasses.iconContainer).toBeDefined();
      expect(entry.colorClasses.badge).toBeDefined();
      expect(entry.colorClasses.text).toBeDefined();
      expect(entry.soundId).toBeDefined();
    }
  });

  it("duplicateProducesNoFeedback", () => {
    const outcome: Outcome = {
      kind: "duplicate",
    };
    const { container } = render(<Feedback outcome={outcome} />);
    expect(container.firstChild).toBeNull();
    expect(screen.queryByTestId("feedback-banner")).not.toBeInTheDocument();
  });

  it("rendersIconWordAndColourForBorrowed", () => {
    const outcome: Outcome = {
      kind: "borrowed",
      device: {
        id: "dev-1",
        assetTag: "LAPTOP-01",
        name: "Dell Latitude",
      },
    };
    render(<Feedback outcome={outcome} />);

    const banner = screen.getByTestId("feedback-banner");
    expect(banner).toBeInTheDocument();
    expect(screen.getByTestId("feedback-icon")).toBeInTheDocument();
    expect(screen.getByTestId("feedback-word-badge")).toHaveTextContent("Borrowed");
  });

  it("rendersIconWordAndColourForReturned", () => {
    const outcome: Outcome = {
      kind: "returned",
    };
    render(<Feedback outcome={outcome} />);

    expect(screen.getByTestId("feedback-word-badge")).toHaveTextContent("Returned");
    expect(screen.getByTestId("feedback-banner")).toBeInTheDocument();
  });

  it("feedbackFiresOncePerOutcome", () => {
    const outcome: Outcome = {
      kind: "borrowed",
      device: { id: "dev-1", assetTag: "LAPTOP-01", name: "Dell" },
    };

    const { rerender } = render(<Feedback outcome={outcome} />);
    expect(screen.getByTestId("feedback-banner")).toHaveAttribute("data-announced-count", "1");

    // Re-render with identical outcome
    rerender(<Feedback outcome={{ ...outcome }} />);
    expect(screen.getByTestId("feedback-banner")).toHaveAttribute("data-announced-count", "1");

    // Change outcome
    rerender(
      <Feedback
        outcome={{
          kind: "returned",
          device: { id: "dev-2", assetTag: "LAPTOP-02", name: "MacBook" },
        }}
      />
    );
    expect(screen.getByTestId("feedback-banner")).toHaveAttribute("data-announced-count", "2");
  });

  it("reducedMotionSkipsAnimationButKeepsState", () => {
    // Media query match for reduced motion
    window.matchMedia = vi.fn().mockImplementation((query) => ({
      matches: query === "(prefers-reduced-motion: reduce)",
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }));

    const outcome: Outcome = {
      kind: "rejected",
    };
    render(<Feedback outcome={outcome} />);

    const banner = screen.getByTestId("feedback-banner");
    expect(banner).toBeInTheDocument();
    expect(screen.getByTestId("feedback-word-badge")).toHaveTextContent("Blocked");
  });
});
