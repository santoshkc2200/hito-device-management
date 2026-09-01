import type { Problem } from "@hdms/api-client";
import { AlertTriangle, RefreshCw, Wrench } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useT } from "@/i18n";

export interface ErrorStateProps {
  error?: unknown;
  title?: string;
  detail?: string;
  requestId?: string;
  task?: string;
  onRetry?: () => void;
  className?: string;
}

function parseProblemOrError(
  error: unknown,
  t: ReturnType<typeof useT>
): {
  title: string;
  detail?: string;
  requestId?: string;
  task?: string;
  status?: number;
} {
  if (!error) {
    return { title: t("states.unexpectedError") };
  }

  if (typeof error === "string") {
    return { title: t("states.error"), detail: error };
  }

  if (typeof error === "object" && error !== null) {
    const err = error as Problem & {
      message?: string;
      task?: string;
      extensions?: Record<string, unknown>;
    };

    const task =
      err.task ||
      (err.extensions?.task as string | undefined) ||
      (typeof err.extensions === "object" && err.extensions !== null && "task" in err.extensions
        ? String(err.extensions.task)
        : undefined);

    const title = err.title || (err.status === 501 ? t("states.notImplemented") : t("states.error"));
    const detail = err.detail || err.message;
    const requestId = err.requestId;
    const status = err.status;

    return { title, detail, requestId, task, status };
  }

  return { title: t("states.unexpectedError") };
}

export function ErrorState({
  error,
  title: customTitle,
  detail: customDetail,
  requestId: customRequestId,
  task: customTask,
  onRetry,
  className = "",
}: ErrorStateProps) {
  const t = useT();
  const parsed = parseProblemOrError(error, t);

  const title = customTitle || parsed.title;
  const detail = customDetail || parsed.detail;
  const requestId = customRequestId || parsed.requestId;
  const task = customTask || parsed.task;
  const isStub = parsed.status === 501 || !!task;

  return (
    <div
      role="alert"
      className={`flex flex-col items-center justify-center rounded-lg border border-border bg-card p-8 text-center ${className}`}
    >
      <div
        className={`mb-4 rounded-full p-3 ${
          isStub ? "bg-amber-500/10 text-amber-600" : "bg-destructive/10 text-destructive"
        }`}
      >
        {isStub ? (
          <Wrench className="size-6" aria-hidden="true" />
        ) : (
          <AlertTriangle className="size-6" aria-hidden="true" />
        )}
      </div>

      <h3 className="text-base font-semibold text-foreground">
        {isStub && task ? t("states.notBuiltYet", { task }) : title}
      </h3>

      {detail && <p className="mt-2 max-w-md text-sm text-muted-foreground">{detail}</p>}

      {requestId && (
        <p className="mt-2 font-mono text-xs text-muted-foreground/80">
          {t("states.requestId")}: <span className="select-all font-semibold">{requestId}</span>
        </p>
      )}

      {onRetry && (
        <div className="mt-6">
          <Button variant="outline" size="sm" onClick={onRetry}>
            <RefreshCw className="mr-2 size-4" />
            {t("states.tryAgain")}
          </Button>
        </div>
      )}
    </div>
  );
}
