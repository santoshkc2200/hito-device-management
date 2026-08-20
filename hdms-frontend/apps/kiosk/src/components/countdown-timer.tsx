import * as React from "react";
import { Clock } from "lucide-react";

export interface CountdownTimerProps {
  expiresAt: string | null;
  totalDurationSeconds?: number;
  className?: string;
}

function calculateDiff(expiresAt: string | null): number | null {
  if (!expiresAt) return null;
  const expiry = new Date(expiresAt).getTime();
  const now = Date.now();
  return Math.max(0, Math.ceil((expiry - now) / 1000));
}

export function CountdownTimer({
  expiresAt,
  totalDurationSeconds = 25,
  className = "",
}: CountdownTimerProps) {
  const [secondsRemaining, setSecondsRemaining] = React.useState<number | null>(() =>
    calculateDiff(expiresAt)
  );

  React.useEffect(() => {
    if (!expiresAt) return;

    const interval = setInterval(() => {
      const remaining = calculateDiff(expiresAt);
      setSecondsRemaining(remaining);
      if (remaining !== null && remaining <= 0) {
        clearInterval(interval);
      }
    }, 500);

    return () => clearInterval(interval);
  }, [expiresAt]);

  const currentSeconds = expiresAt ? secondsRemaining : null;

  if (currentSeconds === null) {
    return null;
  }

  const isUrgent = currentSeconds <= 8;
  const percentage = Math.min(100, Math.max(0, (currentSeconds / totalDurationSeconds) * 100));

  return (
    <div
      role="timer"
      aria-live="polite"
      aria-atomic="true"
      aria-label={`Session expires in ${currentSeconds} seconds`}
      className={`inline-flex items-center gap-2 rounded-full px-3.5 py-1.5 font-mono text-sm font-semibold transition-colors duration-200 ${
        isUrgent
          ? "bg-warning/20 text-warning-foreground border border-warning/50 animate-pulse"
          : "bg-muted text-muted-foreground border border-border"
      } ${className}`}
    >
      <Clock className={`size-4 ${isUrgent ? "text-warning" : "text-muted-foreground"}`} />
      <span>{currentSeconds}s</span>
      <div
        className="hidden"
        aria-hidden="true"
        data-testid="countdown-percentage"
        data-percentage={percentage}
      />
    </div>
  );
}
