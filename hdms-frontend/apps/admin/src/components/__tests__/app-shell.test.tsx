import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { currentAdminQueryKey } from "@/lib/auth";
import { ja } from "@/i18n/ja";
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
    expect(screen.getByText(ja.nav.groups.daily)).toBeInTheDocument();
    expect(screen.getByText(ja.nav.groups.records)).toBeInTheDocument();
    expect(screen.getByText(ja.nav.groups.insight)).toBeInTheDocument();

    // Group header & link both exist with text Settings
    const settingsElements = screen.getAllByText(ja.nav.settings);
    expect(settingsElements.length).toBeGreaterThanOrEqual(2);

    // Admin-specific items
    expect(screen.getByText(ja.nav.audit)).toBeInTheDocument();
    expect(screen.getByText(ja.nav.register)).toBeInTheDocument();
    expect(screen.getByText(ja.nav.backfill)).toBeInTheDocument();
    expect(screen.getByText(ja.nav.cardReaderTest)).toBeInTheDocument();
  });

  it("filters out settings and audit log for viewer role", () => {
    renderShell({
      id: "viewer-1",
      email: "viewer@hito.local",
      fullName: "Guest Viewer",
      role: "viewer",
    });

    // Dashboard & Records should be present
    expect(screen.getByText(ja.nav.dashboard)).toBeInTheDocument();
    expect(screen.getByText(ja.nav.devices)).toBeInTheDocument();
    expect(screen.getByText(ja.nav.users)).toBeInTheDocument();
    expect(screen.getByText(ja.nav.loans)).toBeInTheDocument();
    expect(screen.getByText(ja.nav.reports)).toBeInTheDocument();

    // Viewer should NOT see technician/admin items
    expect(screen.queryByText(ja.nav.backfill)).not.toBeInTheDocument();
    expect(screen.queryByText(ja.nav.register)).not.toBeInTheDocument();
    expect(screen.queryByText(ja.nav.audit)).not.toBeInTheDocument();
    expect(screen.queryByText(ja.nav.cardReaderTest)).not.toBeInTheDocument();
    // Settings group should not render at all for viewer
    expect(screen.queryByText(ja.nav.settings)).not.toBeInTheDocument();
  });

  it("does not render staging banner by default in development/production", () => {
    renderShell({
      id: "admin-1",
      email: "admin@hito.local",
      fullName: "System Admin",
      role: "admin",
    });
    expect(screen.queryByTestId("staging-banner")).not.toBeInTheDocument();
  });

  it("visibly displays staging banner when in staging environment", () => {
    vi.stubEnv("VITE_ENV", "staging");
    try {
      renderShell({
        id: "admin-1",
        email: "admin@hito.local",
        fullName: "System Admin",
        role: "admin",
      });
      const banner = screen.getByTestId("staging-banner");
      expect(banner).toBeInTheDocument();
      expect(banner).toHaveTextContent(ja.staging.banner);
    } finally {
      vi.unstubAllEnvs();
    }
  });
});
