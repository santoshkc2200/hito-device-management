import * as React from "react";
import { CalendarCheck, CheckCircle2, CornerDownLeft, Laptop, PackageCheck } from "lucide-react";
import type { SessionDevice, OutcomeKind } from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { Button } from "@/components/ui/button";
import { formatHumanDueDate } from "@/lib/date-format";
import { useTranslator } from "@/i18n";
import { ScreenFrame } from "./screen-frame";

export interface SuccessScreenProps {
  kind: OutcomeKind | "borrowed" | "returned" | string;
  device?: SessionDevice | null;
  dueAt?: string | null;
  itemCount?: number;
  onDone: () => void;
  onScanAnother?: () => void;
  kioskName?: string;
  supportCode?: string | null;
  scannerReady?: boolean;
  scannerFresh?: boolean;
  onToggleCamera?: () => void;
  onOpenDiagnostics?: () => void;
  onOpenManualEntry?: () => void;
}

export function SuccessScreen({
  kind,
  device,
  dueAt,
  itemCount = 1,
  onDone,
  onScanAnother,
  kioskName = "HDMS Kiosk",
  supportCode = "KIOSK-01",
  scannerReady = true,
  scannerFresh = true,
  onToggleCamera,
  onOpenDiagnostics,
  onOpenManualEntry,
}: SuccessScreenProps) {
  const t = useTranslator();
  const { locale } = useLocale();
  const isReturn = kind === "returned";
  // Phase 6.4d: a reserver collecting their own device gets a distinct
  // confirmation, so the journey reads as "your booking worked" rather
  // than as an ordinary borrow that happened to be allowed. The loan
  // behind it is an ordinary loan — only the wording differs.
  const isCollected = kind === "reservation_collected";
  const dueLine = formatHumanDueDate(dueAt, undefined, locale);

  // Auto-dismiss after 4 seconds (4000 ms)
  React.useEffect(() => {
    const timer = setTimeout(() => {
      onDone();
    }, 4000);
    return () => clearTimeout(timer);
  }, [onDone]);

  // The outcome text comes from this app's catalogue, not from the server's
  // SessionMessage. The backend rule in docs/superpowers/specs/2026-08-31-i18n-l10n-design.md
  // is that responses stay code-only and the client renders the copy; the
  // server's English title would otherwise sit inside an otherwise Japanese
  // screen. `kind` carries everything this screen needs to choose its wording.
  const title = isReturn
    ? t("success.returnTitle")
    : isCollected
      ? t("success.collectedTitle")
      : t("success.borrowTitle");
  const detail = isReturn
    ? t("success.returnDetail")
    : isCollected
      ? t("success.collectedDetail")
      : t("success.borrowDetail");

  return (
    <ScreenFrame
      kioskName={kioskName}
      supportCode={supportCode}
      scannerReady={scannerReady}
      scannerFresh={scannerFresh}
      onToggleCamera={onToggleCamera}
      onOpenDiagnostics={onOpenDiagnostics}
      onOpenManualEntry={onOpenManualEntry}
    >

      <div
        data-testid="success-screen"
        className="flex flex-col items-center justify-between h-full w-full max-w-3xl space-y-6 py-4 text-center"
      >
        {/* Top Outcome Badge & Icon */}
        <div className="space-y-4 flex flex-col items-center">
          <div
            data-testid="success-icon-container"
            className={`p-4 rounded-full shadow-md ${
              isReturn
                ? "bg-blue-500/15 text-blue-600 dark:text-blue-400 animate-feedback-info"
                : "bg-green-500/15 text-green-600 dark:text-green-400 animate-feedback-success"
            }`}
          >
            {isReturn ? (
              <CornerDownLeft className="size-16 stroke-[2.5]" aria-hidden="true" />
            ) : isCollected ? (
              <CalendarCheck className="size-16 stroke-[2.5]" aria-hidden="true" />
            ) : (
              <CheckCircle2 className="size-16 stroke-[2.5]" aria-hidden="true" />
            )}
          </div>

          {/* Outcome Type & Multi-Item Context */}
          <div className="flex flex-wrap items-center justify-center gap-2">
            <span
              data-testid="success-kind-badge"
              className={`font-mono text-xs font-bold uppercase tracking-wider px-3 py-1 rounded-full ${
                isReturn
                  ? "bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-200"
                  : "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-200"
              }`}
            >
              {isReturn
                ? t("success.returnBadge")
                : isCollected
                  ? t("success.collectedBadge")
                  : t("success.borrowBadge")}
            </span>

            {itemCount > 1 && (
              <span
                data-testid="multi-item-badge"
                className="inline-flex items-center gap-1.5 font-sans text-xs font-bold px-3 py-1 rounded-full bg-secondary text-secondary-foreground shadow-xs"
              >
                <PackageCheck className="size-3.5 text-primary" />
                <span>{t("success.itemCount", { count: itemCount })}</span>
              </span>
            )}
          </div>

          {/* Primary Outcome Title */}
          <h2
            data-testid="success-title"
            className="text-kiosk-prompt text-foreground leading-tight"
          >
            {title}
          </h2>

          <p data-testid="success-detail" className="text-kiosk-body text-muted-foreground max-w-xl">
            {detail}
          </p>
        </div>

        {/* Device Information Card */}
        {device && (
          <div
            data-testid="success-device-card"
            className="w-full max-w-lg rounded-2xl border-2 border-border bg-card p-6 shadow-sm flex flex-col items-center space-y-3"
          >
            <div className="flex items-center gap-3">
              <div className="p-2.5 rounded-lg bg-secondary text-secondary-foreground">
                <Laptop className="size-6 text-primary" />
              </div>
              <div className="text-left">
                <p className="text-xl font-bold text-foreground">{device.name}</p>
                <p className="font-mono text-xs font-semibold text-muted-foreground">
                  {t("success.assetTagLabel")}<span className="text-foreground">{device.assetTag}</span>
                </p>
              </div>
            </div>

            {/* Due date line (strictly omitted when null) */}
            {dueLine && (
              <div
                data-testid="success-due-line"
                className="w-full pt-3 border-t border-border/80 text-center"
              >
                <p className="text-sm font-semibold text-foreground/90 bg-muted/60 py-1.5 px-3 rounded-lg">
                  {dueLine}
                </p>
              </div>
            )}
          </div>
        )}

        {/* Bottom Actions and Auto-Return Notice */}
        <div className="w-full max-w-lg space-y-4 pt-2">
          <p className="text-sm text-muted-foreground">
            {t("success.autoDismissHint")}
          </p>

          <div className="flex flex-col sm:flex-row gap-3">
            {onScanAnother && (
              <Button
                type="button"
                variant="outline"
                size="lg"
                data-testid="scan-another-button"
                className="min-h-16 flex-1 text-lg font-bold"
                onClick={onScanAnother}
              >
                {t("success.scanAnother")}
              </Button>
            )}

            <Button
              type="button"
              variant="default"
              size="lg"
              data-testid="done-success-button"
              className="min-h-16 flex-1 text-xl font-bold bg-primary text-primary-foreground shadow-lg hover:bg-primary/90"
              onClick={onDone}
            >
              {t("common.done")}
            </Button>
          </div>
        </div>
      </div>
    </ScreenFrame>
  );
}
