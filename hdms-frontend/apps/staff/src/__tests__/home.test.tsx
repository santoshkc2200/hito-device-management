import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { LocaleProvider } from "@hdms/i18n";
import type { StaffMe } from "@hdms/api-client";
import * as apiClient from "@hdms/api-client";
import { createStaffRouter } from "@/router";
import { currentStaffQueryKey } from "@/lib/auth";

const defaultMe: StaffMe = {
  userId: "u1",
  employeeNo: "E-100",
  fullName: "Test Staff",
  status: "active",
  profileComplete: true,
  mustChangePassword: false,
  hasPassword: true,
  signInMethods: ["password"],
};

function renderHome(
  credentialError: unknown,
  reservationsResult?: { data?: any; error?: any } | Promise<any>,
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  queryClient.setQueryData(currentStaffQueryKey, defaultMe);
  vi.spyOn(apiClient, "getStaffMe").mockResolvedValue({
    data: defaultMe,
    error: undefined,
  } as any);
  vi.spyOn(apiClient, "getStaffMeLoans").mockResolvedValue({
    data: { items: [] },
    error: undefined,
  } as any);
  vi.spyOn(apiClient, "getStaffMeCredential").mockResolvedValue({
    data: undefined,
    error: credentialError,
  } as any);
  if (reservationsResult instanceof Promise) {
    vi.spyOn(apiClient, "getStaffMeReservations").mockReturnValue(
      reservationsResult as any,
    );
  } else {
    vi.spyOn(apiClient, "getStaffMeReservations").mockResolvedValue(
      reservationsResult ?? ({ data: { items: [] }, error: undefined } as any),
    );
  }

  const testRouter = createStaffRouter(
    createMemoryHistory({ initialEntries: ["/"] }),
    queryClient,
  );

  return render(
    <LocaleProvider locale="en">
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={testRouter} />
      </QueryClientProvider>
    </LocaleProvider>,
  );
}

describe("staff home", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("tells a staff member without a card to ask for one, not that loading failed", async () => {
    renderHome({
      type: "https://hdms.hito.local/errors/no-credential",
      title: "No active credential",
      status: 404,
    });

    expect(await screen.findByText(/ask an administrator for a card/i)).toBeInTheDocument();
    expect(screen.queryByText(/failed to load/i)).not.toBeInTheDocument();
  });

  it("still reports other credential failures as a load error", async () => {
    renderHome({
      type: "https://hdms.hito.local/errors/internal",
      title: "Internal error",
      status: 500,
    });

    expect(await screen.findByText(/failed to load/i)).toBeInTheDocument();
  });

  it("renders loading state while reservations are being fetched", async () => {
    const neverResolves = new Promise(() => {});
    renderHome(null, neverResolves);

    expect((await screen.findAllByText(/loading/i)).length).toBeGreaterThan(0);
  });

  it("renders empty state when there are no reservations", async () => {
    renderHome(null, { data: { items: [] }, error: undefined });

    expect(await screen.findByText(/no active reservations/i)).toBeInTheDocument();
  });

  it("renders active reservations when present", async () => {
    renderHome(null, {
      data: {
        items: [
          {
            id: "res-1",
            deviceId: "dev-1",
            deviceAssetTag: "AT-100",
            deviceName: "Portable Ultrasound",
            startAt: "2026-09-24T10:00:00Z",
            endAt: "2026-09-24T12:00:00Z",
            status: "active",
          },
        ],
      },
      error: undefined,
    });

    expect(await screen.findByText("Portable Ultrasound")).toBeInTheDocument();
    expect(screen.queryByText(/no active reservations/i)).not.toBeInTheDocument();
  });
});
