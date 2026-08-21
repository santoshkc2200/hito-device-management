import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { currentAdminQueryKey } from "@/lib/auth";
import { AdminAccountsPanel } from "@/components/admin-accounts-panel";

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

    expect(screen.getByText("Locked")).toBeInTheDocument();
  });

  it("admin role sees 'Add administrator' button and action menus", async () => {
    renderPanel("admin");

    expect(await screen.findByRole("button", { name: /add administrator/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /actions for system admin/i })).toBeInTheDocument();
  });

  it("viewer role cannot see 'Add administrator' button or action menus", async () => {
    renderPanel("viewer");

    expect(await screen.findByText("System Admin")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /add administrator/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /actions for system admin/i })).not.toBeInTheDocument();
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
    await user.click(await screen.findByRole("button", { name: /add administrator/i }));

    expect(screen.getByRole("heading", { name: /add administrator/i })).toBeInTheDocument();

    await user.type(screen.getByLabelText(/full name/i), "New Administrator");
    await user.type(screen.getByLabelText(/email/i), "newadmin@hito.local");
    await user.type(screen.getByLabelText(/initial password/i), "SecureInitialPassword123!");

    await user.click(screen.getByRole("button", { name: /create account/i }));

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
    expect(await screen.findByText(/administrator credentials created/i)).toBeInTheDocument();
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
    await user.click(await screen.findByRole("button", { name: /actions for lead technician/i }));
    await user.click(screen.getByRole("menuitem", { name: /reset password/i }));

    expect(screen.getByRole("heading", { name: /reset password/i })).toBeInTheDocument();

    await user.type(screen.getByLabelText(/new password/i), "BrandNewPassword123!");
    await user.type(screen.getByLabelText(/audit reason/i), "Employee requested reset after lockout");

    await user.click(screen.getByRole("button", { name: /^reset password$/i }));

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
    await user.click(await screen.findByRole("button", { name: /actions for lead technician/i }));
    await user.click(screen.getByRole("menuitem", { name: /force totp re-enrolment/i }));

    expect(screen.getByRole("heading", { name: /force totp re-enrolment/i })).toBeInTheDocument();

    await user.type(screen.getByLabelText(/audit reason/i), "Replacement mobile phone issued");
    await user.click(screen.getByRole("button", { name: /force re-enrolment/i }));

    await waitFor(() => {
      expect(forceSpy).toHaveBeenCalledWith({
        path: { id: "admin-2" },
        body: {
          reason: "Replacement mobile phone issued",
        },
      });
    });

    expect(await screen.findByText(/new totp secret for lead technician/i)).toBeInTheDocument();
    expect(screen.getByText("RESETSECRET456")).toBeInTheDocument();
  });

  it("unlocks a locked account with mandatory audit reason", async () => {
    const unlockSpy = vi.spyOn(apiClient, "unlockAdmin").mockResolvedValueOnce({
      data: undefined,
      error: undefined,
    } as any);

    renderPanel("admin");

    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /actions for locked account/i }));
    await user.click(screen.getByRole("menuitem", { name: /unlock account/i }));

    expect(screen.getByRole("heading", { name: /unlock administrator account/i })).toBeInTheDocument();

    await user.type(screen.getByLabelText(/audit reason/i), "Identity verified in person at helpdesk");
    await user.click(screen.getByRole("button", { name: /unlock account/i }));

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
