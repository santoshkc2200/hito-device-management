import { BookOpen, RefreshCw, WifiOff } from "lucide-react";
import { useTranslator } from "@/i18n";
import { ScreenFrame } from "./screen-frame";

export interface OfflineScreenProps {
  kioskName?: string;
  supportCode?: string | null;
  onToggleCamera?: () => void;
  onOpenDiagnostics?: () => void;
  onOpenManualEntry?: () => void;
}

export function OfflineScreen({
  kioskName = "HDMS Kiosk",
  supportCode = "KIOSK-01",
  onToggleCamera,
  onOpenDiagnostics,
  onOpenManualEntry,
}: OfflineScreenProps) {
  const t = useTranslator();

  return (
    <ScreenFrame
      kioskName={kioskName}
      supportCode={supportCode}
      scannerReady={false}
      scannerFresh={false}
      onToggleCamera={onToggleCamera}
      onOpenDiagnostics={onOpenDiagnostics}
      onOpenManualEntry={onOpenManualEntry}
    >

      <div
        data-testid="offline-screen"
        className="flex flex-col items-center justify-between h-full w-full max-w-3xl space-y-8 py-6 text-center"
      >
        {/* Animated Reconnection Status */}
        <div className="space-y-4 flex flex-col items-center">
          <div
            data-testid="offline-icon-container"
            className="p-5 rounded-full bg-muted text-muted-foreground shadow-md relative"
          >
            <WifiOff className="size-16 stroke-[2.5]" aria-hidden="true" />
            <span className="absolute -bottom-1 -right-1 p-1.5 rounded-full bg-background border-2 border-border">
              <RefreshCw className="size-5 text-primary animate-spin" aria-hidden="true" />
            </span>
          </div>

          <span
            data-testid="offline-status-badge"
            className="font-mono text-xs font-bold uppercase tracking-wider px-3.5 py-1 rounded-full bg-muted text-muted-foreground"
          >
            {t("offline.reconnectingBadge")}
          </span>

          {/* Primary Prompt: ≥ 36px font size */}
          <h2
            data-testid="offline-title"
            className="text-kiosk-prompt text-foreground leading-tight"
          >
            {t("offline.title")}
          </h2>

          <p data-testid="offline-subtitle" className="text-kiosk-body text-muted-foreground max-w-lg">
            {t("offline.subtitle")}
          </p>
        </div>

        {/* Paper Register Fallback Card */}
        <div
          data-testid="offline-paper-card"
          className="w-full max-w-xl rounded-2xl border-2 border-primary/30 bg-primary/5 p-6 md:p-8 shadow-sm space-y-4 text-left"
        >
          <div className="flex items-start gap-4">
            <div className="p-3 rounded-xl bg-primary/10 text-primary shrink-0 mt-0.5">
              <BookOpen className="size-7" aria-hidden="true" />
            </div>
            <div className="space-y-2 flex-1">
              <h3 className="text-lg font-bold text-foreground">
                {t("offline.paperFallbackTitle")}
              </h3>
              <p
                data-testid="offline-fallback-instruction"
                className="text-base text-foreground/80 leading-relaxed font-medium"
              >
                {t("offline.paperFallbackInstruction")}
              </p>
              {/*
                Phase 6.4d. The decided offline policy is that the kiosk
                does NOT enforce reservations while offline — a conflict
                cannot be adjudicated offline without lying to somebody
                standing at the counter — and says so, rather than
                silently letting a reserved device walk out. The paper
                register is the designed overflow lane.
              */}
              <p
                data-testid="offline-reservations-notice"
                className="text-base text-foreground/80 leading-relaxed font-medium"
              >
                {t("offline.reservationsNotEnforced")}
              </p>
            </div>
          </div>
        </div>

        {/* Quiet footer notice */}
        <div className="w-full max-w-lg pt-2 text-xs text-muted-foreground">
          <p>{t("offline.reconnectingNotice")}</p>
        </div>
      </div>
    </ScreenFrame>
  );
}
