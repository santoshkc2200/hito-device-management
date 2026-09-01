import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { translate } from "@hdms/i18n";
import { catalogues } from "@/i18n";
import { ja } from "@/i18n/ja";
import { EmptyState, ErrorState, LoadingState } from "../index";

describe("State Primitives", () => {
  describe("LoadingState", () => {
    it("renders loading status with accessible text", () => {
      render(<LoadingState message="Fetching records..." />);
      expect(screen.getByRole("status")).toBeInTheDocument();
      expect(screen.getByText("Fetching records...")).toBeInTheDocument();
    });

    it("renders default accessible loading text from catalogue", () => {
      render(<LoadingState />);
      expect(screen.getByRole("status")).toBeInTheDocument();
      expect(screen.getByText(ja.states.loading)).toBeInTheDocument();
    });
  });

  describe("EmptyState", () => {
    it("renders required title and explanation", () => {
      render(
        <EmptyState
          title="No devices found"
          explanation="Try adjusting your filter criteria to see more items."
        />
      );
      expect(screen.getByText("No devices found")).toBeInTheDocument();
      expect(
        screen.getByText("Try adjusting your filter criteria to see more items.")
      ).toBeInTheDocument();
    });

    it("renders action button and handles clicks", () => {
      const handleClick = vi.fn();
      render(
        <EmptyState
          title="No records"
          explanation="Get started by creating one."
          action={{
            label: "Create record",
            onClick: handleClick,
          }}
        />
      );

      const btn = screen.getByRole("button", { name: "Create record" });
      expect(btn).toBeInTheDocument();
      fireEvent.click(btn);
      expect(handleClick).toHaveBeenCalledTimes(1);
    });
  });

  describe("ErrorState", () => {
    it("renders problem+json with detail and requestId", () => {
      const problem = {
        type: "about:blank",
        title: "Internal Error",
        status: 500,
        detail: "Database connection failed",
        requestId: "req_12345abc",
      };

      render(<ErrorState error={problem} />);
      expect(screen.getByRole("alert")).toBeInTheDocument();
      expect(screen.getByText("Internal Error")).toBeInTheDocument();
      expect(screen.getByText("Database connection failed")).toBeInTheDocument();
      expect(screen.getByText("req_12345abc")).toBeInTheDocument();
    });

    it("surfaces stub task number when 501 problem contains task extension", () => {
      const stubProblem = {
        type: "about:blank",
        title: "Not Implemented",
        status: 501,
        detail: "Operation is not implemented yet — see task 4.9a.",
        task: "4.9a",
      };

      render(<ErrorState error={stubProblem} />);
      const expectedTitle = translate(catalogues, "ja", "states.notBuiltYet", { task: "4.9a" });
      expect(screen.getByText(expectedTitle)).toBeInTheDocument();
      expect(
        screen.getByText("Operation is not implemented yet — see task 4.9a.")
      ).toBeInTheDocument();
    });

    it("handles retry action", () => {
      const handleRetry = vi.fn();
      render(<ErrorState title="Failed" onRetry={handleRetry} />);
      const retryBtn = screen.getByRole("button", { name: new RegExp(ja.states.tryAgain, "i") });
      fireEvent.click(retryBtn);
      expect(handleRetry).toHaveBeenCalledTimes(1);
    });
  });
});
