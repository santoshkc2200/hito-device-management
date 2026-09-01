import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { translate } from "@hdms/i18n";
import { catalogues } from "@/i18n";
import { ja } from "@/i18n/ja";
import { currentAdminQueryKey } from "@/lib/auth";
import { reportsRoute } from "../routes/reports";

const mockNavigate = vi.fn();
let mockSearchState: Record<string, any> = {};

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    useNavigate: () => mockNavigate,
    useRouter: () => ({ invalidate: vi.fn() }),
  };
});

// Mock reportsRoute.useSearch
vi.spyOn(reportsRoute, "useSearch").mockImplementation(() => mockSearchState as any);

const mockReportSummary: apiClient.ReportSummary = {
  from: "2026-07-22T00:00:00Z",
  to: "2026-08-21T00:00:00Z",
  totalLoans: 142,
  openLoans: 28,
  overdueCount: 4,
  overdueRate: 0.028,
  averageLoanDurationHours: 4.8,
  utilisationByCategory: [
    {
      categoryId: "cat-1",
      categoryName: "Ultrasound Scanners",
      deviceCount: 10,
      loanCount: 45,
      averageDurationHours: 6.2,
      utilisationPct: 38.5,
    },
    {
      categoryId: "cat-2",
      categoryName: "Infusion Pumps",
      deviceCount: 30,
      loanCount: 80,
      averageDurationHours: 12.4,
      utilisationPct: 65.2,
    },
  ],
  topBorrowers: [
    {
      userId: "user-1",
      fullName: "Dr. Alice Walker",
      employeeNo: "EMP-001",
      loanCount: 18,
    },
    {
      userId: "user-2",
      fullName: "Nurse Bob Jones",
      employeeNo: "EMP-002",
      loanCount: 14,
    },
  ],
};

const mockOriginReport: apiClient.OriginReport = {
  from: "2026-07-22T00:00:00Z",
  to: "2026-08-21T00:00:00Z",
  bucket: "day",
  buckets: [
    {
      periodStart: "2026-08-20T00:00:00Z",
      counts: [
        { origin: "kiosk", count: 12 },
        { origin: "paper", count: 3 },
      ],
      total: 15,
    },
    {
      periodStart: "2026-08-21T00:00:00Z",
      counts: [
        { origin: "kiosk", count: 20 },
        { origin: "admin", count: 2 },
      ],
      total: 22,
    },
  ],
};

const mockOperationalHealth: apiClient.OperationalHealth = {
  from: "2026-07-22T00:00:00Z",
  to: "2026-08-21T00:00:00Z",
  totalScans: 250,
  manualEntryCount: 15,
  cameraFallbackCount: 8,
  scansBySource: [
    { source: "scanner", count: 227 },
    { source: "manual", count: 15 },
    { source: "camera", count: 8 },
  ],
  rejectionReasons: [
    {
      reason: "Card not registered",
      resolvedType: "unbound",
      count: 6,
    },
    {
      reason: "Card revoked",
      resolvedType: "revoked",
      count: 2,
    },
  ],
};

function renderReportsPage(queryClient: QueryClient) {
  const Component = reportsRoute.options.component as React.ComponentType;
  return render(
    <QueryClientProvider client={queryClient}>
      <Component />
    </QueryClientProvider>
  );
}

describe("ReportsPage (4.9b)", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.clearAllMocks();
    mockSearchState = {};
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
      },
    });

    queryClient.setQueryData(currentAdminQueryKey, {
      id: "admin-1",
      email: "admin@example.org",
      fullName: "Super Admin",
      role: "admin",
    });

    vi.spyOn(apiClient, "getReportSummary").mockResolvedValue({
      data: mockReportSummary,
      error: undefined,
      response: new Response(),
    } as any);

    vi.spyOn(apiClient, "getTransactionsByOrigin").mockResolvedValue({
      data: mockOriginReport,
      error: undefined,
      response: new Response(),
    } as any);

    vi.spyOn(apiClient, "getOperationalHealth").mockResolvedValue({
      data: mockOperationalHealth,
      error: undefined,
      response: new Response(),
    } as any);
  });

  it("renders report summary metrics and category utilisation", async () => {
    mockSearchState = { tab: "summary" };
    const { container } = renderReportsPage(queryClient);

    await waitFor(() => {
      expect(screen.getByTestId("total-loans")).toHaveTextContent("142");
    });

    expect(screen.getByTestId("open-loans")).toHaveTextContent("28");
    expect(screen.getByTestId("overdue-count")).toHaveTextContent("4");
    expect(screen.getByTestId("avg-duration")).toHaveTextContent("4.8h");

    // Category Utilisation table
    expect(screen.getByText("Ultrasound Scanners")).toBeInTheDocument();
    expect(screen.getByText("38.5%")).toBeInTheDocument();
    expect(screen.getByText("Infusion Pumps")).toBeInTheDocument();
    expect(screen.getByText("65.2%")).toBeInTheDocument();

    // Top Borrowers table
    expect(screen.getByText("Dr. Alice Walker")).toBeInTheDocument();
    expect(screen.getByText("EMP-001")).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("switches to origin trends tab and renders bucketed breakdown", async () => {
    mockSearchState = { tab: "origin" };
    renderReportsPage(queryClient);

    await waitFor(() => {
      expect(screen.getByText(ja.reports.originBreakdownTitle)).toBeInTheDocument();
    });

    expect(
      screen.getByText(translate(catalogues, "ja", "reports.transactionsCount", { count: 15 }))
    ).toBeInTheDocument();
    expect(
      screen.getByText(translate(catalogues, "ja", "reports.transactionsCount", { count: 22 }))
    ).toBeInTheDocument();
  });

  it("switches to operational health tab and renders scan metrics & turnaways", async () => {
    mockSearchState = { tab: "health" };
    renderReportsPage(queryClient);

    await waitFor(() => {
      expect(screen.getByTestId("total-scans")).toHaveTextContent("250");
    });

    expect(screen.getByTestId("manual-scans")).toHaveTextContent("15");
    expect(screen.getByTestId("camera-scans")).toHaveTextContent("8");

    expect(screen.getByText("Card not registered")).toBeInTheDocument();
    expect(screen.getByText("unbound")).toBeInTheDocument();
    expect(screen.getByText("Card revoked")).toBeInTheDocument();
    expect(screen.getByText("revoked")).toBeInTheDocument();
  });

  it("navigates on date range preset button click", async () => {
    mockSearchState = { tab: "summary" };
    renderReportsPage(queryClient);

    const user = userEvent.setup();
    const btn7d = screen.getByTestId("preset-7d");
    await user.click(btn7d);

    expect(mockNavigate).toHaveBeenCalled();
  });
});
