import * as React from "react";
import { cn } from "@hdms/ui";

const TONES = {
  info: "border-primary/30 bg-primary/5 text-foreground",
  warning: "border-warning/50 bg-warning/10 text-foreground",
  error: "border-destructive/40 bg-destructive/5 text-destructive",
} as const;

export function Message({
  tone,
  id,
  testId,
  children,
}: {
  tone: keyof typeof TONES;
  id?: string;
  testId?: string;
  children: React.ReactNode;
}) {
  return (
    <div
      id={id}
      data-testid={testId}
      role={tone === "error" ? "alert" : "status"}
      className={cn("rounded-lg border p-4 text-sm", TONES[tone])}
    >
      {children}
    </div>
  );
}
