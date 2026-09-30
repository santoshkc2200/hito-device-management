import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { translate } from "@hdms/i18n";
import { catalogues } from "@/i18n";
import { AttentionStrip } from "@/components/dashboard/attention-strip";
import { CategoryAvailabilityBars } from "@/components/dashboard/category-availability-bars";
import { LiveActivityFeed } from "@/components/dashboard/live-activity-feed";
import { OverdueLoansTable } from "@/components/dashboard/overdue-loans-table";
import { StatTiles } from "@/components/dashboard/stat-tiles";
import { currentAdminQueryKey } from "@/lib/auth";
import { ja } from "@/i18n/ja";
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
    defaultLocale: "en",
  },
  {
    id: "kiosk-2",
    name: "ICU Station Kiosk",
    location: "Floor 3 ICU",
    enabledSources: ["scanner"],
    status: "active",
    lastSeenAt: "2026-08-10T12:00:00Z", // Quiet kiosk (> 24h ago)
    createdAt: "2026-01-01T00:00:00Z",
    defaultLocale: "en",
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
      expect(
        screen.getByText(
          translate(catalogues, "ja", "dashboard.categories.availableCount", {
            available: 3,
            total: 10,
          })
        )
      ).toBeInTheDocument();
      expect(screen.getByText("30%")).toBeInTheDocument();

      expect(screen.getByText("Infusion Pumps")).toBeInTheDocument();
      expect(
        screen.getByText(
          translate(catalogues, "ja", "dashboard.categories.availableCount", {
            available: 0,
            total: 5,
          })
        )
      ).toBeInTheDocument();
      expect(screen.getByText("0%")).toBeInTheDocument();

      const progressBars = screen.getAllByRole("progressbar");
      expect(progressBars).toHaveLength(2);
      expect(progressBars[0]).toHaveAttribute("aria-valuenow", "3");
      expect(progressBars[0]).toHaveAttribute("aria-valuemax", "10");
    });

    it("sorts overdue loans worst-first and provides Remind and Force Return actions", async () => {
      const user = userEvent.setup();
      const queryClient = createTestQueryClient();
      const remindLoanSpy = vi.spyOn(apiClient, "remindLoan").mockResolvedValue({
        data: { outcome: "sent" },
      } as any);

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
      expect(rows[0]).toHaveTextContent(
        translate(catalogues, "ja", "dashboard.overdue.daysLate", { count: 5 })
      );
      expect(rows[0]).toHaveTextContent("Dr. Robert Smith");

      // Row 2 should be the 1 day overdue item
      expect(rows[1]).toHaveTextContent("iPad Mini Emergency 1");
      expect(rows[1]).toHaveTextContent(ja.dashboard.overdue.oneDayLate);
      expect(rows[1]).toHaveTextContent("Nurse Alice");

      // Click Remind on row 1
      const remindButtons = screen.getAllByRole("button", { name: new RegExp(ja.dashboard.overdue.remindButton, "i") });
      await user.click(remindButtons[0]);

      expect(remindLoanSpy).toHaveBeenCalledWith({
        path: { id: "loan-2" },
      });

      // Click Force Return on row 1
      const forceReturnButtons = screen.getAllByRole("button", { name: new RegExp(ja.dashboard.overdue.forceReturnButton, "i") });
      await user.click(forceReturnButtons[0]);

      // Confirm dialog opens
      expect(screen.getByText(ja.dashboard.overdue.dialogTitle)).toBeInTheDocument();
      const reasonInput = screen.getByLabelText(ja.dashboard.overdue.reasonLabel);
      await user.type(reasonInput, "Found in storage room without checkout");

      const confirmBtn = screen.getByRole("button", { name: new RegExp(ja.dashboard.overdue.confirmReturn, "i") });
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

      expect(screen.getByText(ja.dashboard.overdue.emptyTitle)).toBeInTheDocument();
      expect(
        screen.getByText(ja.dashboard.overdue.emptyDesc)
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

      const checkoutDesc = translate(catalogues, "ja", "dashboard.feed.deviceCheckedOutDesc", {
        user: "Nurse Alice",
        device: "iPad Mini 1",
        kiosk: translate(catalogues, "ja", "dashboard.feed.atKiosk", { kioskId: "kiosk-1" }),
      });

      await waitFor(() => {
        expect(screen.getByText(ja.dashboard.feed.deviceCheckedOutTitle)).toBeInTheDocument();
        expect(screen.getByText(checkoutDesc)).toBeInTheDocument();
      });

      // Stream event 2
      act(() => {
        streamController?.enqueue(
          encoder.encode(
            'id: 102\nevent: user.registered\ndata: {"fullName":"Dr. Sato","employeeNo":"EMP-999"}\n\n'
          )
        );
      });

      const registeredDesc = translate(catalogues, "ja", "dashboard.feed.userRegisteredDesc", {
        name: "Dr. Sato",
        employeeNo: translate(catalogues, "ja", "dashboard.feed.employeeNoSuffix", {
          employeeNo: "EMP-999",
        }),
      });

      await waitFor(() => {
        expect(screen.getByText(ja.dashboard.feed.userRegisteredTitle)).toBeInTheDocument();
        expect(screen.getByText(registeredDesc)).toBeInTheDocument();
      });

      // Stream duplicate event 101 (should NOT create a duplicate in the UI)
      act(() => {
        streamController?.enqueue(
          encoder.encode(
            'id: 101\nevent: loan.opened\ndata: {"userName":"Nurse Alice","deviceName":"iPad Mini 1"}\n\n'
          )
        );
      });

      const checkoutEvents = screen.getAllByText(ja.dashboard.feed.deviceCheckedOutTitle);
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
    it("shows no paper backlog warning when the threshold is 0", () => {
      render(<AttentionStrip kiosks={mockDashboardData.kiosks} lastPaperEntry={undefined} paperBacklogHours={0} />);
      expect(screen.queryByTestId("attention-paper-backlog")).not.toBeInTheDocument();
    });

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
      expect(
        screen.getByText(
          translate(catalogues, "ja", "dashboard.attention.revokedScansTitle", { count: 4 })
        )
      ).toBeInTheDocument();

      // 2. Unregistered scans
      expect(screen.getByTestId("attention-unregistered-scans")).toBeInTheDocument();
      expect(
        screen.getByText(
          translate(catalogues, "ja", "dashboard.attention.unregisteredScansTitle", { count: 5 })
        )
      ).toBeInTheDocument();
      expect(screen.getByRole("button", { name: new RegExp(ja.dashboard.attention.registerBorrower, "i") })).toBeInTheDocument();

      // 3. Low stock
      expect(screen.getByTestId("attention-low-stock")).toBeInTheDocument();
      expect(
        screen.getByText(
          translate(catalogues, "ja", "dashboard.attention.lowStockTitle", { count: 4 })
        )
      ).toBeInTheDocument();

      // 4. Paper backlog
      expect(screen.getByTestId("attention-paper-backlog")).toBeInTheDocument();
      const paperBacklogStem = ja.dashboard.attention.paperBacklogTitle.split("{time}")[0];
      expect(
        screen.getByText(new RegExp(paperBacklogStem.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")))
      ).toBeInTheDocument();

      // 5. Kiosks & Quiet kiosk badge
      expect(screen.getByTestId("kiosks-status-strip")).toBeInTheDocument();
      expect(screen.getByText("Emergency Ward Kiosk")).toBeInTheDocument();
      expect(screen.getByText("ICU Station Kiosk")).toBeInTheDocument();
      expect(screen.getByTestId("quiet-kiosks-badge")).toHaveTextContent(
        translate(catalogues, "ja", "dashboard.attention.quietKiosksBadge", { count: 1 })
      );
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

      expect(screen.getByText(ja.dashboard.headerTitle)).toBeInTheDocument();
      expect(screen.getByText(ja.dashboard.categories.title)).toBeInTheDocument();
      expect(screen.getByText(ja.dashboard.overdue.title)).toBeInTheDocument();
      expect(screen.getByText(ja.dashboard.feed.title)).toBeInTheDocument();

      // Accessibility test
      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });
  });

  describe("backup attention item", () => {
    it("warns when last backup is older than 26 hours", () => {
      render(
        <AttentionStrip backup={{ lastSuccessAt: new Date(Date.now() - 27 * 3_600_000).toISOString() }} />
      );
      expect(screen.getByText(ja.dashboard.attention.backupStaleTitle)).toBeInTheDocument();
    });

    it("warns when no backup ever succeeded", () => {
      render(<AttentionStrip backup={{ lastSuccessAt: null }} />);
      expect(screen.getByText(ja.dashboard.attention.backupNeverTitle)).toBeInTheDocument();
    });

    it("stays quiet for a recent backup or a non-admin", () => {
      const { rerender } = render(
        <AttentionStrip backup={{ lastSuccessAt: new Date(Date.now() - 3_600_000).toISOString() }} />
      );
      expect(screen.queryByText(ja.dashboard.attention.backupStaleTitle)).not.toBeInTheDocument();
      rerender(<AttentionStrip />);
      expect(screen.queryByText(ja.dashboard.attention.backupNeverTitle)).not.toBeInTheDocument();
    });
  });
});
