import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { registerRoute } from "../routes/register";

// Mock TanStack Router
const mockNavigate = vi.fn();
vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    useNavigate: () => mockNavigate,
    useRouter: () => ({ invalidate: vi.fn() }),
    Link: ({ children, to, search, className }: any) => (
      <a href={`${to}?q=${search?.q || ""}`} className={className}>
        {children}
      </a>
    ),
  };
});

const mockDepartments: apiClient.Department[] = [
  { id: "dept-1", name: "Emergency Department" },
  { id: "dept-2", name: "Cardiology" },
];

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
}

describe("4.4b Register borrower form", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(apiClient, "listDepartments").mockResolvedValue({
      data: { items: mockDepartments },
      error: undefined,
    } as any);
    vi.spyOn(apiClient, "getUnboundCredentialCount").mockResolvedValue({
      data: { count: 27 },
      error: undefined,
    } as any);
  });

  it("passes axe accessibility audit", async () => {
    vi.spyOn(apiClient, "checkEmployeeNo").mockResolvedValue({
      data: { employeeNo: "", available: true },
      error: undefined,
    } as any);

    const queryClient = createTestQueryClient();
    const RegisterComponent = registerRoute.options.component!;
    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <RegisterComponent />
      </QueryClientProvider>
    );

    await waitFor(() => {
      expect(screen.getByRole("heading", { name: "Register borrower" })).toBeInTheDocument();
    });

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("surfaces duplicate employee number before submit", async () => {
    const user = userEvent.setup();
    vi.spyOn(apiClient, "checkEmployeeNo").mockImplementation(async ({ query }) => {
      if (query?.employeeNo === "HH-DUPLICATE") {
        return {
          data: {
            employeeNo: "HH-DUPLICATE",
            available: false,
            existingUserId: "existing-user-1",
          },
          error: undefined,
        } as any;
      }
      return {
        data: {
          employeeNo: query?.employeeNo || "",
          available: true,
        },
        error: undefined,
      } as any;
    });

    const queryClient = createTestQueryClient();
    const RegisterComponent = registerRoute.options.component!;
    render(
      <QueryClientProvider client={queryClient}>
        <RegisterComponent />
      </QueryClientProvider>
    );

    const empNoInput = screen.getByLabelText("Employee no.");
    await user.type(empNoInput, "HH-DUPLICATE");

    await waitFor(() => {
      expect(screen.getByText(/is already registered/i)).toBeInTheDocument();
      expect(screen.getByRole("link", { name: /view existing record/i })).toBeInTheDocument();
    });

    // Fill the rest
    await user.type(screen.getByLabelText("Full name"), "Dr. Test User");

    // Select option C (no card)
    const noCardRadio = screen.getByLabelText(/register without a card/i);
    await user.click(noCardRadio);

    // Submit button should be disabled because employee number is not available
    const submitBtn = screen.getByRole("button", { name: /register & issue/i });
    expect(submitBtn).toBeDisabled();
  });

  it("registers without a card (Option C) and shows no card issued state", async () => {
    const user = userEvent.setup();
    vi.spyOn(apiClient, "checkEmployeeNo").mockResolvedValue({
      data: { employeeNo: "HH-2401", available: true },
      error: undefined,
    } as any);

    const createUserSpy = vi.spyOn(apiClient, "createUser").mockResolvedValue({
      data: {
        id: "new-user-1",
        employeeNo: "HH-2401",
        fullName: "Anita Thapa",
        departmentId: "dept-1",
        status: "active",
        registeredAt: "2026-08-21T10:00:00Z",
        registeredBy: "admin:1",
        updatedAt: "2026-08-21T10:00:00Z",
      },
      error: undefined,
    } as any);

    const queryClient = createTestQueryClient();
    const RegisterComponent = registerRoute.options.component!;
    render(
      <QueryClientProvider client={queryClient}>
        <RegisterComponent />
      </QueryClientProvider>
    );

    await user.type(screen.getByLabelText("Full name"), "Anita Thapa");
    await user.type(screen.getByLabelText("Employee no."), "HH-2401");

    await waitFor(() => {
      expect(screen.getByText(/available/i)).toBeInTheDocument();
    });

    // Option C
    await user.click(screen.getByLabelText(/register without a card/i));

    const submitBtn = screen.getByRole("button", { name: /register & issue/i });
    expect(submitBtn).toBeEnabled();
    await user.click(submitBtn);

    await waitFor(() => {
      expect(createUserSpy).toHaveBeenCalledWith(
        expect.objectContaining({
          body: expect.objectContaining({
            fullName: "Anita Thapa",
            employeeNo: "HH-2401",
          }),
        })
      );
      expect(screen.getByRole("heading", { name: /Anita Thapa is registered/i })).toBeInTheDocument();
      expect(screen.getByText(/no card issued yet/i)).toBeInTheDocument();
    });
  });

  it("scans a blank card (Option A) and binds it atomically", async () => {
    const user = userEvent.setup();
    vi.spyOn(apiClient, "checkEmployeeNo").mockResolvedValue({
      data: { employeeNo: "HH-2402", available: true },
      error: undefined,
    } as any);

    const resolveSpy = vi.spyOn(apiClient, "resolveCredential").mockResolvedValue({
      data: {
        credentialId: "cred-blank-1",
        type: "unbound",
        kind: "qr",
        credentialStatus: "active",
      },
      error: undefined,
    } as any);

    const registerSpy = vi.spyOn(apiClient, "registerUserWithCard").mockResolvedValue({
      data: {
        user: {
          id: "new-user-2",
          employeeNo: "HH-2402",
          fullName: "Dr. Blank Scan",
          status: "active",
          registeredAt: "2026-08-21T10:00:00Z",
          registeredBy: "admin:1",
          updatedAt: "2026-08-21T10:00:00Z",
        },
        credential: {
          id: "cred-blank-1",
          kind: "qr",
          status: "active",
          subjectType: "user",
          subjectId: "new-user-2",
          issuedAt: "2026-08-21T10:00:00Z",
          issuedBy: "admin:1",
          version: 1,
        },
      },
      error: undefined,
    } as any);

    const queryClient = createTestQueryClient();
    const RegisterComponent = registerRoute.options.component!;
    render(
      <QueryClientProvider client={queryClient}>
        <RegisterComponent />
      </QueryClientProvider>
    );

    // Verify unbound count is displayed
    await waitFor(() => {
      expect(screen.getByText(/27 remain unbound/i)).toBeInTheDocument();
    });

    await user.type(screen.getByLabelText("Full name"), "Dr. Blank Scan");
    await user.type(screen.getByLabelText("Employee no."), "HH-2402");

    // Scan blank card
    const scanInput = screen.getByPlaceholderText("Waiting for scan…");
    await user.type(scanInput, "BLANK_CARD_TOKEN{enter}");

    await waitFor(() => {
      expect(resolveSpy).toHaveBeenCalled();
      expect(screen.getByText(/ready to bind/i)).toBeInTheDocument();
    });

    const submitBtn = screen.getByRole("button", { name: /register & issue/i });
    expect(submitBtn).toBeEnabled();
    await user.click(submitBtn);

    await waitFor(() => {
      expect(registerSpy).toHaveBeenCalledWith(
        expect.objectContaining({
          body: expect.objectContaining({
            fullName: "Dr. Blank Scan",
            employeeNo: "HH-2402",
            credentialId: "cred-blank-1",
          }),
        })
      );
      expect(screen.getByRole("heading", { name: /Dr. Blank Scan is registered/i })).toBeInTheDocument();
      expect(screen.getByText(/card bound and ready to use/i)).toBeInTheDocument();
    });
  });

  it("handles race 409 conflict gracefully as field error", async () => {
    const user = userEvent.setup();
    vi.spyOn(apiClient, "checkEmployeeNo").mockResolvedValue({
      data: { employeeNo: "HH-RACE", available: true },
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "createUser").mockRejectedValue({
      status: 409,
      type: "unique-constraint-violation",
      title: "Conflict",
    });

    const queryClient = createTestQueryClient();
    const RegisterComponent = registerRoute.options.component!;
    render(
      <QueryClientProvider client={queryClient}>
        <RegisterComponent />
      </QueryClientProvider>
    );

    await user.type(screen.getByLabelText("Full name"), "Race Tester");
    await user.type(screen.getByLabelText("Employee no."), "HH-RACE");
    await user.click(screen.getByLabelText(/register without a card/i));

    const submitBtn = screen.getByRole("button", { name: /register & issue/i });
    await user.click(submitBtn);

    await waitFor(() => {
      expect(screen.getByText(/employee number is already registered/i)).toBeInTheDocument();
    });
  });

  it("'Register another' retains department selection and resets other fields", async () => {
    vi.spyOn(apiClient, "checkEmployeeNo").mockResolvedValue({
      data: { employeeNo: "HH-BATCH-1", available: true },
      error: undefined,
    } as any);

    const createUserSpy = vi.spyOn(apiClient, "createUser").mockResolvedValue({
      data: {
        id: "new-user-batch",
        employeeNo: "HH-BATCH-1",
        fullName: "Batch User 1",
        departmentId: "dept-2",
        status: "active",
        registeredAt: "2026-08-21T10:00:00Z",
        registeredBy: "admin:1",
        updatedAt: "2026-08-21T10:00:00Z",
      },
      error: undefined,
    } as any);

    const queryClient = createTestQueryClient();
    const RegisterComponent = registerRoute.options.component!;
    render(
      <QueryClientProvider client={queryClient}>
        <RegisterComponent />
      </QueryClientProvider>
    );

    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Full name"), "Batch User 1");
    await user.type(screen.getByLabelText("Employee no."), "HH-BATCH-1");

    // Wait for departments query to resolve
    await waitFor(() => {
      expect(apiClient.listDepartments).toHaveBeenCalled();
    });

    // Select cardiology department
    const deptTrigger = screen.getByRole("combobox", { name: /department/i });
    await user.pointer({ keys: "[MouseLeft]", target: deptTrigger });
    const cardioOption = await screen.findByRole("option", { name: "Cardiology" });
    await user.click(cardioOption);


    // Submit with Option C
    await user.click(screen.getByLabelText(/register without a card/i));
    await user.click(screen.getByRole("button", { name: /register & issue/i }));

    await waitFor(() => {
      expect(createUserSpy).toHaveBeenCalledWith(
        expect.objectContaining({
          body: expect.objectContaining({
            departmentId: "dept-2",
          }),
        })
      );
      expect(screen.getByRole("heading", { name: /Batch User 1 is registered/i })).toBeInTheDocument();
    });

    // Click Register another
    const registerAnotherBtn = screen.getByRole("button", { name: /register another/i });
    await user.click(registerAnotherBtn);

    // Verify form is reset except department
    await waitFor(() => {
      expect(screen.getByLabelText("Full name")).toHaveValue("");
      expect(screen.getByLabelText("Employee no.")).toHaveValue("");
      expect(screen.getAllByText("Cardiology").length).toBeGreaterThan(0);
    });
  });
});

