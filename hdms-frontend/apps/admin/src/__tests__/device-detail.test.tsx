import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { currentAdminQueryKey } from "@/lib/auth";
import { DeviceDetailPage, deviceDetailRoute } from "../routes/devices.$deviceId";

// Mock TanStack Router
const mockNavigate = vi.fn();
let mockParams = { deviceId: "dev-100" };

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    useNavigate: () => mockNavigate,
    useRouter: () => ({ invalidate: vi.fn() }),
    Link: ({ to, params, children, className }: any) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>
        {children}
      </a>
    ),
  };
});

vi.spyOn(deviceDetailRoute, "useParams").mockImplementation(() => mockParams as any);

describe("DeviceDetailPage — Phase 4.3a", () => {
  const mockDevice: apiClient.Device = {
    id: "dev-100",
    assetTag: "MONITOR-01",
    name: "Philips Vital Signs Monitor",
    categoryId: "cat-monitors",
    status: "on_loan",
    condition: "good",
    manufacturer: "Philips",
    model: "IntelliVue",
    serialNo: "SN-998877",
    homeLocation: "ICU Bay 3",
    notes: "Calibrated on 2026-07-15",
    acquiredOn: "2025-06-01",
    createdAt: "2025-06-01T10:00:00Z",
    updatedAt: "2026-08-01T10:00:00Z",
  };

  const mockCategories: apiClient.Category[] = [
    { id: "cat-monitors", name: "Monitors", requiresApproval: false, createdAt: "2025-01-01T00:00:00Z" },
  ];

  const mockLoans: apiClient.Loan[] = [
    {
      id: "loan-active-1",
      deviceId: "dev-100",
      userId: "user-dr-smith",
      status: "open",
      origin: "kiosk",
      borrowedAt: "2026-08-20T08:00:00Z",
      dueAt: "2026-08-21T18:00:00Z",
      borrowActor: "kiosk:kiosk-1",
      borrowSource: "scanner",
      disputed: false,
    },
    {
      id: "loan-hist-1",
      deviceId: "dev-100",
      userId: "user-nurse-jones",
      status: "returned",
      origin: "kiosk",
      borrowedAt: "2026-08-10T08:00:00Z",
      returnedAt: "2026-08-10T16:00:00Z",
      dueAt: "2026-08-10T18:00:00Z",
      borrowActor: "kiosk:kiosk-1",
      returnActor: "kiosk:kiosk-1",
      borrowSource: "scanner",
      returnSource: "scanner",
      disputed: false,
    },
  ];

  const mockBorrower: apiClient.User = {
    id: "user-dr-smith",
    employeeNo: "DOC-501",
    fullName: "Dr. Gregory Smith",
    departmentId: "dept-cardio",
    status: "active",
    registeredAt: "2025-01-01T00:00:00Z",
    registeredBy: "admin:1",
    updatedAt: "2025-01-01T00:00:00Z",
  };

  function createTestQueryClient() {
    return new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
  }

  function renderDeviceDetailPage(role: "admin" | "technician" | "viewer" = "technician") {
    const queryClient = createTestQueryClient();
    queryClient.setQueryData(currentAdminQueryKey, {
      id: "admin-1",
      email: "tech@hospital.org",
      fullName: "Test Technician",
      role,
      status: "active",
    });

    return render(
      <QueryClientProvider client={queryClient}>
        <DeviceDetailPage />
      </QueryClientProvider>,
    );
  }

  beforeEach(() => {
    vi.clearAllMocks();
    mockParams = { deviceId: "dev-100" };

    window.HTMLElement.prototype.hasPointerCapture = vi.fn();
    window.HTMLElement.prototype.setPointerCapture = vi.fn();
    window.HTMLElement.prototype.releasePointerCapture = vi.fn();
    window.HTMLElement.prototype.scrollIntoView = vi.fn();

    vi.spyOn(apiClient, "getDevice").mockResolvedValue({
      data: mockDevice,
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "listCategories").mockResolvedValue({
      data: { items: mockCategories },
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "listDeviceLoans").mockResolvedValue({
      data: { items: mockLoans, nextCursor: undefined },
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "getUser").mockResolvedValue({
      data: mockBorrower,
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "listCredentialsBySubject").mockResolvedValue({
      data: { items: [] },
      error: undefined,
    } as any);
  });

  it("renders device attributes, active borrower, and passes axe audit", async () => {
    const { container } = renderDeviceDetailPage();

    await waitFor(() => {
      expect(screen.getAllByText("MONITOR-01").length).toBeGreaterThanOrEqual(1);
    });

    expect(screen.getAllByText("Philips Vital Signs Monitor").length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("IntelliVue")).toBeInTheDocument();
    expect(screen.getByText("SN-998877")).toBeInTheDocument();
    expect(screen.getByText("ICU Bay 3")).toBeInTheDocument();
    expect(screen.getByText(/Calibrated on 2026-07-15/)).toBeInTheDocument();

    // Verify Active Borrower is rendered and linked
    await waitFor(() => {
      expect(screen.getByText(/Dr. Gregory Smith/)).toBeInTheDocument();
    });
    expect(screen.getByText(/\(DOC-501\)/)).toBeInTheDocument();

    // Verify Loan History table
    expect(screen.getByText("Loan History")).toBeInTheDocument();
    expect(screen.getByText("user-nurse-jones")).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("renders empty holder state when device is available", async () => {
    vi.spyOn(apiClient, "getDevice").mockResolvedValueOnce({
      data: { ...mockDevice, status: "available" },
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "listDeviceLoans").mockResolvedValueOnce({
      data: { items: [], nextCursor: undefined },
      error: undefined,
    } as any);

    renderDeviceDetailPage();

    await waitFor(() => {
      expect(screen.getByText(/device is currently available \/ not on loan/i)).toBeInTheDocument();
    });
    expect(screen.getByText(/no loan history for this device/i)).toBeInTheDocument();
  });

  it("triggers force return mutation with mandatory reason", async () => {
    const user = userEvent.setup();
    const forceReturnSpy = vi.spyOn(apiClient, "forceReturnLoan").mockResolvedValueOnce({
      data: { ...mockLoans[0], status: "returned" },
      error: undefined,
    } as any);

    renderDeviceDetailPage();

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /force return/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole("button", { name: /force return/i }));

    await waitFor(() => {
      expect(screen.getByText(/force return device/i)).toBeInTheDocument();
    });

    const reasonInput = screen.getByPlaceholderText(/reason \(required\)/i);
    await user.type(reasonInput, "Found left on desk");

    const submitBtn = screen.getByRole("button", { name: /^force return$/i });
    await user.click(submitBtn);

    await waitFor(() => {
      expect(forceReturnSpy).toHaveBeenCalledWith({
        path: { id: "loan-active-1" },
        body: { reason: "Found left on desk" },
      });
    });
  });
});
