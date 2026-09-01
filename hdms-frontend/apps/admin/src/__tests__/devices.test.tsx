import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { translate } from "@hdms/i18n";
import { catalogues } from "@/i18n";
import { ja } from "@/i18n/ja";
import { currentAdminQueryKey } from "@/lib/auth";
import { DevicesPage, devicesRoute } from "../routes/devices";

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// Mock TanStack Router
const mockNavigate = vi.fn();
let mockSearch: Record<string, any> = {};

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    useNavigate: () => mockNavigate,
    useRouter: () => ({ invalidate: vi.fn() }),
    Link: ({ to, params, children, className, onClick }: any) => (
      <a
        href={to}
        data-params={JSON.stringify(params)}
        className={className}
        onClick={onClick}
      >
        {children}
      </a>
    ),
  };
});

vi.spyOn(devicesRoute, "useSearch").mockImplementation(() => mockSearch as any);

describe("DevicesPage — Phase 4.3a/b", () => {
  const mockCategories: apiClient.Category[] = [
    { id: "cat-1", name: "Laptops", requiresApproval: false, createdAt: "2026-08-01T00:00:00Z" },
    { id: "cat-2", name: "Tablets", requiresApproval: false, createdAt: "2026-08-01T00:00:00Z" },
  ];

  const mockDevices: apiClient.Device[] = [
    {
      id: "dev-1",
      assetTag: "LAPTOP-01",
      name: "Dell Latitude 5420",
      categoryId: "cat-1",
      status: "available",
      condition: "good",
      model: "Latitude 5420",
      manufacturer: "Dell",
      createdAt: "2026-08-01T00:00:00Z",
      updatedAt: "2026-08-01T00:00:00Z",
    },
    {
      id: "dev-2",
      assetTag: "TAB-01",
      name: "iPad Air 5th Gen",
      categoryId: "cat-2",
      status: "on_loan",
      condition: "good",
      model: "iPad Air",
      manufacturer: "Apple",
      createdAt: "2026-08-02T00:00:00Z",
      updatedAt: "2026-08-02T00:00:00Z",
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

  function renderDevicesPage(role: "admin" | "technician" | "viewer" = "technician") {
    const queryClient = createTestQueryClient();
    queryClient.setQueryData(currentAdminQueryKey, {
      id: "admin-1",
      email: "tech@hospital.org",
      fullName: "Test Technician",
      role,
      status: "active",
    });

    return render(
      <QueryClientProvider client={queryClient}>
        <DevicesPage />
      </QueryClientProvider>,
    );
  }

  beforeEach(() => {
    vi.clearAllMocks();
    mockSearch = {};

    window.HTMLElement.prototype.hasPointerCapture = vi.fn();
    window.HTMLElement.prototype.setPointerCapture = vi.fn();
    window.HTMLElement.prototype.releasePointerCapture = vi.fn();
    window.HTMLElement.prototype.scrollIntoView = vi.fn();

    vi.spyOn(apiClient, "listCategories").mockResolvedValue({
      data: { items: mockCategories },
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "listDevices").mockResolvedValue({
      data: { items: mockDevices, nextCursor: undefined },
      error: undefined,
    } as any);
  });

  it("renders devices list with columns, badges, and passes axe audit", async () => {
    const { container } = renderDevicesPage();

    await waitFor(() => {
      expect(screen.getAllByText("LAPTOP-01").length).toBeGreaterThanOrEqual(1);
    });

    expect(screen.getByText("Dell Latitude 5420")).toBeInTheDocument();
    expect(screen.getAllByText("TAB-01").length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("iPad Air 5th Gen")).toBeInTheDocument();
    expect(screen.getByText(/available/i)).toBeInTheDocument();
    expect(screen.getByText(/on loan/i)).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("filters search input and navigates with search query", async () => {
    renderDevicesPage();

    await waitFor(() => {
      expect(screen.getAllByText("LAPTOP-01").length).toBeGreaterThanOrEqual(1);
    });

    const searchInput = screen.getByPlaceholderText(ja.devices.searchPlaceholder);
    fireEvent.change(searchInput, { target: { value: "iPad" } });
    fireEvent.keyDown(searchInput, { key: "Enter" });

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalled();
    });
  });

  it("opens status change dialog and requires mandatory reason", async () => {
    const user = userEvent.setup();
    const setStatusSpy = vi.spyOn(apiClient, "setDeviceStatus").mockResolvedValueOnce({
      data: { ...mockDevices[0], status: "maintenance" },
      error: undefined,
    } as any);

    renderDevicesPage("technician");

    await waitFor(() => {
      expect(screen.getAllByText("LAPTOP-01").length).toBeGreaterThanOrEqual(1);
    });

    // Open row menu for LAPTOP-01
    const menuButtons = screen.getAllByRole("button", { name: ja.columns.openMenu });
    fireEvent.pointerDown(menuButtons[0]);

    const changeStatusItem = await screen.findByRole("menuitem", { name: ja.devices.changeStatus });
    await user.click(changeStatusItem);

    await waitFor(() => {
      expect(
        screen.getByText(
          translate(catalogues, "ja", "devices.statusDialog.title", { assetTag: "LAPTOP-01" })
        )
      ).toBeInTheDocument();
    });

    // Apply button must be disabled without status and reason
    const applyBtn = screen.getByRole("button", { name: ja.devices.statusDialog.apply });
    expect(applyBtn).toBeDisabled();

    // Select status
    const trigger = screen.getByRole("combobox", {
      name: ja.devices.statusDialog.selectNewStatusAria,
    });
    await user.pointer({ keys: "[MouseLeft]", target: trigger });
    // The status option text comes from labelize() (raw enum, underscores to
    // spaces) — not part of the i18n catalogue, so it stays English regardless
    // of locale.
    const maintenanceOption = await screen.findByRole("option", { name: /maintenance/i });
    await user.click(maintenanceOption);

    // Apply still disabled without reason
    expect(applyBtn).toBeDisabled();

    // Enter reason
    const reasonInput = screen.getByPlaceholderText(ja.devices.statusDialog.reasonPlaceholder);
    await user.type(reasonInput, "Battery replacement");

    expect(applyBtn).not.toBeDisabled();
    await user.click(applyBtn);

    await waitFor(() => {
      expect(setStatusSpy).toHaveBeenCalledWith({
        path: { id: "dev-1" },
        body: { status: "maintenance", reason: "Battery replacement" },
      });
    });
  }, 15000);


  it("handles bulk device selection and opens bulk category dialog with count", async () => {
    const user = userEvent.setup();
    renderDevicesPage("technician");

    await waitFor(() => {
      expect(screen.getAllByText("LAPTOP-01").length).toBeGreaterThanOrEqual(1);
    });

    // Select first device checkbox
    const selectDeviceAriaStem = ja.devices.selectDeviceAria.split("{assetTag}")[0];
    const checkboxes = screen.getAllByRole("checkbox", {
      name: new RegExp(escapeRegExp(selectDeviceAriaStem)),
    });
    await user.click(checkboxes[0]);

    // Bulk actions bar appears
    await waitFor(() => {
      expect(
        screen.getByText(
          new RegExp(
            `${escapeRegExp(ja.devices.itemLabel)}.*${escapeRegExp(ja.table.itemsSelectedSuffix)}`
          )
        )
      ).toBeInTheDocument();
    });

    const changeCategoryBtn = screen.getByRole("button", { name: ja.devices.changeCategory });
    await user.click(changeCategoryBtn);

    await waitFor(() => {
      expect(
        screen.getByText(
          translate(catalogues, "ja", "bulkCategoryDialog.title", { count: 1 })
        ),
      ).toBeInTheDocument();
    });
  });
});
