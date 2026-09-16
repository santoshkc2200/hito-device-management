import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { LocaleProvider } from "@hdms/i18n";
import type { StaffMe } from "@hdms/api-client";
import * as apiClient from "@hdms/api-client";
import { createStaffRouter } from "@/router";
import { currentStaffQueryKey } from "@/lib/auth";

const defaultMe: StaffMe = {
  userId: "u1",
  employeeNo: "E-100",
  fullName: "Test Staff",
  departmentName: "Cardiology",
  status: "active",
  profileComplete: true,
  mustChangePassword: false,
  hasPassword: true,
  signInMethods: ["password"],
};

export function renderChangePassword({
  forced = false,
  me,
  initialPath = "/change-password",
}: {
  forced?: boolean;
  me?: Partial<StaffMe>;
  initialPath?: string;
} = {}) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  const resolvedMe: StaffMe = {
    ...defaultMe,
    mustChangePassword: forced,
    ...me,
  };
  queryClient.setQueryData(currentStaffQueryKey, resolvedMe);
  vi.spyOn(apiClient, "getStaffMe").mockResolvedValue({
    data: resolvedMe,
    error: undefined,
  } as any);

  const history = createMemoryHistory({
    initialEntries: [initialPath],
  });
  const testRouter = createStaffRouter(history, queryClient);

  return {
    ...render(
      <LocaleProvider locale="en">
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={testRouter} />
        </QueryClientProvider>
      </LocaleProvider>,
    ),
    router: testRouter,
    queryClient,
  };
}

export function renderSettings({
  me,
  initialPath = "/settings",
}: {
  me?: Partial<StaffMe>;
  initialPath?: string;
} = {}) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  const resolvedMe: StaffMe = {
    ...defaultMe,
    ...me,
  };
  queryClient.setQueryData(currentStaffQueryKey, resolvedMe);
  vi.spyOn(apiClient, "getStaffMe").mockResolvedValue({
    data: resolvedMe,
    error: undefined,
  } as any);

  const history = createMemoryHistory({
    initialEntries: [initialPath],
  });
  const testRouter = createStaffRouter(history, queryClient);

  return {
    ...render(
      <LocaleProvider locale="en">
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={testRouter} />
        </QueryClientProvider>
      </LocaleProvider>,
    ),
    router: testRouter,
    queryClient,
  };
}

describe("change password and settings routes", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("requires the current password and a new one of at least 12 characters", async () => {
    renderChangePassword();

    const currentPasswordInput = await screen.findByLabelText(/current password/i);
    await userEvent.type(currentPasswordInput, "temporary");
    await userEvent.type(screen.getByLabelText(/new password/i), "short");
    await userEvent.click(screen.getByRole("button", { name: /save/i }));

    expect(await screen.findByText(/at least 12 characters/i)).toBeInTheDocument();
  });

  it("explains, on a forced change, why it is being asked", async () => {
    renderChangePassword({ forced: true });
    expect(
      await screen.findByText(/this password was set for you — choose your own/i),
    ).toBeInTheDocument();
  });

  it("hides the password section entirely for a Microsoft-only account", async () => {
    renderSettings({ me: { hasPassword: false, signInMethods: ["microsoft"] } });
    await screen.findByText("Account");
    expect(screen.queryByRole("link", { name: /change password/i })).not.toBeInTheDocument();
  });

  it("renders the password section for accounts with a password", async () => {
    renderSettings({ me: { hasPassword: true, signInMethods: ["password"] } });
    expect(await screen.findByRole("link", { name: /change password/i })).toBeInTheDocument();
  });

  it("submits the password change and updates credentials", async () => {
    const changeSpy = vi.spyOn(apiClient, "changeStaffPassword").mockResolvedValue({
      data: undefined,
      error: undefined,
    } as any);

    renderChangePassword();

    const currentPasswordInput = await screen.findByLabelText(/current password/i);
    await userEvent.type(currentPasswordInput, "temporary-password");
    await userEvent.type(screen.getByLabelText(/new password/i), "a-much-longer-password");
    await userEvent.click(screen.getByRole("button", { name: /save/i }));

    expect(changeSpy).toHaveBeenCalledWith({
      body: {
        currentPassword: "temporary-password",
        newPassword: "a-much-longer-password",
      },
    });
  });

  it("renders account details on settings route", async () => {
    renderSettings({
      me: {
        fullName: "Dr. Alice Smith",
        employeeNo: "E-4242",
        departmentName: "Cardiology",
        signInMethods: ["password", "microsoft"],
      },
    });

    expect(await screen.findByText("Dr. Alice Smith")).toBeInTheDocument();
    expect(screen.getByText("E-4242")).toBeInTheDocument();
    expect(screen.getByText("Cardiology")).toBeInTheDocument();
    expect(screen.getByText(/Password, Microsoft account/)).toBeInTheDocument();
  });

  it("calls staffLogout and clears the query cache on sign out", async () => {
    const logoutSpy = vi.spyOn(apiClient, "staffLogout").mockResolvedValue({
      data: undefined,
      error: undefined,
    } as any);

    const { queryClient } = renderSettings();
    const signOutBtn = await screen.findByRole("button", { name: /sign out/i });
    await userEvent.click(signOutBtn);

    expect(logoutSpy).toHaveBeenCalled();
    expect(queryClient.getQueryData(currentStaffQueryKey)).toBeUndefined();
  });

  it("switches language between English and Japanese", async () => {
    renderSettings();
    expect(await screen.findByText("Account")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "日本語" }));
    expect(await screen.findByText("アカウント")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "English" }));
    expect(await screen.findByText("Account")).toBeInTheDocument();
  });
});
