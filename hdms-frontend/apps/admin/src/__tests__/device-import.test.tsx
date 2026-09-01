import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { translate } from "@hdms/i18n";
import { catalogues } from "@/i18n";
import { ja } from "@/i18n/ja";
import { DeviceImportDialog } from "@/components/device-import-dialog";

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

describe("DeviceImportDialog — Phase 4.3c", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    vi.clearAllMocks();
  });

  const renderDialog = (
    open = true,
    onOpenChange = vi.fn(),
    onPrintLabels = vi.fn(),
  ) => {
    return render(
      <QueryClientProvider client={queryClient}>
        <DeviceImportDialog
          open={open}
          onOpenChange={onOpenChange}
          onPrintLabels={onPrintLabels}
        />
      </QueryClientProvider>,
    );
  };

  it("renders upload step initially and passes a11y audit", async () => {
    const { container } = renderDialog();

    expect(screen.getByText(ja.deviceImportDialog.titleUpload)).toBeInTheDocument();
    expect(screen.getByText(ja.deviceImportDialog.clickToSelectCsv)).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("handles valid CSV preview and displays row actions and summary", async () => {
    const mockPreview: apiClient.ImportPreview = {
      previewId: "prev-dev-123",
      expiresAt: "2026-08-21T12:00:00Z",
      columns: ["asset_tag", "name", "category", "manufacturer", "model"],
      rows: [
        {
          lineNo: 2,
          action: "create",
          values: {
            asset_tag: "DEV-101",
            name: "Vital Signs Monitor",
            category: "Monitors",
            manufacturer: "Philips",
            model: "VS-4",
          },
        },
        {
          lineNo: 3,
          action: "update",
          values: {
            asset_tag: "DEV-102",
            name: "Infusion Pump",
            category: "Pumps",
            manufacturer: "Baxter",
            model: "Sigma Spectrum",
          },
        },
      ],
      summary: {
        totalRows: 2,
        createCount: 1,
        updateCount: 1,
        skipCount: 0,
        invalidCount: 0,
      },
    };

    vi.spyOn(apiClient, "previewDeviceImport").mockResolvedValueOnce({
      data: mockPreview,
      error: undefined,
    } as any);

    renderDialog();

    const file = new File(
      [
        "asset_tag,name,category,manufacturer,model\nDEV-101,Vital Signs Monitor,Monitors,Philips,VS-4\nDEV-102,Infusion Pump,Pumps,Baxter,Sigma Spectrum",
      ],
      "devices.csv",
      { type: "text/csv" },
    );

    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    expect(input).toBeInTheDocument();

    fireEvent.change(input, { target: { files: [file] } });

    await waitFor(() => {
      expect(screen.getByText(ja.deviceImportDialog.titlePreview)).toBeInTheDocument();
    });

    expect(screen.getByText("DEV-101")).toBeInTheDocument();
    expect(screen.getByText("Vital Signs Monitor")).toBeInTheDocument();
    // "新規作成"/"更新" label both the summary stat tile and the row's action
    // badge — Japanese uses the same short word for both, unlike English's
    // "To Create" vs "Create".
    expect(screen.getAllByText(ja.deviceImportDialog.actionCreate).length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("DEV-102")).toBeInTheDocument();
    expect(screen.getAllByText(ja.deviceImportDialog.actionUpdate).length).toBeGreaterThanOrEqual(1);

    // Commit button should be enabled
    const commitBtn = screen.getByRole("button", { name: ja.deviceImportDialog.commitImport });
    expect(commitBtn).not.toBeDisabled();
  });

  it("displays invalid rows and disables commit button when errors exist", async () => {
    const mockInvalidPreview: apiClient.ImportPreview = {
      previewId: "prev-dev-456",
      expiresAt: "2026-08-21T12:00:00Z",
      columns: ["asset_tag", "name", "category"],
      rows: [
        {
          lineNo: 2,
          action: "invalid",
          values: { asset_tag: "", name: "Incomplete Device", category: "Laptops" },
          problems: [
            {
              field: "asset_tag",
              code: "required",
              message: "asset_tag is required",
            },
          ],
        },
      ],
      summary: {
        totalRows: 1,
        createCount: 0,
        updateCount: 0,
        skipCount: 0,
        invalidCount: 1,
      },
    };

    vi.spyOn(apiClient, "previewDeviceImport").mockResolvedValueOnce({
      data: mockInvalidPreview,
      error: undefined,
    } as any);

    renderDialog();

    const file = new File(
      ["asset_tag,name,category\n,Incomplete Device,Laptops"],
      "devices_invalid.csv",
      { type: "text/csv" },
    );
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [file] } });

    await waitFor(() => {
      expect(
        screen.getByText(
          translate(catalogues, "ja", "deviceImportDialog.invalidRowsWarning", { count: 1 })
        )
      ).toBeInTheDocument();
    });

    expect(screen.getByText(/asset_tag is required/i)).toBeInTheDocument();
    expect(
      screen.getAllByText(new RegExp(escapeRegExp(ja.deviceImportDialog.invalid))).length
    ).toBeGreaterThanOrEqual(1);

    const commitBtn = screen.getByRole("button", { name: ja.deviceImportDialog.commitImport });
    expect(commitBtn).toBeDisabled();
  });

  it("commits valid preview, displays summary, and allows label printing transition", async () => {
    const user = userEvent.setup();
    const onPrintLabels = vi.fn();

    const mockPreview: apiClient.ImportPreview = {
      previewId: "prev-dev-789",
      expiresAt: "2026-08-21T12:00:00Z",
      columns: ["asset_tag", "name", "category"],
      rows: [
        {
          lineNo: 2,
          action: "create",
          values: { asset_tag: "DEV-301", name: "New Device", category: "Laptops" },
        },
      ],
      summary: {
        totalRows: 1,
        createCount: 1,
        updateCount: 0,
        skipCount: 0,
        invalidCount: 0,
      },
    };

    const mockResult: apiClient.ImportResult = {
      importId: "imp-dev-batch-101",
      createdCount: 1,
      updatedCount: 0,
      skippedCount: 0,
      createdSubjectIds: ["dev-created-1"],
    };

    vi.spyOn(apiClient, "previewDeviceImport").mockResolvedValueOnce({
      data: mockPreview,
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "commitDeviceImport").mockResolvedValueOnce({
      data: mockResult,
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "getDevice").mockResolvedValueOnce({
      data: {
        id: "dev-created-1",
        assetTag: "DEV-301",
        name: "New Device",
        categoryId: "cat-1",
        status: "available",
        condition: "good",
        createdAt: "2026-08-21T12:00:00Z",
        updatedAt: "2026-08-21T12:00:00Z",
      },
      error: undefined,
    } as any);

    renderDialog(true, vi.fn(), onPrintLabels);

    const file = new File(
      ["asset_tag,name,category\nDEV-301,New Device,Laptops"],
      "devices.csv",
      { type: "text/csv" },
    );
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [file] } });

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: ja.deviceImportDialog.commitImport })
      ).toBeInTheDocument();
    });

    await user.click(screen.getByRole("button", { name: ja.deviceImportDialog.commitImport }));

    await waitFor(() => {
      expect(screen.getByText(ja.deviceImportDialog.titleDone)).toBeInTheDocument();
    });

    expect(screen.getByText("imp-dev-batch-101")).toBeInTheDocument();
    expect(screen.getByText(ja.deviceImportDialog.printLabelsHeading)).toBeInTheDocument();

    const printLabelsBtn = screen.getByRole("button", {
      name: translate(catalogues, "ja", "devices.printLabelsWithCount", { count: 1 }),
    });
    expect(printLabelsBtn).toBeInTheDocument();
    await user.click(printLabelsBtn);

    expect(onPrintLabels).toHaveBeenCalledWith([
      expect.objectContaining({ id: "dev-created-1", assetTag: "DEV-301" }),
    ]);
  });
});
