import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { App } from "./App";

vi.mock("@hdms/api-client", async () => {
  const actual = await vi.importActual<typeof import("@hdms/api-client")>("@hdms/api-client");
  return {
    ...actual,
    getHealthz: vi.fn().mockResolvedValue({ data: { status: "ok" }, error: undefined }),
  };
});

describe("App", () => {
  it("renders the API status once the health check resolves", async () => {
    render(<App />);

    expect(await screen.findByText(/API status: ok/i)).toBeInTheDocument();
    expect(screen.getByText("HDMS Admin")).toBeInTheDocument();
  });
});
