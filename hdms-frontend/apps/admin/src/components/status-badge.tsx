import { cn } from "@hdms/ui";

// One status vocabulary for device/user/credential state, so a color means
// the same thing everywhere in the console (docs/08-admin-console.md).
export type StatusTone = "success" | "warning" | "destructive" | "muted" | "primary";

const toneDot: Record<StatusTone, string> = {
  success: "bg-success",
  warning: "bg-warning",
  destructive: "bg-destructive",
  muted: "bg-muted-foreground",
  primary: "bg-primary",
};

export function StatusBadge({
  label,
  tone,
  className,
}: {
  label: string;
  tone: StatusTone;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 text-sm text-foreground",
        className,
      )}
    >
      <span className={cn("size-1.5 shrink-0 rounded-full", toneDot[tone])} aria-hidden />
      {label}
    </span>
  );
}

export const deviceStatusTone: Record<string, StatusTone> = {
  available: "success",
  on_loan: "primary",
  maintenance: "warning",
  retired: "muted",
  lost: "destructive",
};

export const userStatusTone: Record<string, StatusTone> = {
  active: "success",
  suspended: "warning",
  archived: "muted",
};

export const credentialStatusTone: Record<string, StatusTone> = {
  active: "success",
  revoked: "destructive",
  lost: "warning",
};

export const loanStatusTone: Record<string, StatusTone> = {
  open: "primary",
  returned: "success",
  written_off: "destructive",
  overdue: "warning",
};

export function LoanOriginBadge({
  origin,
  disputed,
  className,
}: {
  origin: "kiosk" | "paper" | "admin" | "import" | string;
  disputed?: boolean;
  className?: string;
}) {
  if (disputed) {
    return (
      <span
        data-testid="origin-badge-disputed"
        className={cn(
          "inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs font-medium border bg-rose-50 border-rose-200 text-rose-800 dark:bg-rose-950/60 dark:border-rose-900 dark:text-rose-300",
          className,
        )}
      >
        <span className="size-1.5 rounded-full bg-rose-500 shrink-0" aria-hidden />
        Disputed
      </span>
    );
  }

  switch (origin) {
    case "paper":
      return (
        <span
          data-testid="origin-badge-paper"
          className={cn(
            "inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs font-medium border bg-amber-50 border-amber-200 text-amber-800 dark:bg-amber-950/60 dark:border-amber-900 dark:text-amber-300",
            className,
          )}
        >
          <span className="size-1.5 rounded-full bg-amber-500 shrink-0" aria-hidden />
          Paper
        </span>
      );
    case "admin":
      return (
        <span
          data-testid="origin-badge-admin"
          className={cn(
            "inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs font-medium border bg-blue-50 border-blue-200 text-blue-800 dark:bg-blue-950/60 dark:border-blue-900 dark:text-blue-300",
            className,
          )}
        >
          <span className="size-1.5 rounded-full bg-blue-500 shrink-0" aria-hidden />
          Admin
        </span>
      );
    case "import":
      return (
        <span
          data-testid="origin-badge-import"
          className={cn(
            "inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs font-medium border bg-purple-50 border-purple-200 text-purple-800 dark:bg-purple-950/60 dark:border-purple-900 dark:text-purple-300",
            className,
          )}
        >
          <span className="size-1.5 rounded-full bg-purple-500 shrink-0" aria-hidden />
          Import
        </span>
      );
    case "kiosk":
    default:
      return (
        <span
          data-testid="origin-badge-kiosk"
          className={cn(
            "inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs font-medium border bg-slate-50 border-slate-200 text-slate-700 dark:bg-slate-900 dark:border-slate-800 dark:text-slate-300",
            className,
          )}
        >
          <span className="size-1.5 rounded-full bg-slate-400 shrink-0" aria-hidden />
          Kiosk
        </span>
      );
  }
}

export function labelize(status: string): string {
  return status.replace(/_/g, " ");
}

