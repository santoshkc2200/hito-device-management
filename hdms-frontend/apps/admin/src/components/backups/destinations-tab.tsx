import {
  deleteBackupDestination,
  listBackupDestinations,
  testBackupDestination,
  updateBackupDestination,
  type BackupDestination,
} from "@hdms/api-client";
import { useLocale } from "@hdms/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createColumnHelper } from "@tanstack/react-table";
import { FlaskConical, PencilLine, Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { DataTable, DataTableColumnHeader, useDataTableColumns } from "@/components/data-table";
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
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useT } from "@/i18n";
import { CloudAccountsSection } from "./cloud-accounts-section";
import { destinationLocation } from "./cloud-format";
import { DestinationWizard } from "./destination-wizard";
import { formatDateTime } from "./format";
import { useBackupConfig } from "./use-backup-config";
import { useBackupRequest } from "./use-backup-request";

const columnHelper = createColumnHelper<BackupDestination>();

export function DestinationsTab() {
  const t = useT();
  const { locale } = useLocale();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<BackupDestination | null>(null);
  const [adding, setAdding] = useState(false);
  const [deleting, setDeleting] = useState<BackupDestination | null>(null);
  const [testId, setTestId] = useState<string | null>(null);
  const { request: testRequest } = useBackupRequest(testId);

  const destinationsQuery = useQuery({
    queryKey: ["backup", "destinations"],
    queryFn: async () => {
      const res = await listBackupDestinations();
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
  });

  useEffect(() => {
    if (testRequest?.status !== "done") return;
    const name = (testRequest.detail?.name as string | undefined) ?? "";
    if (testRequest.outcome === "success") toast.success(t("backups.destinations.testPassed", { name }));
    else toast.error(t("backups.destinations.testFailed", { name }));
  }, [testRequest?.status, testRequest?.outcome, testRequest?.detail, t]);

  const test = useMutation({
    mutationFn: async (id: string) => {
      const res = await testBackupDestination({ path: { id } });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (req) => {
      if (req) setTestId(req.id);
      toast.info(t("backups.destinations.testQueued"));
    },
    onError: () => toast.error(t("backups.errors.save")),
  });

  const remove = useMutation({
    mutationFn: async (id: string) => {
      const res = await deleteBackupDestination({ path: { id } });
      if (res.error) throw res.error;
    },
    onSuccess: () => {
      setDeleting(null);
      toast.success(t("backups.destinations.deleted"));
      void queryClient.invalidateQueries({ queryKey: ["backup"] });
    },
    onError: () => toast.error(t("backups.errors.save")),
  });

  const { data: config } = useBackupConfig();
  const data = useMemo(() => destinationsQuery.data ?? [], [destinationsQuery.data]);

  const columns = useDataTableColumns<BackupDestination>(
    () => [
      columnHelper.accessor("name", {
        id: "name",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("backups.destinations.columns.name")} />,
        cell: ({ row }) => <span className="font-medium">{row.original.name}</span>,
      }),
      columnHelper.accessor("target", {
        id: "target",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("backups.destinations.columns.target")} />,
        cell: ({ row }) => (
          <span className="font-identifier text-xs">
            {destinationLocation(row.original, config, (p) => t(`backups.cloud.providers.${p}` as never))}
          </span>
        ),
      }),
      columnHelper.accessor("retentionVersions", {
        id: "retentionVersions",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("backups.destinations.columns.retention")} />,
      }),
      columnHelper.accessor("lastOkAt", {
        id: "lastOkAt",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("backups.destinations.columns.lastOk")} />,
        cell: ({ row }) => (
          <div className="flex flex-col gap-0.5">
            <span className="text-xs">
              {row.original.lastOkAt ? formatDateTime(row.original.lastOkAt, locale) : t("backups.destinations.neverOk")}
            </span>
            {row.original.lastError && (
              <span className="max-w-xs break-words text-xs text-destructive">{row.original.lastError}</span>
            )}
          </div>
        ),
      }),
      columnHelper.accessor("enabled", {
        id: "enabled",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("backups.destinations.columns.enabled")} />,
        cell: ({ row }) => (
          <Badge variant={row.original.enabled ? "default" : "outline"}>
            {row.original.enabled ? t("backups.destinations.enabledOn") : t("backups.destinations.enabledOff")}
          </Badge>
        ),
      }),
      columnHelper.display({
        id: "actions",
        cell: ({ row }) => (
          <div className="flex justify-end gap-1">
            <Button variant="ghost" size="sm" onClick={() => test.mutate(row.original.id)}>
              <FlaskConical className="size-4" data-icon="inline-start" />
              {t("backups.destinations.test")}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setEditing(row.original)}>
              <PencilLine className="size-4" data-icon="inline-start" />
              {t("backups.destinations.edit")}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setDeleting(row.original)}>
              <Trash2 className="size-4" data-icon="inline-start" />
              {t("backups.destinations.delete")}
            </Button>
          </div>
        ),
      }),
    ],
    [t, locale, test.mutate, config]
  );

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-base font-semibold">{t("backups.destinations.title")}</h2>
          <p className="text-sm text-muted-foreground">{t("backups.destinations.description")}</p>
        </div>
        <Button onClick={() => setAdding(true)}>
          <Plus className="size-4" data-icon="inline-start" />
          {t("backups.destinations.add")}
        </Button>
      </div>

      <DataTable
        tableId="backup-destinations-table"
        columns={columns}
        data={data}
        isLoading={destinationsQuery.isPending}
        isError={destinationsQuery.isError}
        error={destinationsQuery.error}
        onRetry={() => destinationsQuery.refetch()}
        emptyTitle={t("backups.destinations.empty")}
        emptyExplanation={t("backups.destinations.description")}
      />

      <CloudAccountsSection />

      {adding && <DestinationWizard onClose={() => setAdding(false)} />}
      {editing && <EditDestinationDialog destination={editing} onClose={() => setEditing(null)} />}

      <AlertDialog open={deleting !== null} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("backups.destinations.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("backups.destinations.deleteBody", { name: deleting?.name ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("backups.destinations.form.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={() => deleting && remove.mutate(deleting.id)}>
              {t("backups.destinations.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function EditDestinationDialog({ destination, onClose }: { destination: BackupDestination; onClose: () => void }) {
  const t = useT();
  const queryClient = useQueryClient();
  const { data: config } = useBackupConfig();
  const [name, setName] = useState(destination.name);
  const [retention, setRetention] = useState(String(destination.retentionVersions));
  const [enabled, setEnabled] = useState(destination.enabled);
  const [error, setError] = useState<string | null>(null);

  const save = useMutation({
    mutationFn: async () => {
      const res = await updateBackupDestination({
        path: { id: destination.id },
        body: { name: name.trim(), enabled, retentionVersions: Number(retention) },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["backup"] });
      onClose();
    },
    onError: (err: unknown) => {
      const detail = (err as { detail?: string })?.detail;
      setError(detail ?? t("backups.errors.save"));
    },
  });

  const retentionNumber = Number(retention);
  const submit = () => {
    if (!name.trim()) return setError(t("backups.validation.nameRequired"));
    if (!Number.isInteger(retentionNumber) || retentionNumber < 1 || retentionNumber > 100) {
      return setError(t("backups.validation.retentionRange"));
    }
    setError(null);
    save.mutate();
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("backups.destinations.form.editTitle")}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="dest-name">{t("backups.destinations.form.name")}</Label>
            <Input id="dest-name" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="dest-target">{t("backups.destinations.form.target")}</Label>
            <Input id="dest-target" value={destinationLocation(destination, config, (p) => t(`backups.cloud.providers.${p}` as never))} disabled />
            <p className="text-xs text-muted-foreground">{t("backups.destinations.form.targetFixed")}</p>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="dest-retention">{t("backups.destinations.form.retention")}</Label>
            <Input
              id="dest-retention"
              type="number"
              min={1}
              max={100}
              value={retention}
              onChange={(e) => setRetention(e.target.value)}
            />
            {retentionNumber >= 1 && retentionNumber < 3 && (
              <p className="text-xs text-amber-700 dark:text-amber-400">{t("backups.destinations.form.retentionWarning")}</p>
            )}
          </div>
          <div className="flex items-center gap-2">
            <Checkbox id="dest-enabled" checked={enabled} onCheckedChange={(v) => setEnabled(v === true)} />
            <Label htmlFor="dest-enabled">{t("backups.destinations.form.enabled")}</Label>
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>{t("backups.destinations.form.cancel")}</Button>
          <Button onClick={submit} disabled={save.isPending}>{t("backups.destinations.form.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
