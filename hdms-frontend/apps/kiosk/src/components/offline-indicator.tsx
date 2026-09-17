import * as React from "react";
import { WifiOff } from "lucide-react";
import { useTranslator } from "@/i18n";
import { getPendingCount, subscribeQueue } from "@/lib/offline-queue";
import { getKioskConfig } from "@/lib/kiosk-config";

export interface OfflineIndicatorProps {
  count?: number;
  className?: string;
}

export function usePendingQueueCount(kioskId?: string): number {
  const resolvedKioskId = kioskId ?? getKioskConfig()?.kioskId;
  const [count, setCount] = React.useState(0);

  React.useEffect(() => {
    let isMounted = true;

    const updateCount = async () => {
      try {
        const c = await getPendingCount(resolvedKioskId);
        if (isMounted) setCount(c);
      } catch {
        // Ignore DB read errors
      }
    };

    void updateCount();
    const unsubscribe = subscribeQueue(() => {
      void updateCount();
    });

    return () => {
      isMounted = false;
      unsubscribe();
    };
  }, [resolvedKioskId]);

  return count;
}

export function OfflineIndicator({ count: countProp, className = "" }: OfflineIndicatorProps) {
  const t = useTranslator();
  const liveCount = usePendingQueueCount();
  const effectiveCount = countProp !== undefined ? countProp : liveCount;

  // Hides at zero with no toast and no announcement (3.8 quiet-recovery rule)
  if (effectiveCount <= 0) {
    return null;
  }

  const message =
    effectiveCount === 1
      ? t("offline.workingOfflinePendingSingular")
      : t("offline.workingOfflinePending", { count: effectiveCount });

  return (
    <div
      role="status"
      aria-live="polite"
      aria-atomic="true"
      data-testid="offline-indicator"
      className={`w-full bg-amber-400 text-stone-950 dark:bg-amber-400 dark:text-stone-950 px-4 py-2 border-b-2 border-amber-600 font-bold text-sm md:text-base flex items-center justify-center gap-2.5 shadow-sm transition-all ${className}`}
    >
      <WifiOff className="size-5 shrink-0 text-stone-950" aria-hidden="true" />
      <span className="tracking-tight">{message}</span>
    </div>
  );
}
