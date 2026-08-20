import * as React from "react";
import { AlertCircle, FileText, Info } from "lucide-react";
import type { SessionMessage, MessageTone } from "@hdms/api-client";
import { errorMessage, type KioskProblem } from "@/lib/problem";
import { Button } from "@/components/ui/button";
import { ScreenFrame } from "./screen-frame";

export interface BlockedScreenProps {
  message?: SessionMessage | null;
  problem?: KioskProblem | null;
  onDismiss: () => void;
  kioskName?: string;
  supportCode?: string | null;
  scannerReady?: boolean;
  scannerFresh?: boolean;
  onToggleCamera?: () => void;
  onOpenDiagnostics?: () => void;
  onOpenManualEntry?: () => void;
}

export function BlockedScreen({
  message,
  problem,
  onDismiss,
  kioskName = "HDMS Kiosk",
  supportCode = "KIOSK-01",
  scannerReady = true,
  scannerFresh = true,
  onToggleCamera,
  onOpenDiagnostics,
  onOpenManualEntry,
}: BlockedScreenProps) {
  // Auto-dismiss after 8 seconds (8000 ms)
  React.useEffect(() => {
    const timer = setTimeout(() => {
      onDismiss();
    }, 8000);
    return () => clearTimeout(timer);
  }, [onDismiss]);

  const mappedProblem = problem ? errorMessage(problem) : null;

  const title =
    message?.title ||
    mappedProblem?.title ||
    problem?.title ||
    "Action Not Completed";

  const detail =
    message?.detail ||
    mappedProblem?.detail ||
    problem?.detail ||
    "This item cannot be issued right now. Please record your checkout on the paper register or contact the equipment administrator.";

  const tone: MessageTone | string = message?.tone || mappedProblem?.tone || "warning";

  // Pick non-crash tone styling (amber / slate / blue-grey)
  const isInfo = tone === "info" || tone === "neutral";
  const toneBg = isInfo
    ? "bg-sky-500/15 text-sky-700 dark:text-sky-300"
    : "bg-amber-500/15 text-amber-700 dark:text-amber-300";

  const toneBadge = isInfo
    ? "bg-sky-100 text-sky-800 dark:bg-sky-950 dark:text-sky-200"
    : "bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200";

  const effectiveSupportCode = supportCode || problem?.supportCode || null;

  return (
    <ScreenFrame
      kioskName={kioskName}
      supportCode={effectiveSupportCode}
      scannerReady={scannerReady}
      scannerFresh={scannerFresh}
      onToggleCamera={onToggleCamera}
      onOpenDiagnostics={onOpenDiagnostics}
      onOpenManualEntry={onOpenManualEntry}
    >

      <div
        data-testid="blocked-screen"
        className="flex flex-col items-center justify-between h-full w-full max-w-3xl space-y-6 py-4 text-center"
      >
        {/* Top Status Icon & Badge */}
        <div className="space-y-4 flex flex-col items-center">
          <div
            data-testid="blocked-icon-container"
            className={`p-4 rounded-full shadow-md ${toneBg} ${
              isInfo ? "animate-feedback-info" : "animate-feedback-reject"
            }`}
          >
            {isInfo ? (
              <Info className="size-16 stroke-[2.5]" aria-hidden="true" />
            ) : (
              <AlertCircle className="size-16 stroke-[2.5]" aria-hidden="true" />
            )}
          </div>

          <span
            data-testid="blocked-tone-badge"
            className={`font-mono text-xs font-bold uppercase tracking-wider px-3 py-1 rounded-full ${toneBadge}`}
          >
            Notice
          </span>

          {/* Primary Reason Title: ≥ 36px font size */}
          <h2
            data-testid="blocked-title"
            className="text-4xl md:text-5xl font-extrabold tracking-tight text-foreground"
            style={{ fontSize: "clamp(2rem, 5vw, 2.75rem)" }}
          >
            {title}
          </h2>
        </div>

        {/* Prominently Laid-Out Guidance Box (Not squeezed beneath icon) */}
        <div
          data-testid="blocked-guidance-card"
          className="w-full max-w-xl rounded-2xl border-2 border-border bg-card p-6 md:p-8 shadow-sm space-y-4 text-left"
        >
          <div className="flex items-start gap-4">
            <div className="p-3 rounded-xl bg-muted text-foreground shrink-0 mt-0.5">
              <FileText className="size-6 text-primary" aria-hidden="true" />
            </div>
            <div className="space-y-2 flex-1">
              <h3 className="text-base font-bold text-foreground">
                Hospital Guidance
              </h3>
              <p
                data-testid="blocked-detail"
                className="text-base md:text-lg font-medium text-muted-foreground leading-relaxed whitespace-pre-line"
              >
                {detail}
              </p>
            </div>
          </div>
        </div>

        {/* OK Action Button (≥ 64px target) and Auto-Dismiss Notice */}
        <div className="w-full max-w-lg space-y-3 pt-2">
          <p className="text-sm text-muted-foreground">
            Auto-dismissing in 8 seconds
          </p>

          <Button
            type="button"
            variant="default"
            size="lg"
            data-testid="ok-blocked-button"
            className="min-h-16 w-full text-xl font-bold bg-primary text-primary-foreground shadow-lg hover:bg-primary/90"
            onClick={onDismiss}
          >
            OK
          </Button>
        </div>
      </div>
    </ScreenFrame>
  );
}
