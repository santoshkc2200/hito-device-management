import type { Column } from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ChevronsUpDown, EyeOff } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@hdms/ui";
import { useT } from "@/i18n";

interface DataTableColumnHeaderProps<TData, TValue>
  extends React.HTMLAttributes<HTMLDivElement> {
  column: Column<TData, TValue>;
  title: string;
}

export function DataTableColumnHeader<TData, TValue>({
  column,
  title,
  className,
}: DataTableColumnHeaderProps<TData, TValue>) {
  const t = useT();

  if (!column.getCanSort()) {
    return <div className={cn("text-xs font-medium text-muted-foreground", className)}>{title}</div>;
  }

  const isSorted = column.getIsSorted();

  return (
    <div className={cn("flex items-center space-x-2", className)}>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            className="-ml-3 h-8 data-[state=open]:bg-accent text-xs font-medium"
            aria-label={t("table.sortByAria", {
              title,
              state:
                isSorted === "desc"
                  ? t("table.sortedDescending")
                  : isSorted === "asc"
                    ? t("table.sortedAscending")
                    : t("table.unsorted"),
            })}
          >
            <span>{title}</span>
            {isSorted === "desc" ? (
              <ArrowDown className="ml-2 size-3.5" aria-hidden="true" />
            ) : isSorted === "asc" ? (
              <ArrowUp className="ml-2 size-3.5" aria-hidden="true" />
            ) : (
              <ChevronsUpDown className="ml-2 size-3.5 text-muted-foreground" aria-hidden="true" />
            )}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuItem onClick={() => column.toggleSorting(false)}>
            <ArrowUp className="mr-2 size-3.5 text-muted-foreground" />
            {t("table.sortAscending")}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => column.toggleSorting(true)}>
            <ArrowDown className="mr-2 size-3.5 text-muted-foreground" />
            {t("table.sortDescending")}
          </DropdownMenuItem>
          {isSorted && (
            <DropdownMenuItem onClick={() => column.clearSorting()}>
              <ChevronsUpDown className="mr-2 size-3.5 text-muted-foreground" />
              {t("table.clearSort")}
            </DropdownMenuItem>
          )}
          {column.getCanHide() && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => column.toggleVisibility(false)}>
                <EyeOff className="mr-2 size-3.5 text-muted-foreground" />
                {t("table.hideColumn")}
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
