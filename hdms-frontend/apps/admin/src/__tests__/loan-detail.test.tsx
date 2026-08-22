import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { currentAdminQueryKey } from "@/lib/auth";
import { LoanDetailPage, loanDetailRoute } from "../routes/loans.$loanId";

// Mock TanStack Router
const mockNavigate = vi.fn();
let mockParams = { loanId: "loan-paper-1" };

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

vi.spyOn(loanDetailRoute, "useParams").mockImplementation(() => mockParams as any);

describe("LoanDetailPage — Phase 4.8b/c", () => {
  const mockPaperLoan: apiClient.Loan = {
    id: "loan-paper-1",
    deviceId: "dev-1",
    userId: "user-1",
    status: "open",
    origin: "paper",
    borrowedAt: "2026-08-10T09:00:00Z",
    dueAt: "2026-08-12T17:00:00Z",
    borrowActor: "admin:admin-1",
    borrowSource: "paper",
    paperRef: "Slip #402",
    recordedAt: "2026-08-11T10:00:00Z",
    recordedBy: "admin:admin-1",
    conditionOut: "good",
    disputed: false,
  };

  const mockKioskLoan: apiClient.Loan = {
    id: "loan-kiosk-1",
    deviceId: "dev-2",
    userId: "user-2",
    status: "returned",
    origin: "kiosk",
    borrowedAt: "2026-08-01T08:00:00Z",
    dueAt: "2026-08-05T17:00:00Z",
    returnedAt: "2026-08-04T16:00:00Z",
    borrowActor: "kiosk:kiosk-east",
    borrowSource: "scanner",
    returnActor: "kiosk:kiosk-west",
    returnSource: "camera",
    conditionOut: "good",
    conditionIn: "fair",
    disputed: false,
  };

  const mockBorrower: apiClient.User = {
    id: "user-1",
    employeeNo: "EMP-202",
    fullName: "Dr. Gregory House",
    status: "active",
    departmentId: "Diagnostics",
    email: "house@princeton.org",
    registeredAt: "2026-08-01T00:00:00Z",
    registeredBy: "admin:1",
    updatedAt: "2026-08-01T00:00:00Z",
  };

  const mockDevice: apiClient.Device = {
    id: "dev-1",
    assetTag: "ULT-001",
    name: "Handheld Ultrasound",
    categoryId: "cat-1",
    status: "on_loan",
    condition: "good",
    manufacturer: "GE Healthcare",
    model: "Vscan Air",
    createdAt: "2026-08-01T00:00:00Z",
    updatedAt: "2026-08-01T00:00:00Z",
  };

  const mockAuditEvents: apiClient.AuditEvent[] = [
    {
      id: "ev-1",
      action: "loan.opened",
      actor: "admin:admin-1",
      at: "2026-08-11T10:00:00Z",
      subject: "loan:loan-paper-1",
      payload: { origin: "paper", paperRef: "Slip #402" },
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

  function renderLoanDetailPage(role: "admin" | "technician" | "viewer" = "admin") {
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
        <LoanDetailPage />
      </QueryClientProvider>,
    );
  }

  beforeEach(() => {
    vi.clearAllMocks();
    mockParams = { loanId: "loan-paper-1" };

    window.HTMLElement.prototype.hasPointerCapture = vi.fn();
    window.HTMLElement.prototype.setPointerCapture = vi.fn();
    window.HTMLElement.prototype.releasePointerCapture = vi.fn();
    window.HTMLElement.prototype.scrollIntoView = vi.fn();

    vi.spyOn(apiClient, "getLoan").mockImplementation(async ({ path }: any) => {
      if (path.id === "loan-kiosk-1") {
        return { data: mockKioskLoan, error: undefined } as any;
      }
      return { data: mockPaperLoan, error: undefined } as any;
    });

    vi.spyOn(apiClient, "getUser").mockResolvedValue({
      data: mockBorrower,
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "getDevice").mockResolvedValue({
      data: mockDevice,
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "listAuditEvents").mockResolvedValue({
      data: { items: mockAuditEvents },
      error: undefined,
    } as any);
  });

  it("renders paper-origin loan details including slip reference, typist, and timestamp, and passes axe audit", async () => {
    const { container } = renderLoanDetailPage();

    await waitFor(() => {
      expect(screen.getByTestId("borrower-name")).toHaveTextContent("Dr. Gregory House");
    });

    expect(screen.getByTestId("device-name")).toHaveTextContent("Handheld Ultrasound");
    expect(screen.getByTestId("paper-ref")).toHaveTextContent("Slip #402");
    expect(screen.getByTestId("recorded-by")).toHaveTextContent("admin:admin-1");
    expect(screen.getByTestId("recorded-at")).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("renders kiosk loan details with scan source at both ends", async () => {
    mockParams = { loanId: "loan-kiosk-1" };
    renderLoanDetailPage();

    await waitFor(() => {
      expect(screen.getByTestId("borrow-source")).toHaveTextContent(/scanner/i);
    });

    expect(screen.getByTestId("return-source")).toHaveTextContent(/camera/i);
  });

  it("force return dialog opens, requires reason, and can cancel", async () => {
    renderLoanDetailPage("technician");

    await waitFor(() => {
      expect(screen.getByTestId("force-return-button")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId("force-return-button"));

    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText("Force Return Loan")).toBeInTheDocument();

    // Confirm button is disabled without reason
    const confirmBtn = screen.getByRole("button", { name: /confirm return/i });
    expect(confirmBtn).toBeDisabled();

    // Fill in reason
    const reasonInput = screen.getByLabelText(/reason for administrative return/i);
    fireEvent.change(reasonInput, { target: { value: "Returned without badge" } });
    expect(confirmBtn).toBeEnabled();

    // Cancel closes dialog
    fireEvent.click(screen.getByRole("button", { name: /cancel/i }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });

  it("write off dialog explains consequence and requires reason", async () => {
    renderLoanDetailPage("admin");

    await waitFor(() => {
      expect(screen.getByTestId("write-off-button")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId("write-off-button"));

    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText(/permanently closes the custody window/i)).toBeInTheDocument();

    const confirmBtn = screen.getByRole("button", { name: /write off loan/i });
    expect(confirmBtn).toBeDisabled();

    const reasonInput = screen.getByLabelText(/reason for write-off/i);
    fireEvent.change(reasonInput, { target: { value: "Crushed by elevator door" } });
    expect(confirmBtn).toBeEnabled();

    // Cancel branch
    fireEvent.click(screen.getByRole("button", { name: /cancel/i }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });

  it("correct attribution dialog opens and can cancel", async () => {
    renderLoanDetailPage("admin");

    await waitFor(() => {
      expect(screen.getByTestId("correct-attribution-button")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTestId("correct-attribution-button"));

    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText("Correct Loan Attribution")).toBeInTheDocument();

    // Cancel closes dialog
    fireEvent.click(screen.getByRole("button", { name: /cancel/i }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });
});
