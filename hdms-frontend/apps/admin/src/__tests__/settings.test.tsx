import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { currentAdminQueryKey } from "@/lib/auth";
import { PolicyPanel } from "@/components/settings/policy-panel";
import { KiosksPanel } from "@/components/settings/kiosks-panel";
import { TemplatesPanel } from "@/components/settings/templates-panel";

// Mock TanStack Router
vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    useNavigate: () => vi.fn(),
    useRouter: () => ({ invalidate: vi.fn() }),
    Link: ({ children, to }: { children: React.ReactNode; to: string }) => <a href={to}>{children}</a>,
  };
});

// Mock ResizeObserver for Radix UI components
window.ResizeObserver = class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
};


const mockSettings: apiClient.Settings = {
  policy: {
    blockOnOverdue: false,
    sessionIdleTimeoutSeconds: 45,
    kioskSoundEnabled: true,
    lowStockThreshold: 10,
    paperBacklogHours: 48,
  },
  labelTemplate: {
    sheetWidthMm: 210,
    sheetHeightMm: 297,
    columns: 3,
    rows: 8,
    marginTopMm: 15,
    marginLeftMm: 8,
    gutterXMm: 4,
    gutterYMm: 4,
    labelWidthMm: 60,
    labelHeightMm: 30,
  },
  slipTemplate: {
    hospitalName: "HITO HOSPITAL",
    pageRefFormat: "REF-YYYY-MM-pNN",
    rowsPerPage: 25,
    columns: ["#", "Asset Tag", "Device Name", "Employee ID", "Borrow Date", "Return Date", "Sign / Note"],
  },
  updatedAt: "2026-08-20T10:00:00Z",
  updatedBy: "admin:1",
};

const mockCategories: apiClient.Category[] = [
  {
    id: "cat-1",
    name: "Infusion Pump",
    defaultLoanPeriodSeconds: 604800, // 7 days
    requiresApproval: false,
    createdAt: "2026-08-01T00:00:00Z",
  },
  {
    id: "cat-2",
    name: "Ultrasound Probe",
    defaultLoanPeriodSeconds: 86400, // 1 day
    requiresApproval: true,
    createdAt: "2026-08-01T00:00:00Z",
  },
];

const mockKiosks: apiClient.Kiosk[] = [
  {
    id: "kiosk-1",
    name: "Ward 3 Station",
    location: "Ward 3",
    enabledSources: ["scanner", "camera"],
    status: "active",
    lastSeenAt: new Date(Date.now() - 5 * 60 * 1000).toISOString(),
    createdAt: "2026-08-10T10:00:00Z",
  },
  {
    id: "kiosk-2",
    name: "ICU Backup Terminal",
    location: "ICU Entrance",
    enabledSources: ["scanner"],
    status: "disabled",
    lastSeenAt: new Date(Date.now() - 48 * 60 * 60 * 1000).toISOString(),
    createdAt: "2026-08-01T10:00:00Z",
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

function renderWithClient(ui: React.ReactElement, currentUserRole: "admin" | "technician" | "viewer" = "admin") {
  const queryClient = createTestQueryClient();
  queryClient.setQueryData(currentAdminQueryKey, {
    id: "admin-1",
    email: "admin@hito.local",
    fullName: "System Admin",
    role: currentUserRole,
    status: "active",
  });

  return {
    ...render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>),
    queryClient,
  };
}

describe("Settings Panels", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(apiClient, "getSettings").mockResolvedValue({
      data: mockSettings,
      error: undefined,
      response: new Response(),
    });
    vi.spyOn(apiClient, "listCategories").mockResolvedValue({
      data: { items: mockCategories },
      error: undefined,
      response: new Response(),
    });
    vi.spyOn(apiClient, "listKiosks").mockResolvedValue({
      data: { items: mockKiosks },
      error: undefined,
      response: new Response(),
    });
  });

  describe("PolicyPanel", () => {
    it("renders categories and system policy form fields", async () => {
      renderWithClient(<PolicyPanel />);

      await waitFor(() => {
        expect(screen.getByText("Device Categories")).toBeInTheDocument();
        expect(screen.getByText("Infusion Pump")).toBeInTheDocument();
        expect(screen.getByText("Ultrasound Probe")).toBeInTheDocument();
        expect(screen.getByText("System & Checkout Policy")).toBeInTheDocument();
        expect(screen.getByText("Strict Overdue Enforcement")).toBeInTheDocument();
      });
    });

    it("allows admin to open new category modal", async () => {
      const user = userEvent.setup();
      renderWithClient(<PolicyPanel />);

      await waitFor(() => {
        expect(screen.getByText("New Category")).toBeInTheDocument();
      });

      await user.click(screen.getByText("New Category"));

      await waitFor(() => {
        expect(screen.getByText("Specify the default borrowing duration and approval policy for this equipment category.")).toBeInTheDocument();
      });
    });
  });

  describe("KiosksPanel", () => {
    it("renders registered kiosk terminals list", async () => {
      renderWithClient(<KiosksPanel />);

      await waitFor(() => {
        expect(screen.getByText("Registered Kiosk Terminals")).toBeInTheDocument();
        expect(screen.getByText("Ward 3 Station")).toBeInTheDocument();
        expect(screen.getByText("ICU Backup Terminal")).toBeInTheDocument();
      });
    });

    it("displays hardware diagnostic tool banner", async () => {
      renderWithClient(<KiosksPanel />);

      await waitFor(() => {
        expect(screen.getByText("Scanner & Card Reader Diagnostic")).toBeInTheDocument();
        expect(screen.getByText("Launch Diagnostic Tool")).toBeInTheDocument();
      });
    });

    it("issues, displays, and clears a six-digit pairing code from a kiosk row", async () => {
      const user = userEvent.setup();
      vi.spyOn(apiClient, "createKioskPairingCode").mockResolvedValue({
        data: { code: "123456", expiresAt: new Date(Date.now() + 10 * 60 * 1000).toISOString() },
        error: undefined,
        request: new Request("http://localhost/v1/kiosks/kiosk-1/pairing-code"),
        response: new Response(),
      });
      renderWithClient(<KiosksPanel />);

      await user.click(await screen.findByLabelText("Actions for Ward 3 Station"));
      await user.click(await screen.findByText("Issue Pairing Code"));

      await waitFor(() => expect(apiClient.createKioskPairingCode).toHaveBeenCalledWith({ path: { id: "kiosk-1" } }));
      expect(await screen.findByTestId("pairing-code")).toHaveTextContent("123 456");
      expect(screen.getByText(/It works once; issuing another code cancels this one/)).toBeInTheDocument();

      await user.click(screen.getAllByRole("button", { name: "Close" })[0]);
      await waitFor(() => expect(screen.queryByTestId("pairing-code")).not.toBeInTheDocument());
    });

    it("continues kiosk registration directly into pairing", async () => {
      const user = userEvent.setup();
      vi.spyOn(apiClient, "createKiosk").mockResolvedValue({
        data: { ...mockKiosks[0], id: "kiosk-new", name: "New Ward Kiosk", token: "one-shot-token" },
        error: undefined,
        request: new Request("http://localhost/v1/kiosks"),
        response: new Response(),
      });
      vi.spyOn(apiClient, "createKioskPairingCode").mockResolvedValue({
        data: { code: "654321", expiresAt: new Date(Date.now() + 10 * 60 * 1000).toISOString() },
        error: undefined,
        request: new Request("http://localhost/v1/kiosks/kiosk-new/pairing-code"),
        response: new Response(),
      });
      renderWithClient(<KiosksPanel />);

      await user.click(await screen.findByRole("button", { name: "Register Kiosk" }));
      const dialog = screen.getByRole("dialog");
      await user.type(within(dialog).getByLabelText("Kiosk Name"), "New Ward Kiosk");
      await user.click(within(dialog).getByRole("button", { name: "Register Kiosk" }));

      await waitFor(() => expect(apiClient.createKioskPairingCode).toHaveBeenCalledWith({ path: { id: "kiosk-new" } }));
      expect(await screen.findByText("Pair Kiosk: New Ward Kiosk")).toBeInTheDocument();
    });
  });

  describe("TemplatesPanel", () => {
    it("renders label template settings and live layout preview", async () => {
      renderWithClient(<TemplatesPanel />);

      await waitFor(() => {
        expect(screen.getByText("Adhesive Label Sheet Layout")).toBeInTheDocument();
        expect(screen.getByText(/Live Layout Preview/)).toBeInTheDocument();
        expect(screen.getByText("Physical Register Slip Pad Template")).toBeInTheDocument();
      });
    });

    it("renders standard presets buttons", async () => {
      renderWithClient(<TemplatesPanel />);

      await waitFor(() => {
        expect(screen.getByText("A4 Standard (3 × 8 — 24 Labels)")).toBeInTheDocument();
        expect(screen.getByText("A4 Compact (4 × 10 — 40 Labels)")).toBeInTheDocument();
      });
    });

    it("passes axe accessibility checks across settings panels", async () => {
      const { container: c1 } = renderWithClient(<PolicyPanel />);
      await waitFor(() => expect(screen.getByText("Strict Overdue Enforcement")).toBeInTheDocument());
      const r1 = await axe(c1);
      expect(r1).toHaveNoViolations();

      const { container: c2 } = renderWithClient(<KiosksPanel />);
      await waitFor(() => expect(screen.getByText("Registered Kiosk Terminals")).toBeInTheDocument());
      const r2 = await axe(c2);
      expect(r2).toHaveNoViolations();

      const { container: c3 } = renderWithClient(<TemplatesPanel />);
      await waitFor(() => expect(screen.getByText("Adhesive Label Sheet Layout")).toBeInTheDocument());
      const r3 = await axe(c3);
      expect(r3).toHaveNoViolations();
    });
  });
});
