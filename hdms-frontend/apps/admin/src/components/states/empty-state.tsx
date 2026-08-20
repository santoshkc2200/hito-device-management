import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";

export interface EmptyStateProps {
  title: string;
  explanation: string;
  icon?: LucideIcon;
  action?: {
    label: string;
    onClick?: () => void;
    href?: string;
  };
  children?: ReactNode;
  className?: string;
}

export function EmptyState({
  title,
  explanation,
  icon: Icon,
  action,
  children,
  className = "",
}: EmptyStateProps) {
  return (
    <div
      className={`flex flex-col items-center justify-center rounded-lg border border-dashed border-border bg-card p-12 text-center ${className}`}
    >
      {Icon && (
        <div className="mb-4 rounded-full bg-secondary p-3 text-muted-foreground">
          <Icon className="size-6" aria-hidden="true" />
        </div>
      )}
      <h3 className="text-base font-semibold text-foreground">{title}</h3>
      <p className="mt-1.5 max-w-sm text-sm text-muted-foreground">{explanation}</p>

      {action && (
        <div className="mt-6">
          {action.href ? (
            <Button asChild size="sm">
              <a href={action.href}>{action.label}</a>
            </Button>
          ) : (
            <Button size="sm" onClick={action.onClick}>
              {action.label}
            </Button>
          )}
        </div>
      )}

      {children && <div className="mt-6">{children}</div>}
    </div>
  );
}
