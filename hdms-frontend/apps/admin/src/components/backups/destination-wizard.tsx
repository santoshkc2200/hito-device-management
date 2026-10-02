import {
  checkBackupLocation,
  createBackupDestination,
  createBackupLocationFolder,
  listBackupLocations,
  testBackupDestination,
  updateBackupDestination,
  type BackupLocationCheckItem,
  type BackupLocationRoot,
} from "@hdms/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, ArrowUp, CheckCircle2, Cloud, Folder, HardDrive, Info, Loader2, MinusCircle, XCircle } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useT } from "@/i18n";
import { formatBytes } from "./format";
import { realLocation } from "./real-location";
import { useBackupConfig } from "./use-backup-config";
import { useBackupRequest } from "./use-backup-request";

type Step = "where" | "folder" | "check" | "details" | "saving";
const STEPS: Step[] = ["where", "folder", "check", "details", "saving"];

type Problem = { type?: string; status?: number; detail?: string };
const problemIs = (err: unknown, type: string) => (err as Problem | null)?.type?.endsWith(`/${type}`) ?? false;

function useLocations(path: string) {
  return useQuery({
    queryKey: ["backup", "locations", path],
    queryFn: async () => {
      const res = await listBackupLocations({ query: path ? { path } : {} });
      if (res.error) throw res.error;
      return res.data;
    },
  });
}

export function DestinationWizard({ onClose }: { onClose: () => void }) {
  const t = useT();
  const [step, setStep] = useState<Step>("where");
  const [root, setRoot] = useState<BackupLocationRoot | null>(null);
  const [path, setPath] = useState("");
  const [saved, setSaved] = useState<{ id: string; name: string; retentionVersions: number; requestId: string } | null>(null);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("backups.wizard.title")}</DialogTitle>
          <p className="text-xs text-muted-foreground">
            {t("backups.wizard.stepOf", { current: String(STEPS.indexOf(step) + 1), total: String(STEPS.length) })}
          </p>
        </DialogHeader>
        {step === "where" && (
          <WhereStep
            onCancel={onClose}
            onPick={(r) => {
              setRoot(r);
              setPath(r.path);
              setStep("folder");
            }}
          />
        )}
        {step === "folder" && root && (
          <FolderStep path={path} onNavigate={setPath} onBack={() => setStep("where")} onUse={() => setStep("check")} />
        )}
        {step === "check" && <CheckStep path={path} onBack={() => setStep("folder")} onNext={() => setStep("details")} />}
        {step === "details" && (
          <DetailsStep
            path={path}
            onBack={() => setStep("check")}
            onSaved={(s) => {
              setSaved(s);
              setStep("saving");
            }}
          />
        )}
        {step === "saving" && saved && <SavingStep saved={saved} onClose={onClose} />}
      </DialogContent>
    </Dialog>
  );
}

function WorkerOrLoadError({ error }: { error: unknown }) {
  const t = useT();
  return (
    <p className="text-sm text-destructive">
      {problemIs(error, "worker-unavailable") ? t("backups.wizard.workerDown") : t("backups.wizard.loadFailed")}
    </p>
  );
}

function WhereStep({ onPick, onCancel }: { onPick: (root: BackupLocationRoot) => void; onCancel: () => void }) {
  const t = useT();
  const query = useLocations("");
  const roots = query.data?.roots ?? [];

  return (
    <>
      <div className="flex flex-col gap-3">
        <h3 className="text-sm font-medium">{t("backups.wizard.where.heading")}</h3>
        <p className="text-xs text-muted-foreground">{t("backups.wizard.where.drive")}</p>
        {query.isPending && <Loader2 className="size-4 animate-spin" />}
        {query.isError && <WorkerOrLoadError error={query.error} />}
        {query.isSuccess && roots.length === 0 && <p className="text-sm text-muted-foreground">{t("backups.wizard.where.noRoots")}</p>}
        {roots.map((r) => (
          <div key={r.path} className="flex flex-col gap-1">
            <Button
              variant="outline"
              className="h-auto justify-start gap-3 py-3 text-left"
              disabled={!r.connected}
              onClick={() => onPick(r)}
            >
              <HardDrive className="size-5 shrink-0" />
              <span className="flex flex-1 flex-col">
                <span className="whitespace-normal font-medium">{r.name}</span>
                <span className="font-identifier text-xs text-muted-foreground">{r.hostPath ?? r.path}</span>
              </span>
              <Badge variant={r.connected ? "default" : "outline"}>
                {r.connected ? t("backups.wizard.where.connected") : t("backups.wizard.where.notConnected")}
              </Badge>
            </Button>
            {!r.connected && <p className="text-xs text-amber-700 dark:text-amber-400">{t("backups.wizard.where.notConnectedHelp")}</p>}
          </div>
        ))}
        <Button variant="outline" className="h-auto justify-start gap-3 py-3" disabled>
          <Cloud className="size-5 shrink-0" />
          <span className="flex-1 text-left">{t("backups.wizard.where.cloud")}</span>
          <Badge variant="outline">{t("backups.wizard.where.comingSoon")}</Badge>
        </Button>
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onCancel}>{t("backups.wizard.cancel")}</Button>
      </DialogFooter>
    </>
  );
}

function FolderStep({
  path,
  onNavigate,
  onBack,
  onUse,
}: {
  path: string;
  onNavigate: (path: string) => void;
  onBack: () => void;
  onUse: () => void;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  const { data: config } = useBackupConfig();
  const query = useLocations(path);
  const [newName, setNewName] = useState("hdms-backups"); // i18n-allow-literal: suggested folder name, not prose
  const [error, setError] = useState<string | null>(null);

  const mkdir = useMutation({
    mutationFn: async () => {
      const res = await createBackupLocationFolder({ body: { parent: path, name: newName.trim() } });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (folder) => {
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["backup", "locations"] });
      if (folder) onNavigate(folder.path);
    },
    onError: (err: unknown) => {
      if (problemIs(err, "folder-exists")) setError(t("backups.wizard.folder.exists"));
      else if (problemIs(err, "folder-not-writable")) setError(t("backups.wizard.folder.notWritable"));
      else if (problemIs(err, "worker-unavailable")) setError(t("backups.wizard.workerDown"));
      else setError(t("backups.wizard.folder.invalidName"));
    },
  });

  const parent = query.data?.parent;
  const folders = query.data?.folders ?? [];

  return (
    <>
      <div className="flex flex-col gap-3">
        <h3 className="text-sm font-medium">{t("backups.wizard.folder.heading")}</h3>
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">{t("backups.wizard.folder.current")}</span>
          <span className="font-identifier text-xs break-all">{realLocation(path, config)}</span>
        </div>
        {query.isError && <WorkerOrLoadError error={query.error} />}
        <div className="flex max-h-60 flex-col gap-1 overflow-y-auto rounded-md border p-1">
          {parent && (
            <Button variant="ghost" size="sm" className="justify-start" onClick={() => onNavigate(parent)}>
              <ArrowUp className="size-4" data-icon="inline-start" />
              {t("backups.wizard.folder.up")}
            </Button>
          )}
          {query.isSuccess && folders.length === 0 && (
            <p className="p-2 text-sm text-muted-foreground">{t("backups.wizard.folder.empty")}</p>
          )}
          {folders.map((f) => (
            <Button key={f.path} variant="ghost" size="sm" className="justify-start" onClick={() => onNavigate(f.path)}>
              <Folder className="size-4" data-icon="inline-start" />
              <span className="flex-1 text-left">{f.name}</span>
              {f.hasBackup && <Badge variant="outline">{t("backups.wizard.folder.hasBackup")}</Badge>}
            </Button>
          ))}
        </div>
        <div className="flex items-end gap-2">
          <div className="flex flex-1 flex-col gap-1.5">
            <Label htmlFor="wizard-new-folder">{t("backups.wizard.folder.newFolderName")}</Label>
            <Input id="wizard-new-folder" value={newName} onChange={(e) => setNewName(e.target.value)} />
          </div>
          <Button variant="outline" onClick={() => mkdir.mutate()} disabled={mkdir.isPending || !newName.trim()}>
            {t("backups.wizard.folder.create")}
          </Button>
        </div>
        {error && <p className="text-sm text-destructive">{error}</p>}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onBack}>{t("backups.wizard.back")}</Button>
        <Button onClick={onUse} disabled={!query.isSuccess}>{t("backups.wizard.folder.useThis")}</Button>
      </DialogFooter>
    </>
  );
}

function CheckLine({ item, free, needed }: { item: BackupLocationCheckItem; free: number; needed: number }) {
  const t = useT();
  const icon = {
    pass: <CheckCircle2 className="size-4 text-emerald-600" />,
    fail: <XCircle className="size-4 text-destructive" />,
    warn: <AlertTriangle className="size-4 text-amber-600" />,
    info: <Info className="size-4 text-sky-600" />,
    skipped: <MinusCircle className="size-4 text-muted-foreground" />,
  }[item.status];
  const label = t(`backups.wizard.check.pass.${item.name}` as never);
  return (
    <li className="flex gap-2">
      <span className="mt-0.5 shrink-0">{icon}</span>
      <span className="flex flex-col">
        {item.status === "pass" && <span className="text-sm">{label}</span>}
        {item.status === "skipped" && (
          <span className="flex gap-2 text-sm text-muted-foreground">
            <span>{label}</span>
            <span>{t("backups.wizard.check.skipped")}</span>
          </span>
        )}
        {item.code && (
          <>
            <span className="text-sm">
              {t(`backups.wizard.check.codes.${item.code}` as never, { free: formatBytes(free), needed: formatBytes(needed) })}
            </span>
            <span className="text-xs text-muted-foreground">{t(`backups.wizard.check.help.${item.code}` as never)}</span>
          </>
        )}
      </span>
    </li>
  );
}

function CheckStep({ path, onBack, onNext }: { path: string; onBack: () => void; onNext: () => void }) {
  const t = useT();
  const { data: config } = useBackupConfig();
  const query = useQuery({
    queryKey: ["backup", "locations", "check", path],
    gcTime: 0,
    queryFn: async () => {
      const res = await checkBackupLocation({ body: { path } });
      if (res.error) throw res.error;
      return res.data;
    },
  });
  const result = query.data;

  return (
    <>
      <div className="flex flex-col gap-3">
        <h3 className="text-sm font-medium">{t("backups.wizard.check.heading")}</h3>
        <span className="font-identifier text-xs break-all">{realLocation(path, config)}</span>
        {query.isFetching && (
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" />
            {t("backups.wizard.check.running")}
          </p>
        )}
        {query.isError && <WorkerOrLoadError error={query.error} />}
        {result && (
          <>
            <ul className="flex flex-col gap-2">
              {result.checks.map((item) => (
                <CheckLine key={item.name} item={item} free={result.freeBytes} needed={result.neededBytes} />
              ))}
            </ul>
            <p className={result.ok ? "text-sm text-emerald-700 dark:text-emerald-400" : "text-sm text-destructive"}>
              {result.ok ? t("backups.wizard.check.ok") : t("backups.wizard.check.failed")}
            </p>
          </>
        )}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onBack}>{t("backups.wizard.back")}</Button>
        <Button variant="outline" onClick={() => void query.refetch()} disabled={query.isFetching}>
          {t("backups.wizard.check.again")}
        </Button>
        <Button onClick={onNext} disabled={!result?.ok || query.isFetching}>{t("backups.wizard.next")}</Button>
      </DialogFooter>
    </>
  );
}

function DetailsStep({
  path,
  onBack,
  onSaved,
}: {
  path: string;
  onBack: () => void;
  onSaved: (s: { id: string; name: string; retentionVersions: number; requestId: string }) => void;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  const [name, setName] = useState(() => t("backups.wizard.details.defaultName"));
  const [retention, setRetention] = useState("3");
  const [error, setError] = useState<string | null>(null);
  const retentionNumber = Number(retention);

  const save = useMutation({
    mutationFn: async () => {
      const created = await createBackupDestination({
        body: { name: name.trim(), target: path, retentionVersions: retentionNumber, enabled: true },
      });
      if (created.error) throw created.error;
      const destination = created.data!;
      const queued = await testBackupDestination({ path: { id: destination.id } });
      if (queued.error) throw queued.error;
      return { id: destination.id, name: destination.name, retentionVersions: destination.retentionVersions, requestId: queued.data!.id };
    },
    onSuccess: (s) => {
      void queryClient.invalidateQueries({ queryKey: ["backup", "destinations"] });
      onSaved(s);
    },
    onError: (err: unknown) => {
      if (problemIs(err, "location-check-failed")) setError(t("backups.wizard.details.checkChanged"));
      else if (problemIs(err, "worker-unavailable")) setError(t("backups.wizard.workerDown"));
      // Raw server text never reaches the user; the name and retention
      // rules are already checked before the request is sent.
      else setError(t("backups.errors.save"));
    },
  });

  const submit = () => {
    if (!name.trim()) return setError(t("backups.validation.nameRequired"));
    if (!Number.isInteger(retentionNumber) || retentionNumber < 1 || retentionNumber > 100) {
      return setError(t("backups.validation.retentionRange"));
    }
    setError(null);
    save.mutate();
  };

  return (
    <>
      <div className="flex flex-col gap-4">
        <h3 className="text-sm font-medium">{t("backups.wizard.details.heading")}</h3>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="wizard-name">{t("backups.wizard.details.name")}</Label>
          <Input id="wizard-name" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="wizard-retention">{t("backups.wizard.details.retention")}</Label>
          <Input
            id="wizard-retention"
            type="number"
            min={1}
            max={100}
            value={retention}
            onChange={(e) => setRetention(e.target.value)}
          />
          <p className="text-xs text-muted-foreground">
            {t("backups.wizard.details.retentionHelp", { count: String(retentionNumber || 0) })}
          </p>
          {retentionNumber >= 1 && retentionNumber < 3 && (
            <p className="text-xs text-amber-700 dark:text-amber-400">{t("backups.destinations.form.retentionWarning")}</p>
          )}
        </div>
        {error && <p className="text-sm text-destructive">{error}</p>}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onBack}>{t("backups.wizard.back")}</Button>
        <Button onClick={submit} disabled={save.isPending}>{t("backups.wizard.details.save")}</Button>
      </DialogFooter>
    </>
  );
}

function SavingStep({
  saved,
  onClose,
}: {
  saved: { id: string; name: string; retentionVersions: number; requestId: string };
  onClose: () => void;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  const { request } = useBackupRequest(saved.requestId);
  const failed = request?.status === "done" && request.outcome !== "success";
  const disabledOnce = useRef(false);

  // A destination that cannot be prepared is kept but switched off, so the
  // next scheduled backup does not report it as failing every night.
  useEffect(() => {
    if (!failed || disabledOnce.current) return;
    disabledOnce.current = true;
    void updateBackupDestination({
      path: { id: saved.id },
      body: { name: saved.name, enabled: false, retentionVersions: saved.retentionVersions },
    }).then(() => queryClient.invalidateQueries({ queryKey: ["backup", "destinations"] }));
  }, [failed, saved, queryClient]);

  let message = t("backups.wizard.saving.queued");
  if (request?.status === "running") message = t("backups.wizard.saving.running");
  if (request?.status === "done") message = failed ? t("backups.wizard.saving.failed") : t("backups.wizard.saving.ready");

  return (
    <>
      <p className={failed ? "text-sm text-destructive" : "text-sm"}>
        {request?.status !== "done" && <Loader2 className="mr-2 inline size-4 animate-spin" />}
        {message}
      </p>
      <DialogFooter>
        <Button onClick={onClose}>{t("backups.wizard.close")}</Button>
      </DialogFooter>
    </>
  );
}
