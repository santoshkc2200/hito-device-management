import * as React from "react";
import { Camera, QrCode, ShieldAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { CountdownTimer } from "@/components/countdown-timer";
import { LanguageToggle } from "@/components/language-toggle";
import { useTranslator } from "@/i18n";

export interface ScreenFrameProps {
  kioskName?: string;
  supportCode?: string | null;
  scannerReady?: boolean;
  scannerFresh?: boolean;
  expiresAt?: string | null;
  totalDurationSeconds?: number;
  onToggleCamera?: () => void;
  onOpenDiagnostics?: () => void;
  onOpenManualEntry?: () => void;
  children: React.ReactNode;
  className?: string;
}

export function ScreenFrame({
  kioskName = "HDMS Kiosk",
  supportCode,
  scannerReady = true,
  scannerFresh = true,
  expiresAt = null,
  totalDurationSeconds,
  onToggleCamera,
  onOpenDiagnostics,
  onOpenManualEntry,
  children,
  className = "",
}: ScreenFrameProps) {
  const t = useTranslator();
  const longPressTimerRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);

  const handlePointerDown = () => {
    if (onOpenManualEntry) {
      longPressTimerRef.current = setTimeout(() => {
        onOpenManualEntry();
      }, 600);
    }
  };

  const handlePointerUp = () => {
    if (longPressTimerRef.current) {
      clearTimeout(longPressTimerRef.current);
      longPressTimerRef.current = null;
    }
  };

  return (
    <div
      data-testid="screen-frame"
      className={`relative flex min-h-dvh flex-col justify-between bg-background text-foreground select-none ${className}`}
    >
      {/* Top Persistent Chrome Header */}
      <header className="flex w-full items-center justify-between border-b border-border bg-card px-6 py-4 shadow-xs">
        {/* Left: Kiosk Identification & Status */}
        <div className="flex items-center gap-4">
          <div
            data-testid="kiosk-name-heading"
            className="min-h-12 cursor-pointer select-none flex flex-col justify-center"
            onPointerDown={handlePointerDown}
            onPointerUp={handlePointerUp}
            onPointerLeave={handlePointerUp}
            onDoubleClick={() => onOpenManualEntry?.()}
            role="button"
            tabIndex={0}
            aria-label={t("header.longPressAttendantAriaLabel", { name: kioskName })}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                onOpenManualEntry?.();
              }
            }}
          >
            <h1 className="text-xl font-bold tracking-tight text-foreground">
              {kioskName}
            </h1>
            <div className="flex items-center gap-2 pt-0.5 text-xs">

              <span
                data-testid="scanner-status-dot"
                className={`inline-block size-2.5 rounded-full ${
                  scannerReady
                    ? scannerFresh
                      ? "bg-green-500 ring-2 ring-green-500/20 animate-pulse"
                      : "bg-amber-500"
                    : "bg-destructive"
                }`}
                aria-hidden="true"
              />
              <span
                data-testid="scanner-status-text"
                className="font-medium text-muted-foreground"
              >
                {scannerReady
                  ? scannerFresh
                    ? t("header.scannerReady")
                    : t("header.scannerIdle")
                  : t("header.scannerDisconnected")}
              </span>
            </div>
          </div>
        </div>


        {/* Center: Countdown Timer Slot */}
        <div className="flex items-center justify-center">
          {expiresAt && (
            <CountdownTimer
              expiresAt={expiresAt}
              totalDurationSeconds={totalDurationSeconds}
            />
          )}
        </div>

        {/* Right: Actions (LanguageToggle, Camera Toggle, Diagnostics) */}
        <nav aria-label={t("header.controlsNavAriaLabel")} className="flex items-center gap-3">
          <LanguageToggle />
          {onToggleCamera && (
            <Button
              type="button"
              variant="outline"
              size="lg"
              className="min-h-12 min-w-12 gap-2 text-base font-semibold"
              onClick={onToggleCamera}
              aria-label={t("header.cameraAriaLabel")}
            >
              <Camera className="size-5 text-primary" />
              <span>{t("header.cameraButton")}</span>
            </Button>
          )}

          {onOpenDiagnostics && (
            <Button
              type="button"
              variant="ghost"
              size="icon-lg"
              className="min-h-12 min-w-12 text-muted-foreground hover:text-foreground"
              onClick={onOpenDiagnostics}
              aria-label={t("header.diagnosticsAriaLabel")}
              title={t("header.diagnosticsTitle")}
            >
              <ShieldAlert className="size-5" />
            </Button>
          )}
        </nav>
      </header>

      {/* Main Interactive Screen Content */}
      <main className="flex flex-1 flex-col items-center justify-center p-6 md:p-12 overflow-y-auto">
        <div className="w-full max-w-4xl flex-1 flex flex-col items-center justify-center">
          {children}
        </div>
      </main>

      {/* Bottom Persistent Chrome Footer */}
      <footer className="flex w-full items-center justify-between border-t border-border bg-card/60 px-6 py-3 text-xs text-muted-foreground">
        <div className="flex items-center gap-2">
          <QrColorIcon />
          <span>{t("common.hospitalSystem")}</span>
        </div>
        <div>
          {supportCode && (
            <span
              data-testid="kiosk-support-code"
              className="font-mono font-medium tracking-wider text-muted-foreground/80"
            >
              Support Code: <strong className="text-foreground/90">{supportCode}</strong>
            </span>
          )}
        </div>
      </footer>
    </div>
  );
}

function QrColorIcon() {
  return <QrCode className="size-4 text-primary/70" aria-hidden="true" />;
}
