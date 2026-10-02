import { startBackupRestore, undoBackupRestore, type BackupSnapshot } from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useT } from "@/i18n";
import { formatDateTime } from "./format";
import { problemIs } from "./problem";
import { restoreQueryKey } from "./use-restore";

const CONFIRM_WORD = "RESTORE";

// Problem type → backups.restore.problems key.
const PROBLEM_KEYS: Record<string, string> = {
  "reauth-failed": "reauthFailed",
  "account-locked": "accountLocked",
  "restore-running": "restoreRunning",
  "nothing-to-undo": "nothingToUndo",
  "nothing-to-discard": "nothingToDiscard",
  "database-server-down": "databaseServerDown",
  "restore-source-unsupported": "sourceUnsupported",
  "repository-unreadable": "repositoryUnreadable",
  "worker-unavailable": "workerUnavailable",
  "not-found": "notFound",
};

export function restoreProblemKey(err: unknown): string {
  const hit = Object.keys(PROBLEM_KEYS).find((type) => problemIs(err, type));
  return `backups.restore.problems.${hit ? PROBLEM_KEYS[hit] : "failed"}`;
}

type Props =
  | { mode: "restore"; repo: string; snapshot: BackupSnapshot; onClose: () => void }
  | { mode: "undo"; snapshotTakenAt: string; onClose: () => void };

export function RestoreConfirmDialog(props: Props) {
  const t = useT();
  const { locale } = useLocale();
  const queryClient = useQueryClient();
  const [word, setWord] = useState("");
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [error, setError] = useState<string | null>(null);
  const date = formatDateTime(props.mode === "restore" ? props.snapshot.takenAt : props.snapshotTakenAt, locale);

  const submit = useMutation({
    mutationFn: async () => {
      const auth = { confirmation: CONFIRM_WORD as typeof CONFIRM_WORD, password, totpCode: totpCode.trim() };
      const res =
        props.mode === "restore"
          ? await startBackupRestore({ body: { repo: props.repo, snapshotId: props.snapshot.snapshotId, ...auth } })
          : await undoBackupRestore({ body: auth });
      if (res.error) throw res.error;
      return res.data!;
    },
    onSuccess: (data) => {
      queryClient.setQueryData(restoreQueryKey, data);
      props.onClose();
    },
    onError: (err: unknown) => setError(t(restoreProblemKey(err) as never)),
  });

  const ready = word === CONFIRM_WORD && password !== "" && totpCode.trim().length >= 6 && !submit.isPending;
  const restore = props.mode === "restore";

  return (
    <Dialog open onOpenChange={(open) => !open && !submit.isPending && props.onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(restore ? "backups.restore.restoreTitle" : "backups.restore.undoTitle")}</DialogTitle>
          <DialogDescription>
            {restore ? t("backups.restore.restoreBody", { date }) : t("backups.restore.undoBody")}
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            if (ready) submit.mutate();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="restore-word">{t("backups.restore.typeLabel", { word: CONFIRM_WORD })}</Label>
            <Input id="restore-word" autoComplete="off" value={word} onChange={(e) => setWord(e.target.value)} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="restore-password">{t("backups.restore.password")}</Label>
            <Input id="restore-password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="restore-totp">{t("backups.restore.totpCode")}</Label>
            <Input id="restore-totp" inputMode="numeric" autoComplete="one-time-code" value={totpCode} onChange={(e) => setTotpCode(e.target.value)} />
          </div>
          {restore && <p className="text-xs text-muted-foreground">{t("backups.restore.signInNote", { date })}</p>}
          {error && <p className="text-sm text-destructive" role="alert">{error}</p>}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={props.onClose} disabled={submit.isPending}>
              {t("backups.restore.cancel")}
            </Button>
            <Button type="submit" variant="destructive" disabled={!ready}>
              {t(restore ? "backups.restore.restoreSubmit" : "backups.restore.undoSubmit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
