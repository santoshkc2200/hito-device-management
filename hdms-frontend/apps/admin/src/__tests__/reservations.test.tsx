import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { currentAdminQueryKey } from "@/lib/auth";
import { ja } from "@/i18n";
import { ReservationsPage, reservationsRoute } from "../routes/reservations";

// Mock TanStack Router
const mockNavigate = vi.fn();
let mockSearch: Record<string, any> = {};

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    useNavigate: () => mockNavigate,
    useRouter: () => ({ invalidate: vi.fn() }),
    Link: ({ to, params, children, className, onClick }: any) => (
      <a
        href={to}
        data-params={JSON.stringify(params)}
        className={className}
        onClick={onClick}
      >
        {children}
      </a>
    ),
  };
});

vi.spyOn(reservationsRoute, "useSearch").mockImplementation(() => mockSearch as any);

describe("ReservationsPage — Phase 6.4b", () => {
  const mockUsers: apiClient.User[] = [
    {
      id: "user-1",
      employeeNo: "EMP-101",
      fullName: "Dr. Alice Walker",
      status: "active",
      registeredAt: "2026-08-01T00:00:00Z",
      registeredBy: "admin:1",
      updatedAt: "2026-08-01T00:00:00Z",
    },
    {
      id: "user-2",
      employeeNo: "EMP-102",
      fullName: "Nurse Bob Smith",
      status: "active",
      registeredAt: "2026-08-01T00:00:00Z",
      registeredBy: "admin:1",
      updatedAt: "2026-08-01T00:00:00Z",
    },
  ];

  const mockDevices: apiClient.Device[] = [
    {
      id: "dev-1",
      assetTag: "LAPTOP-01",
      name: "Dell Latitude 5420",
      categoryId: "cat-1",
      status: "available",
      condition: "good",
      createdAt: "2026-08-01T00:00:00Z",
      updatedAt: "2026-08-01T00:00:00Z",
    },
    {
      id: "dev-2",
      assetTag: "TAB-01",
      name: "iPad Air 5th Gen",
      categoryId: "cat-2",
      status: "available",
      condition: "good",
      createdAt: "2026-08-01T00:00:00Z",
      updatedAt: "2026-08-01T00:00:00Z",
    },
  ];

  const mockReservations: apiClient.Reservation[] = [
    {
      id: "res-1",
      deviceId: "dev-1",
      userId: "user-1",
      deviceName: "Dell Latitude 5420",
      deviceAssetTag: "LAPTOP-01",
      userName: "Dr. Alice Walker",
      userEmployeeNo: "EMP-101",
      status: "active",
      startAt: "2026-09-20T10:00:00Z",
      endAt: "2026-09-20T14:00:00Z",
      createdBy: "admin:admin-1",
      createdSource: "admin",
      createdAt: "2026-09-18T08:00:00Z",
      updatedAt: "2026-09-18T08:00:00Z",
    },
    {
      id: "res-2",
      deviceId: "dev-2",
      userId: "user-2",
      deviceName: "iPad Air 5th Gen",
      deviceAssetTag: "TAB-01",
      userName: "Nurse Bob Smith",
      userEmployeeNo: "EMP-102",
      status: "cancelled",
      startAt: "2026-09-19T09:00:00Z",
      endAt: "2026-09-19T12:00:00Z",
      createdBy: "admin:admin-1",
      createdSource: "admin",
      cancelledAt: "2026-09-18T09:00:00Z",
      cancelledBy: "admin:admin-1",
      cancellationReason: "Schedule conflict",
      createdAt: "2026-09-17T08:00:00Z",
      updatedAt: "2026-09-18T09:00:00Z",
    },
  ];

  function createTestQueryClient() {
    return new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
  }

  function renderReservationsPage(role: "admin" | "technician" | "viewer" = "admin") {
    const queryClient = createTestQueryClient();
    queryClient.setQueryData(currentAdminQueryKey, {
      id: "admin-1",
      email: "admin@hospital.org",
      fullName: "Test Admin",
      role,
      status: "active",
    });

    return render(
      <QueryClientProvider client={queryClient}>
        <ReservationsPage />
      </QueryClientProvider>,
    );
  }

  beforeEach(() => {
    vi.clearAllMocks();
    mockSearch = {};

    window.HTMLElement.prototype.hasPointerCapture = vi.fn();
    window.HTMLElement.prototype.setPointerCapture = vi.fn();
    window.HTMLElement.prototype.releasePointerCapture = vi.fn();
    window.HTMLElement.prototype.scrollIntoView = vi.fn();

    vi.spyOn(apiClient, "listUsers").mockResolvedValue({
      data: { items: mockUsers, nextCursor: undefined },
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "listDevices").mockResolvedValue({
      data: { items: mockDevices, nextCursor: undefined },
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "listReservations").mockImplementation((async (opts: any) => {
      const statusFilter = opts?.query?.status;
      const items = statusFilter
        ? mockReservations.filter((r) => r.status === statusFilter)
        : mockReservations;
      return {
        data: { items, nextCursor: undefined },
        error: undefined,
      };
    }) as any);
  });

  it("renders reservations list and passes axe audit", async () => {
    const { container } = renderReservationsPage();

    await waitFor(() => {
      expect(screen.getAllByText("Dell Latitude 5420").length).toBeGreaterThanOrEqual(1);
    });

    expect(screen.getByText("Dr. Alice Walker")).toBeInTheDocument();
    expect(screen.getByText("iPad Air 5th Gen")).toBeInTheDocument();
    expect(screen.getByText("Nurse Bob Smith")).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  }, 15000);

  it("filters reservations by status", async () => {
    mockSearch = { status: "active" };
    renderReservationsPage();

    await waitFor(() => {
      expect(apiClient.listReservations).toHaveBeenCalledWith(
        expect.objectContaining({
          query: expect.objectContaining({
            status: "active",
          }),
        }),
      );
    });

    await waitFor(() => {
      expect(screen.getByText("Dell Latitude 5420")).toBeInTheDocument();
    });
    expect(screen.queryByText("iPad Air 5th Gen")).not.toBeInTheDocument();
  });

  it("cancelling prompts for a reason and calls the cancelReservation API", async () => {
    const cancelSpy = vi.spyOn(apiClient, "cancelReservation").mockResolvedValue({
      data: { ...mockReservations[0], status: "cancelled" },
      error: undefined,
    } as any);

    renderReservationsPage();

    await waitFor(() => {
      expect(screen.getByText("Dell Latitude 5420")).toBeInTheDocument();
    });

    // Open row actions dropdown
    const menuButtons = screen.getAllByRole("button", { name: ja.columns.openMenu });
    fireEvent.pointerDown(menuButtons[0]);

    // Click cancel reservation action
    const cancelMenuItem = await screen.findByRole("menuitem", { name: ja.reservations.cancelAction });
    fireEvent.click(cancelMenuItem);

    // Prompt for reason is visible
    const reasonInput = await screen.findByPlaceholderText(ja.reservations.cancelReasonPlaceholder);
    expect(reasonInput).toBeInTheDocument();

    // Fill reason
    fireEvent.change(reasonInput, { target: { value: "User requested postponement" } });

    // Confirm cancellation
    const confirmBtn = screen.getByRole("button", { name: ja.reservations.confirmCancel });
    fireEvent.click(confirmBtn);

    await waitFor(() => {
      expect(cancelSpy).toHaveBeenCalledWith({
        path: { id: "res-1" },
        body: { reason: "User requested postponement" },
      });
    });
  });

  it("surfaces conflict error as a clear message when creating an overlapping reservation", async () => {
    vi.spyOn(apiClient, "createReservation").mockResolvedValue({
      data: undefined,
      error: {
        status: 409,
        type: "https://hdms.hospital/errors/reservation-conflict",
        title: "Device already reserved for overlapping interval",
        detail: "This device is already reserved for an overlapping time interval.",
      },
    } as any);

    renderReservationsPage();

    await waitFor(() => {
      expect(screen.getByTestId("create-reservation-btn")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId("create-reservation-btn"));

    // Dialog opens
    await screen.findByRole("dialog");

    const selects = screen.getAllByRole("combobox");
    expect(selects.length).toBeGreaterThanOrEqual(2);

    // Fill inputs
    const startInput = screen.getByLabelText(ja.reservations.startAtLabel);
    const endInput = screen.getByLabelText(ja.reservations.endAtLabel);

    fireEvent.change(startInput, { target: { value: "2026-09-20T10:00" } });
    fireEvent.change(endInput, { target: { value: "2026-09-20T14:00" } });

    // Select device
    fireEvent.click(selects[0]);
    const devOption = await screen.findByText("Dell Latitude 5420 (LAPTOP-01)");
    fireEvent.click(devOption);

    // Select user
    fireEvent.click(selects[1]);
    const userOption = await screen.findByText("Dr. Alice Walker (EMP-101)");
    fireEvent.click(userOption);

    // Submit create
    const submitBtn = screen.getByRole("button", { name: ja.reservations.save });
    expect(submitBtn).not.toBeDisabled();
    fireEvent.click(submitBtn);

    // Assert that the conflict message is surfaced clearly
    await waitFor(() => {
      expect(
        screen.getByText("This device is already reserved for an overlapping time interval."),
      ).toBeInTheDocument();
    });
  });
});
