import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { LoginPage } from "@/routes/login";
import { ReauthDialog } from "@/components/reauth-dialog";
import { RecoveryCodesDialog } from "@/components/recovery-codes-dialog";
import { ForcedPasswordChangeDialog } from "@/components/forced-password-change-dialog";
import { ForcedTotpDialog } from "@/components/forced-totp-dialog";
import { AdminAccountsPanel } from "@/components/admin-accounts-panel";
import { currentAdminQueryKey } from "@/lib/auth";
import * as apiClient from "@hdms/api-client";

// Mock TanStack Router
vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    useNavigate: () => vi.fn(),
    useRouter: () => ({ invalidate: vi.fn() }),
  };
});

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
}

describe("4.1c Auth UI — Accessibility (axe) Audits", () => {
  it("LoginPage in TOTP mode passes axe audit", async () => {
    const queryClient = createTestQueryClient();
    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <LoginPage />
      </QueryClientProvider>
    );

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("ReauthDialog passes axe audit", async () => {
    const queryClient = createTestQueryClient();
    queryClient.setQueryData(currentAdminQueryKey, {
      id: "admin-1",
      email: "admin@hito.local",
      fullName: "System Admin",
      role: "admin",
    });

    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <ReauthDialog />
      </QueryClientProvider>
    );

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("RecoveryCodesDialog passes axe audit", async () => {
    const { container } = render(
      <RecoveryCodesDialog
        open={true}
        codes={["REC-1111", "REC-2222", "REC-3333", "REC-4444"]}
        onDismiss={() => {}}
      />
    );

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("ForcedPasswordChangeDialog passes axe audit", async () => {
    const queryClient = createTestQueryClient();
    queryClient.setQueryData(currentAdminQueryKey, {
      id: "admin-1",
      email: "admin@hito.local",
      fullName: "System Admin",
      role: "admin",
      mustChangePassword: true,
    });

    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <ForcedPasswordChangeDialog />
      </QueryClientProvider>
    );

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("ForcedTotpDialog passes axe audit", async () => {
    vi.spyOn(apiClient, "beginTotpReenrolment").mockResolvedValueOnce({
      data: {
        otpauthUrl: "otpauth://totp/HitoHospital:admin?secret=JBSWY3DPEHPK3PXP",
        totpSecret: "JBSWY3DPEHPK3PXP",
      },
      error: undefined,
    } as any);

    const queryClient = createTestQueryClient();
    queryClient.setQueryData(currentAdminQueryKey, {
      id: "admin-1",
      email: "admin@hito.local",
      fullName: "System Admin",
      role: "admin",
      mustReenrolTotp: true,
    });

    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <ForcedTotpDialog />
      </QueryClientProvider>
    );

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("AdminAccountsPanel passes axe audit", async () => {
    vi.spyOn(apiClient, "listAdmins").mockResolvedValueOnce({
      data: {
        items: [
          {
            id: "admin-1",
            email: "admin@hito.local",
            fullName: "System Admin",
            role: "admin",
            status: "active",
            lastLoginAt: "2026-08-20T10:00:00Z",
          },
        ],
      },
      error: undefined,
    } as any);

    const queryClient = createTestQueryClient();
    queryClient.setQueryData(currentAdminQueryKey, {
      id: "admin-1",
      email: "admin@hito.local",
      fullName: "System Admin",
      role: "admin",
      status: "active",
    });

    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <AdminAccountsPanel />
      </QueryClientProvider>
    );

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
