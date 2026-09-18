import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as apiClient from "@hdms/api-client";
import { toast } from "sonner";
import { OverdueLoansTable } from "@/components/dashboard/overdue-loans-table";
import { ja } from "@/i18n/ja";

// Mock TanStack Router
vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    useNavigate: () => vi.fn(),
    useRouter: () => ({ invalidate: vi.fn() }),
    Link: ({ children, to, ...props }: any) => (
      <a href={to} {...props}>
        {children}
      </a>
    ),
  };
});

const mockLoan: apiClient.OverdueLoanSummary = {
  loanId: "test-loan-1",
  deviceId: "dev-1",
  assetTag: "ASSET-101",
  deviceName: "Ultrasound Probe",
  userId: "user-1",
  userFullName: "Nurse Tanaka",
  userEmployeeNo: "EMP-001",
  dueAt: "2026-09-18T10:00:00Z",
  daysOverdue: 2,
};

function renderComponent() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <OverdueLoansTable loans={[mockLoan]} />
    </QueryClientProvider>
  );
}

describe("Remind button outcomes (Slice 6.2e)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("handles outcome: 'sent' with success toast", async () => {
    const user = userEvent.setup();
    const successSpy = vi.spyOn(toast, "success");
    vi.spyOn(apiClient, "remindLoan").mockResolvedValue({
      data: { outcome: "sent", deliveryId: "del-1" },
    } as any);

    renderComponent();

    const remindBtn = screen.getByRole("button", { name: new RegExp(ja.dashboard.overdue.remindButton, "i") });
    await user.click(remindBtn);

    await waitFor(() => {
      expect(successSpy).toHaveBeenCalledWith(ja.dashboard.overdue.remindSuccess);
    });
  });

  it("handles outcome: 'queued_quiet_hours' with info toast", async () => {
    const user = userEvent.setup();
    const infoSpy = vi.spyOn(toast, "info");
    vi.spyOn(apiClient, "remindLoan").mockResolvedValue({
      data: { outcome: "queued_quiet_hours", deliveryId: "del-2" },
    } as any);

    renderComponent();

    const remindBtn = screen.getByRole("button", { name: new RegExp(ja.dashboard.overdue.remindButton, "i") });
    await user.click(remindBtn);

    await waitFor(() => {
      expect(infoSpy).toHaveBeenCalledWith(ja.dashboard.overdue.remindQueuedQuietHours);
    });
  });

  it("handles outcome: 'refused' with warning toast including reason", async () => {
    const user = userEvent.setup();
    const warningSpy = vi.spyOn(toast, "warning");
    vi.spyOn(apiClient, "remindLoan").mockResolvedValue({
      data: { outcome: "refused", reason: "loan is disputed, notifications suppressed" },
    } as any);

    renderComponent();

    const remindBtn = screen.getByRole("button", { name: new RegExp(ja.dashboard.overdue.remindButton, "i") });
    await user.click(remindBtn);

    await waitFor(() => {
      expect(warningSpy).toHaveBeenCalledWith(
        expect.stringContaining("loan is disputed, notifications suppressed")
      );
    });
  });

  it("handles HTTP 429 rate limit with rate-limited error toast", async () => {
    const user = userEvent.setup();
    const errorSpy = vi.spyOn(toast, "error");
    vi.spyOn(apiClient, "remindLoan").mockResolvedValue({
      error: { title: "Too Many Requests", status: 429, detail: "Rate limit exceeded" },
      response: { status: 429 },
    } as any);

    renderComponent();

    const remindBtn = screen.getByRole("button", { name: new RegExp(ja.dashboard.overdue.remindButton, "i") });
    await user.click(remindBtn);

    await waitFor(() => {
      expect(errorSpy).toHaveBeenCalledWith(ja.dashboard.overdue.remindRateLimited);
    });
  });
});
