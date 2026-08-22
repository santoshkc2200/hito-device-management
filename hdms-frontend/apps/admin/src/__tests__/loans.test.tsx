import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { currentAdminQueryKey } from "@/lib/auth";
import { LoansPage, loansRoute } from "../routes/loans";

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

vi.spyOn(loansRoute, "useSearch").mockImplementation(() => mockSearch as any);

describe("LoansPage — Phase 4.8a/c", () => {
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
      status: "on_loan",
      condition: "good",
      createdAt: "2026-08-01T00:00:00Z",
      updatedAt: "2026-08-01T00:00:00Z",
    },
    {
      id: "dev-2",
      assetTag: "TAB-01",
      name: "iPad Air 5th Gen",
      categoryId: "cat-2",
      status: "on_loan",
      condition: "good",
      createdAt: "2026-08-01T00:00:00Z",
      updatedAt: "2026-08-01T00:00:00Z",
    },
  ];

  const mockLoans: apiClient.Loan[] = [
    {
      id: "loan-1",
      deviceId: "dev-1",
      userId: "user-1",
      status: "open",
      origin: "paper",
      borrowedAt: "2026-08-10T09:00:00Z",
      dueAt: "2026-08-12T17:00:00Z", // overdue
      borrowActor: "admin:admin-1",
      borrowSource: "paper",
      paperRef: "Slip #402",
      recordedAt: "2026-08-11T10:00:00Z",
      recordedBy: "admin:admin-1",
      disputed: false,
    },
    {
      id: "loan-2",
      deviceId: "dev-2",
      userId: "user-2",
      status: "open",
      origin: "admin",
      borrowedAt: "2026-08-15T14:00:00Z",
      dueAt: "2026-08-20T17:00:00Z", // overdue
      borrowActor: "admin:admin-1",
      borrowSource: "manual",
      disputed: false,
    },
    {
      id: "loan-3",
      deviceId: "dev-1",
      userId: "user-2",
      status: "returned",
      origin: "kiosk",
      borrowedAt: "2026-08-01T08:00:00Z",
      dueAt: "2026-08-05T17:00:00Z",
      returnedAt: "2026-08-04T16:00:00Z",
      borrowActor: "kiosk:kiosk-1",
      borrowSource: "scanner",
      returnActor: "kiosk:kiosk-1",
      returnSource: "scanner",
      disputed: false,
    },
    {
      id: "loan-4",
      deviceId: "dev-2",
      userId: "user-1",
      status: "returned",
      origin: "import",
      borrowedAt: "2026-07-20T08:00:00Z",
      returnedAt: "2026-07-22T16:00:00Z",
      borrowActor: "import",
      borrowSource: "manual",
      disputed: false,
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

  function renderLoansPage(role: "admin" | "technician" | "viewer" = "admin") {
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
        <LoansPage />
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

    vi.spyOn(apiClient, "listLoans").mockResolvedValue({
      data: { items: mockLoans, nextCursor: undefined },
      error: undefined,
    } as any);
  });

  it("renders loans list with origin badges for paper, admin, import, and kiosk and passes axe audit", async () => {
    const { container } = renderLoansPage();

    await waitFor(() => {
      expect(screen.getAllByText("Dell Latitude 5420").length).toBeGreaterThanOrEqual(1);
    });

    // Verify origin badges render with text
    expect(screen.getByTestId("origin-badge-paper")).toHaveTextContent("Paper");
    expect(screen.getByTestId("origin-badge-admin")).toHaveTextContent("Admin");
    expect(screen.getByTestId("origin-badge-kiosk")).toHaveTextContent("Kiosk");
    expect(screen.getByTestId("origin-badge-import")).toHaveTextContent("Import");

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  }, 15000);


  it("sorts overdue loans worst-first when filtered to overdue", async () => {
    mockSearch = { status: "overdue" };
    renderLoansPage();

    await waitFor(() => {
      expect(screen.getAllByText("Dell Latitude 5420").length).toBeGreaterThanOrEqual(1);
    });

    // loan-1 due Aug 12 (earlier/worse) should precede loan-2 due Aug 20
    const rows = screen.getAllByRole("row");
    expect(rows[1]).toHaveTextContent("Dell Latitude 5420");
    expect(rows[2]).toHaveTextContent("iPad Air 5th Gen");
  });

  it("remind button copies formatted reminder message to clipboard", async () => {
    const writeTextMock = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, {
      clipboard: { writeText: writeTextMock },
    });

    renderLoansPage();

    await waitFor(() => {
      expect(screen.getByTestId("remind-btn-loan-1")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId("remind-btn-loan-1"));

    await waitFor(() => {
      expect(writeTextMock).toHaveBeenCalledWith(
        expect.stringContaining("Dr. Alice Walker"),
      );
    });
    expect(writeTextMock).toHaveBeenCalledWith(
      expect.stringContaining("LAPTOP-01"),
    );
  });
});
