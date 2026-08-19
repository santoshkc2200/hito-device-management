import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { App } from "./App";

vi.mock("@hdms/api-client", async () => {
  const actual = await vi.importActual<typeof import("@hdms/api-client")>("@hdms/api-client");
  return {
    ...actual,
    getCurrentAdmin: vi.fn().mockResolvedValue({
      data: undefined,
      error: { type: "unauthorized", title: "Unauthorized", status: 401 },
      response: new Response(null, { status: 401 }),
    }),
  };
});

describe("App", () => {
  it("redirects an unauthenticated visitor to the login screen", async () => {
    render(<App />);

    expect(await screen.findByRole("heading", { name: /device management/i })).toBeInTheDocument();
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
  });
});
