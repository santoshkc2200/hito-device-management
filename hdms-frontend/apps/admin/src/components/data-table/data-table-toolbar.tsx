import type { Table } from "@tanstack/react-table";
import { Search, X } from "lucide-react";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { DataTableColumnVisibility } from "./column-visibility";
import { cn } from "@hdms/ui";
import { useT } from "@/i18n";

interface DataTableToolbarProps<TData> {
  table: Table<TData>;
  tableId?: string;
  searchQuery?: string;
  onSearchChange?: (value: string) => void;
  searchPlaceholder?: string;
  isFiltered?: boolean;
  onResetFilters?: () => void;
  children?: ReactNode;
  actions?: ReactNode;
  className?: string;
  enableColumnVisibility?: boolean;
}

export function DataTableToolbar<TData>({
  table,
  tableId,
  searchQuery = "",
  onSearchChange,
  searchPlaceholder,
  isFiltered = false,
  onResetFilters,
  children,
  actions,
  className,
  enableColumnVisibility = true,
}: DataTableToolbarProps<TData>) {
  const t = useT();
  const placeholder = searchPlaceholder ?? t("table.searchPlaceholder");
  const [prevSearchQuery, setPrevSearchQuery] = useState(searchQuery);
  const [internalSearch, setInternalSearch] = useState(searchQuery);
  const searchInputRef = useRef<HTMLInputElement>(null);

  if (prevSearchQuery !== searchQuery) {
    setPrevSearchQuery(searchQuery);
    setInternalSearch(searchQuery);
  }

  // Handle global "/" shortcut to focus search input
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === "/" && !e.ctrlKey && !e.metaKey && !e.altKey) {
        const active = document.activeElement;
        const isInput =
          active instanceof HTMLInputElement ||
          active instanceof HTMLTextAreaElement ||
          active instanceof HTMLSelectElement ||
          (active instanceof HTMLElement && active.isContentEditable);

        if (!isInput && searchInputRef.current) {
          e.preventDefault();
          searchInputRef.current.focus();
          searchInputRef.current.select();
        }
      }
    }

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  const handleCommitSearch = (value: string) => {
    onSearchChange?.(value);
  };

  return (
    <div className={cn("flex flex-wrap items-center justify-between gap-2.5", className)}>
      <div className="flex flex-1 flex-wrap items-center gap-2">
        {onSearchChange && (
          <div className="relative w-full max-w-xs min-w-[220px]">
            <Search className="absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground pointer-events-none" />
            <Input
              ref={searchInputRef}
              placeholder={placeholder}
              value={internalSearch}
              onChange={(e) => setInternalSearch(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  handleCommitSearch(internalSearch);
                } else if (e.key === "Escape") {
                  setInternalSearch("");
                  handleCommitSearch("");
                  searchInputRef.current?.blur();
                }
              }}
              onBlur={() => {
                if (internalSearch !== searchQuery) {
                  handleCommitSearch(internalSearch);
                }
              }}
              className="pl-8 pr-14 h-8 text-xs"
              aria-label={placeholder}
            />
            {internalSearch ? (
              <button
                type="button"
                onClick={() => {
                  setInternalSearch("");
                  handleCommitSearch("");
                  searchInputRef.current?.focus();
                }}
                className="absolute top-1/2 right-2 -translate-y-1/2 text-muted-foreground hover:text-foreground p-0.5 rounded-sm"
                aria-label={t("table.clearSearchText")}
              >
                <X className="size-3.5" />
              </button>
            ) : (
              <kbd className="pointer-events-none absolute top-1/2 right-2 -translate-y-1/2 hidden h-4 select-none items-center gap-1 rounded border border-border bg-muted px-1.5 font-mono text-[10px] font-medium text-muted-foreground sm:inline-flex">
                /
              </kbd>
            )}
          </div>
        )}

        {children}

        {isFiltered && onResetFilters && (
          <Button
            variant="ghost"
            onClick={onResetFilters}
            className="h-8 px-2.5 text-xs text-muted-foreground hover:text-foreground"
            size="sm"
          >
            <X className="mr-1.5 size-3.5" />
            {t("table.resetFilters")}
          </Button>
        )}
      </div>

      <div className="flex items-center gap-2">
        {actions}
        {enableColumnVisibility && <DataTableColumnVisibility table={table} tableId={tableId} />}
      </div>
    </div>
  );
}
