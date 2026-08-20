import * as React from "react";

export interface CountdownRingProps {
  expiresAt: string | null;
  /** Max threshold under which the ring appears (defaults to 8s) */
  thresholdSeconds?: number;
  totalDurationSeconds?: number;
  className?: string;
  showAlways?: boolean;
  isSuspended?: boolean;
}

export type CountdownTimerProps = CountdownRingProps;

function calculateDiff(expiresAt: string | null): number | null {
  if (!expiresAt) return null;
  const expiry = new Date(expiresAt).getTime();
  if (Number.isNaN(expiry)) return null;
  const now = Date.now();
  return Math.max(0, Math.ceil((expiry - now) / 1000));
}

export function CountdownRing({
  expiresAt,
  thresholdSeconds = 8,
  className = "",
  showAlways = false,
  isSuspended = false,
}: CountdownRingProps) {
  const [, setTick] = React.useState(0);
  const [prevSuspended, setPrevSuspended] = React.useState(isSuspended);
  const [frozenSeconds, setFrozenSeconds] = React.useState<number | null>(null);

  if (isSuspended !== prevSuspended) {
    setPrevSuspended(isSuspended);
    if (isSuspended) {
      setFrozenSeconds(expiresAt ? calculateDiff(expiresAt) : null);
    } else {
      setFrozenSeconds(null);
    }
  }

  React.useEffect(() => {
    if (!expiresAt || isSuspended) {
      return;
    }

    const interval = setInterval(() => {
      setTick((t) => t + 1);
    }, 250);
    return () => clearInterval(interval);
  }, [expiresAt, isSuspended]);

  const currentSeconds = isSuspended
    ? frozenSeconds ?? (expiresAt ? calculateDiff(expiresAt) : null)
    : expiresAt
    ? calculateDiff(expiresAt)
    : null;

  // Only render when ≤ thresholdSeconds (defaults to 8s) and > 0, unless showAlways is set
  if (currentSeconds === null || currentSeconds <= 0) {
    return null;
  }

  if (!showAlways && currentSeconds > thresholdSeconds) {
    return null;
  }

  // SVG Circular Ring parameters
  const radius = 14;
  const strokeWidth = 3;
  const circumference = 2 * Math.PI * radius;
  const progress = Math.min(1, Math.max(0, currentSeconds / thresholdSeconds));
  const strokeDashoffset = circumference * (1 - progress);

  return (
    <div
      role="timer"
      aria-live="polite"
      aria-atomic="true"
      aria-label={`Session expires in ${currentSeconds} seconds`}
      data-testid="countdown-ring"
      data-seconds-remaining={currentSeconds}
      className={`inline-flex items-center gap-2 rounded-full px-3 py-1 font-mono text-sm font-bold bg-amber-500/15 text-amber-800 dark:text-amber-200 border border-amber-500/40 shadow-xs ${className}`}
    >
      <svg
        className="size-6 -rotate-90 transform"
        viewBox="0 0 36 36"
        aria-hidden="true"
      >
        {/* Track circle */}
        <circle
          cx="18"
          cy="18"
          r={radius}
          fill="none"
          className="stroke-amber-500/20"
          strokeWidth={strokeWidth}
        />
        {/* Animated countdown stroke */}
        <circle
          cx="18"
          cy="18"
          r={radius}
          fill="none"
          className="stroke-amber-600 dark:stroke-amber-400 transition-all duration-200 ease-linear"
          strokeWidth={strokeWidth}
          strokeDasharray={circumference}
          strokeDashoffset={strokeDashoffset}
          strokeLinecap="round"
        />
      </svg>
      <span className="tabular-nums tracking-tight">{currentSeconds}s</span>
    </div>
  );
}

export const CountdownTimer = CountdownRing;
