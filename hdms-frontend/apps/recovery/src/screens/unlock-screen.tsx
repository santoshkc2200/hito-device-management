import * as React from "react";
import { Message } from "@/components/message";
import { Button } from "@/components/ui/button";
import { useT, type RecoveryKey, type Translate } from "@/i18n";
import { asRecoveryError, recoveryApi, type RecoveryError, type Source } from "@/lib/api";
import { commonError } from "@/lib/errors";
import { sourceTitle } from "@/lib/source";

const UNLOCK_ERRORS: Record<string, RecoveryKey> = {
  key_typo: "unlock.errors.key_typo",
  key_format: "unlock.errors.key_format",
  key_wrong: "unlock.errors.key_wrong",
  no_bundle: "unlock.errors.no_bundle",
  bundle_damaged: "unlock.errors.bundle_damaged",
  source_not_found: "unlock.errors.source_not_found",
};

function unlockError(t: Translate, err: RecoveryError): string {
  if (err.code === "too_many_attempts") {
    const minutes = Math.max(1, Math.ceil((err.retryAfterSeconds ?? 60) / 60));
    return t("unlock.errors.too_many_attempts", { minutes });
  }
  const key = UNLOCK_ERRORS[err.code];
  return key ? t(key) : commonError(t, err);
}

export function UnlockScreen({
  source,
  sessionEnded,
  onUnlocked,
  onMismatch,
  onBack,
}: {
  source: Source;
  sessionEnded: boolean;
  onUnlocked: () => void;
  onMismatch: () => void;
  onBack: () => void;
}) {
  const t = useT();
  const [key, setKey] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<RecoveryError | null>(null);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (busy || key.trim() === "") return;
    setBusy(true);
    setError(null);
    try {
      await recoveryApi.unlock(source.id, key);
      setKey("");
      onUnlocked();
    } catch (err) {
      const e = asRecoveryError(err);
      if (e.code === "keys_mismatch") {
        setKey("");
        onMismatch();
        return;
      }
      // The key stays in the field: retyping 28 characters is how a second
      // mistake gets made.
      setError(e);
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="space-y-6">
      <div className="space-y-2">
        <h2 className="text-lg font-semibold">{t("unlock.heading")}</h2>
        <p className="text-sm text-muted-foreground">{t("unlock.from", { place: sourceTitle(t, source) })}</p>
        <p className="text-sm">{t("unlock.hint")}</p>
      </div>
      {sessionEnded && <Message tone="info">{t("unlock.sessionEnded")}</Message>}
      <form onSubmit={submit} className="space-y-4" noValidate>
        <div className="space-y-2">
          <label htmlFor="recovery-key" className="block text-sm font-medium">
            {t("unlock.label")}
          </label>
          <input
            id="recovery-key"
            name="recovery-key"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            autoComplete="off"
            autoCapitalize="characters"
            spellCheck={false}
            placeholder={t("unlock.placeholder")}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? "unlock-error" : undefined}
            className="h-12 w-full rounded-md border bg-background px-3 font-mono text-lg uppercase tracking-wider focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
          />
        </div>
        {error && (
          <Message tone="error" id="unlock-error" testId="unlock-error">
            {unlockError(t, error)}
          </Message>
        )}
        <div className="flex flex-wrap gap-3">
          <Button type="submit" size="lg" disabled={busy || key.trim() === ""}>
            {busy ? t("unlock.submitting") : t("unlock.submit")}
          </Button>
          <Button variant="outline" size="lg" onClick={onBack}>
            {t("common.back")}
          </Button>
        </div>
      </form>
    </section>
  );
}
