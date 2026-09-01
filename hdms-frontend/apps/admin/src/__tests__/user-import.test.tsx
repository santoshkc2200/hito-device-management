import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import * as apiClient from "@hdms/api-client";
import { translate } from "@hdms/i18n";
import { catalogues } from "@/i18n";
import { ja } from "@/i18n/ja";
import { UserImportDialog } from "@/components/user-import-dialog";

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

describe("UserImportDialog", () => {
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

  const renderDialog = (open = true, onOpenChange = vi.fn()) => {
    return render(
      <QueryClientProvider client={queryClient}>
        <UserImportDialog open={open} onOpenChange={onOpenChange} />
      </QueryClientProvider>,
    );
  };

  it("renders upload step initially and passes a11y audit", async () => {
    const { container } = renderDialog();

    expect(screen.getByText(ja.userImportDialog.titleUpload)).toBeInTheDocument();
    expect(screen.getByText(ja.deviceImportDialog.clickToSelectCsv)).toBeInTheDocument();

    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it("handles valid CSV preview and displays row actions and summary", async () => {
    const mockPreview: apiClient.ImportPreview = {
      previewId: "prev-123",
      expiresAt: "2026-08-21T12:00:00Z",
      columns: ["employee_no", "full_name", "department"],
      rows: [
        {
          lineNo: 2,
          action: "create",
          values: {
            employee_no: "HH-2001",
            full_name: "Alice Smith",
            department: "Emergency",
          },
        },
        {
          lineNo: 3,
          action: "update",
          values: {
            employee_no: "HH-2002",
            full_name: "Bob Jones",
            department: "Cardiology",
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

    vi.spyOn(apiClient, "previewUserImport").mockResolvedValueOnce({
      data: mockPreview,
      error: undefined,
    } as any);

    renderDialog();

    // Trigger file selection
    const file = new File(
      ["employee_no,full_name,department\nHH-2001,Alice Smith,Emergency\nHH-2002,Bob Jones,Cardiology"],
      "staff.csv",
      { type: "text/csv" },
    );

    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    expect(input).toBeInTheDocument();

    fireEvent.change(input, { target: { files: [file] } });

    await waitFor(() => {
      expect(screen.getByText(ja.userImportDialog.titlePreview)).toBeInTheDocument();
    });

    expect(screen.getByText("HH-2001")).toBeInTheDocument();
    expect(screen.getByText("Alice Smith")).toBeInTheDocument();
    // "新規作成"/"更新" label both the summary stat tile and the row's action
    // badge — Japanese uses the same short word for both.
    expect(
      screen.getAllByText(ja.deviceImportDialog.actionCreate).length
    ).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("HH-2002")).toBeInTheDocument();
    expect(
      screen.getAllByText(ja.deviceImportDialog.actionUpdate).length
    ).toBeGreaterThanOrEqual(1);

    // Commit button should be enabled
    const commitBtn = screen.getByRole("button", { name: ja.deviceImportDialog.commitImport });
    expect(commitBtn).not.toBeDisabled();
  });

  it("displays invalid rows and disables commit button when errors are present", async () => {
    const mockInvalidPreview: apiClient.ImportPreview = {
      previewId: "prev-456",
      expiresAt: "2026-08-21T12:00:00Z",
      columns: ["employee_no", "full_name"],
      rows: [
        {
          lineNo: 2,
          action: "invalid",
          values: { employee_no: "", full_name: "Incomplete User" },
          problems: [
            {
              field: "employee_no",
              code: "required",
              message: "employee_no is required",
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

    vi.spyOn(apiClient, "previewUserImport").mockResolvedValueOnce({
      data: mockInvalidPreview,
      error: undefined,
    } as any);

    renderDialog();

    const file = new File(
      ["employee_no,full_name\n,Incomplete User"],
      "staff_invalid.csv",
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

    expect(screen.getByText(/employee_no is required/i)).toBeInTheDocument();
    expect(
      screen.getAllByText(new RegExp(escapeRegExp(ja.deviceImportDialog.invalid))).length
    ).toBeGreaterThanOrEqual(1);

    const commitBtn = screen.getByRole("button", { name: ja.deviceImportDialog.commitImport });
    expect(commitBtn).toBeDisabled();
  });

  it("commits valid preview, displays summary and distribution sheet (FR-77)", async () => {
    const user = userEvent.setup();

    const mockPreview: apiClient.ImportPreview = {
      previewId: "prev-789",
      expiresAt: "2026-08-21T12:00:00Z",
      columns: ["employee_no", "full_name"],
      rows: [
        {
          lineNo: 2,
          action: "create",
          values: { employee_no: "HH-3001", full_name: "Charlie Brown" },
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
      importId: "imp-batch-999",
      createdCount: 1,
      updatedCount: 0,
      skippedCount: 0,
      createdSubjectIds: ["user-new-1"],
    };

    vi.spyOn(apiClient, "previewUserImport").mockResolvedValueOnce({
      data: mockPreview,
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "commitUserImport").mockResolvedValueOnce({
      data: mockResult,
      error: undefined,
    } as any);

    vi.spyOn(apiClient, "getUser").mockResolvedValueOnce({
      data: {
        id: "user-new-1",
        employeeNo: "HH-3001",
        fullName: "Charlie Brown",
        departmentId: "dept-1",
        status: "active",
        registeredAt: "2026-08-21T12:00:00Z",
        registeredBy: "import:imp-batch-999",
        updatedAt: "2026-08-21T12:00:00Z",
      },
      error: undefined,
    } as any);

    renderDialog();

    const file = new File(
      ["employee_no,full_name\nHH-3001,Charlie Brown"],
      "staff.csv",
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
      expect(screen.getByText(ja.userImportDialog.titleDone)).toBeInTheDocument();
    });

    expect(screen.getByText("imp-batch-999")).toBeInTheDocument();
    expect(screen.getByText(ja.userImportDialog.distributionSheetHeading)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: ja.userImportDialog.printSheet })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: ja.userImportDialog.issueCardsToNewStaff })
    ).toBeInTheDocument();

    // Verify print button triggers window.print
    const printSpy = vi.spyOn(window, "print").mockImplementation(() => {});
    await user.click(screen.getByRole("button", { name: ja.userImportDialog.printSheet }));
    expect(printSpy).toHaveBeenCalled();
  });
});
