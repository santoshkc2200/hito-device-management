import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { App } from "./App";

vi.mock("@hdms/api-client", async () => {
  const actual = await vi.importActual<typeof import("@hdms/api-client")>("@hdms/api-client");
  return {
    ...actual,
    getHealthz: vi.fn().mockResolvedValue({ data: { status: "ok" }, error: undefined }),
  };
});

describe("App", () => {
  it("renders the API status once the health check resolves and has no a11y violations", async () => {
    const { container } = render(<App />);

    expect(await screen.findByText(/API status: ok/i)).toBeInTheDocument();
    expect(screen.getByText("HDMS Kiosk")).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
