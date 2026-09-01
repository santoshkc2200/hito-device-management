import { X } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { useT } from "@/i18n";
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
  itemLabel,
}: DataTableBulkActionsProps) {
  const t = useT();

  if (selectedCount === 0) return null;

  // The caller may name the entity ("device"); with no name we fall back to the
  // catalogue's generic one. Pluralisation stays English-shaped only for the
  // caller-supplied label, which Task 6 revisits together with its call sites.
  const pluralLabel = itemLabel
    ? selectedCount === 1
      ? itemLabel
      : `${itemLabel}s`
    : selectedCount === 1
      ? t("table.defaultItemLabel")
      : t("table.defaultItemLabelPlural");

  return (
    <div
      role="region"
      aria-label={t("table.bulkActionsAria")}
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
          {" "}{pluralLabel} {t("table.itemsSelectedSuffix")}
        </span>
        <Button
          variant="ghost"
          size="sm"
          onClick={onClearSelection}
          className="h-7 px-2 text-xs text-muted-foreground hover:text-foreground"
          aria-label={t("table.deselectAll")}
        >
          <X className="mr-1 size-3.5" />
          {t("table.deselectAll")}
        </Button>
      </div>

      {children && <div className="flex items-center gap-2">{children}</div>}
    </div>
  );
}
