import type { Table } from "@tanstack/react-table";
import { ChevronDown, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useT } from "@/i18n";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

interface DataTablePaginationProps<TData> {
  table: Table<TData>;
  totalLoaded?: number;
  hasNextPage?: boolean;
  isFetchingNextPage?: boolean;
  onFetchNextPage?: () => void;
  pageSize?: number;
  onPageSizeChange?: (pageSize: number) => void;
  pageSizeOptions?: number[];
}

export function DataTablePagination<TData>({
  table,
  totalLoaded,
  hasNextPage,
  isFetchingNextPage = false,
  onFetchNextPage,
  pageSize,
  onPageSizeChange,
  pageSizeOptions = [25, 50, 100, 200],
}: DataTablePaginationProps<TData>) {
  const t = useT();
  const rowCount = totalLoaded ?? table.getRowModel().rows.length;
  const selectedCount = table.getFilteredSelectedRowModel().rows.length;

  return (
    <div className="flex flex-wrap items-center justify-between gap-4 py-2 text-xs text-muted-foreground">
      <div className="flex items-center gap-2">
        <span>
          {selectedCount > 0 ? (
            <>
              <strong className="text-foreground">{selectedCount}</strong>{" "}
              {t("table.selectedOfPrefix")}{" "}
              <strong className="text-foreground">{rowCount}</strong>{" "}
              {t("table.rowsSelectedSuffix")}
            </>
          ) : (
            <>
              {t("table.showingPrefix")} <strong className="text-foreground">{rowCount}</strong>{" "}
              {rowCount === 1 ? t("table.rowsSuffixSingular") : t("table.rowsSuffixPlural")}
            </>
          )}
        </span>

        {pageSize && onPageSizeChange && (
          <div className="flex items-center gap-1.5 ml-4">
            <span>{t("table.rowsPerPage")}</span>
            <Select
              value={String(pageSize)}
              onValueChange={(val) => onPageSizeChange(Number(val))}
            >
              <SelectTrigger className="h-7 w-[70px] text-xs">
                <SelectValue placeholder={pageSize} />
              </SelectTrigger>
              <SelectContent side="top">
                {pageSizeOptions.map((size) => (
                  <SelectItem key={size} value={String(size)} className="text-xs">
                    {size}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
      </div>

      {hasNextPage && onFetchNextPage && (
        <Button
          variant="outline"
          size="sm"
          onClick={onFetchNextPage}
          disabled={isFetchingNextPage}
          className="h-8 text-xs font-medium"
        >
          {isFetchingNextPage ? (
            <>
              <Loader2 className="mr-2 size-3.5 animate-spin" />
              {t("table.loadingMore")}
            </>
          ) : (
            <>
              <ChevronDown className="mr-1.5 size-3.5" />
              {t("table.loadMore")}
            </>
          )}
        </Button>
      )}
    </div>
  );
}
