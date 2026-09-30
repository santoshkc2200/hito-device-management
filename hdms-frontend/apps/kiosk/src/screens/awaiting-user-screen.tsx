import { IdCard, Laptop, XCircle } from "lucide-react";
import type { SessionDevice } from "@hdms/api-client";
import { Button } from "@/components/ui/button";
import { useTranslator } from "@/i18n";
import { ScreenFrame } from "./screen-frame";

export interface AwaitingUserScreenProps {
  pendingDevice: SessionDevice | null;
  expiresAt: string | null;
  kioskName?: string;
  supportCode?: string | null;
  scannerReady?: boolean;
  scannerFresh?: boolean;
  onCancel: () => void;
  onToggleCamera?: () => void;
  onOpenDiagnostics?: () => void;
  onOpenManualEntry?: () => void;
}

export function AwaitingUserScreen({
  pendingDevice,
  expiresAt,
  kioskName = "HDMS Kiosk",
  supportCode = "KIOSK-01",
  scannerReady = true,
  scannerFresh = true,
  onCancel,
  onToggleCamera,
  onOpenDiagnostics,
  onOpenManualEntry,
}: AwaitingUserScreenProps) {
  const t = useTranslator();
  // A device someone else already has is still held here, because its
  // borrower may be the one returning it — but the screen must say so
  // before anyone scans a card expecting to borrow it.
  const isOnLoan = pendingDevice?.status === "on_loan";

  return (
    <ScreenFrame
      kioskName={kioskName}
      supportCode={supportCode}
      scannerReady={scannerReady}
      scannerFresh={scannerFresh}
      expiresAt={expiresAt}
      totalDurationSeconds={45}
      onToggleCamera={onToggleCamera}
      onOpenDiagnostics={onOpenDiagnostics}
      onOpenManualEntry={onOpenManualEntry}
    >

      <div className="flex flex-col items-center text-center space-y-8 py-4 max-w-2xl w-full">
        {/* Main Directive Prompt: ≥ 36px font-size */}
        <div className="space-y-3">
          <div className="inline-flex items-center gap-2 rounded-full bg-primary/10 px-4 py-1.5 text-sm font-semibold text-primary">
            <IdCard className="size-4" />
            <span>{t("awaitingUser.stepBadge")}</span>
          </div>

          <h2
            data-testid="awaiting-user-prompt"
            className="text-kiosk-prompt text-foreground leading-tight"
          >
            {isOnLoan ? t("awaitingUser.onLoanPrompt") : t("awaitingUser.prompt")}
          </h2>

          <p data-testid="awaiting-user-hint" className="text-kiosk-body text-muted-foreground">
            {isOnLoan ? t("awaitingUser.onLoanHint") : t("awaitingUser.hint")}
          </p>
        </div>

        {/* Pending Scanned Device Card */}
        {pendingDevice && (
          <div
            data-testid="pending-device-card"
            className="w-full rounded-2xl border-2 border-border bg-card p-6 shadow-sm text-left space-y-4"
          >
            <div className="flex items-center justify-between border-b border-border pb-3 gap-3">
              <div className="flex items-center gap-3 min-w-0 flex-1">
                <div className="p-3 rounded-xl bg-secondary text-secondary-foreground shrink-0">
                  <Laptop className="size-6 text-primary" />
                </div>
                <div className="min-w-0 flex-1">
                  <span className="text-kiosk-badge uppercase tracking-wider text-muted-foreground">
                    {t("awaitingUser.scannedDeviceBadge")}
                  </span>
                  <h3 className="text-kiosk-heading text-foreground break-words">
                    {pendingDevice.name || t("awaitingUser.defaultDeviceName")}
                  </h3>
                </div>
              </div>

              {/* Status Badge: Never names the holder */}
              <div
                data-testid="device-status-badge"
                className={`rounded-full px-3.5 py-1 text-kiosk-badge uppercase tracking-wider shrink-0 ${
                  isOnLoan
                    ? "bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200"
                    : "bg-green-100 text-green-900 dark:bg-green-950 dark:text-green-200"
                }`}
              >
                {isOnLoan ? t("awaitingUser.statusOnLoan") : t("awaitingUser.statusAvailable")}
              </div>
            </div>

            <div className="grid grid-cols-2 gap-4 text-sm pt-1">
              <div>
                <span className="text-muted-foreground block text-xs">{t("awaitingUser.assetTagLabel")}</span>
                <span
                  data-testid="pending-device-asset-tag"
                  className="font-mono text-kiosk-mono-lg font-bold text-foreground tracking-wider"
                >
                  {pendingDevice.assetTag}
                </span>
              </div>
              {(pendingDevice as any)?.category && (
                <div>
                  <span className="text-muted-foreground block text-xs">{t("awaitingUser.categoryLabel")}</span>
                  <span className="font-semibold text-foreground text-base">
                    {(pendingDevice as any).category}
                  </span>
                </div>
              )}
            </div>
          </div>
        )}

        {/* Cancellation and Primary Action Row */}
        <div className="w-full pt-4">
          <Button
            type="button"
            variant="outline"
            size="lg"
            data-testid="cancel-session-button"
            className="min-h-16 w-full text-lg font-semibold border-2 hover:bg-destructive/10 hover:text-destructive hover:border-destructive/40"
            onClick={onCancel}
          >
            <XCircle className="size-6" />
            <span>{t("common.cancel")}</span>
          </Button>
        </div>
      </div>
    </ScreenFrame>
  );
}
