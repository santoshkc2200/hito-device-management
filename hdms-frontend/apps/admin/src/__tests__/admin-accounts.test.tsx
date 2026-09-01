import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { translate } from "@hdms/i18n";
import { currentAdminQueryKey } from "@/lib/auth";
import { AdminAccountsPanel } from "@/components/admin-accounts-panel";
import { catalogues } from "@/i18n";
import { ja } from "@/i18n/ja";

// Mock TanStack Router
vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    useNavigate: () => vi.fn(),
    useRouter: () => ({ invalidate: vi.fn() }),
  };
});

const mockAdmins: apiClient.Admin[] = [
  {
    id: "admin-1",
    email: "admin@hito.local",
    fullName: "System Admin",
    role: "admin",
    status: "active",
    lastLoginAt: "2026-08-20T10:00:00Z",
  },
  {
    id: "admin-2",
    email: "tech@hito.local",
    fullName: "Lead Technician",
    role: "technician",
    status: "active",
    lastLoginAt: "2026-08-19T14:30:00Z",
  },
  {
    id: "admin-3",
    email: "locked@hito.local",
    fullName: "Locked Account",
    role: "technician",
    status: "locked",
    lockedUntil: "2026-08-21T12:00:00Z",
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

function renderPanel(currentUserRole: "admin" | "technician" | "viewer" = "admin") {
  const queryClient = createTestQueryClient();
  queryClient.setQueryData(currentAdminQueryKey, {
    id: "admin-1",
    email: "admin@hito.local",
    fullName: "System Admin",
    role: currentUserRole,
    status: "active",
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <AdminAccountsPanel />
    </QueryClientProvider>
  );
}

describe("4.1c Admin Accounts Management & Role Gating", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(apiClient, "listAdmins").mockResolvedValue({
      data: { items: mockAdmins },
      error: undefined,
    } as any);
  });

  it("renders admin list with roles and statuses", async () => {
    renderPanel("admin");

    expect(await screen.findByText("System Admin")).toBeInTheDocument();
    expect(screen.getByText("Lead Technician")).toBeInTheDocument();
    expect(screen.getByText("Locked Account")).toBeInTheDocument();

    expect(screen.getByText(ja.adminAccountsPanel.statusLocked)).toBeInTheDocument();
  });

  it("admin role sees 'Add administrator' button and action menus", async () => {
    renderPanel("admin");

    expect(
      await screen.findByRole("button", { name: ja.adminAccountsPanel.addAdministrator })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", {
        name: translate(catalogues, "ja", "adminAccountsPanel.actionsForAria", {
          name: "System Admin",
        }),
      })
    ).toBeInTheDocument();
  });

  it("viewer role cannot see 'Add administrator' button or action menus", async () => {
    renderPanel("viewer");

    expect(await screen.findByText("System Admin")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: ja.adminAccountsPanel.addAdministrator })
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", {
        name: translate(catalogues, "ja", "adminAccountsPanel.actionsForAria", {
          name: "System Admin",
        }),
      })
    ).not.toBeInTheDocument();
  });

  it("creates a new admin and presents enrolment QR code and recovery codes", async () => {
    const createSpy = vi.spyOn(apiClient, "createAdmin").mockResolvedValueOnce({
      data: {
        admin: {
          id: "admin-new",
          email: "newadmin@hito.local",
          fullName: "New Administrator",
          role: "admin",
          status: "active",
        },
        enrolment: {
          otpauthUrl: "otpauth://totp/HitoHospital:newadmin?secret=NEWSECRET123",
          totpSecret: "NEWSECRET123",
        },
        recoveryCodes: {
          codes: ["REC-1111", "REC-2222", "REC-3333", "REC-4444"],
        },
      },
      error: undefined,
    } as any);

    renderPanel("admin");

    const user = userEvent.setup();
    await user.click(
      await screen.findByRole("button", { name: ja.adminAccountsPanel.addAdministrator })
    );

    expect(
      screen.getByRole("heading", { name: ja.adminAccountsPanel.addAdministrator })
    ).toBeInTheDocument();

    await user.type(screen.getByLabelText(ja.userDetail.fullNameLabel), "New Administrator");
    await user.type(screen.getByLabelText(ja.userDetail.emailLabel), "newadmin@hito.local");
    await user.type(
      screen.getByLabelText(ja.adminAccountsPanel.initialPasswordLabel),
      "SecureInitialPassword123!"
    );

    await user.click(screen.getByRole("button", { name: ja.adminAccountsPanel.createAccount }));

    await waitFor(() => {
      expect(createSpy).toHaveBeenCalledWith({
        body: {
          fullName: "New Administrator",
          email: "newadmin@hito.local",
          role: "technician",
          password: "SecureInitialPassword123!",
        },
      });
    });

    // Enrolment result dialog is shown with QR secret & recovery codes
    expect(
      await screen.findByText(ja.adminAccountsPanel.credentialsCreatedTitle)
    ).toBeInTheDocument();
    expect(screen.getByText("NEWSECRET123")).toBeInTheDocument();
    expect(screen.getByText("REC-1111")).toBeInTheDocument();
    expect(screen.getByText("REC-4444")).toBeInTheDocument();
  });

  it("resets password with mandatory audit reason", async () => {
    const resetSpy = vi.spyOn(apiClient, "resetAdminPassword").mockResolvedValueOnce({
      data: undefined,
      error: undefined,
    } as any);

    renderPanel("admin");

    const user = userEvent.setup();
    await user.click(
      await screen.findByRole("button", {
        name: translate(catalogues, "ja", "adminAccountsPanel.actionsForAria", {
          name: "Lead Technician",
        }),
      })
    );
    await user.click(
      screen.getByRole("menuitem", { name: ja.adminAccountsPanel.resetPasswordAction })
    );

    expect(
      screen.getByRole("heading", { name: ja.adminAccountsPanel.resetPasswordAction })
    ).toBeInTheDocument();

    await user.type(
      screen.getByLabelText(ja.forcedPasswordChangeDialog.newPasswordLabel),
      "BrandNewPassword123!"
    );
    await user.type(
      screen.getByLabelText(ja.adminAccountsPanel.auditReasonLabel),
      "Employee requested reset after lockout"
    );

    await user.click(
      screen.getByRole("button", { name: ja.adminAccountsPanel.resetPasswordAction })
    );

    await waitFor(() => {
      expect(resetSpy).toHaveBeenCalledWith({
        path: { id: "admin-2" },
        body: {
          password: "BrandNewPassword123!",
          reason: "Employee requested reset after lockout",
        },
      });
    });
  });

  it("forces TOTP re-enrolment with mandatory audit reason", async () => {
    const forceSpy = vi.spyOn(apiClient, "forceAdminTotpReenrolment").mockResolvedValueOnce({
      data: {
        otpauthUrl: "otpauth://totp/HitoHospital:tech?secret=RESETSECRET456",
        totpSecret: "RESETSECRET456",
      },
      error: undefined,
    } as any);

    renderPanel("admin");

    const user = userEvent.setup();
    await user.click(
      await screen.findByRole("button", {
        name: translate(catalogues, "ja", "adminAccountsPanel.actionsForAria", {
          name: "Lead Technician",
        }),
      })
    );
    await user.click(screen.getByRole("menuitem", { name: ja.adminAccountsPanel.forceTotpAction }));

    expect(
      screen.getByRole("heading", { name: ja.adminAccountsPanel.forceTotpAction })
    ).toBeInTheDocument();

    await user.type(
      screen.getByLabelText(ja.adminAccountsPanel.auditReasonLabel),
      "Replacement mobile phone issued"
    );
    await user.click(
      screen.getByRole("button", { name: ja.adminAccountsPanel.forceReenrolment })
    );

    await waitFor(() => {
      expect(forceSpy).toHaveBeenCalledWith({
        path: { id: "admin-2" },
        body: {
          reason: "Replacement mobile phone issued",
        },
      });
    });

    expect(
      await screen.findByText(
        translate(catalogues, "ja", "adminAccountsPanel.newTotpSecretTitle", {
          name: "Lead Technician",
        })
      )
    ).toBeInTheDocument();
    expect(screen.getByText("RESETSECRET456")).toBeInTheDocument();
  });

  it("unlocks a locked account with mandatory audit reason", async () => {
    const unlockSpy = vi.spyOn(apiClient, "unlockAdmin").mockResolvedValueOnce({
      data: undefined,
      error: undefined,
    } as any);

    renderPanel("admin");

    const user = userEvent.setup();
    await user.click(
      await screen.findByRole("button", {
        name: translate(catalogues, "ja", "adminAccountsPanel.actionsForAria", {
          name: "Locked Account",
        }),
      })
    );
    await user.click(
      screen.getByRole("menuitem", { name: ja.adminAccountsPanel.unlockAccountAction })
    );

    expect(
      screen.getByRole("heading", { name: ja.adminAccountsPanel.unlockAccountTitle })
    ).toBeInTheDocument();

    await user.type(
      screen.getByLabelText(ja.adminAccountsPanel.auditReasonLabel),
      "Identity verified in person at helpdesk"
    );
    await user.click(
      screen.getByRole("button", { name: ja.adminAccountsPanel.unlockAccountAction })
    );

    await waitFor(() => {
      expect(unlockSpy).toHaveBeenCalledWith({
        path: { id: "admin-3" },
        body: {
          reason: "Identity verified in person at helpdesk",
        },
      });
    });
  });
});
