import { act, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { client, type Admin } from "@hdms/api-client";
import {
  clearMaintenance,
  installMaintenanceInterceptor,
  reportMaintenance,
  resetMaintenanceForTesting,
} from "@hdms/ui";
import { currentAdminQueryKey } from "@/lib/auth";
import { router } from "@/router";

const admin: Admin = {
  id: "admin-1",
  email: "admin@hito.local",
  fullName: "System Admin",
  role: "admin",
  status: "active",
  locale: "en",
};

function renderAt(path: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  queryClient.setQueryData(currentAdminQueryKey, admin);
  const testRouter = createRouter({
    routeTree: router.routeTree,
    history: createMemoryHistory({ initialEntries: [path] }),
    context: { queryClient },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={testRouter} />
    </QueryClientProvider>,
  );
  return { queryClient, testRouter };
}

function maintenance503(): Response {
  return new Response(
    JSON.stringify({ type: "https://hdms.hito.local/errors/maintenance", title: "Under maintenance", status: 503 }),
    { status: 503, headers: { "Content-Type": "application/problem+json", "Retry-After": "15" } },
  );
}

describe("admin console under maintenance", () => {
  beforeEach(() => {
    resetMaintenanceForTesting(async () => false);
  });

  afterEach(() => {
    resetMaintenanceForTesting();
    vi.restoreAllMocks();
  });

  it("a 503 maintenance from any call replaces the page with the notice and a way to Backups", async () => {
    // Node's Request rejects the relative "/v1" base the browser accepts.
    client.setConfig({ baseUrl: "http://localhost/v1" });
    installMaintenanceInterceptor();
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => maintenance503());
    renderAt("/dashboard");

    expect(await screen.findByTestId("maintenance-notice")).toHaveTextContent("HDMS is under maintenance");
    expect(screen.getByRole("link", { name: "Open Backups" })).toHaveAttribute("href", "/backups");
  });

  it("leaves the Backups page usable during maintenance", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response("{}", { status: 500 }));
    const { testRouter } = renderAt("/backups");
    act(() => reportMaintenance());

    await waitFor(() => expect(testRouter.state.status).toBe("idle"));
    expect(screen.queryByTestId("maintenance-notice")).toBeNull();
  });

  it("refetches everything when maintenance ends", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response("{}", { status: 500 }));
    const { queryClient } = renderAt("/dashboard");
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    act(() => reportMaintenance());
    expect(await screen.findByTestId("maintenance-notice")).toBeInTheDocument();

    act(() => clearMaintenance());
    await waitFor(() => expect(screen.queryByTestId("maintenance-notice")).toBeNull());
    expect(invalidate).toHaveBeenCalled();
  });
});
