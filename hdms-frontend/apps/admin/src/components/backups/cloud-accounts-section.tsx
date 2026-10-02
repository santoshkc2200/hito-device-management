import { deleteBackupCloudAccount, type BackupCloudAccount } from "@hdms/api-client";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useT } from "@/i18n";
import { accountLabel } from "./cloud-format";
import { CloudConnectDialog } from "./cloud-connect-dialog";
import { CLOUD_ACCOUNTS_KEY, useCloudAccounts } from "./use-cloud-accounts";

type Problem = { type?: string };
const problemIs = (err: unknown, type: string) => (err as Problem | null)?.type?.endsWith(`/${type}`) ?? false;

export function CloudAccountsSection() {
  const t = useT();
  const queryClient = useQueryClient();
  const accounts = useCloudAccounts();
  const [connecting, setConnecting] = useState<BackupCloudAccount | "new" | null>(null);
  const [removing, setRemoving] = useState<BackupCloudAccount | null>(null);
  const [error, setError] = useState<string | null>(null);
  const providerLabel = (p: string) => t(`backups.cloud.providers.${p}` as never);

  const remove = useMutation({
    mutationFn: async (id: string) => {
      const res = await deleteBackupCloudAccount({ path: { id } });
      if (res.error) throw res.error;
    },
    onSuccess: () => {
      setError(null);
      setRemoving(null);
      toast.success(t("backups.cloud.disconnected"));
      void queryClient.invalidateQueries({ queryKey: CLOUD_ACCOUNTS_KEY });
    },
    onError: (err: unknown) => {
      setRemoving(null);
      setError(problemIs(err, "cloud-account-in-use") ? t("backups.cloud.inUse") : t("backups.errors.save"));
    },
  });

  const items = accounts.data ?? [];
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h3 className="text-sm font-medium">{t("backups.cloud.title")}</h3>
          <p className="text-xs text-muted-foreground">{t("backups.cloud.description")}</p>
        </div>
        <Button variant="outline" onClick={() => setConnecting("new")}>{t("backups.cloud.add")}</Button>
      </div>
      {accounts.isSuccess && items.length === 0 && <p className="text-sm text-muted-foreground">{t("backups.cloud.empty")}</p>}
      <ul className="flex flex-col gap-2">
        {items.map((a) => (
          <li key={a.id} className="flex items-center gap-3 rounded-md border p-3">
            <span className="flex flex-1 flex-col">
              <span className="font-medium">{a.name}</span>
              <span className="text-xs text-muted-foreground">{accountLabel(a, providerLabel)}</span>
              {a.lastError && a.status !== "connected" && <span className="text-xs text-destructive">{a.lastError}</span>}
            </span>
            <Badge variant={a.status === "connected" ? "default" : "outline"}>{t(`backups.cloud.status.${a.status}` as never)}</Badge>
            {(a.status === "expired" || a.status === "revoked") && (
              <Button size="sm" variant="outline" onClick={() => setConnecting(a)}>{t("backups.cloud.reconnect")}</Button>
            )}
            <Button size="sm" variant="ghost" onClick={() => setRemoving(a)}>{t("backups.cloud.disconnect")}</Button>
          </li>
        ))}
      </ul>
      {error && <p className="text-sm text-destructive">{error}</p>}
      {connecting && (
        <CloudConnectDialog
          account={connecting === "new" ? undefined : connecting}
          onClose={() => setConnecting(null)}
          onConnected={() => setConnecting(null)}
        />
      )}
      <AlertDialog open={removing !== null} onOpenChange={(open) => !open && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("backups.cloud.disconnectTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("backups.cloud.disconnectBody", { name: removing?.name ?? "" })}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("backups.destinations.form.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={() => removing && remove.mutate(removing.id)}>{t("backups.cloud.disconnect")}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}
