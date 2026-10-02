import type { BackupRestore } from "@hdms/api-client";
import { Check, Circle, Loader2, X } from "lucide-react";
import { useT } from "@/i18n";

type StepState = "done" | "current" | "failed" | "pending";

function stepState(restore: BackupRestore, i: number): StepState {
  const at = restore.steps.indexOf(restore.step);
  if (restore.phase === "completed" || i < at) return "done";
  if (i > at) return "pending";
  return restore.phase === "failed" ? "failed" : "current";
}

export function RestoreSteps({ restore }: { restore: BackupRestore }) {
  const t = useT();
  return (
    <ol className="flex flex-col gap-1.5" aria-label={t("backups.restore.stepsLabel")}>
      {restore.steps.map((step, i) => {
        const state = stepState(restore, i);
        return (
          <li key={step} data-state={state} className="flex items-center gap-2 text-sm">
            {state === "done" && <Check className="size-4 text-primary" aria-hidden="true" />}
            {state === "current" && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
            {state === "failed" && <X className="size-4 text-destructive" aria-hidden="true" />}
            {state === "pending" && <Circle className="size-4 text-muted-foreground" aria-hidden="true" />}
            <span className={state === "pending" ? "text-muted-foreground" : undefined}>
              {t(`backups.restore.steps.${step}` as never)}
            </span>
          </li>
        );
      })}
    </ol>
  );
}
