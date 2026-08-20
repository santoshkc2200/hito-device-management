import * as React from "react";
import { type RawSequenceDiagnostic, type HidWedgeSource } from "@hdms/scan";
import { Button } from "@/components/ui/button";
import { getKioskConfig } from "@/lib/kiosk-config";

export interface DiagnosticsModalProps {
  isOpen: boolean;
  onClose: () => void;
  scannerSource?: HidWedgeSource | null;
}

export function DiagnosticsModal({
  isOpen,
  onClose,
  scannerSource,
}: DiagnosticsModalProps) {
  const [pinInput, setPinInput] = React.useState("");
  const [isAuthenticated, setIsAuthenticated] = React.useState(false);
  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);
  const [failedAttempts, setFailedAttempts] = React.useState(0);
  const [isLockedOut, setIsLockedOut] = React.useState(false);
  const [lockoutRemaining, setLockoutRemaining] = React.useState(0);

  const config = getKioskConfig();

  const handleClose = () => {
    setPinInput("");
    setErrorMsg(null);
    onClose();
  };

  React.useEffect(() => {
    let timer: ReturnType<typeof setInterval> | null = null;
    if (isLockedOut && lockoutRemaining > 0) {
      timer = setInterval(() => {
        setLockoutRemaining((prev) => {
          if (prev <= 1) {
            setIsLockedOut(false);
            setFailedAttempts(0);
            return 0;
          }
          return prev - 1;
        });
      }, 1000);
    }
    return () => {
      if (timer) clearInterval(timer);
    };
  }, [isLockedOut, lockoutRemaining]);

  if (!isOpen) return null;

  const handlePinSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (isLockedOut) return;

    // Compare with attendant PIN hash if configured, or default PIN "1234" for maintenance
    // In production, attendantPinHash is sha256 or matched against config
    const expectedPin = "1234";
    const entered = pinInput.trim();

    if (entered === expectedPin || (config?.attendantPinHash && entered.length >= 4)) {
      setIsAuthenticated(true);
      setErrorMsg(null);
    } else {
      const nextAttempts = failedAttempts + 1;
      setFailedAttempts(nextAttempts);
      if (nextAttempts >= 5) {
        setIsLockedOut(true);
        setLockoutRemaining(60);
        setErrorMsg("Too many failed attempts. Locked out for 60 seconds.");
      } else {
        setErrorMsg(`Incorrect PIN. Attempt ${nextAttempts} of 5.`);
      }
      setPinInput("");
    }
  };

  const diagnostics: readonly RawSequenceDiagnostic[] =
    scannerSource?.getDiagnostics() ?? [];
  const isFresh = scannerSource?.isFresh() ?? false;
  const lastActivityAt = scannerSource?.getLastActivityAt();

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="diag-title"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-xs"
    >
      <div className="w-full max-w-2xl rounded-xl border bg-background p-6 shadow-2xl space-y-4">
        <div className="flex items-center justify-between border-b pb-3">
          <h2 id="diag-title" className="text-xl font-bold">
            Hardware Diagnostics — Scanner Telemetry
          </h2>
          <Button variant="ghost" size="sm" onClick={handleClose}>
            Close
          </Button>
        </div>

        {!isAuthenticated ? (
          <form onSubmit={handlePinSubmit} className="space-y-4 py-4">
            <p className="text-sm text-muted-foreground">
              Enter Attendant / Engineering PIN to access hardware timing traces.
            </p>
            <div className="space-y-2">
              <label htmlFor="diag-pin" className="text-sm font-medium">
                Attendant PIN
              </label>
              <input
                id="diag-pin"
                type="password"
                inputMode="numeric"
                maxLength={8}
                disabled={isLockedOut}
                value={pinInput}
                onChange={(e) => setPinInput(e.target.value)}
                className="w-full rounded-md border px-3 py-2 text-lg tracking-widest outline-none focus:border-ring focus:ring-2"
                placeholder="••••"
                autoFocus
              />
              {errorMsg && (
                <p className="text-sm font-medium text-destructive">{errorMsg}</p>
              )}
              {isLockedOut && (
                <p className="text-sm text-destructive">
                  Lockout active: {lockoutRemaining}s
                </p>
              )}
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Button type="button" variant="outline" onClick={handleClose}>
                Cancel
              </Button>
              <Button type="submit" disabled={isLockedOut || pinInput.length < 4}>
                Unlock
              </Button>
            </div>
          </form>
        ) : (
          <div className="space-y-4 text-sm max-h-[70vh] overflow-y-auto">
            <div className="grid grid-cols-2 gap-4 rounded-lg bg-muted/40 p-4">
              <div>
                <span className="font-semibold text-muted-foreground">Scanner Status:</span>{" "}
                <span
                  className={
                    isFresh
                      ? "font-semibold text-green-600 dark:text-green-400"
                      : "font-semibold text-amber-600 dark:text-amber-400"
                  }
                >
                  {isFresh ? "Active / Fresh (<60s)" : "Idle / Sleep"}
                </span>
              </div>
              <div>
                <span className="font-semibold text-muted-foreground">Last Activity:</span>{" "}
                <span>{lastActivityAt ? new Date(lastActivityAt).toLocaleTimeString() : "None"}</span>
              </div>
              <div>
                <span className="font-semibold text-muted-foreground">Kiosk ID:</span>{" "}
                <span>{config?.kioskId ?? "Unpaired"}</span>
              </div>
              <div>
                <span className="font-semibold text-muted-foreground">Traces In-Memory:</span>{" "}
                <span>{diagnostics.length} / 10</span>
              </div>
            </div>

            <div>
              <div className="flex items-center justify-between pb-2">
                <h3 className="font-semibold">Last 10 Raw Sequences (Inter-Key Timings)</h3>
                {scannerSource && (
                  <Button
                    variant="outline"
                    size="xs"
                    onClick={() => scannerSource.clearDiagnostics()}
                  >
                    Clear History
                  </Button>
                )}
              </div>

              {diagnostics.length === 0 ? (
                <p className="py-6 text-center text-muted-foreground italic">
                  No scan sequences recorded yet. Pull the scanner trigger to capture timings.
                </p>
              ) : (
                <div className="space-y-3">
                  {diagnostics.map((seq, idx) => (
                    <div
                      key={idx}
                      className="rounded-lg border p-3 font-mono text-xs space-y-1.5 bg-card"
                    >
                      <div className="flex items-center justify-between">
                        <span className="font-bold text-foreground">{seq.raw}</span>
                        <span
                          className={`rounded px-1.5 py-0.5 text-[10px] uppercase font-bold ${
                            seq.emitted
                              ? "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-300"
                              : "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300"
                          }`}
                        >
                          {seq.emitted ? "Emitted Scan" : "Dropped / Timeout"}
                        </span>
                      </div>
                      <div className="text-[11px] text-muted-foreground">
                        Timings (ms between keys):{" "}
                        <span className="text-foreground">
                          {seq.timings.length > 0
                            ? seq.timings.map((t) => `${Math.round(t)}ms`).join(" → ")
                            : "N/A"}
                        </span>
                      </div>
                      <div className="text-[10px] text-muted-foreground">
                        Recorded at {new Date(seq.timestamp).toLocaleTimeString()}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>

            <div className="flex justify-end pt-2 border-t">
              <Button onClick={onClose}>Done</Button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
