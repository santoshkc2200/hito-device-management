import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
    error: credentialError ?? { type: "https://hdms.hito.local/errors/no-credential" },
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

  return { ...render(
    <LocaleProvider locale="en">
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={testRouter} />
      </QueryClientProvider>
    </LocaleProvider>,
  ), queryClient };
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
    expect(screen.getByText(/19:00/)).toBeInTheDocument();
    expect(screen.getByText(/21:00/)).toBeInTheDocument();
    expect(screen.queryByText(/no active reservations/i)).not.toBeInTheDocument();
  });

  it("loads more reservations so older bookings remain cancellable", async () => {
    const get = vi.spyOn(apiClient, "getStaffMeReservations");
    renderHome(null, {
      data: { items: [{ id: "res-1", deviceName: "Later booking", startAt: "2030-01-02T10:00:00Z", endAt: "2030-01-02T12:00:00Z" }], nextCursor: "page-two" },
    });
    expect(await screen.findByText("Later booking")).toBeInTheDocument();
    get.mockResolvedValueOnce({ data: { items: [{ id: "res-2", deviceName: "Earlier booking", startAt: "2030-01-01T10:00:00Z", endAt: "2030-01-01T12:00:00Z" }] }, error: undefined } as any);
    await userEvent.click(screen.getByRole("button", { name: "Load more reservations" }));
    expect(await screen.findByText("Earlier booking")).toBeInTheDocument();
    expect(get).toHaveBeenCalledWith({ query: { cursor: "page-two" } });
  });

  it("keeps loaded reservations visible when loading the next page fails", async () => {
    const get = vi.spyOn(apiClient, "getStaffMeReservations");
    renderHome(null, {
      data: { items: [{ id: "res-1", deviceName: "Visible booking", startAt: "2030-01-02T10:00:00Z", endAt: "2030-01-02T12:00:00Z" }], nextCursor: "page-two" },
    });
    expect(await screen.findByText("Visible booking")).toBeInTheDocument();
    get.mockResolvedValueOnce({ data: undefined, error: { type: "https://hdms.hospital/errors/internal" } } as any);
    await userEvent.click(screen.getByRole("button", { name: "Load more reservations" }));
    expect(await screen.findByText("Could not load more reservations. Try again.")).toBeInTheDocument();
    expect(screen.getByText("Visible booking")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cancel reservation" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Load more reservations" })).toBeInTheDocument();
  });

  it("confirms cancellation of an active reservation and refreshes the list", async () => {
    const cancel = vi.spyOn(apiClient, "cancelStaffReservation").mockResolvedValue({
      data: { id: "res-1", deviceId: "dev-1", deviceAssetTag: "AT-100", deviceName: "Portable Ultrasound", startAt: "2030-01-01T10:00:00Z", endAt: "2030-01-01T12:00:00Z", status: "cancelled" },
      error: undefined,
    } as any);
    const { queryClient } = renderHome(null, {
      data: { items: [{ id: "res-1", deviceId: "dev-1", deviceAssetTag: "AT-100", deviceName: "Portable Ultrasound", startAt: "2030-01-01T10:00:00Z", endAt: "2030-01-01T12:00:00Z", status: "active" }] },
      error: undefined,
    });
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    await userEvent.click(await screen.findByRole("button", { name: "Cancel reservation" }));
    expect(cancel).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "Confirm cancellation" }));

    await waitFor(() => expect(cancel).toHaveBeenCalledWith({ path: { id: "res-1" } }));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["staff", "me", "reservations"] });
  });
});
