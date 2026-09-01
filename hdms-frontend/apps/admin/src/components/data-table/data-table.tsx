import {
  type ColumnDef,
  type ColumnFiltersState,
  type OnChangeFn,
  type Row,
  type RowSelectionState,
  type SortingState,
  type VisibilityState,
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  useReactTable,
} from "@tanstack/react-table";
import { type LucideIcon, Inbox, SearchX } from "lucide-react";
import {
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  memo,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { EmptyState, ErrorState } from "@/components/states";
import { useT } from "@/i18n";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@hdms/ui";
import { DataTableBulkActions } from "./bulk-actions-bar";
import { getStoredColumnVisibility } from "./column-visibility";
import { DataTablePagination } from "./data-table-pagination";
import { DataTableToolbar } from "./data-table-toolbar";

export interface DataTableProps<TData, TValue = unknown> {
  tableId?: string;
  columns: ColumnDef<TData, TValue>[];
  data: TData[];
  getRowId?: (row: TData, index: number) => string;
  // State indicators
  isLoading?: boolean;
  isError?: boolean;
  error?: unknown;
  onRetry?: () => void;
  // Search & Filters
  searchQuery?: string;
  onSearchChange?: (query: string) => void;
  searchPlaceholder?: string;
  isFiltered?: boolean;
  onResetFilters?: () => void;
  filterControls?: ReactNode;
  toolbarActions?: ReactNode;
  hideToolbar?: boolean;
  enableColumnVisibility?: boolean;
  // Sorting
  sorting?: SortingState;
  onSortingChange?: OnChangeFn<SortingState>;
  // Selection
  rowSelection?: RowSelectionState;
  onRowSelectionChange?: OnChangeFn<RowSelectionState>;
  enableRowSelection?: boolean | ((row: Row<TData>) => boolean);
  bulkActions?: (selectedRows: Row<TData>[]) => ReactNode;
  itemLabel?: string;
  // Pagination
  hasNextPage?: boolean;
  isFetchingNextPage?: boolean;
  onFetchNextPage?: () => void;
  pageSize?: number;
  onPageSizeChange?: (pageSize: number) => void;
  pageSizeOptions?: number[];
  totalLoaded?: number;
  // Interaction
  onRowClick?: (item: TData) => void;
  // Empty state customisation
  emptyTitle?: string;
  emptyExplanation?: string;
  emptyIcon?: LucideIcon;
  emptyAction?: {
    label: string;
    onClick?: () => void;
    href?: string;
  };
  className?: string;
}

function DataTableInner<TData, TValue>({
  tableId,
  columns,
  data,
  getRowId,
  isLoading = false,
  isError = false,
  error,
  onRetry,
  searchQuery,
  onSearchChange,
  searchPlaceholder,
  isFiltered = false,
  onResetFilters,
  filterControls,
  toolbarActions,
  hideToolbar = false,
  enableColumnVisibility = true,
  sorting: externalSorting,
  onSortingChange: externalOnSortingChange,
  rowSelection: externalRowSelection,
  onRowSelectionChange: externalOnRowSelectionChange,
  enableRowSelection = false,
  bulkActions,
  itemLabel,
  hasNextPage,
  isFetchingNextPage,
  onFetchNextPage,
  pageSize,
  onPageSizeChange,
  pageSizeOptions,
  totalLoaded,
  onRowClick,
  emptyTitle,
  emptyExplanation,
  emptyIcon = Inbox,
  emptyAction,
  className,
}: DataTableProps<TData, TValue>) {
  const t = useT();

  // Memoize data to protect against callers accidentally passing inline arrays
  const memoizedData = useMemo(() => data, [data]);

  // Local sorting state fallback if not controlled externally
  const [internalSorting, setInternalSorting] = useState<SortingState>([]);
  const sorting = externalSorting ?? internalSorting;
  const onSortingChange = externalOnSortingChange ?? setInternalSorting;

  // Local row selection state fallback if not controlled externally
  const [internalRowSelection, setInternalRowSelection] = useState<RowSelectionState>({});
  const rowSelection = externalRowSelection ?? internalRowSelection;
  const onRowSelectionChange = externalOnRowSelectionChange ?? setInternalRowSelection;

  // Local column visibility state initialized from localStorage when tableId is provided
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>(() => {
    return tableId ? getStoredColumnVisibility(tableId) : {};
  });

  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([]);

  // Keyboard navigation focus state
  const [focusedRowIndex, setFocusedRowIndex] = useState<number>(-1);
  const tableContainerRef = useRef<HTMLDivElement>(null);

  const table = useReactTable({
    data: memoizedData,
    columns,
    getRowId,
    state: {
      sorting,
      columnVisibility,
      rowSelection,
      columnFilters,
    },
    enableRowSelection,
    onSortingChange,
    onColumnVisibilityChange: setColumnVisibility,
    onRowSelectionChange,
    onColumnFiltersChange: setColumnFilters,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
  });

  const rows = table.getRowModel().rows;
  const selectedRows = table.getSelectedRowModel().rows;

  // Reset keyboard focus if data rows change
  useEffect(() => {
    if (focusedRowIndex >= rows.length) {
      setFocusedRowIndex(rows.length > 0 ? 0 : -1);
    }
  }, [rows.length, focusedRowIndex]);

  // Handle keyboard navigation over table rows
  const handleKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (rows.length === 0) return;

    if (e.key === "ArrowDown") {
      e.preventDefault();
      setFocusedRowIndex((prev) => (prev < rows.length - 1 ? prev + 1 : prev));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setFocusedRowIndex((prev) => (prev > 0 ? prev - 1 : 0));
    } else if (e.key === "Enter") {
      if (focusedRowIndex >= 0 && focusedRowIndex < rows.length) {
        const row = rows[focusedRowIndex];
        if (row) {
          e.preventDefault();
          onRowClick?.(row.original);
        }
      }
    } else if (e.key === " " && enableRowSelection) {
      if (focusedRowIndex >= 0 && focusedRowIndex < rows.length) {
        const row = rows[focusedRowIndex];
        if (row && row.getCanSelect()) {
          e.preventDefault();
          row.toggleSelected();
        }
      }
    } else if (e.key === "Escape") {
      setFocusedRowIndex(-1);
    }
  };

  if (isError) {
    return (
      <div className={cn("space-y-4", className)}>
        <ErrorState error={error} onRetry={onRetry} />
      </div>
    );
  }

  const effectiveIsFiltered = isFiltered || !!searchQuery;

  return (
    <div className={cn("flex flex-col gap-3.5", className)}>
      {!hideToolbar && (
        <DataTableToolbar
          table={table}
          tableId={tableId}
          searchQuery={searchQuery}
          onSearchChange={onSearchChange}
          searchPlaceholder={searchPlaceholder ?? t("table.searchPlaceholder")}
          isFiltered={effectiveIsFiltered}
          onResetFilters={onResetFilters}
          enableColumnVisibility={enableColumnVisibility}
          actions={toolbarActions}
        >
          {filterControls}
        </DataTableToolbar>
      )}

      {selectedRows.length > 0 && bulkActions && (
        <DataTableBulkActions
          selectedCount={selectedRows.length}
          totalCount={rows.length}
          itemLabel={itemLabel}
          onClearSelection={() => table.resetRowSelection()}
        >
          {bulkActions(selectedRows)}
        </DataTableBulkActions>
      )}

      <div
        ref={tableContainerRef}
        tabIndex={0}
        role="region"
        aria-label={t("table.regionAria")}
        onKeyDown={handleKeyDown}
        className="relative rounded-md border border-border bg-card shadow-2xs outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
      >
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => {
                  return (
                    <TableHead
                      key={header.id}
                      colSpan={header.colSpan}
                      aria-sort={
                        header.column.getIsSorted() === "asc"
                          ? "ascending"
                          : header.column.getIsSorted() === "desc"
                            ? "descending"
                            : undefined
                      }
                    >
                      {header.isPlaceholder
                        ? null
                        : flexRender(header.column.columnDef.header, header.getContext())}
                    </TableHead>
                  );
                })}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {isLoading ? (
              Array.from({ length: 5 }).map((_, idx) => (
                <TableRow key={`skeleton-row-${idx}`}>
                  {columns.map((_, colIdx) => (
                    <TableCell key={`skeleton-cell-${idx}-${colIdx}`}>
                      <Skeleton className="h-4 w-full min-w-[60px]" />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : rows.length > 0 ? (
              rows.map((row, index) => {
                const isFocused = index === focusedRowIndex;
                const isSelected = row.getIsSelected();

                return (
                  <TableRow
                    key={row.id}
                    data-state={isSelected ? "selected" : undefined}
                    data-focused={isFocused ? "true" : undefined}
                    className={cn(
                      onRowClick && "cursor-pointer",
                      isFocused &&
                        "bg-accent/70 ring-1 ring-inset ring-primary/40 text-accent-foreground font-medium"
                    )}
                    onClick={() => {
                      setFocusedRowIndex(index);
                      onRowClick?.(row.original);
                    }}
                  >
                    {row.getVisibleCells().map((cell) => (
                      <TableCell key={cell.id}>
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </TableCell>
                    ))}
                  </TableRow>
                );
              })
            ) : (
              <TableRow>
                <TableCell colSpan={columns.length} className="h-64 text-center p-0">
                  {effectiveIsFiltered ? (
                    <EmptyState
                      icon={SearchX}
                      title={t("table.noMatchingResults")}
                      explanation={t("table.noMatchingResultsExplanation")}
                      action={
                        onResetFilters
                          ? {
                              label: t("table.clearFilters"),
                              onClick: onResetFilters,
                            }
                          : undefined
                      }
                      className="border-0 rounded-none bg-transparent"
                    />
                  ) : (
                    <EmptyState
                      icon={emptyIcon}
                      title={emptyTitle ?? t("table.emptyTitle")}
                      explanation={emptyExplanation ?? t("table.emptyExplanation")}
                      action={emptyAction}
                      className="border-0 rounded-none bg-transparent"
                    />
                  )}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <DataTablePagination
        table={table}
        totalLoaded={totalLoaded}
        hasNextPage={hasNextPage}
        isFetchingNextPage={isFetchingNextPage}
        onFetchNextPage={onFetchNextPage}
        pageSize={pageSize}
        onPageSizeChange={onPageSizeChange}
        pageSizeOptions={pageSizeOptions}
      />
    </div>
  );
}

export const DataTable = memo(DataTableInner) as typeof DataTableInner;
