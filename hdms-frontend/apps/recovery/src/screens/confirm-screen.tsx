import * as React from "react";
import { useLocale } from "@hdms/i18n";
import { Message } from "@/components/message";
import { Button } from "@/components/ui/button";
import { useT, type RecoveryKey } from "@/i18n";
import { CONFIRM_WORD, asRecoveryError, recoveryApi, type RecoveryError, type Snapshot } from "@/lib/api";
import { commonError } from "@/lib/errors";
import { formatSnapshotDate } from "@/lib/format";

export type ConfirmMode = { kind: "restore"; snapshot: Snapshot } | { kind: "undo"; snapshotTakenAt: string };

const CONFIRM_ERRORS: Record<string, RecoveryKey> = {
  database_server_down: "confirm.errors.database_server_down",
  snapshot_not_found: "confirm.errors.snapshot_not_found",
  nothing_to_undo: "confirm.errors.nothing_to_undo",
};

export function ConfirmScreen({
  mode,
  onStarted,
  onBack,
  onSessionLost,
}: {
  mode: ConfirmMode;
  onStarted: () => void;
  onBack: () => void;
  onSessionLost: () => void;
}) {
  const t = useT();
  const { locale } = useLocale();
  const [typed, setTyped] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<RecoveryError | null>(null);

  const date = formatSnapshotDate(locale, mode.kind === "restore" ? mode.snapshot.takenAt : mode.snapshotTakenAt);
  // Case does not matter to a person; the worker always receives RESTORE.
  const confirmed = typed.trim().toUpperCase() === CONFIRM_WORD;

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (!confirmed || busy) return;
    setBusy(true);
    setError(null);
    try {
      if (mode.kind === "restore") await recoveryApi.startRestore(mode.snapshot.id);
      else await recoveryApi.undo();
      onStarted();
    } catch (err) {
      const e = asRecoveryError(err);
      if (e.sessionLost) {
        onSessionLost();
        return;
      }
      if (e.code === "restore_running") {
        onStarted();
        return;
      }
      setError(e);
      setBusy(false);
    }
  }

  const errorKey = error ? CONFIRM_ERRORS[error.code] : undefined;
  return (
    <section className="space-y-6">
      <h2 className="text-lg font-semibold">
        {t(mode.kind === "restore" ? "confirm.heading" : "confirm.undoHeading")}
      </h2>
      <Message tone="warning">
        <p className="font-semibold">
          {mode.kind === "restore" ? t("confirm.replace", { date }) : t("confirm.undoBody", { date })}
        </p>
        <p className="mt-1">{t("confirm.kiosks")}</p>
      </Message>
      <form onSubmit={submit} className="space-y-4" noValidate>
        <div className="space-y-2">
          <label htmlFor="confirm-word" className="block text-sm font-medium">
            {t("confirm.typeLabel")}
          </label>
          <input
            id="confirm-word"
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            autoComplete="off"
            autoCapitalize="characters"
            spellCheck={false}
            className="h-12 w-full rounded-md border bg-background px-3 font-mono text-lg uppercase tracking-wider focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
          />
        </div>
        {error && (
          <Message tone="error" testId="confirm-error">
            {errorKey ? t(errorKey) : commonError(t, error)}
          </Message>
        )}
        <div className="flex flex-wrap gap-3">
          <Button type="submit" variant="destructive" size="lg" disabled={!confirmed || busy}>
            {busy ? t("confirm.starting") : t(mode.kind === "restore" ? "confirm.start" : "confirm.startUndo")}
          </Button>
          <Button variant="outline" size="lg" onClick={onBack}>
            {t("common.back")}
          </Button>
        </div>
      </form>
    </section>
  );
}
