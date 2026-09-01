import * as React from "react";
import { AlertTriangle, BookOpen, RefreshCw } from "lucide-react";
import { useTranslator } from "@/i18n";

export interface ErrorBoundaryProps {
  kioskName?: string;
  children: React.ReactNode;
  onReload?: () => void;
}

export interface ErrorBoundaryState {
  hasError: boolean;
  supportCode: string | null;
  countdown: number;
}

function ErrorBoundaryView({
  kioskName,
  supportCode,
  countdown,
  onReload,
}: {
  kioskName: string;
  supportCode: string | null;
  countdown: number;
  onReload: () => void;
}) {
  const t = useTranslator();

  return (
    <div
      data-testid="error-boundary-screen"
      className="flex min-h-dvh flex-col justify-between bg-background text-foreground select-none p-6 md:p-12 font-sans"
    >
      {/* Header */}
      <header className="flex w-full items-center justify-between border-b border-border pb-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-foreground">
            {kioskName}
          </h1>
          <p className="text-xs text-muted-foreground">{t("errorBoundary.selfHealing")}</p>
        </div>
        {supportCode && (
          <span
            data-testid="error-boundary-support-code"
            className="font-mono text-xs font-bold px-3 py-1.5 rounded-md bg-muted text-foreground"
          >
            Incident: <strong>{supportCode}</strong>
          </span>
        )}
      </header>

      {/* Main Error Recovery Notice */}
      <main className="flex flex-1 flex-col items-center justify-center text-center space-y-6 py-6 max-w-2xl mx-auto">
        <div className="p-5 rounded-full bg-amber-500/15 text-amber-600 dark:text-amber-400 shadow-md">
          <AlertTriangle className="size-16 stroke-[2.5]" aria-hidden="true" />
        </div>

        <div className="space-y-2">
          <h2
            data-testid="error-boundary-title"
            className="text-3xl md:text-4xl font-extrabold tracking-tight text-foreground"
          >
            {t("errorBoundary.title")}
          </h2>
          <p className="text-base md:text-lg text-muted-foreground">
            {t("errorBoundary.subtitle")}
          </p>
        </div>

        {/* Paper Fallback Box */}
        <div className="w-full rounded-2xl border-2 border-border bg-card p-6 text-left space-y-3 shadow-xs">
          <div className="flex items-start gap-3">
            <div className="p-2.5 rounded-lg bg-secondary text-secondary-foreground shrink-0">
              <BookOpen className="size-6 text-primary" aria-hidden="true" />
            </div>
            <div className="space-y-1">
              <h3 className="font-bold text-foreground">{t("errorBoundary.paperFallbackTitle")}</h3>
              <p className="text-sm text-muted-foreground leading-relaxed">
                {t("errorBoundary.paperFallbackDetail")}
              </p>
            </div>
          </div>
        </div>

        {/* Countdown and Restart Action */}
        <div className="w-full space-y-3 pt-2">
          <p
            data-testid="error-boundary-countdown"
            className="text-sm font-semibold text-muted-foreground"
          >
            {t("errorBoundary.restartingCountdown", { seconds: countdown })}
          </p>

          <button
            type="button"
            data-testid="error-boundary-reload-button"
            className="inline-flex min-h-16 w-full items-center justify-center gap-2 rounded-xl bg-primary text-primary-foreground text-lg font-bold shadow-lg hover:bg-primary/90 transition-all cursor-pointer"
            onClick={onReload}
          >
            <RefreshCw className="size-5" aria-hidden="true" />
            <span>{t("errorBoundary.restartNow")}</span>
          </button>
        </div>
      </main>

      {/* Footer */}
      <footer className="border-t border-border pt-4 text-center text-xs text-muted-foreground">
        <span>{t("common.automaticRecovery")}</span>
      </footer>
    </div>
  );
}

export class ErrorBoundary extends React.Component<
  ErrorBoundaryProps,
  ErrorBoundaryState
> {
  private timer: ReturnType<typeof setInterval> | null = null;

  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = {
      hasError: false,
      supportCode: null,
      countdown: 15,
    };
  }

  static getDerivedStateFromError(): Partial<ErrorBoundaryState> {
    return { hasError: true };
  }

  componentDidCatch(error: Error): void {
    const kioskName = this.props.kioskName || "KIOSK-01";
    const incidentId = Math.random().toString(36).substring(2, 8).toUpperCase();
    const supportCode = `${kioskName}-${incidentId}`;

    this.setState({ supportCode, countdown: 15 });

    // Safely write to local diagnostics ring buffer
    try {
      if (typeof window !== "undefined" && window.localStorage) {
        const raw = window.localStorage.getItem("hdms_incident_logs");
        const logs = raw ? JSON.parse(raw) : [];
        logs.unshift({
          timestamp: new Date().toISOString(),
          supportCode,
          message: error?.message || "Unknown render error",
          stack: error?.stack,
        });
        window.localStorage.setItem(
          "hdms_incident_logs",
          JSON.stringify(logs.slice(0, 50))
        );
      }
    } catch {
      // Intentionally silent: boundary must never crash during logging
    }

    // Auto-reload after 15 seconds
    this.timer = setInterval(() => {
      this.setState((prev) => {
        if (prev.countdown <= 1) {
          if (this.timer) clearInterval(this.timer);
          this.handleReload();
          return { ...prev, countdown: 0 };
        }
        return { ...prev, countdown: prev.countdown - 1 };
      });
    }, 1000);
  }

  componentWillUnmount(): void {
    if (this.timer) {
      clearInterval(this.timer);
    }
  }

  private handleReload = () => {
    if (this.props.onReload) {
      this.props.onReload();
    } else if (typeof window !== "undefined" && window.location) {
      window.location.reload();
    }
  };

  render(): React.ReactNode {
    if (!this.state.hasError) {
      return this.props.children;
    }

    const { supportCode, countdown } = this.state;
    const kioskName = this.props.kioskName || "HDMS Kiosk";

    return (
      <ErrorBoundaryView
        kioskName={kioskName}
        supportCode={supportCode}
        countdown={countdown}
        onReload={this.handleReload}
      />
    );
  }
}
