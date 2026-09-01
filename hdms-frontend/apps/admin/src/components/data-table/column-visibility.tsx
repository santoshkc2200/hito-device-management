import type { Table, VisibilityState } from "@tanstack/react-table";
import { SlidersHorizontal } from "lucide-react";
import { useEffect } from "react";
import { Button } from "@/components/ui/button";
import { useT } from "@/i18n";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

interface DataTableColumnVisibilityProps<TData> {
  table: Table<TData>;
  tableId?: string;
}

export function getStoredColumnVisibility(tableId: string): VisibilityState {
  try {
    const raw = localStorage.getItem(`hdms_table_visibility_${tableId}`);
    return raw ? JSON.parse(raw) : {};
  } catch {
    return {};
  }
}

export function setStoredColumnVisibility(tableId: string, visibility: VisibilityState) {
  try {
    localStorage.setItem(`hdms_table_visibility_${tableId}`, JSON.stringify(visibility));
  } catch {
    // ignore storage quota or private mode errors
  }
}

export function DataTableColumnVisibility<TData>({
  table,
  tableId,
}: DataTableColumnVisibilityProps<TData>) {
  const t = useT();
  const columns = table
    .getAllColumns()
    .filter((column) => typeof column.accessorFn !== "undefined" && column.getCanHide());

  // Save changes to localStorage whenever visibility changes
  const visibilityState = table.getState().columnVisibility;
  useEffect(() => {
    if (tableId && Object.keys(visibilityState).length > 0) {
      setStoredColumnVisibility(tableId, visibilityState);
    }
  }, [tableId, visibilityState]);

  if (columns.length === 0) return null;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className="ml-auto hidden h-8 lg:flex text-xs"
          aria-label={t("table.toggleColumnVisibilityAria")}
        >
          <SlidersHorizontal className="mr-2 size-3.5" />
          {t("table.viewColumns")}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-[180px]">
        <DropdownMenuLabel className="text-xs">{t("table.toggleColumns")}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {columns.map((column) => {
          return (
            <DropdownMenuCheckboxItem
              key={column.id}
              className="capitalize text-xs"
              checked={column.getIsVisible()}
              onCheckedChange={(value) => column.toggleVisibility(!!value)}
            >
              {column.id.replace(/([A-Z])/g, " $1").toLowerCase()}
            </DropdownMenuCheckboxItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
