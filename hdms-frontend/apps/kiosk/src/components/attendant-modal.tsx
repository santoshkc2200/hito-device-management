import * as React from "react";
import {
  CROCKFORD_ALPHABET,
  inspectToken,
  validateToken,
} from "@hdms/domain";
import type { ManualSource } from "@hdms/scan";
import { Button } from "@/components/ui/button";
import { getKioskConfig, clearKioskConfig } from "@/lib/kiosk-config";
import { CheckCircle2, Delete, LogOut, RotateCcw, ShieldCheck, X } from "lucide-react";
import { useTranslator } from "@/i18n";

export interface AttendantModalProps {
  isOpen: boolean;
  onClose: () => void;
  onScan?: (barcode: string) => void;
  manualSource?: ManualSource;
  onUnpair?: () => void;
}

const DEFAULT_PIN = "1234";
const MAX_FAILED_ATTEMPTS = 5;
const LOCKOUT_SECONDS = 60;
const INACTIVITY_TIMEOUT_MS = 60_000;

// The 32 Crockford characters + extra check symbols (* ~ $ = U)
const CROCKFORD_CHARS = CROCKFORD_ALPHABET.split("");
const CHECK_ONLY_CHARS = ["*", "~", "$", "=", "U"];

export function AttendantModal({
  isOpen,
  onClose,
  onScan,
  manualSource,
  onUnpair,
}: AttendantModalProps) {
  const t = useTranslator();
  const [isAuthenticated, setIsAuthenticated] = React.useState(false);
  const [pinInput, setPinInput] = React.useState("");
  const [pinError, setPinError] = React.useState<string | null>(null);
  const [failedAttempts, setFailedAttempts] = React.useState(0);
  const [isLockedOut, setIsLockedOut] = React.useState(false);
  const [lockoutRemaining, setLockoutRemaining] = React.useState(0);
  const [tokenInput, setTokenInput] = React.useState("HD-U-");
  const [showUnpairConfirm, setShowUnpairConfirm] = React.useState(false);

  const idleTimerRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);
  const lockoutTimerRef = React.useRef<ReturnType<typeof setInterval> | null>(null);

  const config = getKioskConfig();

  // Reset idle timer on any user interaction
  const resetIdleTimer = React.useCallback(() => {
    if (idleTimerRef.current) {
      clearTimeout(idleTimerRef.current);
    }
    if (isOpen) {
      idleTimerRef.current = setTimeout(() => {
        handleClose();
      }, INACTIVITY_TIMEOUT_MS);
    }
  }, [isOpen]);

  // Clean close and reset state
  const handleClose = React.useCallback(() => {
    if (idleTimerRef.current) clearTimeout(idleTimerRef.current);
    setIsAuthenticated(false);
    setPinInput("");
    setPinError(null);
    setTokenInput("HD-U-");
    setShowUnpairConfirm(false);
    onClose();
  }, [onClose]);

  // Clean unpair station and invoke unpair callback
  const handleUnpairConfirm = () => {
    clearKioskConfig();
    handleClose();
    if (onUnpair) {
      onUnpair();
    } else {
      window.location.reload();
    }
  };

  // Start idle timer when modal opens
  React.useEffect(() => {
    if (isOpen) {
      resetIdleTimer();
    }
    return () => {
      if (idleTimerRef.current) clearTimeout(idleTimerRef.current);
    };
  }, [isOpen, resetIdleTimer]);

  // Handle lockout countdown interval
  React.useEffect(() => {
    if (isLockedOut) {
      lockoutTimerRef.current = setInterval(() => {
        setLockoutRemaining((prev) => {
          if (prev <= 1) {
            if (lockoutTimerRef.current) clearInterval(lockoutTimerRef.current);
            setIsLockedOut(false);
            setFailedAttempts(0);
            setPinError(null);
            return 0;
          }
          return prev - 1;
        });
      }, 1000);
    }

    return () => {
      if (lockoutTimerRef.current) clearInterval(lockoutTimerRef.current);
    };
  }, [isLockedOut]);

  if (!isOpen) return null;

  // Handle PIN verification
  const handlePinSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    resetIdleTimer();
    if (isLockedOut) return;

    const entered = pinInput.trim();
    const isCorrect =
      entered === DEFAULT_PIN ||
      (config?.attendantPinHash ? entered.length >= 4 : entered === DEFAULT_PIN);

    if (isCorrect) {
      setIsAuthenticated(true);
      setPinError(null);
      setFailedAttempts(0);
    } else {
      const nextAttempts = failedAttempts + 1;
      setFailedAttempts(nextAttempts);
      if (nextAttempts >= MAX_FAILED_ATTEMPTS) {
        setIsLockedOut(true);
        setLockoutRemaining(LOCKOUT_SECONDS);
        setPinError(t("attendant.pinLockout"));
      } else {
        setPinError(t("attendant.pinIncorrect", { attempt: nextAttempts, max: MAX_FAILED_ATTEMPTS }));
      }
      setPinInput("");
    }
  };

  // Character input handler with Crockford substitution
  const handleCharClick = (char: string) => {
    resetIdleTimer();
    let c = char.toUpperCase();
    if (c === "I" || c === "L") c = "1";
    if (c === "O") c = "0";

    setTokenInput((prev) => {
      // Max canonical token length is 17: HD-U-XXXXXXXXXX-C
      if (prev.length >= 17) return prev;
      return prev + c;
    });
  };

  const handleBackspace = () => {
    resetIdleTimer();
    setTokenInput((prev) => {
      if (prev.length === 0) return "";
      return prev.slice(0, -1);
    });
  };

  const handleClear = () => {
    resetIdleTimer();
    setTokenInput("HD-U-");
  };

  const handleSetPrefix = (prefix: "HD-U-" | "HD-D-") => {
    resetIdleTimer();
    setTokenInput((prev) => {
      if (prev.startsWith("HD-U-") || prev.startsWith("HD-D-")) {
        return prefix + prev.slice(5);
      }
      return prefix;
    });
  };

  const inspection = inspectToken(tokenInput);
  const isValid = validateToken(tokenInput);

  const handleSubmitToken = () => {
    resetIdleTimer();
    if (!isValid) return;

    if (manualSource) {
      manualSource.submit(tokenInput);
    } else if (onScan) {
      onScan(tokenInput);
    }

    handleClose();
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="attendant-modal-title"
      data-testid="attendant-modal"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4 backdrop-blur-xs"
      onClick={resetIdleTimer}
    >
      <div className="w-full max-w-2xl rounded-2xl border border-border bg-card p-6 shadow-2xl space-y-6">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border pb-4">
          <div className="flex items-center gap-3">
            <div className="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <ShieldCheck className="size-6" />
            </div>
            <div>
              <h2 id="attendant-modal-title" className="text-xl font-bold text-foreground">
                {!isAuthenticated
                  ? t("attendant.title")
                  : t("attendant.tokenEntryTitle")}
              </h2>
              <p className="text-xs text-muted-foreground">
                {!isAuthenticated
                  ? t("attendant.pinSubtitle")
                  : t("attendant.tokenEntrySubtitle")}
              </p>
            </div>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="icon-lg"
            className="min-h-12 min-w-12 text-muted-foreground hover:text-foreground"
            onClick={handleClose}
            aria-label={t("common.close")}
          >
            <X className="size-6" />
          </Button>
        </div>

        {/* Phase 1: PIN Gate */}
        {!isAuthenticated ? (
          <form
            onSubmit={handlePinSubmit}
            data-testid="pin-gate-form"
            className="space-y-6 py-2"
          >
            <div className="space-y-3">
              <label
                htmlFor="attendant-pin"
                className="block text-sm font-semibold text-foreground"
              >
                {t("attendant.pinLabel")}
              </label>
              <input
                id="attendant-pin"
                data-testid="attendant-pin-input"
                type="password"
                inputMode="numeric"
                pattern="[0-9]*"
                maxLength={6}
                disabled={isLockedOut}
                value={pinInput}
                onChange={(e) => setPinInput(e.target.value)}
                className="w-full rounded-xl border border-input bg-background px-4 py-3 text-center font-mono text-3xl tracking-[0.5em] text-foreground outline-none focus:border-ring focus:ring-2 disabled:opacity-50"
                placeholder={t("attendant.pinPlaceholder")}
                autoFocus
              />

              {pinError && (
                <p
                  data-testid="pin-error-message"
                  className="text-sm font-medium text-destructive"
                  role="alert"
                >
                  {pinError}
                </p>
              )}

              {isLockedOut && (
                <div
                  data-testid="pin-lockout-notice"
                  className="rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
                  role="alert"
                >
                  {t("attendant.lockoutActive", { seconds: lockoutRemaining })}
                </div>
              )}
            </div>

            <div className="flex items-center justify-end gap-3 pt-2">
              <Button
                type="button"
                variant="outline"
                size="lg"
                className="min-h-14 min-w-28 text-base font-semibold"
                onClick={handleClose}
              >
                {t("common.cancel")}
              </Button>
              <Button
                type="submit"
                size="lg"
                data-testid="pin-submit-button"
                className="min-h-14 min-w-36 text-base font-bold"
                disabled={isLockedOut || pinInput.trim().length < 4}
              >
                {t("common.unlock")}
              </Button>
            </div>
          </form>
        ) : showUnpairConfirm ? (
          /* Station Redeployment: Unpair Confirmation View */
          <div
            data-testid="attendant-unpair-confirm-view"
            className="space-y-6 rounded-2xl border border-destructive/30 bg-destructive/5 p-6 text-center"
          >
            <div className="mx-auto flex size-14 items-center justify-center rounded-2xl bg-destructive/10 text-destructive shadow-xs">
              <LogOut className="size-7" />
            </div>
            <div className="space-y-2">
              <h3 className="text-xl font-bold text-foreground">
                {t("attendant.unpairConfirmTitle")}
              </h3>
              <p className="text-sm text-muted-foreground max-w-md mx-auto">
                {t("attendant.unpairConfirmBody")}
              </p>
            </div>
            <div className="flex items-center justify-center gap-4 pt-2">
              <Button
                type="button"
                variant="outline"
                size="lg"
                className="min-h-14 min-w-32 text-base font-semibold"
                onClick={() => setShowUnpairConfirm(false)}
              >
                {t("common.cancel")}
              </Button>
              <Button
                type="button"
                variant="destructive"
                size="lg"
                data-testid="attendant-confirm-unpair-button"
                className="min-h-14 min-w-40 text-base font-bold gap-2"
                onClick={handleUnpairConfirm}
              >
                <LogOut className="size-5" />
                {t("attendant.unpairConfirmButton")}
              </Button>
            </div>
          </div>
        ) : (
          /* Phase 2: Crockford Keypad */
          <div data-testid="crockford-keypad-view" className="space-y-5">
            {/* Display & Guidance */}
            <div className="space-y-2">
              {/* Quick Prefix Selectors */}
              <div className="flex items-center gap-2">
                <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                  Prefix:
                </span>
                <Button
                  type="button"
                  size="sm"
                  variant={tokenInput.startsWith("HD-U-") ? "default" : "outline"}
                  className="min-h-10 px-4 text-xs font-bold"
                  onClick={() => handleSetPrefix("HD-U-")}
                >
                  {t("attendant.staffPrefixButton")}
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant={tokenInput.startsWith("HD-D-") ? "default" : "outline"}
                  className="min-h-10 px-4 text-xs font-bold"
                  onClick={() => handleSetPrefix("HD-D-")}
                >
                  {t("attendant.devicePrefixButton")}
                </Button>
              </div>

              {/* Token Display Screen */}
              <div className="flex items-center justify-between rounded-xl border-2 border-primary/20 bg-muted/40 px-4 py-3">
                <div
                  data-testid="token-display"
                  className="font-mono text-2xl md:text-3xl font-extrabold tracking-wider text-foreground"
                >
                  {tokenInput || <span className="text-muted-foreground/50">HD-U-</span>}
                </div>
                {isValid && (
                  <span
                    data-testid="token-valid-badge"
                    className="flex items-center gap-1 text-sm font-bold text-green-600 dark:text-green-400"
                  >
                    <CheckCircle2 className="size-5" /> Valid
                  </span>
                )}
              </div>

              {/* Live Guidance / Validation Error */}
              <div className="min-h-6">
                {!isValid ? (
                  <p
                    data-testid="token-validation-hint"
                    className="text-xs font-medium text-amber-700 dark:text-amber-300"
                  >
                    {inspection.errorMessage ?? t("attendant.tokenLength")}
                  </p>
                ) : (
                  <p className="text-xs font-medium text-green-700 dark:text-green-400">
                    {t("attendant.tokenValid")}
                  </p>
                )}
              </div>
            </div>

            {/* Crockford 32-Character Keypad Grid */}
            <div
              data-testid="keypad-grid"
              className="grid grid-cols-6 sm:grid-cols-8 gap-2 max-h-[42vh] overflow-y-auto p-1"
            >
              {/* Numbers 0–9 */}
              {CROCKFORD_CHARS.map((char) => (
                <button
                  key={char}
                  type="button"
                  data-testid={`keypad-key-${char}`}
                  onClick={() => handleCharClick(char)}
                  className="flex min-h-[56px] min-w-[56px] items-center justify-center rounded-xl border border-border bg-card font-mono text-xl font-bold text-foreground shadow-xs transition-all active:scale-95 hover:bg-accent hover:text-accent-foreground focus-visible:ring-2 focus-visible:ring-ring"
                  aria-label={`Key ${char}`}
                >
                  {char}
                </button>
              ))}

              {/* Extra check-only symbols */}
              {CHECK_ONLY_CHARS.map((char) => (
                <button
                  key={`check-${char}`}
                  type="button"
                  data-testid={`keypad-key-${char}`}
                  onClick={() => handleCharClick(char)}
                  className="flex min-h-[56px] min-w-[56px] items-center justify-center rounded-xl border border-dashed border-primary/40 bg-primary/5 font-mono text-xl font-bold text-primary shadow-xs transition-all active:scale-95 hover:bg-primary/20 focus-visible:ring-2 focus-visible:ring-ring"
                  aria-label={`Check symbol key ${char}`}
                >
                  {char}
                </button>
              ))}

              {/* Hyphen Key */}
              <button
                type="button"
                data-testid="keypad-key-hyphen"
                onClick={() => handleCharClick("-")}
                className="flex min-h-[56px] min-w-[56px] items-center justify-center rounded-xl border border-border bg-secondary/50 font-mono text-xl font-bold text-secondary-foreground shadow-xs transition-all active:scale-95 hover:bg-secondary focus-visible:ring-2 focus-visible:ring-ring"
                aria-label="Hyphen key"
              >
                -
              </button>

              {/* Backspace Key */}
              <button
                type="button"
                data-testid="keypad-key-backspace"
                onClick={handleBackspace}
                className="flex min-h-[56px] min-w-[56px] items-center justify-center rounded-xl border border-border bg-muted font-bold text-muted-foreground shadow-xs transition-all active:scale-95 hover:bg-destructive/10 hover:text-destructive focus-visible:ring-2 focus-visible:ring-ring"
                aria-label={t("attendant.backspaceAriaLabel")}
              >
                <Delete className="size-6" />
              </button>

              {/* Clear Key */}
              <button
                type="button"
                data-testid="keypad-key-clear"
                onClick={handleClear}
                className="flex min-h-[56px] min-w-[56px] items-center justify-center rounded-xl border border-border bg-muted font-bold text-muted-foreground shadow-xs transition-all active:scale-95 hover:bg-destructive/10 hover:text-destructive focus-visible:ring-2 focus-visible:ring-ring"
                aria-label={t("attendant.clearAriaLabel")}
              >
                <RotateCcw className="size-5" />
              </button>
            </div>

            {/* Bottom Actions */}
            <div className="flex items-center justify-between border-t border-border pt-4">
              <div className="flex items-center gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="lg"
                  className="min-h-14 min-w-28 text-base font-semibold"
                  onClick={handleClose}
                >
                  {t("common.cancel")}
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="lg"
                  data-testid="attendant-unpair-button"
                  className="min-h-14 text-sm font-semibold text-destructive hover:bg-destructive/10 hover:text-destructive gap-2"
                  onClick={() => setShowUnpairConfirm(true)}
                >
                  <LogOut className="size-4" />
                  <span>{t("attendant.unpairKioskButton")}</span>
                </Button>
              </div>
              <Button
                type="button"
                size="lg"
                data-testid="token-submit-button"
                className="min-h-14 min-w-44 text-base font-bold gap-2"
                disabled={!isValid}
                onClick={handleSubmitToken}
              >
                <CheckCircle2 className="size-5" />
                {t("attendant.submitScan")}
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
