import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { currentAdminQueryKey } from "@/lib/auth";
import { AppShell } from "../app-shell";

// Mock @tanstack/react-router
vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children, className }: any) => (
    <a href={to} className={className}>
      {children}
    </a>
  ),
  Outlet: () => <div data-testid="outlet" />,
  useNavigate: () => vi.fn(),
  useRouter: () => ({ invalidate: vi.fn() }),
}));

function renderShell(adminData?: { id: string; email: string; fullName: string; role: any }) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  if (adminData) {
    queryClient.setQueryData(currentAdminQueryKey, adminData);
  }

  return render(
    <QueryClientProvider client={queryClient}>
      <AppShell />
    </QueryClientProvider>
  );
}

describe("AppShell Navigation & Role Visibility", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders full navigation groups for admin role", () => {
    renderShell({
      id: "admin-1",
      email: "admin@hito.local",
      fullName: "System Admin",
      role: "admin",
    });

    // Group headers
    expect(screen.getByText("Daily")).toBeInTheDocument();
    expect(screen.getByText("Records")).toBeInTheDocument();
    expect(screen.getByText("Insight")).toBeInTheDocument();

    // Group header & link both exist with text Settings
    const settingsElements = screen.getAllByText("Settings");
    expect(settingsElements.length).toBeGreaterThanOrEqual(2);

    // Admin-specific items
    expect(screen.getByText("Audit log")).toBeInTheDocument();
    expect(screen.getByText("Register borrower")).toBeInTheDocument();
    expect(screen.getByText("Paper backfill")).toBeInTheDocument();
    expect(screen.getByText("Card reader test")).toBeInTheDocument();
  });

  it("filters out settings and audit log for viewer role", () => {
    renderShell({
      id: "viewer-1",
      email: "viewer@hito.local",
      fullName: "Guest Viewer",
      role: "viewer",
    });

    // Dashboard & Records should be present
    expect(screen.getByText("Dashboard")).toBeInTheDocument();
    expect(screen.getByText("Devices")).toBeInTheDocument();
    expect(screen.getByText("Users")).toBeInTheDocument();
    expect(screen.getByText("Loans")).toBeInTheDocument();
    expect(screen.getByText("Reports")).toBeInTheDocument();

    // Viewer should NOT see technician/admin items
    expect(screen.queryByText("Paper backfill")).not.toBeInTheDocument();
    expect(screen.queryByText("Register borrower")).not.toBeInTheDocument();
    expect(screen.queryByText("Audit log")).not.toBeInTheDocument();
    expect(screen.queryByText("Card reader test")).not.toBeInTheDocument();
    // Settings group should not render at all for viewer
    expect(screen.queryByText("Settings")).not.toBeInTheDocument();
  });
});
