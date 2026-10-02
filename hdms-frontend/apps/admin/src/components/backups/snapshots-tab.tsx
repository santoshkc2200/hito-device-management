import {
  listBackupDestinations,
  listBackupSnapshots,
  verifyBackups,
  type BackupSnapshot,
} from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { useMutation, useQuery } from "@tanstack/react-query";
import { createColumnHelper } from "@tanstack/react-table";
import { History, ShieldCheck } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { DataTable, DataTableColumnHeader, useDataTableColumns } from "@/components/data-table";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useT } from "@/i18n";
import { formatBytes, formatDateTime } from "./format";
import { RestoreConfirmDialog } from "./restore-confirm-dialog";
import { useBackupRequest } from "./use-backup-request";
import { useRestore } from "./use-restore";

const columnHelper = createColumnHelper<BackupSnapshot>();

export function SnapshotsTab() {
  const t = useT();
  const { locale } = useLocale();
  const [repo, setRepo] = useState("local");
  const [verifyId, setVerifyId] = useState<string | null>(null);
  const { request: verifyRequest, isRunning } = useBackupRequest(verifyId);
  const { running: restoreRunning } = useRestore();
  const [restoreTarget, setRestoreTarget] = useState<BackupSnapshot | null>(null);

  const destinationsQuery = useQuery({
    queryKey: ["backup", "destinations"],
    queryFn: async () => {
      const res = await listBackupDestinations();
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
  });
  const snapshotsQuery = useQuery({
    queryKey: ["backup", "snapshots", repo],
    queryFn: async () => {
      const res = await listBackupSnapshots({ query: { repo } });
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
  });

  useEffect(() => {
    if (verifyRequest?.status === "done") {
      toast.info(t("backups.snapshots.verifyDone", { outcome: t(`backups.outcome.${verifyRequest.outcome ?? "failure"}` as never) }));
    }
  }, [verifyRequest?.status, verifyRequest?.outcome, t]);

  const verify = useMutation({
    mutationFn: async () => {
      const res = await verifyBackups({ body: repo === "local" ? {} : { destinationId: repo } });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (req) => {
      if (req) setVerifyId(req.id);
      toast.info(t("backups.snapshots.verifyQueued"));
    },
    onError: () => toast.error(t("backups.errors.save")),
  });

  const data = useMemo(() => snapshotsQuery.data ?? [], [snapshotsQuery.data]);
  const columns = useDataTableColumns<BackupSnapshot>(
    () => [
      columnHelper.accessor("takenAt", {
        id: "takenAt",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("backups.snapshots.columns.takenAt")} />,
        cell: ({ row }) => formatDateTime(row.original.takenAt, locale),
      }),
      columnHelper.accessor("sizeBytes", {
        id: "sizeBytes",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("backups.snapshots.columns.size")} />,
        cell: ({ row }) => formatBytes(row.original.sizeBytes),
      }),
      columnHelper.accessor("verifiedAt", {
        id: "verifiedAt",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("backups.snapshots.columns.verified")} />,
        cell: ({ row }) =>
          row.original.verifiedAt ? formatDateTime(row.original.verifiedAt, locale) : t("backups.snapshots.notVerified"),
      }),
      columnHelper.display({
        id: "actions",
        header: () => <span className="sr-only">{t("backups.restore.actions")}</span>,
        cell: ({ row }) => (
          <Button size="sm" variant="outline" disabled={restoreRunning} onClick={() => setRestoreTarget(row.original)}>
            <History className="size-4" data-icon="inline-start" />
            {t("backups.restore.button")}
          </Button>
        ),
      }),
    ],
    [t, locale, restoreRunning]
  );

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="snapshot-repo">{t("backups.snapshots.repository")}</Label>
          <Select value={repo} onValueChange={setRepo}>
            <SelectTrigger id="snapshot-repo" className="w-64"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="local">{t("backups.snapshots.local")}</SelectItem>
              {(destinationsQuery.data ?? []).map((d) => (
                <SelectItem key={d.id} value={d.id}>{d.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button onClick={() => verify.mutate()} disabled={verify.isPending || isRunning}>
          <ShieldCheck className="size-4" data-icon="inline-start" />
          {t("backups.snapshots.verifyNow")}
        </Button>
      </div>
      <DataTable
        tableId="backup-snapshots-table"
        columns={columns}
        data={data}
        isLoading={snapshotsQuery.isPending}
        isError={snapshotsQuery.isError}
        error={snapshotsQuery.error}
        onRetry={() => snapshotsQuery.refetch()}
        emptyTitle={t("backups.snapshots.empty")}
        emptyExplanation={t("backups.overview.localRetention")}
      />
      {restoreTarget && (
        <RestoreConfirmDialog mode="restore" repo={repo} snapshot={restoreTarget} onClose={() => setRestoreTarget(null)} />
      )}
    </div>
  );
}
