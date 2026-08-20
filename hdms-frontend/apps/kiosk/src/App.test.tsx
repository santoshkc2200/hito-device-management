import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { axe } from "vitest-axe";
import { App } from "./App";

describe("App (Kiosk Entrypoint)", () => {
  it("renders the primary IdleScreen with prompt and scanner ready status, with zero a11y violations", async () => {
    const { container } = render(<App />);

    expect(
      await screen.findByText(/Scan your ID card or a device barcode/i)
    ).toBeInTheDocument();
    expect(screen.getByText("HDMS Kiosk")).toBeInTheDocument();
    expect(screen.getByText(/Scanner Ready/i)).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
