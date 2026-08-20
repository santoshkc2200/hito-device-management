import * as React from "react";
import { pairKiosk } from "@hdms/api-client";
import { setKioskConfig } from "@/lib/kiosk-config";
import { parseProblem } from "@/lib/problem";
import { Button } from "@/components/ui/button";
import { CheckCircle2, Delete, KeyRound, Loader2, RotateCcw } from "lucide-react";

export interface PairingScreenProps {
  onPaired?: (config: { kioskId: string; kioskName: string }) => void;
  initialSupportCode?: string | null;
  initialError?: string | null;
}

export function PairingScreen({
  onPaired,
  initialSupportCode,
  initialError,
}: PairingScreenProps) {
  const [code, setCode] = React.useState("");
  const [isSubmitting, setIsSubmitting] = React.useState(false);
  const [errorMessage, setErrorMessage] = React.useState<string | null>(
    initialError ?? null
  );
  const [supportCode, setSupportCode] = React.useState<string | null>(
    initialSupportCode ?? null
  );

  const handleDigitClick = (digit: string) => {
    setErrorMessage(null);
    setCode((prev) => {
      if (prev.length >= 6) return prev;
      return prev + digit;
    });
  };

  const handleBackspace = () => {
    setErrorMessage(null);
    setCode((prev) => prev.slice(0, -1));
  };

  const handleClear = () => {
    setErrorMessage(null);
    setCode("");
  };

  const handlePair = React.useCallback(
    async (codeToSubmit: string) => {
      const trimmed = codeToSubmit.trim();
      if (trimmed.length < 6 || isSubmitting) return;

      setIsSubmitting(true);
      setErrorMessage(null);

      try {
        const res = await pairKiosk({
          body: { code: trimmed },
        });

        if (res.error || !res.data) {
          const problem = await parseProblem(res.error);
          setErrorMessage(
            problem.detail || problem.title || "Invalid or expired pairing code."
          );
          setSupportCode(problem.supportCode);
          setIsSubmitting(false);
          return;
        }

        // Store kiosk configuration and token securely
        setKioskConfig({
          kioskId: res.data.kioskId,
          kioskName: res.data.name,
          token: res.data.token,
        });

        if (onPaired) {
          onPaired({
            kioskId: res.data.kioskId,
            kioskName: res.data.name,
          });
        }
      } catch (err) {
        const problem = await parseProblem(err);
        setErrorMessage(
          problem.detail || problem.title || "Failed to pair kiosk. Please try again."
        );
        setSupportCode(problem.supportCode);
      } finally {
        setIsSubmitting(false);
      }
    },
    [isSubmitting, onPaired]
  );

  // Support physical hardware typing
  React.useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key >= "0" && e.key <= "9") {
        handleDigitClick(e.key);
      } else if (e.key === "Backspace") {
        handleBackspace();
      } else if (e.key === "Enter" && code.length >= 6) {
        void handlePair(code);
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [code, handlePair]);

  return (
    <div
      data-testid="pairing-screen"
      className="flex min-h-dvh flex-col items-center justify-between bg-background text-foreground p-6 md:p-12 select-none"
    >
      {/* Header */}
      <header className="w-full max-w-xl text-center space-y-2 pt-4">
        <div className="mx-auto inline-flex size-14 items-center justify-center rounded-2xl bg-primary/10 text-primary shadow-xs">
          <KeyRound className="size-8" aria-hidden="true" />
        </div>
        <h1
          data-testid="pairing-title"
          className="text-kiosk-heading text-foreground"
        >
          This iPad is not yet paired
        </h1>
        <p className="text-kiosk-body text-muted-foreground max-w-md mx-auto">
          Ask an administrator for a 6-digit pairing code from the Admin Console.
        </p>
      </header>

      {/* Main Form & Keypad */}
      <main className="w-full max-w-md space-y-6 my-auto">
        {/* Code Input Display */}
        <div className="space-y-3">
          <label htmlFor="pairing-code-input" className="sr-only">
            6-Digit Pairing Code
          </label>
          <div
            data-testid="pairing-code-display"
            className="flex items-center justify-center gap-3 py-2"
          >
            {Array.from({ length: 6 }).map((_, index) => {
              const char = code[index];
              const isCurrent = index === code.length;
              return (
                <div
                  key={index}
                  data-testid={`pairing-digit-${index}`}
                  className={`flex h-16 w-12 items-center justify-center rounded-xl border-2 font-mono text-kiosk-mono-xl transition-all ${
                    char
                      ? "border-primary bg-primary/10 text-primary shadow-xs"
                      : isCurrent
                      ? "border-ring bg-card ring-2 ring-ring/30"
                      : "border-border bg-card/50 text-muted-foreground"
                  }`}
                >
                  {char || ""}
                </div>
              );
            })}
          </div>

          <input
            id="pairing-code-input"
            data-testid="pairing-code-hidden-input"
            type="text"
            inputMode="numeric"
            pattern="[0-9]*"
            maxLength={6}
            value={code}
            onChange={(e) => {
              const cleaned = e.target.value.replace(/\D/g, "").slice(0, 6);
              setCode(cleaned);
              setErrorMessage(null);
            }}
            className="sr-only"
            aria-label="6-Digit Pairing Code"
          />

          {/* Error message */}
          {errorMessage && (
            <div
              role="alert"
              data-testid="pairing-error-message"
              className="rounded-xl border border-destructive/30 bg-destructive/10 p-3 text-center text-sm font-medium text-destructive"
            >
              <p>{errorMessage}</p>
              {supportCode && (
                <p className="mt-1 font-mono text-xs text-destructive/80">
                  Support Code: <strong>{supportCode}</strong>
                </p>
              )}
            </div>
          )}
        </div>

        {/* 10-Digit Touch Keypad (Touch Targets ≥ 56px) */}
        <div
          data-testid="pairing-keypad"
          className="grid grid-cols-3 gap-3 p-2 bg-card rounded-2xl border border-border shadow-xs"
        >
          {["1", "2", "3", "4", "5", "6", "7", "8", "9"].map((digit) => (
            <button
              key={digit}
              type="button"
              data-testid={`pairing-key-${digit}`}
              disabled={isSubmitting}
              onClick={() => handleDigitClick(digit)}
              className="flex min-h-[58px] min-w-[58px] items-center justify-center rounded-xl border border-border/80 bg-background font-mono text-2xl font-bold text-foreground shadow-2xs transition-all active:scale-95 hover:bg-accent hover:text-accent-foreground focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
              aria-label={`Digit ${digit}`}
            >
              {digit}
            </button>
          ))}

          {/* Clear Key */}
          <button
            type="button"
            data-testid="pairing-key-clear"
            disabled={isSubmitting || code.length === 0}
            onClick={handleClear}
            className="flex min-h-[58px] min-w-[58px] items-center justify-center rounded-xl border border-border/80 bg-muted/60 font-bold text-muted-foreground shadow-2xs transition-all active:scale-95 hover:bg-destructive/10 hover:text-destructive focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-30"
            aria-label="Clear code"
          >
            <RotateCcw className="size-6" />
          </button>

          {/* 0 Key */}
          <button
            type="button"
            data-testid="pairing-key-0"
            disabled={isSubmitting}
            onClick={() => handleDigitClick("0")}
            className="flex min-h-[58px] min-w-[58px] items-center justify-center rounded-xl border border-border/80 bg-background font-mono text-2xl font-bold text-foreground shadow-2xs transition-all active:scale-95 hover:bg-accent hover:text-accent-foreground focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
            aria-label="Digit 0"
          >
            0
          </button>

          {/* Backspace Key */}
          <button
            type="button"
            data-testid="pairing-key-backspace"
            disabled={isSubmitting || code.length === 0}
            onClick={handleBackspace}
            className="flex min-h-[58px] min-w-[58px] items-center justify-center rounded-xl border border-border/80 bg-muted/60 font-bold text-muted-foreground shadow-2xs transition-all active:scale-95 hover:bg-destructive/10 hover:text-destructive focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-30"
            aria-label="Backspace"
          >
            <Delete className="size-6" />
          </button>
        </div>

        {/* Submit Action */}
        <Button
          type="button"
          size="lg"
          data-testid="pairing-submit-button"
          disabled={code.length < 6 || isSubmitting}
          onClick={() => void handlePair(code)}
          className="min-h-14 w-full text-base font-bold gap-2 shadow-sm"
        >
          {isSubmitting ? (
            <>
              <Loader2 className="size-5 animate-spin" />
              <span>Pairing Kiosk...</span>
            </>
          ) : (
            <>
              <CheckCircle2 className="size-5" />
              <span>Pair Kiosk</span>
            </>
          )}
        </Button>
      </main>

      {/* Footer */}
      <footer className="w-full max-w-xl text-center pt-4 text-xs text-muted-foreground">
        <span>Hospital Device Management System · Kiosk Appliance Setup</span>
      </footer>
    </div>
  );
}
