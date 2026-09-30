import {
  getBackupConfig,
  runBackupNow,
  updateBackupSchedule,
  type BackupConfig,
  type BackupSchedule,
} from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, DatabaseBackup, Play, Save } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { LoadingState } from "@/components/states";
import { useT } from "@/i18n";
import { formatBytes, formatDateTime, WORKER_STALE_MS } from "./format";
import { useBackupRequest } from "./use-backup-request";

const TIME_PATTERN = /^([01][0-9]|2[0-3]):[0-5][0-9]$/;

export function outcomeTone(outcome?: string): "default" | "secondary" | "destructive" | "outline" {
  if (outcome === "success") return "default";
  if (outcome === "degraded") return "secondary";
  if (outcome === "failure") return "destructive";
  return "outline";
}

export function OverviewTab() {
  const t = useT();
  const { locale } = useLocale();
  const configQuery = useQuery({
    queryKey: ["backup", "config"],
    queryFn: async () => {
      const res = await getBackupConfig();
      if (res.error) throw res.error;
      return res.data as BackupConfig;
    },
    refetchInterval: 30_000,
  });
  const [requestId, setRequestId] = useState<string | null>(null);
  const { request, isRunning } = useBackupRequest(requestId);

  const runNow = useMutation({
    mutationFn: async () => {
      const res = await runBackupNow();
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (req) => {
      if (req) setRequestId(req.id);
      toast.info(t("backups.overview.queued"));
    },
    onError: () => toast.error(t("backups.errors.save")),
  });

  if (configQuery.isPending) return <LoadingState />;
  if (configQuery.isError || !configQuery.data) {
    return <p className="text-sm text-destructive">{t("backups.errors.load")}</p>;
  }
  const cfg = configQuery.data;
  const workerSeen = cfg.workerSeenAt ? new Date(cfg.workerSeenAt).getTime() : 0;
  const workerDown = Date.now() - workerSeen > WORKER_STALE_MS;

  return (
    <div className="flex flex-col gap-4">
      {workerDown && (
        <div role="alert" className="flex items-start gap-3 rounded-lg border border-destructive/40 bg-destructive/10 p-4">
          <AlertTriangle className="mt-0.5 size-5 shrink-0 text-destructive" />
          <div>
            <p className="text-sm font-semibold">{t("backups.overview.workerDown")}</p>
            <p className="text-xs text-muted-foreground">{t("backups.overview.workerDownHelp")}</p>
          </div>
        </div>
      )}

      <Card>
        <CardContent className="flex flex-wrap items-center justify-between gap-4 pt-6">
          <div className="flex flex-col gap-1">
            <span className="text-xs text-muted-foreground">{t("backups.overview.lastBackup")}</span>
            {cfg.lastRun ? (
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">{formatDateTime(cfg.lastRun.startedAt, locale)}</span>
                <Badge variant={outcomeTone(cfg.lastRun.outcome)}>{t(`backups.outcome.${cfg.lastRun.outcome}` as never)}</Badge>
              </div>
            ) : (
              <span className="text-sm">{t("backups.overview.never")}</span>
            )}
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-xs text-muted-foreground">{t("backups.overview.nextBackup")}</span>
            <span className="text-sm font-medium">
              {cfg.nextRunAt ? formatDateTime(cfg.nextRunAt, locale) : t("backups.overview.scheduleOff")}
            </span>
          </div>
          <div className="flex flex-col items-end gap-1">
            <Button onClick={() => runNow.mutate()} disabled={runNow.isPending || isRunning}>
              <Play className="size-4" data-icon="inline-start" />
              {t("backups.overview.backUpNow")}
            </Button>
            {request && (
              <span className="text-xs text-muted-foreground">
                {request.status === "done"
                  ? t("backups.overview.finished", { outcome: t(`backups.outcome.${request.outcome ?? "failure"}` as never) })
                  : t(`backups.status.${request.status}` as never)}
              </span>
            )}
          </div>
        </CardContent>
      </Card>

      <ScheduleCard schedule={cfg.schedule} />

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <DatabaseBackup className="size-4" />
            {t("backups.overview.localTitle")}
          </CardTitle>
          <CardDescription>{t("backups.overview.localRetention")}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-2 text-sm sm:grid-cols-3">
          <div>
            <div className="text-xs text-muted-foreground">{t("backups.overview.localPath")}</div>
            <div className="font-identifier break-all">{cfg.local.path}</div>
          </div>
          <div>
            <div className="text-xs text-muted-foreground">{t("backups.overview.localCount")}</div>
            <div>{cfg.local.snapshotCount}</div>
          </div>
          <div>
            <div className="text-xs text-muted-foreground">{t("backups.overview.localSize")}</div>
            <div>{cfg.local.latestSizeBytes != null ? formatBytes(cfg.local.latestSizeBytes) : "—"}</div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

function ScheduleCard({ schedule }: { schedule: BackupSchedule }) {
  const t = useT();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<BackupSchedule>(schedule);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => setDraft(schedule), [schedule]);

  const save = useMutation({
    mutationFn: async (body: BackupSchedule) => {
      const res = await updateBackupSchedule({ body });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (cfg) => {
      queryClient.setQueryData(["backup", "config"], cfg);
      toast.success(t("backups.overview.saved"));
    },
    onError: () => toast.error(t("backups.errors.save")),
  });

  const submit = () => {
    if (draft.mode === "interval" && (draft.intervalMinutes < 15 || draft.intervalMinutes > 720)) {
      setError(t("backups.validation.intervalRange"));
      return;
    }
    if (draft.mode !== "interval" && !TIME_PATTERN.test(draft.timeLocal)) {
      setError(t("backups.validation.timeFormat"));
      return;
    }
    setError(null);
    save.mutate(draft);
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("backups.overview.scheduleTitle")}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex items-center gap-2">
          <Checkbox
            id="backup-enabled"
            checked={draft.enabled}
            onCheckedChange={(v) => setDraft({ ...draft, enabled: v === true })}
          />
          <Label htmlFor="backup-enabled">{t("backups.overview.enabled")}</Label>
        </div>
        <div className="grid gap-4 sm:grid-cols-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="backup-mode">{t("backups.overview.mode")}</Label>
            <Select value={draft.mode} onValueChange={(v) => setDraft({ ...draft, mode: v as BackupSchedule["mode"] })}>
              <SelectTrigger id="backup-mode"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="interval">{t("backups.overview.modes.interval")}</SelectItem>
                <SelectItem value="daily">{t("backups.overview.modes.daily")}</SelectItem>
                <SelectItem value="weekly">{t("backups.overview.modes.weekly")}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          {draft.mode === "interval" ? (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="backup-interval">{t("backups.overview.intervalMinutes")}</Label>
              <Input
                id="backup-interval"
                type="number"
                min={15}
                max={720}
                value={draft.intervalMinutes}
                onChange={(e) => setDraft({ ...draft, intervalMinutes: Number(e.target.value) })}
              />
            </div>
          ) : (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="backup-time">{t("backups.overview.timeLocal")}</Label>
              <Input
                id="backup-time"
                value={draft.timeLocal}
                placeholder="02:00" // i18n-allow-literal: time format example, not translatable prose
                onChange={(e) => setDraft({ ...draft, timeLocal: e.target.value })}
              />
            </div>
          )}
          {draft.mode === "weekly" && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="backup-weekday">{t("backups.overview.weekday")}</Label>
              <Select value={String(draft.weekday)} onValueChange={(v) => setDraft({ ...draft, weekday: Number(v) })}>
                <SelectTrigger id="backup-weekday"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {(["0", "1", "2", "3", "4", "5", "6"] as const).map((d) => (
                    <SelectItem key={d} value={d}>{t(`backups.overview.weekdays.${d}` as never)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
        </div>
        {error && <p className="text-sm text-destructive">{error}</p>}
        <div>
          <Button onClick={submit} disabled={save.isPending}>
            <Save className="size-4" data-icon="inline-start" />
            {t("backups.overview.save")}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
