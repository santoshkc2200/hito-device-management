import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { LocaleProvider } from "@hdms/i18n";
import type { StaffMe } from "@hdms/api-client";
import * as apiClient from "@hdms/api-client";
import { createStaffRouter } from "@/router";
import { currentStaffQueryKey } from "@/lib/auth";

function renderApp({
  me,
  initialPath = "/",
}: {
  me: StaffMe;
  initialPath?: string;
}) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  queryClient.setQueryData(currentStaffQueryKey, me);

  vi.spyOn(apiClient, "getStaffMe").mockResolvedValue({
    data: me,
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

describe("first-run profile completion and session guard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("sends a signed-in user with an incomplete profile to the completion screen", async () => {
    const me: StaffMe = {
      userId: "u1",
      employeeNo: "MS-AB12CD34",
      fullName: "New Person",
      status: "active",
      profileComplete: false,
      mustChangePassword: false,
      hasPassword: false,
      signInMethods: ["microsoft"],
    };
    renderApp({ me, initialPath: "/devices" });

    expect(await screen.findByRole("heading", { name: /one more thing/i })).toBeInTheDocument();
    expect(screen.getByLabelText(/employee number/i)).toBeInTheDocument();
  });

  it("explains why the employee number is needed rather than just demanding it", async () => {
    const me: StaffMe = {
      userId: "u1",
      employeeNo: "MS-AB12CD34",
      fullName: "New Person",
      status: "active",
      profileComplete: false,
      mustChangePassword: false,
      hasPassword: false,
      signInMethods: ["microsoft"],
    };
    renderApp({ me, initialPath: "/" });

    expect(await screen.findByText(/your qr code is created once we have it/i)).toBeInTheDocument();
  });

  it("submits the updated employee number, invalidates me, and navigates home", async () => {
    const me: StaffMe = {
      userId: "u1",
      employeeNo: "MS-AB12CD34",
      fullName: "New Person",
      status: "active",
      profileComplete: false,
      mustChangePassword: false,
      hasPassword: false,
      signInMethods: ["microsoft"],
    };
    const completeSpy = vi.spyOn(apiClient, "completeStaffProfile").mockResolvedValue({
      data: { ...me, employeeNo: "E-9999", profileComplete: true },
      error: undefined,
    } as any);

    renderApp({ me, initialPath: "/" });

    const input = await screen.findByLabelText(/employee number/i);
    await userEvent.clear(input);
    await userEvent.type(input, "E-9999");
    await userEvent.click(screen.getByRole("button", { name: /complete/i }));

    expect(completeSpy).toHaveBeenCalledWith({
      body: { employeeNo: "E-9999" },
    });
  });

  it("shows an error alert when employee number submission fails", async () => {
    const me: StaffMe = {
      userId: "u1",
      employeeNo: "MS-AB12CD34",
      fullName: "New Person",
      status: "active",
      profileComplete: false,
      mustChangePassword: false,
      hasPassword: false,
      signInMethods: ["microsoft"],
    };
    vi.spyOn(apiClient, "completeStaffProfile").mockResolvedValue({
      data: undefined,
      error: { status: 409, title: "Employee number already in use" },
    } as any);

    renderApp({ me, initialPath: "/" });

    const input = await screen.findByLabelText(/employee number/i);
    await userEvent.clear(input);
    await userEvent.type(input, "E-TAKEN");
    await userEvent.click(screen.getByRole("button", { name: /complete/i }));

    expect(await screen.findByRole("alert")).toBeInTheDocument();
  });

  it("sends a signed-in user who must change their password to the password change screen", async () => {
    const me: StaffMe = {
      userId: "u1",
      employeeNo: "E-100",
      fullName: "Test Staff",
      status: "active",
      profileComplete: true,
      mustChangePassword: true,
      hasPassword: true,
      signInMethods: ["password"],
    };
    renderApp({ me, initialPath: "/devices" });

    expect(await screen.findByText("Change Password")).toBeInTheDocument();
  });

  it("sends an unauthenticated user to the login screen", async () => {
    vi.spyOn(apiClient, "getStaffMe").mockResolvedValue({
      data: undefined,
      error: { status: 401, title: "Unauthorized" },
    } as any);

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });

    const history = createMemoryHistory({
      initialEntries: ["/"],
    });

    const testRouter = createStaffRouter(history, queryClient);

    render(
      <LocaleProvider locale="en">
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={testRouter} />
        </QueryClientProvider>
      </LocaleProvider>,
    );

    expect(await screen.findByRole("button", { name: /sign in/i })).toBeInTheDocument();
  });
});

