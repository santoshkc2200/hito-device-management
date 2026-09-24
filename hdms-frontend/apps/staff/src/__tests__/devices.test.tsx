import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { LocaleProvider } from "@hdms/i18n";
import type { StaffDevice, StaffMe } from "@hdms/api-client";
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

function upcomingHospitalWindow() {
  const start = new Date(Date.now() + 24 * 60 * 60_000);
  const end = new Date(start.getTime() + 60 * 60_000);
  const local = (date: Date) => new Date(date.getTime() + 9 * 60 * 60_000).toISOString().slice(0, 16);
  const startInput = local(start);
  const endInput = local(end);
  return { startInput, endInput,
    startISO: new Date(`${startInput}:00+09:00`).toISOString(),
    endISO: new Date(`${endInput}:00+09:00`).toISOString() };
}

function renderDevices({
  items,
  error,
  initialPath = "/devices",
  bookingPolicy = { advanceDays: 90, maxDurationDays: 30, returnBufferMinutes: 60 },
}: {
  items?: Partial<StaffDevice>[];
  error?: unknown;
  initialPath?: string;
  bookingPolicy?: { advanceDays: number; maxDurationDays: number; returnBufferMinutes: number };
} = {}) {
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
  vi.spyOn(apiClient, "getStaffBookingPolicy").mockResolvedValue({
    data: bookingPolicy,
    error: undefined,
  } as any);

  if (error) {
    vi.spyOn(apiClient, "getStaffDevices").mockRejectedValue(error);
  } else if (items) {
    vi.spyOn(apiClient, "getStaffDevices").mockResolvedValue({
      data: { items: items as StaffDevice[] },
      error: undefined,
    } as any);
  }

  const history = createMemoryHistory({
    initialEntries: [initialPath],
  });

  const testRouter = createStaffRouter(history, queryClient);

  return {
    ...render(
      <LocaleProvider locale="en">
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={testRouter} />
        </QueryClientProvider>
      </LocaleProvider>,
    ),
    router: testRouter,
    queryClient,
  };
}

describe("staff devices", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("shows availability and the expected return time, and never a borrower", async () => {
    renderDevices({
      items: [
        {
          id: "d1",
          assetTag: "AT-1",
          name: "Projector",
          availability: "in_use",
          // 06:30 UTC is 15:30 in Asia/Tokyo, the timezone @hdms/i18n formats in
          expectedBackAt: "2026-09-15T06:30:00Z",
        },
        {
          id: "d2",
          assetTag: "AT-2",
          name: "Laptop",
          availability: "available",
        },
      ],
    });

    expect(await screen.findByText("Projector")).toBeInTheDocument();
    expect(screen.getByText(/in use/i)).toBeInTheDocument();
    expect(screen.getByText(/15:30/)).toBeInTheDocument();
    expect(screen.getByText(/available/i)).toBeInTheDocument();
  });

  it("says the list needs a connection rather than showing a stale one", async () => {
    renderDevices({ error: new TypeError("Failed to fetch") });
    expect(await screen.findByText(/you need a connection to see devices/i)).toBeInTheDocument();
  });

  it("filters devices by search query matching name or asset tag", async () => {
    renderDevices({
      items: [
        { id: "d1", assetTag: "AT-001", name: "Ultrasound Probe", availability: "available" },
        { id: "d2", assetTag: "AT-002", name: "Infusion Pump", availability: "in_use" },
      ],
    });

    expect(await screen.findByText("Ultrasound Probe")).toBeInTheDocument();
    expect(screen.getByText("Infusion Pump")).toBeInTheDocument();

    const searchInput = screen.getByPlaceholderText(/search by name or asset tag/i);
    await userEvent.type(searchInput, "probe");

    expect(screen.getByText("Ultrasound Probe")).toBeInTheDocument();
    expect(screen.queryByText("Infusion Pump")).not.toBeInTheDocument();

    await userEvent.clear(searchInput);
    await userEvent.type(searchInput, "AT-002");

    expect(screen.queryByText("Ultrasound Probe")).not.toBeInTheDocument();
    expect(screen.getByText("Infusion Pump")).toBeInTheDocument();
  });

  it("renders device detail screen with model, category, and availability", async () => {
    const device: StaffDevice = {
      id: "d1",
      assetTag: "AT-1",
      name: "Projector",
      model: "Epson EB-X49",
      categoryName: "AV Equipment",
      availability: "available",
    };
    vi.spyOn(apiClient, "getStaffDevice").mockResolvedValue({
      data: device,
      error: undefined,
    } as any);

    renderDevices({ initialPath: "/devices/d1" });

    expect(await screen.findByRole("heading", { name: "Projector" })).toBeInTheDocument();
    expect(screen.getByText("AT-1")).toBeInTheDocument();
    expect(screen.getByText("Epson EB-X49")).toBeInTheDocument();
    expect(screen.getByText("AV Equipment")).toBeInTheDocument();
    expect(screen.getByText(/available/i)).toBeInTheDocument();
  });

  it("says device detail needs a connection when fetch fails", async () => {
    vi.spyOn(apiClient, "getStaffDevice").mockRejectedValue(new TypeError("Failed to fetch"));

    renderDevices({ initialPath: "/devices/d1" });

    expect(await screen.findByText(/you need a connection to see devices/i)).toBeInTheDocument();
  });

  it("books a device for the signed-in staff member and refreshes their reservations", async () => {
	const window = upcomingHospitalWindow();
    vi.spyOn(apiClient, "getStaffDevice").mockResolvedValue({
      data: { id: "d1", assetTag: "AT-1", name: "Projector", availability: "available" },
      error: undefined,
    } as any);
    const create = vi.spyOn(apiClient, "createStaffReservation").mockResolvedValue({
      data: { id: "r1", deviceId: "d1", deviceAssetTag: "AT-1", deviceName: "Projector", status: "active", startAt: "2030-01-01T10:00:00Z", endAt: "2030-01-01T11:00:00Z" },
      error: undefined,
    } as any);
    const { queryClient } = renderDevices({ initialPath: "/devices/d1" });
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    fireEvent.change(await screen.findByLabelText("Start time"), { target: { value: window.startInput } });
    fireEvent.change(screen.getByLabelText("End time"), { target: { value: window.endInput } });
    await userEvent.click(screen.getByRole("button", { name: "Reserve device" }));

    await waitFor(() => expect(create).toHaveBeenCalledWith({ body: {
      deviceId: "d1",
      startAt: window.startISO,
      endAt: window.endISO,
    } }));
    expect(await screen.findByRole("status")).toHaveTextContent("Reservation created");
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["staff", "me", "reservations"] });
  });

  it("shows the current admin booking policy on the device form", async () => {
    vi.spyOn(apiClient, "getStaffDevice").mockResolvedValue({
      data: { id: "d1", assetTag: "AT-1", name: "Projector", availability: "in_use", expectedBackAt: new Date(Date.now() + 2 * 60 * 60_000).toISOString() },
      error: undefined,
    } as any);
    renderDevices({ initialPath: "/devices/d1", bookingPolicy: { advanceDays: 7, maxDurationDays: 2, returnBufferMinutes: 15 } });
    expect(await screen.findByText("Start within 7 days; reserve for up to 2 days.")).toBeInTheDocument();
    expect(screen.getByText(/15 minutes after its expected return/)).toBeInTheDocument();
  });

  it("keeps the form open and explains a booking conflict", async () => {
	const window = upcomingHospitalWindow();
    vi.spyOn(apiClient, "getStaffDevice").mockResolvedValue({
      data: { id: "d1", assetTag: "AT-1", name: "Projector", availability: "available" },
      error: undefined,
    } as any);
    vi.spyOn(apiClient, "createStaffReservation").mockResolvedValue({
      error: { type: "https://hdms.hospital/errors/reservation-conflict" },
      data: undefined,
    } as any);
    renderDevices({ initialPath: "/devices/d1" });

    fireEvent.change(await screen.findByLabelText("Start time"), { target: { value: window.startInput } });
    fireEvent.change(screen.getByLabelText("End time"), { target: { value: window.endInput } });
    await userEvent.click(screen.getByRole("button", { name: "Reserve device" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("already reserved");
    expect(screen.getByLabelText("Start time")).toHaveValue(window.startInput);
  });

  it("explains the gap required between back-to-back reservations", async () => {
    const window = upcomingHospitalWindow();
    vi.spyOn(apiClient, "getStaffDevice").mockResolvedValue({
      data: { id: "d1", assetTag: "AT-1", name: "Projector", availability: "available" },
      error: undefined,
    } as any);
    vi.spyOn(apiClient, "createStaffReservation").mockResolvedValue({
      error: { type: "https://hdms.hospital/errors/reservation-too-close" },
      data: undefined,
    } as any);
    renderDevices({ initialPath: "/devices/d1", bookingPolicy: { advanceDays: 90, maxDurationDays: 30, returnBufferMinutes: 45 } });

    expect(await screen.findByText("Bookings need 45 minutes clear of other reservations for this device.")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Start time"), { target: { value: window.startInput } });
    fireEvent.change(screen.getByLabelText("End time"), { target: { value: window.endInput } });
    await userEvent.click(screen.getByRole("button", { name: "Reserve device" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Leave at least 45 minutes between this and other reservations");
  });

  it("does not offer booking for an unavailable device", async () => {
    vi.spyOn(apiClient, "getStaffDevice").mockResolvedValue({
      data: { id: "d1", assetTag: "AT-1", name: "Projector", availability: "unavailable" },
      error: undefined,
    } as any);
    renderDevices({ initialPath: "/devices/d1" });

    expect(await screen.findByRole("heading", { name: "Projector" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reserve device" })).not.toBeInTheDocument();
  });
});
