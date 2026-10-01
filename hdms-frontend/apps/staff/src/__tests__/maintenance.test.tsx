import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { LocaleProvider } from "@hdms/i18n";
import { clearMaintenance, reportMaintenance, resetMaintenanceForTesting } from "@hdms/ui";
import * as apiClient from "@hdms/api-client";
import { createStaffRouter } from "@/router";
import { MaintenanceOverlay } from "@/components/maintenance-notice";

const maintenanceProblem = {
  type: "https://hdms.hito.local/errors/maintenance",
  title: "Under maintenance",
  status: 503,
};

describe("staff app under maintenance", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetMaintenanceForTesting(async () => false);
  });

  afterEach(() => {
    resetMaintenanceForTesting();
    vi.restoreAllMocks();
  });

  it("shows the notice instead of sending a signed-in member to login, then reloads when it ends", async () => {
    const getMe = vi.spyOn(apiClient, "getStaffMe").mockImplementation(async () => {
      // What installMaintenanceInterceptor does for a real 503 maintenance.
      reportMaintenance();
      return { data: undefined, error: maintenanceProblem } as any;
    });
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const testRouter = createStaffRouter(createMemoryHistory({ initialEntries: ["/devices"] }), queryClient);

    render(
      <LocaleProvider locale="en">
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={testRouter} />
          <MaintenanceOverlay router={testRouter} queryClient={queryClient} />
        </QueryClientProvider>
      </LocaleProvider>,
    );

    expect(await screen.findByTestId("maintenance-notice")).toHaveTextContent("HDMS is under maintenance");
    expect(testRouter.state.location.pathname).toBe("/devices");

    const callsDuringMaintenance = getMe.mock.calls.length;
    getMe.mockResolvedValue({
      data: undefined,
      error: { type: "https://hdms.hito.local/errors/unauthorized", status: 401 },
    } as any);
    act(() => clearMaintenance());

    await waitFor(() => expect(screen.queryByTestId("maintenance-notice")).toBeNull());
    // The guard ran again on its own: the router was invalidated.
    await waitFor(() => expect(getMe.mock.calls.length).toBeGreaterThan(callsDuringMaintenance));
  });
});
