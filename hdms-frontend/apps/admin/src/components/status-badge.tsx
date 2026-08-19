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

export function labelize(status: string): string {
  return status.replace(/_/g, " ");
}
