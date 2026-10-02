import { discardBackupRestore, endBackupMaintenance, type BackupRestore } from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useT } from "@/i18n";
import { formatDateTime } from "./format";
import { restoreProblemKey } from "./problem";
import { RestoreConfirmDialog } from "./restore-confirm-dialog";
import { RestoreSteps } from "./restore-steps";
import { restoreQueryKey, useRestore } from "./use-restore";

// A failed restore stays in the state file until the next one; the banner
// shows it for a day.
const FAILED_SHOWN_FOR_MS = 24 * 60 * 60 * 1000;

function worthShowing(r: BackupRestore): boolean {
  if (r.phase === "running" || r.canUndo || r.canDiscard) return true;
  if (r.phase === "failed" && r.finishedAt) return Date.now() - Date.parse(r.finishedAt) < FAILED_SHOWN_FOR_MS;
  return false;
}

export function RestoreBanner() {
  const t = useT();
  const { locale } = useLocale();
  const queryClient = useQueryClient();
  const { status, running } = useRestore();
  const [undoOpen, setUndoOpen] = useState(false);
  const [discardOpen, setDiscardOpen] = useState(false);

  const onError = (err: unknown) => toast.error(t(restoreProblemKey(err) as never));
  const discard = useMutation({
    mutationFn: async () => {
      const res = await discardBackupRestore();
      if (res.error) throw res.error;
      return res.data!;
    },
    onSuccess: (data) => {
      queryClient.setQueryData(restoreQueryKey, data);
      toast.success(t("backups.restore.discarded"));
    },
    onError,
  });
  const endMaintenance = useMutation({
    mutationFn: async () => {
      const res = await endBackupMaintenance();
      if (res.error) throw res.error;
      return res.data!;
    },
    onSuccess: (data) => {
      queryClient.setQueryData(restoreQueryKey, data);
      toast.success(t("backups.restore.maintenanceEnded"));
    },
    onError,
  });

  if (!status) return null;
  const restore = status.restore && worthShowing(status.restore) ? status.restore : null;
  const showMaintenance = status.maintenance && !running;
  if (!restore && !showMaintenance) return null;
  const date = restore ? formatDateTime(restore.snapshotTakenAt, locale) : "";

  let title = "";
  let help: string | null = null;
  if (restore?.phase === "running") {
    title = restore.kind === "restore" ? t("backups.restore.runningRestore", { date }) : t("backups.restore.runningUndo");
  } else if (restore?.phase === "failed") {
    title = t("backups.restore.failed");
    help =
      restore.error === "no_admins" || restore.error === "interrupted"
        ? t(`backups.restore.errors.${restore.error}` as never)
        : t("backups.restore.errors.stepFailed", { step: t(`backups.restore.steps.${restore.step}` as never) });
  } else if (restore) {
    title = restore.kind === "restore" ? t("backups.restore.doneRestore", { date }) : t("backups.restore.doneUndo");
    help = restore.kind === "restore" ? t("backups.restore.doneRestoreHelp") : t("backups.restore.doneUndoHelp");
  }

  return (
    <div className="flex flex-col gap-3">
      {restore && (
        <Card data-testid="restore-banner">
          <CardHeader>
            <CardTitle className="text-base">{title}</CardTitle>
            {help && <CardDescription>{help}</CardDescription>}
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            {restore.phase !== "completed" && <RestoreSteps restore={restore} />}
            {restore.warning && (
              <p className="text-sm text-destructive">
                {t("backups.restore.warning", {
                  step: t(`backups.restore.steps.${restore.warning.replace(/_failed$/, "")}` as never),
                })}
              </p>
            )}
            {(restore.canUndo || restore.canDiscard) && (
              <div className="flex flex-wrap gap-2">
                {restore.canUndo && (
                  <Button variant="outline" onClick={() => setUndoOpen(true)}>{t("backups.restore.rollBack")}</Button>
                )}
                {restore.canDiscard && (
                  <Button variant="ghost" onClick={() => setDiscardOpen(true)} disabled={discard.isPending}>
                    {t("backups.restore.discard")}
                  </Button>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      )}
      {showMaintenance && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("backups.restore.maintenanceTitle")}</CardTitle>
            <CardDescription>{t("backups.restore.maintenanceHelp")}</CardDescription>
          </CardHeader>
          <CardContent>
            <Button onClick={() => endMaintenance.mutate()} disabled={endMaintenance.isPending}>
              {t("backups.restore.endMaintenance")}
            </Button>
          </CardContent>
        </Card>
      )}
      {undoOpen && restore && (
        <RestoreConfirmDialog mode="undo" snapshotTakenAt={restore.snapshotTakenAt} onClose={() => setUndoOpen(false)} />
      )}
      <AlertDialog open={discardOpen} onOpenChange={setDiscardOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("backups.restore.discardTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("backups.restore.discardBody")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("backups.restore.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={() => discard.mutate()}>{t("backups.restore.discardConfirm")}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
