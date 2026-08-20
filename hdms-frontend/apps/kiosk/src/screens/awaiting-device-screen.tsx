import * as React from "react";
import {
  AlertTriangle,
  Calendar,
  CheckCircle2,
  CornerDownLeft,
  Laptop,
  UserCheck,
} from "lucide-react";
import type { SessionOpenLoan, SessionUser } from "@hdms/api-client";
import { Button } from "@/components/ui/button";
import { ScreenFrame } from "./screen-frame";

export interface AwaitingDeviceScreenProps {
  user: SessionUser | null;
  openLoans: SessionOpenLoan[];
  expiresAt: string | null;
  kioskName?: string;
  supportCode?: string | null;
  scannerReady?: boolean;
  scannerFresh?: boolean;
  onReturnLoan: (loanId: string) => void;
  onClose: () => void;
  onToggleCamera?: () => void;
  onOpenDiagnostics?: () => void;
  onOpenManualEntry?: () => void;
}

export function AwaitingDeviceScreen({
  user,
  openLoans,
  expiresAt,
  kioskName = "HDMS Kiosk",
  supportCode = "KIOSK-01",
  scannerReady = true,
  scannerFresh = true,
  onReturnLoan,
  onClose,
  onToggleCamera,
  onOpenDiagnostics,
  onOpenManualEntry,
}: AwaitingDeviceScreenProps) {

  // Track which loan is currently in confirmation state for two-tap return
  const [confirmingLoanId, setConfirmingLoanId] = React.useState<string | null>(null);
  const [currentTime, setCurrentTime] = React.useState<number>(() => Date.now());

  React.useEffect(() => {
    const timer = setInterval(() => {
      setCurrentTime(Date.now());
    }, 10000);
    return () => clearInterval(timer);
  }, []);

  const handleReturnTap = (loanId: string) => {
    if (confirmingLoanId === loanId) {
      // Second tap: execute return
      onReturnLoan(loanId);
      setConfirmingLoanId(null);
    } else {
      // First tap: enter confirmation mode for this row
      setConfirmingLoanId(loanId);
    }
  };

  const formatDate = (isoString?: string) => {
    if (!isoString) return "N/A";
    try {
      const date = new Date(isoString);
      return date.toLocaleDateString(undefined, {
        month: "short",
        day: "numeric",
      });
    } catch {
      return isoString;
    }
  };

  const isOverdue = React.useCallback(
    (loan: SessionOpenLoan): boolean => {
      if (!loan.dueAt) return false;
      try {
        return new Date(loan.dueAt).getTime() < currentTime;
      } catch {
        return false;
      }
    },
    [currentTime]
  );

  const calculateOverdueDays = React.useCallback(
    (loan: SessionOpenLoan): number => {
      if (!loan.dueAt) return 0;
      try {
        const diffMs = currentTime - new Date(loan.dueAt).getTime();
        return Math.max(1, Math.floor(diffMs / (1000 * 60 * 60 * 24)));
      } catch {
        return 1;
      }
    },
    [currentTime]
  );

  return (
    <ScreenFrame
      kioskName={kioskName}
      supportCode={supportCode}
      scannerReady={scannerReady}
      scannerFresh={scannerFresh}
      expiresAt={expiresAt}
      totalDurationSeconds={25}
      onToggleCamera={onToggleCamera}
      onOpenDiagnostics={onOpenDiagnostics}
      onOpenManualEntry={onOpenManualEntry}
    >

      <div className="flex flex-col h-full w-full max-w-3xl space-y-6 py-2">
        {/* Sticky/Fixed Directive & Greeting Header Area */}
        <div className="space-y-4 text-center border-b border-border pb-4">
          {user && (
            <div
              data-testid="user-greeting-badge"
              className="inline-flex items-center gap-2 rounded-full bg-secondary px-4 py-1.5 text-sm font-semibold text-secondary-foreground"
            >
              <UserCheck className="size-4 text-primary" />
              <span>
                Hello, <strong>{user.fullName}</strong>
              </span>
              {user.department && (
                <span className="text-muted-foreground">· {user.department}</span>
              )}
            </div>
          )}

          {/* Primary Prompt: ≥ 36px font-size */}
          <div className="space-y-2">
            <h2
              data-testid="awaiting-device-prompt"
              className="text-4xl md:text-5xl font-extrabold tracking-tight text-foreground leading-tight"
              style={{ fontSize: "clamp(2.25rem, 5vw, 3.25rem)" }}
            >
              Scan a device to borrow or return
            </h2>
            <p className="text-base md:text-lg text-muted-foreground">
              Scan barcode on any device, or tap RETURN on one of your borrowed items below.
            </p>
          </div>
        </div>

        {/* Scrollable Open Loans Area */}
        <section
          aria-labelledby="borrowed-items-heading"
          className="flex-1 overflow-y-auto max-h-[46vh] pr-1 space-y-3"
        >
          <div className="flex items-center justify-between sticky top-0 bg-background/95 backdrop-blur-xs py-1 z-10">
            <h3
              id="borrowed-items-heading"
              className="text-lg font-bold tracking-tight text-foreground"
            >
              Your Active Loans ({openLoans.length})
            </h3>
            <span className="text-xs text-muted-foreground">
              Tap RETURN twice to record return
            </span>
          </div>

          {openLoans.length === 0 ? (
            <div
              data-testid="empty-loans-container"
              className="rounded-2xl border-2 border-dashed border-border bg-card/60 p-8 text-center space-y-3"
            >
              <div className="p-3 mx-auto w-fit rounded-full bg-primary/10 text-primary">
                <CheckCircle2 className="size-8" />
              </div>
              <div className="space-y-1">
                <p className="text-lg font-bold text-foreground">
                  No devices currently borrowed
                </p>
                <p className="text-sm text-muted-foreground">
                  Scan any available equipment barcode to borrow it now.
                </p>
              </div>
            </div>
          ) : (
            <div className="space-y-3" role="list">
              {openLoans.map((loan) => {
                const overdue = isOverdue(loan);
                const overdueDays = overdue ? calculateOverdueDays(loan) : 0;
                const isConfirming = confirmingLoanId === loan.id;

                return (
                  <div
                    key={loan.id}
                    role="listitem"
                    data-testid={`loan-row-${loan.id}`}
                    className={`flex flex-col sm:flex-row sm:items-center justify-between gap-4 p-4 rounded-xl border-2 min-h-16 transition-all ${
                      isConfirming
                        ? "border-primary bg-primary/5 shadow-md"
                        : overdue
                        ? "border-warning/40 bg-warning/5"
                        : "border-border bg-card shadow-xs"
                    }`}
                  >
                    {/* Device Details */}
                    <div className="flex items-start sm:items-center gap-3">
                      <div className="p-2.5 rounded-lg bg-secondary text-secondary-foreground shrink-0 mt-1 sm:mt-0">
                        <Laptop className="size-5 text-primary" />
                      </div>
                      <div className="space-y-1">
                        <div className="flex items-center gap-2 flex-wrap">
                          <span className="font-bold text-base text-foreground">
                            {loan.deviceName}
                          </span>
                          <span className="font-mono text-xs font-bold px-2 py-0.5 rounded bg-muted text-foreground/90">
                            {loan.assetTag}
                          </span>
                        </div>

                        <div className="flex items-center gap-4 text-xs text-muted-foreground flex-wrap">
                          <span className="inline-flex items-center gap-1">
                            <Calendar className="size-3.5" />
                            Out: {formatDate(loan.borrowedAt)}
                          </span>
                          {loan.dueAt && (
                            <span className="inline-flex items-center gap-1">
                              Due: {formatDate(loan.dueAt)}
                            </span>
                          )}
                          {/* Overdue Flag: Icon AND Text, not colour alone */}
                          {overdue && (
                            <span
                              data-testid={`overdue-flag-${loan.id}`}
                              className="inline-flex items-center gap-1 font-bold text-warning-foreground bg-warning/20 px-2 py-0.5 rounded"
                            >
                              <AlertTriangle className="size-3.5 text-warning" />
                              <span>
                                {overdueDays > 1
                                  ? `${overdueDays} days overdue`
                                  : "Overdue"}
                              </span>
                            </span>
                          )}
                        </div>
                      </div>
                    </div>

                    {/* Return Action with 2-Tap Confirmation */}
                    <div className="shrink-0 flex items-center justify-end">
                      <Button
                        type="button"
                        variant={isConfirming ? "destructive" : "outline"}
                        size="lg"
                        data-testid={`return-button-${loan.id}`}
                        className={`min-h-12 text-sm font-bold tracking-wide transition-all ${
                          isConfirming
                            ? "bg-destructive text-white hover:bg-destructive/90 min-w-44 animate-pulse"
                            : "min-w-28 hover:border-primary hover:text-primary"
                        }`}
                        onClick={() => handleReturnTap(loan.id)}
                        aria-label={
                          isConfirming
                            ? `Confirm returning ${loan.deviceName}`
                            : `Return ${loan.deviceName}`
                        }
                      >
                        <CornerDownLeft className="size-4" />
                        <span>{isConfirming ? "Confirm Return" : "RETURN"}</span>
                      </Button>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </section>

        {/* Footer Done Action */}
        <div className="w-full pt-2">
          <Button
            type="button"
            variant="default"
            size="lg"
            data-testid="done-session-button"
            className="min-h-16 w-full text-xl font-bold bg-primary text-primary-foreground shadow-lg hover:bg-primary/90"
            onClick={onClose}
          >
            Done
          </Button>
        </div>
      </div>
    </ScreenFrame>
  );
}
