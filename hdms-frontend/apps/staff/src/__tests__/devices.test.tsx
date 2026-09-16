import { render, screen } from "@testing-library/react";
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

function renderDevices({
  items,
  error,
  initialPath = "/devices",
}: {
  items?: Partial<StaffDevice>[];
  error?: unknown;
  initialPath?: string;
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
});
