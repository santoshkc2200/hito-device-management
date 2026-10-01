import * as React from "react";
import { CheckCircle2, Circle, Loader2 } from "lucide-react";
import { useLocale } from "@hdms/i18n";
import { Message } from "@/components/message";
import { Button } from "@/components/ui/button";
import { buttonVariants } from "@/components/ui/button-variants";
import { useT, type RecoveryKey, type Translate } from "@/i18n";
import { asRecoveryError, recoveryApi, type RecoveryError, type RestoreView, type Step } from "@/lib/api";
import { commonError } from "@/lib/errors";
import { formatSnapshotDate } from "@/lib/format";

export const POLL_MS = 2000;
/** The admin console in production (Caddy serves it at /admin/). */
export const ADMIN_URL = "/admin/";

const STEP_LABELS: Record<Step, RecoveryKey> = {
  safety_backup: "progress.steps.safety_backup",
  restore_scratch: "progress.steps.restore_scratch",
  migrate_scratch: "progress.steps.migrate_scratch",
  validate: "progress.steps.validate",
  maintenance_on: "progress.steps.maintenance_on",
  copy_forward: "progress.steps.copy_forward",
  swap: "progress.steps.swap",
  maintenance_off: "progress.steps.maintenance_off",
  record: "progress.steps.record",
};

const WARNINGS: Record<string, RecoveryKey> = {
  maintenance_off_failed: "done.warnings.maintenance_off_failed",
  record_failed: "done.warnings.record_failed",
};

/** state.go: Error is "<step>_failed", "interrupted" or "no_admins". */
function failureReason(t: Translate, code?: string): string | null {
  if (code === "interrupted") return t("failed.errors.interrupted");
  if (code === "no_admins") return t("failed.errors.no_admins");
  const step = code?.replace(/_failed$/, "") as Step | undefined;
  if (step && step in STEP_LABELS) return t("failed.errors.step", { step: t(STEP_LABELS[step]) });
  return null;
}

type StepState = "done" | "current" | "pending";

function StepList({ view }: { view: RestoreView }) {
  const t = useT();
  const current = view.steps.indexOf(view.step);
  return (
    <ol className="space-y-2">
      {view.steps.map((step, i) => {
        const state: StepState =
          view.phase === "completed" || i < current ? "done" : i === current ? "current" : "pending";
        const Icon = state === "done" ? CheckCircle2 : state === "current" ? Loader2 : Circle;
        const stateKey: RecoveryKey =
          state === "done" ? "progress.stepDone" : state === "current" ? "progress.stepCurrent" : "progress.stepPending";
        return (
          <li key={step} data-testid={`step-${step}`} data-state={state} className="flex items-center gap-3">
            <Icon
              aria-hidden="true"
              className={
                state === "done"
                  ? "size-5 text-success"
                  : state === "current"
                    ? "size-5 animate-spin text-primary"
                    : "size-5 text-muted-foreground"
              }
            />
            <span className={state === "pending" ? "text-muted-foreground" : undefined}>{t(STEP_LABELS[step])}</span>
            <span className="sr-only">{t(stateKey)}</span>
          </li>
        );
      })}
    </ol>
  );
}

export function ProgressScreen({
  pollMs = POLL_MS,
  onUndo,
  onRestart,
  onSessionLost,
}: {
  pollMs?: number;
  onUndo: (snapshotTakenAt: string) => void;
  onRestart: () => void;
  onSessionLost: () => void;
}) {
  const t = useT();
  const { locale } = useLocale();
  const [view, setView] = React.useState<RestoreView | null>(null);
  const [error, setError] = React.useState<RecoveryError | null>(null);
  const sessionLost = React.useRef(onSessionLost);
  React.useLayoutEffect(() => {
    sessionLost.current = onSessionLost;
  });

  // Poll until the run ends. A failed poll (the worker restarting, Caddy
  // answering 502) keeps polling; only a lost session leaves the screen.
  React.useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const poll = async () => {
      try {
        const next = await recoveryApi.restore();
        if (cancelled) return;
        setView(next);
        setError(null);
        if (next?.phase !== "running") return;
      } catch (err) {
        if (cancelled) return;
        const e = asRecoveryError(err);
        if (e.sessionLost) {
          sessionLost.current();
          return;
        }
        setError(e);
      }
      timer = setTimeout(poll, pollMs);
    };
    void poll();
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [pollMs]);

  if (!view) {
    return error ? (
      <Message tone="warning">{commonError(t, error)}</Message>
    ) : (
      <p className="text-muted-foreground">{t("common.loading")}</p>
    );
  }

  const date = formatSnapshotDate(locale, view.snapshotTakenAt);

  if (view.phase === "failed") {
    const reason = failureReason(t, view.error);
    return (
      <section className="space-y-6">
        <Message tone="error" testId="restore-failed">
          <h2 className="font-semibold">{t(view.kind === "undo" ? "failed.undoHeading" : "failed.heading")}</h2>
          <p className="mt-1">{t("failed.body")}</p>
          {reason && <p className="mt-1">{reason}</p>}
        </Message>
        <Button onClick={onRestart}>{t("failed.again")}</Button>
      </section>
    );
  }

  if (view.phase === "completed") {
    const warning = view.warning ? WARNINGS[view.warning] : undefined;
    return (
      <section className="space-y-6">
        <Message tone="info" testId="restore-done">
          <h2 className="font-semibold">{t(view.kind === "undo" ? "done.undoneHeading" : "done.heading")}</h2>
          <p className="mt-1">{view.kind === "undo" ? t("done.undoneBody") : t("done.body", { date })}</p>
        </Message>
        {view.warning && (
          <Message tone="warning" testId="restore-warning">
            {warning ? t(warning) : t("common.unexpected")}
          </Message>
        )}
        <div className="flex flex-wrap gap-3">
          <a href={ADMIN_URL} className={buttonVariants({ size: "lg" })}>
            {t("done.openAdmin")}
          </a>
          {view.canUndo && (
            <Button variant="outline" size="lg" onClick={() => onUndo(view.snapshotTakenAt)}>
              {t("snapshots.undo")}
            </Button>
          )}
        </div>
      </section>
    );
  }

  return (
    <section className="space-y-6">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold">{t(view.kind === "undo" ? "progress.undoHeading" : "progress.heading")}</h2>
        <p className="text-sm text-muted-foreground">{t("progress.keepOpen")}</p>
      </div>
      {error && <Message tone="warning">{commonError(t, error)}</Message>}
      <StepList view={view} />
    </section>
  );
}
