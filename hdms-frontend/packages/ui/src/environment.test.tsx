import "@testing-library/jest-dom/vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getHealthz } from "@hdms/api-client";
import { EnvironmentBanner, EnvironmentProvider } from "./environment";

vi.mock("@hdms/api-client", () => ({ getHealthz: vi.fn() }));

const labels = { development: "DEV BANNER", staging: "STAGING BANNER" };

function healthzReturns(environment: string | undefined) {
  vi.mocked(getHealthz).mockResolvedValue({ data: { status: "ok", environment } } as never);
}

function renderBanner() {
  return render(
    <EnvironmentProvider>
      <EnvironmentBanner labels={labels} />
    </EnvironmentProvider>,
  );
}

describe("EnvironmentBanner", () => {
  beforeEach(() => {
    document.head.innerHTML = '<link rel="icon" href="/favicon.svg" />';
    document.title = "HDMS Admin";
  });

  afterEach(() => {
    vi.resetAllMocks();
  });

  it.each([
    ["staging", "STAGING BANNER", "[STG] HDMS Admin"],
    ["development", "DEV BANNER", "[DEV] HDMS Admin"],
    ["test", "DEV BANNER", "[TEST] HDMS Admin"],
  ])("marks a %s deployment", async (env, text, title) => {
    healthzReturns(env);
    renderBanner();

    const banner = await screen.findByTestId("environment-banner");
    expect(banner).toHaveTextContent(text);
    expect(banner).toHaveAttribute("data-environment", env);
    expect(document.title).toBe(title);
    expect(document.querySelector('link[rel="icon"]')?.getAttribute("href")).toMatch(/^data:image\/svg\+xml,/);
  });

  it.each([["production"], [undefined]])("stays unmarked when the API reports %s", async (env) => {
    healthzReturns(env);
    renderBanner();

    await waitFor(() => expect(getHealthz).toHaveBeenCalled());
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.queryByTestId("environment-banner")).not.toBeInTheDocument();
    expect(document.title).toBe("HDMS Admin");
    expect(document.querySelector('link[rel="icon"]')?.getAttribute("href")).toBe("/favicon.svg");
  });

  it("stays unmarked when the API is unreachable", async () => {
    vi.mocked(getHealthz).mockRejectedValue(new Error("offline"));
    renderBanner();

    await waitFor(() => expect(getHealthz).toHaveBeenCalled());
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.queryByTestId("environment-banner")).not.toBeInTheDocument();
    expect(document.title).toBe("HDMS Admin");
  });

  it("restores the tab title and favicon on unmount", async () => {
    healthzReturns("staging");
    const { unmount } = renderBanner();
    await screen.findByTestId("environment-banner");

    unmount();
    expect(document.title).toBe("HDMS Admin");
    expect(document.querySelector('link[rel="icon"]')?.getAttribute("href")).toBe("/favicon.svg");
  });
});
