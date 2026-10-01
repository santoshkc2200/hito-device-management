import * as React from "react";
import { cn } from "@hdms/ui";
import { useLocale } from "@hdms/i18n";
import { Message } from "@/components/message";
import { Button } from "@/components/ui/button";
import { useT } from "@/i18n";
import { asRecoveryError, recoveryApi, type RecoveryError, type RestoreView, type Snapshot } from "@/lib/api";
import { commonError } from "@/lib/errors";
import { formatAgo, formatBytes, formatSnapshotDate } from "@/lib/format";

type Loaded = { snapshots: Snapshot[]; last: RestoreView | null };

export function SnapshotsScreen({
  onPick,
  onUndo,
  onRunning,
  onSessionLost,
}: {
  onPick: (snapshot: Snapshot) => void;
  onUndo: (snapshotTakenAt: string) => void;
  onRunning: () => void;
  onSessionLost: () => void;
}) {
  const t = useT();
  const { locale } = useLocale();
  const [loaded, setLoaded] = React.useState<Loaded | null>(null);
  const [error, setError] = React.useState<RecoveryError | null>(null);
  // The flow passes fresh callbacks on every render; the load must not re-run
  // because of that.
  const leave = React.useRef({ onRunning, onSessionLost });
  React.useLayoutEffect(() => {
    leave.current = { onRunning, onSessionLost };
  });

  const load = React.useCallback(async () => {
    try {
      // A restore already running (started from another tab, or before the
      // worker restarted) is followed, never started twice.
      const last = await recoveryApi.restore();
      if (last?.phase === "running") {
        leave.current.onRunning();
        return;
      }
      setLoaded({ snapshots: await recoveryApi.snapshots(), last });
    } catch (err) {
      const e = asRecoveryError(err);
      if (e.sessionLost) {
        leave.current.onSessionLost();
        return;
      }
      setError(e);
    }
  }, []);

  // Loaded once per visit to this screen; Try again reloads.
  React.useEffect(() => {
    void load();
  }, [load]);

  function reload() {
    setError(null);
    void load();
  }

  if (error) {
    return (
      <section className="space-y-4">
        <Message tone="error" testId="snapshots-error">
          {error.code === "repository_unreadable" ? t("snapshots.unreadable") : commonError(t, error)}
        </Message>
        <Button onClick={reload}>{t("common.retry")}</Button>
      </section>
    );
  }
  if (!loaded) return <p className="text-muted-foreground">{t("common.loading")}</p>;

  const { snapshots, last } = loaded;
  const now = new Date();
  return (
    <section className="space-y-6">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold">{t("snapshots.heading")}</h2>
        <p className="text-sm text-muted-foreground">{t("snapshots.hint")}</p>
      </div>

      {last?.canUndo && (
        <Message tone="info" testId="last-restore">
          <p>{t("snapshots.lastRestore", { date: formatSnapshotDate(locale, last.snapshotTakenAt) })}</p>
          <Button variant="outline" className="mt-3" onClick={() => onUndo(last.snapshotTakenAt)}>
            {t("snapshots.undo")}
          </Button>
        </Message>
      )}

      {snapshots.length === 0 ? (
        <p>{t("snapshots.empty")}</p>
      ) : (
        <ul className="space-y-3">
          {snapshots.map((s, i) => (
            <li
              key={s.id}
              data-testid="snapshot"
              data-newest={i === 0 ? "true" : undefined}
              className={cn(
                "flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4",
                i === 0 && "border-primary bg-primary/5",
              )}
            >
              <div className="space-y-1">
                <p className="font-semibold">
                  {t("snapshots.when", {
                    date: formatSnapshotDate(locale, s.takenAt),
                    ago: formatAgo(locale, s.takenAt, now),
                  })}
                  {i === 0 && (
                    <span className="ml-2 rounded-full bg-primary px-2 py-0.5 text-xs text-primary-foreground">
                      {t("snapshots.newest")}
                    </span>
                  )}
                </p>
                <p className="text-sm text-muted-foreground">{t("snapshots.size", { size: formatBytes(s.sizeBytes) })}</p>
              </div>
              <Button variant={i === 0 ? "default" : "outline"} onClick={() => onPick(s)}>
                {t("snapshots.choose")}
              </Button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
