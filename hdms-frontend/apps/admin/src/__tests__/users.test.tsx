import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { currentAdminQueryKey } from "@/lib/auth";
import { UsersPage, usersRoute } from "../routes/users";

// Mock TanStack Router
const mockNavigate = vi.fn();
let mockSearch: Record<string, any> = {};

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    useNavigate: () => mockNavigate,
    useRouter: () => ({ invalidate: vi.fn() }),
  };
});

// Mock the route's useSearch hook
vi.spyOn(usersRoute, "useSearch").mockImplementation(() => mockSearch as any);

const mockDepartments: apiClient.Department[] = [
  { id: "dept-1", name: "Emergency Department" },
  { id: "dept-2", name: "Cardiology" },
];

const mockUsers: apiClient.User[] = [
  {
    id: "user-1",
    employeeNo: "HH-1001",
    fullName: "Dr. Taro Yamada",
    departmentId: "dept-1",
    status: "active",
    registeredAt: "2026-08-01T09:00:00Z",
    registeredBy: "admin:1",
    updatedAt: "2026-08-01T09:00:00Z",
  },
  {
    id: "user-2",
    employeeNo: "HH-1002",
    fullName: "Nurse Hanako Sato",
    departmentId: "dept-2",
    status: "suspended",
    registeredAt: "2026-08-02T10:00:00Z",
    registeredBy: "admin:1",
    updatedAt: "2026-08-02T10:00:00Z",
  },
  {
    id: "user-3",
    employeeNo: "HH-1003",
    fullName: "Staff Kenji Tanaka",
    departmentId: "dept-1",
    status: "active",
    registeredAt: "2026-08-03T11:00:00Z",
    registeredBy: "admin:1",
    updatedAt: "2026-08-03T11:00:00Z",
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

function renderUsersPage(role: "admin" | "technician" | "viewer" = "admin") {
  const queryClient = createTestQueryClient();
  queryClient.setQueryData(currentAdminQueryKey, {
    id: "admin-1",
    email: "admin@hito.local",
    fullName: "System Admin",
    role,
    status: "active",
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <UsersPage />
    </QueryClientProvider>
  );
}

describe("4.4a Users Table & Actions", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockSearch = {};

    window.HTMLElement.prototype.hasPointerCapture = vi.fn();
    window.HTMLElement.prototype.setPointerCapture = vi.fn();
    window.HTMLElement.prototype.releasePointerCapture = vi.fn();
    window.HTMLElement.prototype.scrollIntoView = vi.fn();

    vi.spyOn(apiClient, "listDepartments").mockResolvedValue({
      data: { items: mockDepartments },
      error: undefined,
    } as any);
    vi.spyOn(apiClient, "listUsers").mockResolvedValue({
      data: { items: mockUsers, nextCursor: undefined },
      error: undefined,
    } as any);
  });

  it("renders users list with columns, resolved departments, and status badges", async () => {
    renderUsersPage("admin");

    expect(await screen.findByText("Dr. Taro Yamada")).toBeInTheDocument();
    expect(screen.getByText("HH-1001")).toBeInTheDocument();
    expect(screen.getAllByText("Emergency Department").length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("Nurse Hanako Sato")).toBeInTheDocument();
    expect(screen.getByText("Cardiology")).toBeInTheDocument();
    expect(screen.getByText("Staff Kenji Tanaka")).toBeInTheDocument();
  });

  it("displays 'no card issued' badge when hasCredential=false work queue filter is active", async () => {
    mockSearch = { hasCredential: false };

    renderUsersPage("admin");

    expect(await screen.findByText("Dr. Taro Yamada")).toBeInTheDocument();
    const badges = await screen.findAllByText("no card issued");
    expect(badges.length).toBe(mockUsers.length);
  });

  it("round-trips hasCredential filter changes via navigate search params", async () => {
    renderUsersPage("admin");

    await screen.findByText("Dr. Taro Yamada");

    // Click card status filter
    const cardStatusTrigger = screen.getByRole("combobox", { name: /filter by card status/i });
    const user = userEvent.setup();
    await user.pointer({ keys: "[MouseLeft]", target: cardStatusTrigger });

    const noCardOption = await screen.findByRole("option", { name: /no card issued/i });
    await user.click(noCardOption);

    expect(mockNavigate).toHaveBeenCalledWith({
      search: expect.any(Function),
    });

    const updater = mockNavigate.mock.calls[0][0].search;
    const nextParams = updater({ q: "test" });
    expect(nextParams).toEqual({ q: "test", hasCredential: false });
  });

  it("performs borrower suspend with mandatory reason", async () => {
    const suspendSpy = vi.spyOn(apiClient, "suspendUser").mockResolvedValueOnce({
      data: { ...mockUsers[0], status: "suspended" },
      error: undefined,
    } as any);

    renderUsersPage("technician");

    await screen.findByText("Dr. Taro Yamada");

    const user = userEvent.setup();
    const suspendButtons = screen.getAllByRole("button", { name: /^suspend$/i });
    await user.click(suspendButtons[0]);

    expect(screen.getByRole("heading", { name: /suspend borrower/i })).toBeInTheDocument();

    await user.type(screen.getByPlaceholderText(/reason \(required\)/i), "Misplaced badge");
    await user.click(screen.getByRole("button", { name: /^suspend$/i }));

    await waitFor(() => {
      expect(suspendSpy).toHaveBeenCalledWith({
        path: { id: "user-1" },
        body: { reason: "Misplaced badge" },
      });
    });
  });

  it("performs borrower archive with mandatory reason", async () => {
    const archiveSpy = vi.spyOn(apiClient, "archiveUser").mockResolvedValueOnce({
      data: { ...mockUsers[0], status: "archived" },
      error: undefined,
    } as any);

    renderUsersPage("admin");

    await screen.findByText("Dr. Taro Yamada");

    const user = userEvent.setup();
    const archiveButtons = screen.getAllByRole("button", { name: /^archive$/i });
    await user.click(archiveButtons[0]);

    expect(screen.getByRole("heading", { name: /archive borrower/i })).toBeInTheDocument();

    await user.type(screen.getByPlaceholderText(/reason \(required\)/i), "Staff departed hospital");
    await user.click(screen.getByRole("button", { name: /^archive$/i }));

    await waitFor(() => {
      expect(archiveSpy).toHaveBeenCalledWith({
        path: { id: "user-1" },
        body: { reason: "Staff departed hospital" },
      });
    });
  });

  it("passes accessibility check with axe", async () => {
    const { container } = renderUsersPage("admin");
    await screen.findByText("Dr. Taro Yamada");

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });
});
