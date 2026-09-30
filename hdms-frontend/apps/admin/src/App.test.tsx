import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { getHealthz } from "@hdms/api-client";
import { ja } from "@/i18n/ja";
import { App } from "./App";

vi.mock("@hdms/api-client", async () => {
  const actual = await vi.importActual<typeof import("@hdms/api-client")>("@hdms/api-client");
  return {
    ...actual,
    getHealthz: vi.fn().mockResolvedValue({ data: { status: "ok", environment: "production" } }),
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

    expect(
      await screen.findByRole("heading", { name: ja.login.systemTitle })
    ).toBeInTheDocument();
    expect(screen.getByLabelText(ja.login.emailLabel)).toBeInTheDocument();
  });

  it("marks a staging deployment from the login screen onwards", async () => {
    vi.mocked(getHealthz).mockResolvedValueOnce({ data: { status: "ok", environment: "staging" } } as never);
    render(<App />);

    expect(await screen.findByTestId("environment-banner")).toHaveTextContent(ja.environment.staging);
    expect(await screen.findByRole("heading", { name: ja.login.systemTitle })).toBeInTheDocument();
  });

  it("shows no banner on production", async () => {
    render(<App />);

    expect(await screen.findByRole("heading", { name: ja.login.systemTitle })).toBeInTheDocument();
    expect(screen.queryByTestId("environment-banner")).not.toBeInTheDocument();
  });
});
