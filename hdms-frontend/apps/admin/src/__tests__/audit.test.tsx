import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { currentAdminQueryKey } from "@/lib/auth";
import { ja } from "@/i18n/ja";
import { auditRoute } from "../routes/audit";

const mockNavigate = vi.fn();
let mockSearchState: Record<string, any> = {};

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    useNavigate: () => mockNavigate,
    useRouter: () => ({ invalidate: vi.fn() }),
  };
});

// Mock auditRoute.useSearch
vi.spyOn(auditRoute, "useSearch").mockImplementation(() => mockSearchState as any);

const mockAuditList: apiClient.AuditEventList = {
  items: [
    {
      id: "evt-1",
      at: "2026-08-21T10:00:00Z",
      actor: "admin:super",
      actorIp: "192.168.1.100",
      action: "device.created",
      subject: "device:01923e5c-0000-7000-8000-000000000001",
      payload: { assetTag: "TAB-999", name: "Tablet 999" },
      requestId: "req-12345",
    },
    {
      id: "evt-2",
      at: "2026-08-21T11:00:00Z",
      actor: "kiosk:1",
      action: "loan.borrowed",
      subject: "loan:01923e5c-0000-7000-8000-000000000002",
      payload: { deviceId: "dev-1", userId: "user-1" },
    },
  ],
  nextCursor: "eyJhdCI6IjIwMjYtMDgtMjFUMTE6MDA6MDBaIiwiaWQiOiJldnQtMiJ9",
};

function renderAuditPage(queryClient: QueryClient) {
  const Component = auditRoute.options.component as React.ComponentType;
  return render(
    <QueryClientProvider client={queryClient}>
      <Component />
    </QueryClientProvider>
  );
}

describe("AuditPage (4.9d)", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.clearAllMocks();
    mockSearchState = {};
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
      },
    });

    queryClient.setQueryData(currentAdminQueryKey, {
      id: "admin-1",
      email: "admin@example.org",
      fullName: "Super Admin",
      role: "admin",
    });

    vi.spyOn(apiClient, "listAuditEvents").mockResolvedValue({
      data: mockAuditList,
      error: undefined,
      response: new Response(),
    } as any);
  });

  it("renders audit event list with columns and badges", async () => {
    const { container } = renderAuditPage(queryClient);

    await waitFor(() => {
      expect(screen.getByText("device.created")).toBeInTheDocument();
    });

    expect(screen.getByText("admin:super")).toBeInTheDocument();
    expect(screen.getByText(/192.168.1.100/i)).toBeInTheDocument();
    expect(screen.getByText("loan.borrowed")).toBeInTheDocument();
    expect(screen.getByText("kiosk:1")).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("opens payload inspector modal when Payload button is clicked", async () => {
    renderAuditPage(queryClient);

    const user = userEvent.setup();
    await waitFor(() => {
      expect(screen.getByTestId("view-event-evt-1")).toBeInTheDocument();
    });

    await user.click(screen.getByTestId("view-event-evt-1"));

    expect(screen.getByText(ja.audit.detailsTitle)).toBeInTheDocument();
    expect(screen.getByText(`${ja.audit.eventIdLabel} evt-1`)).toBeInTheDocument();
    expect(screen.getByText(/TAB-999/i)).toBeInTheDocument();
  });

  it("applies filters on search submission", async () => {
    renderAuditPage(queryClient);

    const user = userEvent.setup();
    const actorInput = screen.getByTestId("filter-actor-input");
    await user.type(actorInput, "admin:super");

    const applyBtn = screen.getByTestId("apply-filters-btn");
    await user.click(applyBtn);

    expect(mockNavigate).toHaveBeenCalled();
  });

  it("renders next page cursor button and allows pagination", async () => {
    renderAuditPage(queryClient);

    const user = userEvent.setup();
    await waitFor(() => {
      expect(screen.getByTestId("next-page-btn")).toBeInTheDocument();
    });

    await user.click(screen.getByTestId("next-page-btn"));
    expect(mockNavigate).toHaveBeenCalled();
  });
});
