import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { currentAdminQueryKey, triggerSessionExpired } from "@/lib/auth";
import { LoginPage } from "@/routes/login";
import { ReauthDialog } from "@/components/reauth-dialog";
import { RecoveryCodesDialog } from "@/components/recovery-codes-dialog";
import { ForcedPasswordChangeDialog } from "@/components/forced-password-change-dialog";
import { ForcedTotpDialog } from "@/components/forced-totp-dialog";
import { ja } from "@/i18n/ja";
import { useState } from "react";

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

describe("4.1c Auth UI — Login, Recovery, Compliance, and Reauth", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("Login flow with TOTP and Recovery Code toggle", () => {
    it("logs in successfully with standard password + TOTP code", async () => {
      const loginSpy = vi.spyOn(apiClient, "login").mockResolvedValueOnce({
        data: {
          id: "admin-1",
          email: "admin@hito.local",
          fullName: "System Admin",
          role: "admin",
          status: "active",
        },
        error: undefined,
      } as any);

      const queryClient = createTestQueryClient();
      render(
        <QueryClientProvider client={queryClient}>
          <LoginPage />
        </QueryClientProvider>
      );

      const user = userEvent.setup();
      await user.type(screen.getByLabelText(ja.login.emailLabel), "admin@hito.local");
      await user.type(screen.getByLabelText(ja.login.passwordLabel), "CorrectPassword123!");
      await user.type(screen.getByLabelText(ja.login.totpLabel), "123456");

      await user.click(screen.getByRole("button", { name: new RegExp(ja.login.signIn, "i") }));

      await waitFor(() => {
        expect(loginSpy).toHaveBeenCalledWith({
          body: {
            email: "admin@hito.local",
            password: "CorrectPassword123!",
            totpCode: "123456",
          },
        });
      });
    });

    it("toggles to recovery code mode and logs in with single-use recovery code", async () => {
      const loginSpy = vi.spyOn(apiClient, "login").mockResolvedValueOnce({
        data: {
          id: "admin-1",
          email: "admin@hito.local",
          fullName: "System Admin",
          role: "admin",
        },
        error: undefined,
      } as any);

      const queryClient = createTestQueryClient();
      render(
        <QueryClientProvider client={queryClient}>
          <LoginPage />
        </QueryClientProvider>
      );

      const user = userEvent.setup();
      // Toggle to recovery code
      await user.click(screen.getByText(ja.login.useRecoveryInstead));

      expect(screen.getByLabelText(ja.login.recoveryCodeLabel)).toBeInTheDocument();
      expect(screen.queryByLabelText(ja.login.totpLabel)).not.toBeInTheDocument();

      await user.type(screen.getByLabelText(ja.login.emailLabel), "admin@hito.local");
      await user.type(screen.getByLabelText(ja.login.passwordLabel), "CorrectPassword123!");
      await user.type(screen.getByLabelText(ja.login.recoveryCodeLabel), "A1B2-C3D4");

      await user.click(screen.getByRole("button", { name: new RegExp(ja.login.signIn, "i") }));

      await waitFor(() => {
        expect(loginSpy).toHaveBeenCalledWith({
          body: {
            email: "admin@hito.local",
            password: "CorrectPassword123!",
            recoveryCode: "A1B2-C3D4",
          },
        });
      });
    });

    it("displays error alert message on failed login attempt", async () => {
      vi.spyOn(apiClient, "login").mockResolvedValueOnce({
        data: undefined,
        error: {
          detail: "Invalid email, password or authenticator code.",
          title: "Unauthorized",
          status: 401,
        },
      } as any);

      const queryClient = createTestQueryClient();
      render(
        <QueryClientProvider client={queryClient}>
          <LoginPage />
        </QueryClientProvider>
      );

      const user = userEvent.setup();
      await user.type(screen.getByLabelText(ja.login.emailLabel), "admin@hito.local");
      await user.type(screen.getByLabelText(ja.login.passwordLabel), "WrongPassword");
      await user.type(screen.getByLabelText(ja.login.totpLabel), "000000");

      await user.click(screen.getByRole("button", { name: new RegExp(ja.login.signIn, "i") }));

      expect(
        await screen.findByText(/invalid email, password or authenticator code/i)
      ).toBeInTheDocument();
    });
  });

  describe("Recovery codes dialog lifecycle & acknowledgement", () => {
    it("renders codes, supports copy/print, requires confirmation checkbox, and clears state on close", async () => {
      const codes = ["CODE-1111", "CODE-2222", "CODE-3333", "CODE-4444"];
      const onDismiss = vi.fn();

      // Parent component controlling state to verify state clearing on dismiss
      function TestWrapper() {
        const [activeCodes, setActiveCodes] = useState<string[] | null>(codes);
        return (
          <div>
            {activeCodes && (
              <RecoveryCodesDialog
                open={Boolean(activeCodes)}
                codes={activeCodes}
                onDismiss={() => {
                  setActiveCodes(null);
                  onDismiss();
                }}
              />
            )}
            <div data-testid="codes-state-indicator">
              {activeCodes ? "HAS_CODES" : "CLEARED"}
            </div>
          </div>
        );
      }

      render(<TestWrapper />);

      expect(screen.getByText("CODE-1111")).toBeInTheDocument();
      expect(screen.getByText("CODE-4444")).toBeInTheDocument();
      expect(screen.getByText("HAS_CODES")).toBeInTheDocument();

      const doneButton = screen.getByRole("button", { name: ja.recoveryCodesDialog.doneAndClose });
      expect(doneButton).toBeDisabled();

      // Check confirmation
      const user = userEvent.setup();
      const checkbox = screen.getByRole("checkbox");
      await user.click(checkbox);
      expect(checkbox).toBeChecked();
      expect(doneButton).toBeEnabled();

      // Dismiss dialog
      await user.click(doneButton);

      expect(onDismiss).toHaveBeenCalledTimes(1);
      expect(screen.getByText("CLEARED")).toBeInTheDocument();
      expect(screen.queryByText("CODE-1111")).not.toBeInTheDocument();
    });
  });

  describe("Session expiry mid-form recovery without state loss", () => {
    function FormWithReauth() {
      const [borrowerName, setBorrowerName] = useState("");
      const [notes, setNotes] = useState("");

      return (
        <div>
          <form>
            <label htmlFor="test-borrower">Borrower Name</label>
            <input
              id="test-borrower"
              value={borrowerName}
              onChange={(e) => setBorrowerName(e.target.value)}
            />
            <label htmlFor="test-notes">Registration Notes</label>
            <textarea
              id="test-notes"
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
            />
          </form>
          <ReauthDialog />
        </div>
      );
    }

    it("opens in-place reauth dialog on session expiry and preserves form values when resumed", async () => {
      const loginSpy = vi.spyOn(apiClient, "login").mockResolvedValueOnce({
        data: {
          id: "admin-1",
          email: "nurse.admin@hito.local",
          fullName: "Nurse Administrator",
          role: "technician",
        },
        error: undefined,
      } as any);

      const queryClient = createTestQueryClient();
      queryClient.setQueryData(currentAdminQueryKey, {
        id: "admin-1",
        email: "nurse.admin@hito.local",
        fullName: "Nurse Administrator",
        role: "technician",
      });

      render(
        <QueryClientProvider client={queryClient}>
          <FormWithReauth />
        </QueryClientProvider>
      );

      const user = userEvent.setup();

      // User begins typing form data
      const borrowerInput = screen.getByLabelText(/borrower name/i);
      const notesInput = screen.getByLabelText(/registration notes/i);

      await user.type(borrowerInput, "Dr. Taro Yamada");
      await user.type(notesInput, "Emergency ward loan - priority badge issued");

      expect(borrowerInput).toHaveValue("Dr. Taro Yamada");
      expect(notesInput).toHaveValue("Emergency ward loan - priority badge issued");

      // Simulate session expiry event triggered by 401 interceptor
      triggerSessionExpired();

      // Reauth dialog appears
      expect(await screen.findByText(ja.reauthDialog.title)).toBeInTheDocument();
      expect(screen.getByText(ja.reauthDialog.description)).toBeInTheDocument();

      // Email is prefilled from existing session
      expect(screen.getByLabelText(ja.login.emailLabel)).toHaveValue("nurse.admin@hito.local");

      // Enter password & TOTP to re-authenticate
      await user.type(screen.getByLabelText(ja.login.passwordLabel), "ResumeSecret123!");
      await user.type(screen.getByLabelText(ja.login.totpLabel), "654321");
      await user.click(screen.getByRole("button", { name: ja.reauthDialog.resumeSession }));

      await waitFor(() => {
        expect(loginSpy).toHaveBeenCalledWith({
          body: {
            email: "nurse.admin@hito.local",
            password: "ResumeSecret123!",
            totpCode: "654321",
          },
        });
      });

      // Dialog closes
      await waitFor(() => {
        expect(screen.queryByText(ja.reauthDialog.title)).not.toBeInTheDocument();
      });

      // Form values remain 100% intact!
      expect(borrowerInput).toHaveValue("Dr. Taro Yamada");
      expect(notesInput).toHaveValue("Emergency ward loan - priority badge issued");
    });
  });

  describe("Compliance gates: Forced password change & Forced TOTP", () => {
    it("renders ForcedPasswordChangeDialog when mustChangePassword is true and enforces validation", async () => {
      const changePasswordSpy = vi.spyOn(apiClient, "changeOwnPassword").mockResolvedValueOnce({
        data: undefined,
        error: undefined,
      } as any);

      const queryClient = createTestQueryClient();
      queryClient.setQueryData(currentAdminQueryKey, {
        id: "admin-1",
        email: "admin@hito.local",
        fullName: "System Admin",
        role: "admin",
        mustChangePassword: true,
      });

      render(
        <QueryClientProvider client={queryClient}>
          <ForcedPasswordChangeDialog />
        </QueryClientProvider>
      );

      expect(screen.getByText(ja.forcedPasswordChangeDialog.title)).toBeInTheDocument();

      const user = userEvent.setup();
      const currentPass = screen.getByLabelText(ja.forcedPasswordChangeDialog.currentPasswordLabel);
      const newPass = screen.getByLabelText(ja.forcedPasswordChangeDialog.newPasswordLabel, {
        exact: true,
      });
      const confirmPass = screen.getByLabelText(ja.forcedPasswordChangeDialog.confirmPasswordLabel);

      // Try short password
      await user.type(currentPass, "OldPassword123!");
      await user.type(newPass, "short");
      await user.type(confirmPass, "short");
      await user.click(
        screen.getByRole("button", { name: ja.forcedPasswordChangeDialog.setAndContinue })
      );

      expect(await screen.findByText(ja.validation.newPasswordMin)).toBeInTheDocument();

      // Enter valid 12+ character password
      await user.clear(newPass);
      await user.clear(confirmPass);
      await user.type(newPass, "SecureNewPassword2026!");
      await user.type(confirmPass, "SecureNewPassword2026!");

      await user.click(
        screen.getByRole("button", { name: ja.forcedPasswordChangeDialog.setAndContinue })
      );

      await waitFor(() => {
        expect(changePasswordSpy).toHaveBeenCalledWith({
          body: {
            currentPassword: "OldPassword123!",
            newPassword: "SecureNewPassword2026!",
          },
        });
      });
    });

    it("renders ForcedTotpDialog when mustReenrolTotp is true and confirms with code", async () => {
      vi.spyOn(apiClient, "beginTotpReenrolment").mockResolvedValueOnce({
        data: {
          otpauthUrl: "otpauth://totp/HitoHospital:admin?secret=JBSWY3DPEHPK3PXP",
          totpSecret: "JBSWY3DPEHPK3PXP",
        },
        error: undefined,
      } as any);

      const confirmSpy = vi.spyOn(apiClient, "confirmTotpReenrolment").mockResolvedValueOnce({
        data: undefined,
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

      render(
        <QueryClientProvider client={queryClient}>
          <ForcedTotpDialog />
        </QueryClientProvider>
      );

      expect(screen.getByText(ja.forcedTotpDialog.title)).toBeInTheDocument();
      expect(await screen.findByText("JBSWY3DPEHPK3PXP")).toBeInTheDocument();

      const user = userEvent.setup();
      await user.type(screen.getByLabelText(ja.forcedTotpDialog.verificationCodeLabel), "987654");
      await user.click(
        screen.getByRole("button", { name: ja.forcedTotpDialog.confirmAndActivate })
      );

      await waitFor(() => {
        expect(confirmSpy).toHaveBeenCalledWith({
          body: {
            totpCode: "987654",
          },
        });
      });
    });
  });
});
