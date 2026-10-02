import {
  createBackupCloudAccount,
  getBackupCloudAccount,
  reconnectBackupCloudAccount,
  type BackupCloudAccount,
  type BackupCloudProvider,
  type BackupCloudSignIn,
} from "@hdms/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useT } from "@/i18n";
import { CLOUD_ACCOUNTS_KEY, cloudPoll } from "./use-cloud-accounts";

type Problem = { type?: string };
const problemIs = (err: unknown, type: string) => (err as Problem | null)?.type?.endsWith(`/${type}`) ?? false;

const PROVIDERS: BackupCloudProvider[] = ["google_drive", "onedrive"];

/**
 * Connects a new account (no `account`), or restarts the sign-in of an
 * expired or revoked one (`account` given). onConnected receives the account
 * once the person presses Done.
 */
export function CloudConnectDialog({
  account,
  onClose,
  onConnected,
}: {
  account?: BackupCloudAccount;
  onClose: () => void;
  onConnected: (account: BackupCloudAccount) => void;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  const [signIn, setSignIn] = useState<BackupCloudSignIn | null>(null);
  const [error, setError] = useState<string | null>(null);

  const fail = (err: unknown) => {
    if (problemIs(err, "cloud-provider-rejected")) setError(t("backups.cloud.connect.errors.rejected"));
    else if (problemIs(err, "cloud-provider-unreachable")) setError(t("backups.cloud.connect.errors.unreachable"));
    else if (problemIs(err, "cloud-unavailable")) setError(t("backups.cloud.connect.errors.unavailable"));
    else setError(t("backups.cloud.connect.errors.generic"));
  };
  const started = (s: BackupCloudSignIn) => {
    setError(null);
    setSignIn(s);
    void queryClient.invalidateQueries({ queryKey: CLOUD_ACCOUNTS_KEY });
  };

  const create = useMutation({
    mutationFn: async (body: { provider: BackupCloudProvider; name: string; clientId: string; clientSecret?: string; tenant?: string }) => {
      const res = await createBackupCloudAccount({ body });
      if (res.error) throw res.error;
      return res.data!;
    },
    onSuccess: started,
    onError: fail,
  });
  const reconnect = useMutation({
    mutationFn: async (id: string) => {
      const res = await reconnectBackupCloudAccount({ path: { id } });
      if (res.error) throw res.error;
      return res.data!;
    },
    onSuccess: started,
    onError: fail,
  });

  const reconnected = useRef(false);
  useEffect(() => {
    if (account && !reconnected.current) {
      reconnected.current = true;
      reconnect.mutate(account.id);
    }
  }, [account, reconnect]);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("backups.cloud.connect.title")}</DialogTitle>
        </DialogHeader>
        {signIn ? (
          <CodePhase
            signIn={signIn}
            onDone={onConnected}
            onRestart={() => reconnect.mutate(signIn.id)}
            onClose={onClose}
          />
        ) : account ? (
          <>
            {reconnect.isPending && <Loader2 className="size-4 animate-spin" />}
            {error && <p className="text-sm text-destructive">{error}</p>}
            <DialogFooter>
              <Button variant="outline" onClick={onClose}>{t("backups.cloud.connect.close")}</Button>
            </DialogFooter>
          </>
        ) : (
          <ConnectForm busy={create.isPending} error={error} onCancel={onClose} onSubmit={(b) => create.mutate(b)} />
        )}
      </DialogContent>
    </Dialog>
  );
}

function ConnectForm({
  busy,
  error,
  onCancel,
  onSubmit,
}: {
  busy: boolean;
  error: string | null;
  onCancel: () => void;
  onSubmit: (body: { provider: BackupCloudProvider; name: string; clientId: string; clientSecret?: string; tenant?: string }) => void;
}) {
  const t = useT();
  const [provider, setProvider] = useState<BackupCloudProvider>("google_drive");
  const [name, setName] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [tenant, setTenant] = useState("common"); // i18n-allow-literal: the directory ID Microsoft treats as "any", not prose
  const [local, setLocal] = useState<string | null>(null);
  const google = provider === "google_drive";

  const submit = () => {
    if (!name.trim() || !clientId.trim() || (google && !clientSecret.trim())) {
      setLocal(t("backups.cloud.connect.errors.required"));
      return;
    }
    setLocal(null);
    onSubmit(
      google
        ? { provider, name: name.trim(), clientId: clientId.trim(), clientSecret: clientSecret.trim() }
        : { provider, name: name.trim(), clientId: clientId.trim(), tenant: tenant.trim() || undefined },
    );
  };

  return (
    <>
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <Label>{t("backups.cloud.connect.provider")}</Label>
          <div className="flex gap-2">
            {PROVIDERS.map((p) => (
              <Button key={p} type="button" variant={provider === p ? "default" : "outline"} aria-pressed={provider === p} onClick={() => setProvider(p)}>
                {t(`backups.cloud.providers.${p}` as never)}
              </Button>
            ))}
          </div>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="cloud-name">{t("backups.cloud.connect.name")}</Label>
          <Input id="cloud-name" value={name} onChange={(e) => setName(e.target.value)} />
          <p className="text-xs text-muted-foreground">{t("backups.cloud.connect.nameHelp")}</p>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="cloud-client-id">{t("backups.cloud.connect.clientId")}</Label>
          <Input id="cloud-client-id" value={clientId} autoComplete="off" onChange={(e) => setClientId(e.target.value)} />
        </div>
        {google ? (
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="cloud-client-secret">{t("backups.cloud.connect.clientSecret")}</Label>
            <Input id="cloud-client-secret" type="password" autoComplete="off" value={clientSecret} onChange={(e) => setClientSecret(e.target.value)} />
            <p className="text-xs text-muted-foreground">{t("backups.cloud.connect.clientSecretHelp")}</p>
          </div>
        ) : (
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="cloud-tenant">{t("backups.cloud.connect.tenant")}</Label>
            <Input id="cloud-tenant" value={tenant} onChange={(e) => setTenant(e.target.value)} />
            <p className="text-xs text-muted-foreground">{t("backups.cloud.connect.tenantHelp")}</p>
          </div>
        )}
        <p className="text-xs text-muted-foreground">{t("backups.cloud.connect.help")}</p>
        {(local ?? error) && <p className="text-sm text-destructive">{local ?? error}</p>}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onCancel}>{t("backups.cloud.connect.cancel")}</Button>
        <Button onClick={submit} disabled={busy}>{t("backups.cloud.connect.start")}</Button>
      </DialogFooter>
    </>
  );
}

function CodePhase({
  signIn,
  onDone,
  onRestart,
  onClose,
}: {
  signIn: BackupCloudSignIn;
  onDone: (account: BackupCloudAccount) => void;
  onRestart: () => void;
  onClose: () => void;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  // Asking is a side-effecting GET: the server contacts the provider when due.
  const poll = useQuery({
    queryKey: [...CLOUD_ACCOUNTS_KEY, "poll", signIn.id, signIn.expiresAt],
    queryFn: async () => {
      const res = await getBackupCloudAccount({ path: { id: signIn.id } });
      if (res.error) throw res.error;
      return res.data!;
    },
    refetchInterval: (q) => (q.state.data && q.state.data.status !== "pending" ? false : cloudPoll.ms),
    refetchOnWindowFocus: false,
    gcTime: 0,
    staleTime: 0,
    retry: false,
  });
  const account = poll.data;
  const status = account?.status ?? "pending";

  useEffect(() => {
    if (status === "connected") void queryClient.invalidateQueries({ queryKey: CLOUD_ACCOUNTS_KEY });
  }, [status, queryClient]);

  if (account && status === "connected") {
    return (
      <>
        <p className="text-sm">
          {account.accountEmail
            ? t("backups.cloud.connect.connectedAs", { email: account.accountEmail })
            : t("backups.cloud.connect.connected")}
        </p>
        <DialogFooter>
          <Button onClick={() => onDone(account)}>{t("backups.cloud.connect.done")}</Button>
        </DialogFooter>
      </>
    );
  }
  if (account && (status === "expired" || status === "revoked")) {
    return (
      <>
        <p className="text-sm text-destructive">{account.lastError ?? t(`backups.cloud.status.${status}` as never)}</p>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>{t("backups.cloud.connect.close")}</Button>
          <Button onClick={onRestart}>{t("backups.cloud.connect.retry")}</Button>
        </DialogFooter>
      </>
    );
  }
  return (
    <>
      <div className="flex flex-col gap-3">
        <h3 className="text-sm font-medium">{t("backups.cloud.connect.codeHeading")}</h3>
        <p className="text-sm">
          {t("backups.cloud.connect.codeSteps", { uri: signIn.verificationUri })}
        </p>
        <a className="text-sm underline" href={signIn.verificationUri} target="_blank" rel="noreferrer">
          {signIn.verificationUri}
        </a>
        <p className="font-identifier text-2xl tracking-widest">{signIn.userCode}</p>
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" />
          {t("backups.cloud.connect.waiting")}
        </p>
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>{t("backups.cloud.connect.close")}</Button>
      </DialogFooter>
    </>
  );
}
