import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { currentAdminQueryKey } from "@/lib/auth";
import { DisputedLoansPage } from "../routes/disputed";

// Mock TanStack Router
const mockNavigate = vi.fn();

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

describe("DisputedLoansPage — Phase 4.8a", () => {
  const mockUsers: apiClient.User[] = [
    {
      id: "user-1",
      employeeNo: "EMP-101",
      fullName: "Dr. Disputed User",
      status: "active",
      registeredAt: "2026-08-01T00:00:00Z",
      registeredBy: "admin:1",
      updatedAt: "2026-08-01T00:00:00Z",
    },
  ];

  const mockDevices: apiClient.Device[] = [
    {
      id: "dev-1",
      assetTag: "DEV-DISP-01",
      name: "Disputed Tablet",
      categoryId: "cat-1",
      status: "available",
      condition: "good",
      createdAt: "2026-08-01T00:00:00Z",
      updatedAt: "2026-08-01T00:00:00Z",
    },
  ];

  const mockDisputedLoans: apiClient.Loan[] = [
    {
      id: "loan-disp-1",
      deviceId: "dev-1",
      userId: "user-1",
      status: "open",
      origin: "paper",
      borrowedAt: "2026-08-10T09:00:00Z",
      borrowActor: "admin:admin-1",
      borrowSource: "paper",
      notes: "Forced past overlapping custody conflict with kiosk transaction",
      disputed: true,
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

  function renderDisputedLoansPage() {
    const queryClient = createTestQueryClient();
    queryClient.setQueryData(currentAdminQueryKey, {
      id: "admin-1",
      email: "admin@hospital.org",
      fullName: "Test Admin",
      role: "admin",
      status: "active",
    });

    return render(
      <QueryClientProvider client={queryClient}>
        <DisputedLoansPage />
      </QueryClientProvider>,
    );
  }

  beforeEach(() => {
    vi.clearAllMocks();

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

    vi.spyOn(apiClient, "listDisputedLoans").mockResolvedValue({
      data: { items: mockDisputedLoans, nextCursor: undefined },
      error: undefined,
    } as any);
  });

  it("renders disputed loans with permanent disputed badges and passes axe audit", async () => {
    const { container } = renderDisputedLoansPage();

    await waitFor(() => {
      expect(screen.getByText("Disputed Tablet")).toBeInTheDocument();
    });

    expect(screen.getByText("Dr. Disputed User")).toBeInTheDocument();
    expect(screen.getByTestId("origin-badge-disputed")).toHaveTextContent("Disputed");
    expect(screen.getByText(/Forced past overlapping custody conflict/i)).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
