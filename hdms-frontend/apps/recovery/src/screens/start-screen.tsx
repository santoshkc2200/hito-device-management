import * as React from "react";
import { Message } from "@/components/message";
import { Button } from "@/components/ui/button";
import { useT, type RecoveryKey } from "@/i18n";
import { asRecoveryError, recoveryApi, type LiveState, type RecoveryError, type Source, type Status } from "@/lib/api";
import { commonError } from "@/lib/errors";
import { sourceDetail, sourceTitle } from "@/lib/source";

const DATABASE_TEXT: Record<LiveState, { title: RecoveryKey; hint: RecoveryKey; tone: "info" | "error" }> = {
  working: { title: "status.working", hint: "status.workingHint", tone: "info" },
  empty: { title: "status.empty", hint: "status.emptyHint", tone: "info" },
  damaged: { title: "status.damaged", hint: "status.damagedHint", tone: "error" },
  server_down: { title: "status.serverDown", hint: "status.serverDownHint", tone: "error" },
};

type Loaded = { status: Status; sources: Source[] };

export function StartScreen({ onChoose }: { onChoose: (source: Source) => void }) {
  const t = useT();
  const [loaded, setLoaded] = React.useState<Loaded | null>(null);
  const [error, setError] = React.useState<RecoveryError | null>(null);

  const load = React.useCallback(async () => {
    try {
      const status = await recoveryApi.status();
      // Nothing can be restored into a database server that is not running.
      const sources = status.database === "server_down" ? [] : await recoveryApi.sources();
      setLoaded({ status, sources });
    } catch (err) {
      setError(asRecoveryError(err));
    }
  }, []);

  React.useEffect(() => {
    void load();
  }, [load]);

  function reload() {
    setLoaded(null);
    setError(null);
    void load();
  }

  if (error) {
    return (
      <section className="space-y-4">
        <Message tone="error" testId="start-error">
          {commonError(t, error)}
        </Message>
        <Button onClick={reload}>{t("common.retry")}</Button>
      </section>
    );
  }
  if (!loaded) return <p className="text-muted-foreground">{t("common.loading")}</p>;

  const { status, sources } = loaded;
  const text = DATABASE_TEXT[status.database] ?? DATABASE_TEXT.damaged;
  return (
    <section className="space-y-8">
      <div className="space-y-3">
        <h2 className="text-lg font-semibold">{t("status.heading")}</h2>
        <Message tone={text.tone} testId="database-status">
          <p className="font-semibold">{t(text.title)}</p>
          <p className="mt-1">{t(text.hint)}</p>
        </Message>
        {status.restoreRunning && (
          <Message tone="info" testId="restore-running">
            {t("status.restoreRunning")}
          </Message>
        )}
      </div>

      {status.database === "server_down" ? (
        <Button onClick={reload}>{t("status.checkAgain")}</Button>
      ) : (
        <div className="space-y-3">
          <h2 className="text-lg font-semibold">{t("sources.heading")}</h2>
          <ul className="space-y-3">
            {sources.map((s) => (
              <li key={s.id}>
                <button
                  type="button"
                  data-testid={`source-${s.id}`}
                  disabled={!s.hasRecoveryKey}
                  onClick={() => onChoose(s)}
                  className="w-full rounded-lg border bg-background p-4 text-left transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  <span className="block font-semibold">{sourceTitle(t, s)}</span>
                  <span className="block break-all text-sm text-muted-foreground">{sourceDetail(t, s)}</span>
                  {!s.hasRecoveryKey && (
                    <span className="mt-1 block text-sm text-destructive">{t("sources.noKey")}</span>
                  )}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
