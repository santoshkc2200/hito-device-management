import { createColumnHelper } from "@tanstack/react-table";
import { fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { axe } from "vitest-axe";
import { useMemo, useState } from "react";
import { ja } from "@/i18n/ja";
import {
  DataTable,
  DataTableColumnHeader,
  useDataTableColumns,
} from "../index";

interface TestItem {
  id: string;
  name: string;
  category: string;
  status: "active" | "inactive";
}

const mockData: TestItem[] = [
  { id: "1", name: "Alpha Device", category: "Scanner", status: "active" },
  { id: "2", name: "Beta Phone", category: "Handheld", status: "active" },
  { id: "3", name: "Gamma Tablet", category: "Handheld", status: "inactive" },
];

const columnHelper = createColumnHelper<TestItem>();

describe("4.2b DataTable Component", () => {
  it("renders table data headers and cells correctly", () => {
    const { result } = renderHook(() =>
      useDataTableColumns(
        () => [
          columnHelper.accessor("id", {
            header: ({ column }) => <DataTableColumnHeader column={column} title="ID" />,
          }),
          columnHelper.accessor("name", {
            header: ({ column }) => <DataTableColumnHeader column={column} title="Name" />,
          }),
          columnHelper.accessor("category", {
            header: "Category",
          }),
          columnHelper.accessor("status", {
            header: "Status",
          }),
        ],
        []
      )
    );

    render(<DataTable columns={result.current} data={mockData} />);

    expect(screen.getByText("Alpha Device")).toBeInTheDocument();
    expect(screen.getByText("Beta Phone")).toBeInTheDocument();
    expect(screen.getByText("Gamma Tablet")).toBeInTheDocument();
    expect(screen.getByText("Scanner")).toBeInTheDocument();
  });

  describe("Memoization loop regression test", () => {
    it("does not re-render row cells when parent re-renders with identical data and columns", () => {
      let renderCellCount = 0;

      function TestParent() {
        const [, setTick] = useState(0);

        const columns = useDataTableColumns(
          () => [
            columnHelper.accessor("name", {
              header: "Name",
              cell: (info) => {
                renderCellCount++;
                return <span>{info.getValue()}</span>;
              },
            }),
          ],
          []
        );

        return (
          <div>
            <button data-testid="re-render-trigger" onClick={() => setTick((t) => t + 1)}>
              Tick
            </button>
            <DataTable columns={columns} data={mockData} />
          </div>
        );
      }

      render(<TestParent />);

      const initialRenderCellCount = renderCellCount;
      expect(initialRenderCellCount).toBe(mockData.length);

      // Trigger a parent re-render
      fireEvent.click(screen.getByTestId("re-render-trigger"));

      // Cells should NOT have re-evaluated since column refs and data remained identical
      expect(renderCellCount).toBe(initialRenderCellCount);
    });
  });

  describe("Empty States", () => {
    it("renders empty-without-filters explanatory state when data is empty and no filters are active", () => {
      const { result } = renderHook(() =>
        useDataTableColumns(
          () => [
            columnHelper.accessor("name", { header: "Name" }),
          ],
          []
        )
      );

      render(
        <DataTable
          columns={result.current}
          data={[]}
          emptyTitle="No devices registered"
          emptyExplanation="No devices have been added to the inventory yet."
        />
      );

      expect(screen.getByText("No devices registered")).toBeInTheDocument();
      expect(
        screen.getByText("No devices have been added to the inventory yet.")
      ).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: ja.table.clearFilters })).not.toBeInTheDocument();
    });

    it("renders empty-with-filters state and clear filters action when isFiltered is true", () => {
      const { result } = renderHook(() =>
        useDataTableColumns(
          () => [
            columnHelper.accessor("name", { header: "Name" }),
          ],
          []
        )
      );

      const handleReset = vi.fn();

      render(
        <DataTable
          columns={result.current}
          data={[]}
          isFiltered={true}
          onResetFilters={handleReset}
        />
      );

      expect(screen.getByText(ja.table.noMatchingResults)).toBeInTheDocument();
      expect(
        screen.getByText(ja.table.noMatchingResultsExplanation)
      ).toBeInTheDocument();

      const clearBtn = screen.getByRole("button", { name: ja.table.clearFilters });
      expect(clearBtn).toBeInTheDocument();
      fireEvent.click(clearBtn);
      expect(handleReset).toHaveBeenCalledTimes(1);
    });
  });

  describe("Row selection alongside row navigation", () => {
    it("selects a row without opening it when the selection checkbox is clicked", async () => {
      const user = userEvent.setup();
      const onRowClick = vi.fn();

      function SelectableTable() {
        const [rowSelection, setRowSelection] = useState<Record<string, boolean>>({});
        const columns = useMemo(
          () => [
            columnHelper.display({
              id: "select",
              header: () => null,
              cell: ({ row }) => (
                <input
                  type="checkbox"
                  aria-label={`Select row ${row.original.id}`}
                  checked={row.getIsSelected()}
                  onChange={row.getToggleSelectedHandler()}
                />
              ),
            }),
            columnHelper.accessor("name", { header: "Name" }),
          ],
          []
        );

        return (
          <DataTable
            columns={columns}
            data={mockData}
            enableRowSelection={true}
            rowSelection={rowSelection}
            onRowSelectionChange={setRowSelection}
            onRowClick={onRowClick}
          />
        );
      }

      render(<SelectableTable />);

      await user.click(screen.getByRole("checkbox", { name: "Select row 1" }));

      expect(screen.getByRole("checkbox", { name: "Select row 1" })).toBeChecked();
      expect(onRowClick).not.toHaveBeenCalled();

      // A click on the row itself still opens it.
      await user.click(screen.getByText(mockData[0].name));
      expect(onRowClick).toHaveBeenCalledWith(mockData[0]);
    });
  });

  describe("Keyboard Navigation", () => {
    it("supports arrow keys navigation, enter to trigger row click, and visible focus indicator", async () => {
      const user = userEvent.setup();
      const onRowClick = vi.fn();

      const { result } = renderHook(() =>
        useDataTableColumns(
          () => [
            columnHelper.accessor("name", { header: "Name" }),
          ],
          []
        )
      );

      render(
        <DataTable
          columns={result.current}
          data={mockData}
          onRowClick={onRowClick}
        />
      );

      const tableRegion = screen.getByRole("region", { name: ja.table.regionAria });
      tableRegion.focus();

      // Press ArrowDown to focus first row
      await user.keyboard("{ArrowDown}");
      const rows = screen.getAllByRole("row");
      // Row 0 is header, Row 1 is first data row
      expect(rows[1]).toHaveAttribute("data-focused", "true");

      // Press ArrowDown to focus second row
      await user.keyboard("{ArrowDown}");
      expect(rows[2]).toHaveAttribute("data-focused", "true");
      expect(rows[1]).not.toHaveAttribute("data-focused");

      // Press Enter to open the focused row
      await user.keyboard("{Enter}");
      expect(onRowClick).toHaveBeenCalledWith(mockData[1]);

      // Press ArrowUp to move back
      await user.keyboard("{ArrowUp}");
      expect(rows[1]).toHaveAttribute("data-focused", "true");
    });

    it("focuses the search input when pressing '/' global shortcut", async () => {
      const user = userEvent.setup();
      const onSearchChange = vi.fn();

      const { result } = renderHook(() =>
        useDataTableColumns(
          () => [
            columnHelper.accessor("name", { header: "Name" }),
          ],
          []
        )
      );

      render(
        <DataTable
          columns={result.current}
          data={mockData}
          searchQuery=""
          onSearchChange={onSearchChange}
          searchPlaceholder="Search inventory…"
        />
      );

      const searchInput = screen.getByRole("textbox", { name: /search inventory…/i });
      expect(searchInput).not.toHaveFocus();

      // Type '/' when focused on body
      await user.keyboard("/");
      expect(searchInput).toHaveFocus();
    });
  });

  describe("Column Visibility Persistence", () => {
    it("loads and persists column visibility to localStorage when tableId is provided", async () => {
      const tableId = "test-devices-table";
      localStorage.setItem(
        `hdms_table_visibility_${tableId}`,
        JSON.stringify({ category: false })
      );

      const { result } = renderHook(() =>
        useDataTableColumns(
          () => [
            columnHelper.accessor("name", { header: "Name", enableHiding: true }),
            columnHelper.accessor("category", { header: "Category", enableHiding: true }),
          ],
          []
        )
      );

      render(
        <DataTable
          tableId={tableId}
          columns={result.current}
          data={mockData}
        />
      );

      // Category column should be hidden based on localStorage
      expect(screen.queryByRole("columnheader", { name: "Category" })).not.toBeInTheDocument();
      expect(screen.getByRole("columnheader", { name: "Name" })).toBeInTheDocument();
    });
  });

  describe("Row Selection and Bulk Actions", () => {
    it("renders bulk actions bar when rows are selected and supports deselect all", async () => {
      const user = userEvent.setup();

      function SelectableTable() {
        const [rowSelection, setRowSelection] = useState<Record<string, boolean>>({});

        const columns = useDataTableColumns(
          () => [
            columnHelper.display({
              id: "select",
              header: ({ table }) => (
                <input
                  type="checkbox"
                  aria-label="Select all"
                  checked={table.getIsAllPageRowsSelected()}
                  onChange={table.getToggleAllPageRowsSelectedHandler()}
                />
              ),
              cell: ({ row }) => (
                <input
                  type="checkbox"
                  aria-label={`Select row ${row.original.id}`}
                  checked={row.getIsSelected()}
                  onChange={row.getToggleSelectedHandler()}
                />
              ),
            }),
            columnHelper.accessor("name", { header: "Name" }),
          ],
          []
        );

        return (
          <DataTable
            columns={columns}
            data={mockData}
            enableRowSelection={true}
            rowSelection={rowSelection}
            onRowSelectionChange={setRowSelection}
            bulkActions={(selected) => (
              <button onClick={() => {}} data-testid="bulk-export-btn">
                Export ({selected.length})
              </button>
            )}
          />
        );
      }

      render(<SelectableTable />);

      expect(screen.queryByRole("region", { name: ja.table.bulkActionsAria })).not.toBeInTheDocument();

      // Select first row
      const firstCheckbox = screen.getByRole("checkbox", { name: "Select row 1" });
      await user.click(firstCheckbox);

      const bulkRegion = screen.getByRole("region", { name: ja.table.bulkActionsAria });
      expect(bulkRegion).toBeInTheDocument();
      expect(bulkRegion).toHaveTextContent("1");
      expect(bulkRegion).toHaveTextContent(ja.table.itemsSelectedSuffix);
      expect(screen.getByTestId("bulk-export-btn")).toHaveTextContent("Export (1)");

      // Deselect all
      const deselectBtn = screen.getByRole("button", { name: ja.table.deselectAll });
      await user.click(deselectBtn);

      await waitFor(() => {
        expect(screen.queryByRole("region", { name: ja.table.bulkActionsAria })).not.toBeInTheDocument();
      });
    });
  });

  describe("Accessibility (axe) Audit", () => {
    it("passes axe audit in populated state with sort controls and toolbar", async () => {
      const { result } = renderHook(() =>
        useDataTableColumns(
          () => [
            columnHelper.accessor("name", {
              header: ({ column }) => <DataTableColumnHeader column={column} title="Name" />,
            }),
            columnHelper.accessor("category", {
              header: ({ column }) => <DataTableColumnHeader column={column} title="Category" />,
            }),
            columnHelper.accessor("status", {
              header: "Status",
            }),
          ],
          []
        )
      );

      const { container } = render(
        <DataTable
          tableId="axe-test-table"
          columns={result.current}
          data={mockData}
          searchQuery=""
          onSearchChange={() => {}}
          searchPlaceholder="Search items…"
        />
      );

      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });

    it("passes axe audit in empty state with filter reset action", async () => {
      const { result } = renderHook(() =>
        useDataTableColumns(
          () => [
            columnHelper.accessor("name", { header: "Name" }),
          ],
          []
        )
      );

      const { container } = render(
        <DataTable
          columns={result.current}
          data={[]}
          isFiltered={true}
          onResetFilters={() => {}}
        />
      );

      const results = await axe(container);
      expect(results).toHaveNoViolations();
    });
  });
});
