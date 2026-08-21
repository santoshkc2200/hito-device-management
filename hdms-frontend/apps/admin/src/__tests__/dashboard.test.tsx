import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { AttentionStrip } from "@/components/dashboard/attention-strip";
import { CategoryAvailabilityBars } from "@/components/dashboard/category-availability-bars";
import { LiveActivityFeed } from "@/components/dashboard/live-activity-feed";
import { OverdueLoansTable } from "@/components/dashboard/overdue-loans-table";
import { StatTiles } from "@/components/dashboard/stat-tiles";
import { currentAdminQueryKey } from "@/lib/auth";
import { dashboardRoute } from "../routes/dashboard";

// Mock TanStack Router
const mockNavigate = vi.fn();
vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    useNavigate: () => mockNavigate,
    useRouter: () => ({ invalidate: vi.fn() }),
    Link: ({ children, to, search, params, ...props }: any) => (
      <a
        href={`${to}?${new URLSearchParams(search).toString()}`}
        data-to={to}
        data-search={JSON.stringify(search)}
        data-params={JSON.stringify(params)}
        {...props}
      >
        {children}
      </a>
    ),
  };
});

const mockOverdueLoans: apiClient.OverdueLoanSummary[] = [
  {
    loanId: "loan-1",
    deviceId: "dev-1",
    assetTag: "TAB-001",
    deviceName: "iPad Mini Emergency 1",
    userId: "user-1",
    userFullName: "Nurse Alice",
    userEmployeeNo: "EMP-001",
    dueAt: "2026-08-20T10:00:00Z",
    daysOverdue: 1,
  },
  {
    loanId: "loan-2",
    deviceId: "dev-2",
    assetTag: "PUM-042",
    deviceName: "Infusion Pump B",
    userId: "user-2",
    userFullName: "Dr. Robert Smith",
    userEmployeeNo: "EMP-042",
    dueAt: "2026-08-16T08:00:00Z",
    daysOverdue: 5,
  },
];

const mockCategories: apiClient.CategoryAvailability[] = [
  {
    categoryId: "cat-1",
    categoryName: "Tablets",
    availableCount: 3,
    totalCount: 10,
  },
  {
    categoryId: "cat-2",
    categoryName: "Infusion Pumps",
    availableCount: 0,
    totalCount: 5,
  },
];

const mockKiosks: apiClient.Kiosk[] = [
  {
    id: "kiosk-1",
    name: "Emergency Ward Kiosk",
    location: "Floor 1 ER",
    enabledSources: ["scanner", "camera"],
    status: "active",
    lastSeenAt: new Date().toISOString(),
    createdAt: "2026-01-01T00:00:00Z",
  },
  {
    id: "kiosk-2",
    name: "ICU Station Kiosk",
    location: "Floor 3 ICU",
    enabledSources: ["scanner"],
    status: "active",
    lastSeenAt: "2026-08-10T12:00:00Z", // Quiet kiosk (> 24h ago)
    createdAt: "2026-01-01T00:00:00Z",
  },
];

const mockDashboardData: apiClient.Dashboard = {
  availableCount: 15,
  onLoanCount: 8,
  overdueCount: 2,
  maintenanceCount: 1,
  availabilityByCategory: mockCategories,
  turnedAwayCounts: [
    { resolvedType: "revoked", distinctTokens: 2, totalScans: 4 },
    { resolvedType: "unbound", distinctTokens: 3, totalScans: 5 },
  ],
  lastPaperEntry: {
    paperRef: "SHEET-2026-08-15",
    recordedAt: "2026-08-15T09:00:00Z", // backlog (> 48h)
    recordedBy: "admin:bootstrap",
  },
  overdueLoans: mockOverdueLoans,
  unboundCredentialCount: 4, // low stock (< 10)
  lowStockThreshold: 10,
  paperBacklogHours: 48,
  kiosks: mockKiosks,
};

function createTestQueryClient() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });
  queryClient.setQueryData(currentAdminQueryKey, {
    id: "admin-1",
    username: "superadmin",
    role: "superadmin",
    status: "active",
  });
  return queryClient;
}

describe("Phase 4.7 — Dashboard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("4.7a — Static Panels", () => {
    it("renders 4 stat tiles linking to correctly filtered list views", () => {
      render(
        <StatTiles
          availableCount={15}
          onLoanCount={8}
          overdueCount={2}
          maintenanceCount={1}
        />
      );

      const availableTile = screen.getByTestId("stat-tile-available");
      expect(availableTile).toHaveTextContent("15");
      expect(availableTile.getAttribute("data-search")).toContain('"status":"available"');

      const onLoanTile = screen.getByTestId("stat-tile-on_loan");
      expect(onLoanTile).toHaveTextContent("8");
      expect(onLoanTile.getAttribute("data-search")).toContain('"status":"on_loan"');

      const overdueTile = screen.getByTestId("stat-tile-overdue");
      expect(overdueTile).toHaveTextContent("2");
      expect(overdueTile.getAttribute("data-search")).toContain('"status":"overdue"');

      const maintenanceTile = screen.getByTestId("stat-tile-maintenance");
      expect(maintenanceTile).toHaveTextContent("1");
      expect(maintenanceTile.getAttribute("data-search")).toContain('"status":"maintenance"');
    });

    it("renders category availability bars with text ratios and accessibility attributes", () => {
      render(<CategoryAvailabilityBars categories={mockCategories} />);

      expect(screen.getByText("Tablets")).toBeInTheDocument();
      expect(screen.getByText("3 / 10 available")).toBeInTheDocument();
      expect(screen.getByText("30%")).toBeInTheDocument();

      expect(screen.getByText("Infusion Pumps")).toBeInTheDocument();
      expect(screen.getByText("0 / 5 available")).toBeInTheDocument();
      expect(screen.getByText("0%")).toBeInTheDocument();

      const progressBars = screen.getAllByRole("progressbar");
      expect(progressBars).toHaveLength(2);
      expect(progressBars[0]).toHaveAttribute("aria-valuenow", "3");
      expect(progressBars[0]).toHaveAttribute("aria-valuemax", "10");
    });

    it("sorts overdue loans worst-first and provides Remind and Force Return actions", async () => {
      const user = userEvent.setup();
      const queryClient = createTestQueryClient();
      const clipboardWriteSpy = vi.spyOn(navigator.clipboard, "writeText");

      vi.spyOn(apiClient, "forceReturnLoan").mockResolvedValue({
        data: {} as any,
      } as any);

      render(
        <QueryClientProvider client={queryClient}>
          <OverdueLoansTable loans={mockOverdueLoans} />
        </QueryClientProvider>
      );

      const rows = screen.getAllByTestId(/overdue-row-/);
      expect(rows).toHaveLength(2);
      // Row 1 should be the 5 days overdue item (worst first)
      expect(rows[0]).toHaveTextContent("Infusion Pump B");
      expect(rows[0]).toHaveTextContent("5 days late");
      expect(rows[0]).toHaveTextContent("Dr. Robert Smith");

      // Row 2 should be the 1 day overdue item
      expect(rows[1]).toHaveTextContent("iPad Mini Emergency 1");
      expect(rows[1]).toHaveTextContent("1 day late");
      expect(rows[1]).toHaveTextContent("Nurse Alice");

      // Click Remind on row 1
      const remindButtons = screen.getAllByRole("button", { name: /remind/i });
      await user.click(remindButtons[0]);

      expect(clipboardWriteSpy).toHaveBeenCalledWith(
        expect.stringContaining("Dr. Robert Smith")
      );
      expect(clipboardWriteSpy).toHaveBeenCalledWith(
        expect.stringContaining("Infusion Pump B")
      );

      // Click Force Return on row 1
      const forceReturnButtons = screen.getAllByRole("button", { name: /force return/i });
      await user.click(forceReturnButtons[0]);

      // Confirm dialog opens
      expect(screen.getByText("Force Return Loan")).toBeInTheDocument();
      const reasonInput = screen.getByLabelText(/reason for administrative return/i);
      await user.type(reasonInput, "Found in storage room without checkout");

      const confirmBtn = screen.getByRole("button", { name: /confirm return/i });
      await user.click(confirmBtn);

      await waitFor(() => {
        expect(apiClient.forceReturnLoan).toHaveBeenCalledWith({
          path: { id: "loan-2" },
          body: { reason: "Found in storage room without checkout" },
        });
      });
    });

    it("renders designed reassuring empty state when there are no overdue loans", () => {
      const queryClient = createTestQueryClient();
      render(
        <QueryClientProvider client={queryClient}>
          <OverdueLoansTable loans={[]} />
        </QueryClientProvider>
      );

      expect(screen.getByText("Nothing overdue")).toBeInTheDocument();
      expect(
        screen.getByText(/all active loans are currently within their scheduled return period/i)
      ).toBeInTheDocument();
    });
  });

  describe("4.7b — Live Activity Feed over SSE", () => {
    it("renders incoming events newest first and de-duplicates by event ID", async () => {
      const mockStreamUrl = "/v1/events/stream-test";
      let streamController: ReadableStreamDefaultController | null = null;

      const mockStream = new ReadableStream({
        start(controller) {
          streamController = controller;
        },
      });

      const encoder = new TextEncoder();
      vi.spyOn(globalThis, "fetch").mockImplementation(async (url: any) => {
        if (String(url).includes("stream-test")) {
          return new Response(mockStream, {
            status: 200,
            headers: { "Content-Type": "text/event-stream" },
          });
        }
        return new Response("Not found", { status: 404 });
      });

      render(<LiveActivityFeed streamUrl={mockStreamUrl} />);

      // Wait for connection
      await waitFor(() => {
        expect(screen.getByTestId("connection-status-connected")).toBeInTheDocument();
      });

      // Stream event 1
      act(() => {
        streamController?.enqueue(
          encoder.encode(
            'id: 101\nevent: loan.opened\ndata: {"userName":"Nurse Alice","deviceName":"iPad Mini 1","kioskId":"kiosk-1"}\n\n'
          )
        );
      });

      await waitFor(() => {
        expect(screen.getByText("Device Checked Out")).toBeInTheDocument();
        expect(screen.getByText(/Nurse Alice borrowed iPad Mini 1 at kiosk kiosk-1/)).toBeInTheDocument();
      });

      // Stream event 2
      act(() => {
        streamController?.enqueue(
          encoder.encode(
            'id: 102\nevent: user.registered\ndata: {"fullName":"Dr. Sato","employeeNo":"EMP-999"}\n\n'
          )
        );
      });

      await waitFor(() => {
        expect(screen.getByText("User Registered")).toBeInTheDocument();
        expect(screen.getByText(/Dr. Sato \(EMP-999\) was added to system/)).toBeInTheDocument();
      });

      // Stream duplicate event 101 (should NOT create a duplicate in the UI)
      act(() => {
        streamController?.enqueue(
          encoder.encode(
            'id: 101\nevent: loan.opened\ndata: {"userName":"Nurse Alice","deviceName":"iPad Mini 1"}\n\n'
          )
        );
      });

      const checkoutEvents = screen.getAllByText("Device Checked Out");
      expect(checkoutEvents).toHaveLength(1);
    });

    it("displays honest connection states on network failure and provides Reconnect", async () => {
      vi.spyOn(globalThis, "fetch").mockRejectedValue(new Error("Network error"));

      render(<LiveActivityFeed streamUrl="/v1/events/stream-error" />);

      // Starts reconnecting
      expect(screen.getByTestId("connection-status-reconnecting")).toBeInTheDocument();
    });
  });

  describe("4.7c — Attention Strip", () => {
    it("renders revoked scan alert, unregistered scan CTA, low stock warning, paper backlog, and quiet kiosks", () => {
      render(
        <AttentionStrip
          turnedAwayCounts={mockDashboardData.turnedAwayCounts}
          unboundCredentialCount={mockDashboardData.unboundCredentialCount}
          lowStockThreshold={mockDashboardData.lowStockThreshold}
          kiosks={mockDashboardData.kiosks}
          lastPaperEntry={mockDashboardData.lastPaperEntry}
          paperBacklogHours={mockDashboardData.paperBacklogHours}
        />
      );

      // 1. Revoked scans
      expect(screen.getByTestId("attention-revoked-scans")).toBeInTheDocument();
      expect(screen.getByText(/Revoked Card Scan Attempts \(4 scans\)/)).toBeInTheDocument();

      // 2. Unregistered scans
      expect(screen.getByTestId("attention-unregistered-scans")).toBeInTheDocument();
      expect(screen.getByText(/5 Turned Away/)).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /register borrower/i })).toBeInTheDocument();

      // 3. Low stock
      expect(screen.getByTestId("attention-low-stock")).toBeInTheDocument();
      expect(screen.getByText(/Blank Card Stock Low \(4 remaining\)/)).toBeInTheDocument();

      // 4. Paper backlog
      expect(screen.getByTestId("attention-paper-backlog")).toBeInTheDocument();
      expect(screen.getByText(/Paper Backlog Warning/)).toBeInTheDocument();

      // 5. Kiosks & Quiet kiosk badge
      expect(screen.getByTestId("kiosks-status-strip")).toBeInTheDocument();
      expect(screen.getByText("Emergency Ward Kiosk")).toBeInTheDocument();
      expect(screen.getByText("ICU Station Kiosk")).toBeInTheDocument();
      expect(screen.getByTestId("quiet-kiosks-badge")).toHaveTextContent("1 quiet kiosk");
    });
  });

  describe("Full Dashboard Route Integration & A11y", () => {
    it("loads dashboard data and renders complete operational overview", async () => {
      const queryClient = createTestQueryClient();
      vi.spyOn(apiClient, "getDashboard").mockResolvedValue({
        data: mockDashboardData,
      } as any);

      const Component = (dashboardRoute as any).options.component;

      const { container } = render(
        <QueryClientProvider client={queryClient}>
          <Component />
        </QueryClientProvider>
      );

      await waitFor(() => {
        expect(screen.getByTestId("dashboard-page")).toBeInTheDocument();
      });

      expect(screen.getByText("Equipment Operations")).toBeInTheDocument();
      expect(screen.getByText("Availability by Category")).toBeInTheDocument();
      expect(screen.getByText("Overdue Loans")).toBeInTheDocument();
      expect(screen.getByText("Live Activity Feed")).toBeInTheDocument();

      // Accessibility test
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });
  });
});
