import { X } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { cn } from "@hdms/ui";

interface DataTableBulkActionsProps {
  selectedCount: number;
  totalCount?: number;
  onClearSelection: () => void;
  children?: ReactNode;
  className?: string;
  itemLabel?: string;
}

export function DataTableBulkActions({
  selectedCount,
  onClearSelection,
  children,
  className,
  itemLabel = "item",
}: DataTableBulkActionsProps) {
  if (selectedCount === 0) return null;

  const pluralLabel = selectedCount === 1 ? itemLabel : `${itemLabel}s`;

  return (
    <div
      role="region"
      aria-label="Bulk actions"
      aria-live="polite"
      className={cn(
        "flex flex-wrap items-center justify-between gap-3 rounded-lg border border-primary/20 bg-primary/5 px-4 py-2.5 text-sm text-foreground shadow-xs animate-in fade-in slide-in-from-top-2 duration-200",
        className
      )}
    >
      <div className="flex items-center gap-2">
        <span className="font-medium text-foreground">
          <span className="inline-flex items-center justify-center rounded-full bg-primary px-2 py-0.5 text-xs font-semibold text-primary-foreground mr-1.5">
            {selectedCount}
          </span>
          {" "}{pluralLabel} selected
        </span>
        <Button
          variant="ghost"
          size="sm"
          onClick={onClearSelection}
          className="h-7 px-2 text-xs text-muted-foreground hover:text-foreground"
          aria-label="Deselect all"
        >
          <X className="mr-1 size-3.5" />
          Deselect all
        </Button>
      </div>

      {children && <div className="flex items-center gap-2">{children}</div>}
    </div>
  );
}
