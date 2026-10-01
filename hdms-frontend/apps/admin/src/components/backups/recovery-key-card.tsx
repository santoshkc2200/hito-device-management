import {
  confirmBackupRecoveryKey,
  createBackupRecoveryKey,
  listBackupDestinations,
  type BackupRecoveryKeyState,
} from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Printer } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useT } from "@/i18n";
import { formatDateTime } from "./format";

type Problem = { type?: string };
const problemIs = (err: unknown, type: string) => (err as Problem | null)?.type?.endsWith(`/${type}`) ?? false;

const TONE: Record<BackupRecoveryKeyState["status"], "default" | "secondary" | "destructive" | "outline"> = {
  missing: "destructive",
  unconfirmed: "secondary",
  ready: "default",
  outdated: "destructive",
};

export function RecoveryKeyCard({ state, localPath }: { state: BackupRecoveryKeyState; localPath: string }) {
  const t = useT();
  const { locale } = useLocale();
  const [open, setOpen] = useState(false);
  const help = {
    missing: t("backups.recoveryKey.missingHelp"),
    unconfirmed: t("backups.recoveryKey.unconfirmedHelp"),
    outdated: t("backups.recoveryKey.outdatedHelp"),
    ready: null,
  }[state.status];

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <KeyRound className="size-4" />
          {t("backups.recoveryKey.title")}
          <Badge variant={TONE[state.status]}>{t(`backups.recoveryKey.status.${state.status}` as never)}</Badge>
        </CardTitle>
        <CardDescription>{t("backups.recoveryKey.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {help && <p className="text-sm">{help}</p>}
        {state.createdAt && (
          <p className="text-xs text-muted-foreground">
            {t("backups.recoveryKey.createdAt", { date: formatDateTime(state.createdAt, locale) })}
          </p>
        )}
        <div className="flex flex-col gap-1">
          <Button className="self-start" variant={state.status === "ready" ? "outline" : "default"} onClick={() => setOpen(true)}>
            {state.status === "missing" ? t("backups.recoveryKey.create") : t("backups.recoveryKey.replace")}
          </Button>
          {state.status !== "missing" && (
            <p className="text-xs text-muted-foreground">{t("backups.recoveryKey.replaceWarning")}</p>
          )}
        </div>
      </CardContent>
      {open && <RecoveryKeyDialog localPath={localPath} onClose={() => setOpen(false)} />}
    </Card>
  );
}

function RecoveryKeyDialog({ localPath, onClose }: { localPath: string; onClose: () => void }) {
  const t = useT();
  const { locale } = useLocale();
  const queryClient = useQueryClient();
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [issued, setIssued] = useState<{ key: string; createdAt: string } | null>(null);
  const [lastGroup, setLastGroup] = useState("");

  const destinations = useQuery({
    queryKey: ["backup", "destinations"],
    enabled: issued !== null,
    queryFn: async () => {
      const res = await listBackupDestinations();
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
  });

  const create = useMutation({
    mutationFn: async () => {
      const res = await createBackupRecoveryKey({ body: { password, totpCode: totpCode.trim() } });
      if (res.error) throw res.error;
      return res.data!;
    },
    onSuccess: (data) => {
      setPassword("");
      setTotpCode("");
      setError(null);
      setIssued({ key: data.key, createdAt: data.createdAt });
      void queryClient.invalidateQueries({ queryKey: ["backup", "config"] });
    },
    onError: (err: unknown) => {
      if (problemIs(err, "reauth-failed")) setError(t("backups.recoveryKey.reauthFailed"));
      else if (problemIs(err, "account-locked")) setError(t("backups.recoveryKey.locked"));
      else if (problemIs(err, "backup-key-missing")) setError(t("backups.recoveryKey.secretsMissing"));
      else setError(t("backups.recoveryKey.failed"));
    },
  });

  const confirm = useMutation({
    mutationFn: async () => {
      const res = await confirmBackupRecoveryKey();
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: () => {
      toast.success(t("backups.recoveryKey.confirmed"));
      void queryClient.invalidateQueries({ queryKey: ["backup", "config"] });
      onClose();
    },
    onError: () => setError(t("backups.recoveryKey.failed")),
  });

  const submitConfirm = () => {
    const expected = issued?.key.slice(-4) ?? "";
    if (lastGroup.trim().toUpperCase() !== expected) {
      setError(t("backups.recoveryKey.confirmMismatch"));
      return;
    }
    setError(null);
    confirm.mutate();
  };

  const recoveryUrl = `${window.location.origin}/recovery`;

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{issued ? t("backups.recoveryKey.sheetTitle") : t("backups.recoveryKey.reauthTitle")}</DialogTitle>
        </DialogHeader>

        {!issued && (
          <>
            <div className="flex flex-col gap-3">
              <p className="text-sm text-muted-foreground">{t("backups.recoveryKey.reauthHelp")}</p>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="rk-password">{t("backups.recoveryKey.password")}</Label>
                <Input id="rk-password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="rk-totp">{t("backups.recoveryKey.totpCode")}</Label>
                <Input id="rk-totp" inputMode="numeric" autoComplete="one-time-code" value={totpCode} onChange={(e) => setTotpCode(e.target.value)} />
              </div>
              {error && <p className="text-sm text-destructive">{error}</p>}
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={onClose}>{t("backups.recoveryKey.cancel")}</Button>
              <Button onClick={() => create.mutate()} disabled={create.isPending || !password || !totpCode.trim()}>
                {t("backups.recoveryKey.continue")}
              </Button>
            </DialogFooter>
          </>
        )}

        {issued && (
          <>
            <p className="text-sm font-medium text-destructive">{t("backups.recoveryKey.sheetOnce")}</p>
            <div className="print-area register-slip flex flex-col gap-3 rounded-md border p-4">
              <h2 className="text-lg font-semibold">{t("backups.recoveryKey.sheetTitle")}</h2>
              <p className="font-identifier text-2xl tracking-wider">{issued.key}</p>
              <p className="text-sm">{t("backups.recoveryKey.sheetSite", { site: window.location.host })}</p>
              <p className="text-sm">{t("backups.recoveryKey.sheetCreated", { date: formatDateTime(issued.createdAt, locale) })}</p>
              <div className="text-sm">
                <p className="font-medium">{t("backups.recoveryKey.sheetWhere")}</p>
                <ul className="list-disc pl-5">
                  <li>{t("backups.recoveryKey.sheetLocal", { path: localPath })}</li>
                  {(destinations.data ?? []).map((d) => (
                    <li key={d.id}>
                      {d.name} <span className="font-identifier text-xs">{d.target}</span>
                    </li>
                  ))}
                </ul>
              </div>
              <ol className="list-decimal pl-5 text-sm">
                <li>{t("backups.recoveryKey.sheetStep1", { url: recoveryUrl })}</li>
                <li>{t("backups.recoveryKey.sheetStep2")}</li>
                <li>{t("backups.recoveryKey.sheetStep3")}</li>
              </ol>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="rk-confirm">{t("backups.recoveryKey.confirmLabel")}</Label>
              <Input id="rk-confirm" className="w-32 font-identifier" maxLength={4} value={lastGroup} onChange={(e) => setLastGroup(e.target.value)} />
              {error && <p className="text-sm text-destructive">{error}</p>}
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => window.print()}>
                <Printer className="size-4" data-icon="inline-start" />
                {t("backups.recoveryKey.print")}
              </Button>
              <Button onClick={submitConfirm} disabled={confirm.isPending || lastGroup.trim().length !== 4}>
                {t("backups.recoveryKey.confirm")}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
