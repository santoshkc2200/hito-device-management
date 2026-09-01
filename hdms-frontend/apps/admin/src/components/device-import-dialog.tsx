import {
  type Device,
  type ImportPreview,
  type ImportResult,
  commitDeviceImport,
  getDevice,
  previewDeviceImport,
} from "@hdms/api-client";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  Printer,
  Upload,
} from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useT } from "@/i18n";

interface DeviceImportDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onPrintLabels?: (devices: Device[]) => void;
}

export function DeviceImportDialog({
  open,
  onOpenChange,
  onPrintLabels,
}: DeviceImportDialogProps) {
  const t = useT();
  const queryClient = useQueryClient();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const [step, setStep] = useState<"upload" | "preview" | "done">("upload");
  const [fileName, setFileName] = useState("");
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [importResult, setImportResult] = useState<ImportResult | null>(null);
  const [createdDevices, setCreatedDevices] = useState<Device[]>([]);

  const reset = () => {
    setStep("upload");
    setFileName("");
    setPreview(null);
    setImportResult(null);
    setCreatedDevices([]);
  };

  const handleClose = (openState: boolean) => {
    if (!openState) {
      reset();
      onOpenChange(false);
    } else {
      onOpenChange(true);
    }
  };

  const previewMutation = useMutation({
    mutationFn: async (csvContent: string) => {
      const resp = await previewDeviceImport({
        body: csvContent,
      });
      if (resp.error) {
        throw new Error(
          (resp.error as { detail?: string; title?: string }).detail ||
            (resp.error as { title?: string }).title ||
            t("deviceImportDialog.failedToParseCsv"),
        );
      }
      return resp.data as ImportPreview;
    },
    onSuccess: (data) => {
      setPreview(data);
      setStep("preview");
    },
    onError: (err: Error) => {
      toast.error(err.message || t("deviceImportDialog.failedToValidateCsv"));
    },
  });

  const commitMutation = useMutation({
    mutationFn: async (previewId: string) => {
      const resp = await commitDeviceImport({
        body: { previewId },
      });
      if (resp.error) {
        throw new Error(
          (resp.error as { detail?: string; title?: string }).detail ||
            (resp.error as { title?: string }).title ||
            t("deviceImportDialog.failedToCommit"),
        );
      }
      return resp.data as ImportResult;
    },
    onSuccess: async (data) => {
      setImportResult(data);
      setStep("done");
      await queryClient.invalidateQueries({ queryKey: ["devices"] });
      toast.success(
        t("deviceImportDialog.importCompletedToast", {
          created: data.createdCount,
          updated: data.updatedCount,
        }),
      );

      // Fetch newly created devices for optional label printing
      if (data.createdSubjectIds && data.createdSubjectIds.length > 0) {
        try {
          const loaded: Device[] = [];
          for (const devId of data.createdSubjectIds) {
            const devResp = await getDevice({ path: { id: devId } });
            if (devResp.data) {
              loaded.push(devResp.data);
            }
          }
          setCreatedDevices(loaded);
        } catch {
          // non-blocking
        }
      }
    },
    onError: (err: Error) => {
      toast.error(err.message || t("deviceImportDialog.failedToCommit"));
    },
  });

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setFileName(file.name);

    const reader = new FileReader();
    reader.onload = (event) => {
      const text = event.target?.result as string;
      if (text) {
        previewMutation.mutate(text);
      }
    };
    reader.readAsText(file);
  };

  const hasInvalidRows = useMemo(() => {
    return (preview?.summary.invalidCount ?? 0) > 0;
  }, [preview]);

  const handlePrintCreatedLabels = () => {
    if (createdDevices.length > 0 && onPrintLabels) {
      onPrintLabels(createdDevices);
      handleClose(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>
            {step === "upload" && t("deviceImportDialog.titleUpload")}
            {step === "preview" && t("deviceImportDialog.titlePreview")}
            {step === "done" && t("deviceImportDialog.titleDone")}
          </DialogTitle>
          <DialogDescription>
            {step === "upload" && t("deviceImportDialog.descUpload")}
            {step === "preview" &&
              t("deviceImportDialog.descPreview", { fileName })}
            {step === "done" && t("deviceImportDialog.descDone")}
          </DialogDescription>
        </DialogHeader>

        {/* Step 1: Upload */}
        {step === "upload" && (
          <div className="flex flex-col items-center justify-center gap-4 py-8">
            <div
              className="flex w-full cursor-pointer flex-col items-center justify-center rounded-lg border-2 border-dashed border-border p-10 hover:bg-muted/50"
              onClick={() => fileInputRef.current?.click()}
            >
              <Upload className="mb-2 size-8 text-muted-foreground" />
              <p className="text-sm font-medium">{t("deviceImportDialog.clickToSelectCsv")}</p>
              <p className="text-xs text-muted-foreground">
                {t("deviceImportDialog.csvColumnsHint")}
              </p>
              <input
                ref={fileInputRef}
                type="file"
                accept=".csv,text/csv"
                className="hidden"
                onChange={handleFileChange}
              />
            </div>
            {previewMutation.isPending && (
              <p className="text-sm text-muted-foreground animate-pulse">
                {t("deviceImportDialog.validatingRows")}
              </p>
            )}
          </div>
        )}

        {/* Step 2: Preview & Validation */}
        {step === "preview" && preview && (
          <div className="flex flex-col gap-4">
            {/* Summary metrics */}
            <div className="grid grid-cols-4 gap-2 text-center text-xs sm:text-sm">
              <div className="rounded-md border p-2 bg-background">
                <span className="text-muted-foreground">{t("deviceImportDialog.totalRows")}</span>
                <p className="text-lg font-semibold">{preview.summary.totalRows}</p>
              </div>
              <div className="rounded-md border border-emerald-200 bg-emerald-50/50 p-2 text-emerald-900 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-300">
                <span className="text-muted-foreground">{t("deviceImportDialog.toCreate")}</span>
                <p className="text-lg font-semibold">{preview.summary.createCount}</p>
              </div>
              <div className="rounded-md border border-blue-200 bg-blue-50/50 p-2 text-blue-900 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-300">
                <span className="text-muted-foreground">{t("deviceImportDialog.toUpdate")}</span>
                <p className="text-lg font-semibold">{preview.summary.updateCount}</p>
              </div>
              <div className="rounded-md border border-destructive/30 bg-destructive/10 p-2 text-destructive">
                <span className="text-muted-foreground">{t("deviceImportDialog.invalid")}</span>
                <p className="text-lg font-semibold">{preview.summary.invalidCount}</p>
              </div>
            </div>

            {hasInvalidRows && (
              <div className="flex items-center gap-2 rounded-md border border-destructive/50 bg-destructive/10 p-3 text-xs text-destructive">
                <AlertTriangle className="size-4 shrink-0" />
                <span>
                  {t("deviceImportDialog.invalidRowsWarning", {
                    count: preview.summary.invalidCount,
                  })}
                </span>
              </div>
            )}

            {/* Preview table */}
            <div className="max-h-72 overflow-auto rounded-md border text-xs">
              <table className="w-full text-left">
                <thead className="sticky top-0 bg-muted font-medium text-muted-foreground">
                  <tr>
                    <th className="p-2">{t("deviceImportDialog.colLine")}</th>
                    <th className="p-2">{t("deviceImportDialog.colAssetTag")}</th>
                    <th className="p-2">{t("deviceImportDialog.colName")}</th>
                    <th className="p-2">{t("deviceImportDialog.colCategory")}</th>
                    <th className="p-2">{t("deviceImportDialog.colAction")}</th>
                    <th className="p-2">{t("deviceImportDialog.colValidationNotes")}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {preview.rows.map((row) => (
                    <tr
                      key={row.lineNo}
                      className={
                        row.action === "invalid"
                          ? "bg-destructive/10"
                          : row.action === "create"
                            ? "hover:bg-muted/30"
                            : "hover:bg-muted/30 bg-blue-50/20"
                      }
                    >
                      <td className="p-2 font-mono">{row.lineNo}</td>
                      <td className="p-2 font-mono font-medium">
                        {row.values.asset_tag || "—"}
                      </td>
                      <td className="p-2">{row.values.name || "—"}</td>
                      <td className="p-2">{row.values.category || "—"}</td>
                      <td className="p-2">
                        {row.action === "create" && (
                          <Badge variant="outline" className="border-emerald-500 text-emerald-600">
                            {t("deviceImportDialog.actionCreate")}
                          </Badge>
                        )}
                        {row.action === "update" && (
                          <Badge variant="outline" className="border-blue-500 text-blue-600">
                            {t("deviceImportDialog.actionUpdate")}
                          </Badge>
                        )}
                        {row.action === "invalid" && (
                          <Badge variant="destructive">{t("deviceImportDialog.invalid")}</Badge>
                        )}
                      </td>
                      <td className="p-2">
                        {row.problems && row.problems.length > 0 ? (
                          <div className="flex flex-col gap-0.5 text-destructive font-medium">
                            {row.problems.map((p, idx) => (
                              <span key={idx}>• {p.message}</span>
                            ))}
                          </div>
                        ) : row.action === "update" ? (
                          <span className="text-muted-foreground">
                            {t("deviceImportDialog.existingDeviceNote")}
                          </span>
                        ) : (
                          <span className="text-muted-foreground">
                            {t("deviceImportDialog.newDeviceNote")}
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {/* Step 3: Done */}
        {step === "done" && importResult && (
          <div className="flex flex-col gap-6">
            <div className="flex items-center gap-3 rounded-md border border-emerald-500/40 bg-emerald-50/30 p-4 text-sm text-emerald-950 dark:bg-emerald-950/20 dark:text-emerald-200">
              <CheckCircle2 className="size-5 text-emerald-600 shrink-0" />
              <div>
                <p className="font-semibold">{t("deviceImportDialog.importBatchCommitted")}</p>
                <p className="text-xs text-muted-foreground">
                  {t("deviceImportDialog.batchIdLabel")}{" "}
                  <span className="font-mono">{importResult.importId}</span> ·{" "}
                  {t("deviceImportDialog.batchCounts", {
                    created: importResult.createdCount,
                    updated: importResult.updatedCount,
                  })}
                </p>
              </div>
            </div>

            {createdDevices.length > 0 && (
              <div className="flex items-center justify-between rounded-lg border border-border p-4">
                <div>
                  <h4 className="text-sm font-semibold">{t("deviceImportDialog.printLabelsHeading")}</h4>
                  <p className="text-xs text-muted-foreground">
                    {t("deviceImportDialog.printLabelsDesc", { count: createdDevices.length })}
                  </p>
                </div>
                <Button size="sm" onClick={handlePrintCreatedLabels}>
                  <Printer className="size-4" data-icon="inline-start" />
                  {t("devices.printLabelsWithCount", { count: createdDevices.length })}
                </Button>
              </div>
            )}
          </div>
        )}

        <DialogFooter>
          {step === "upload" && (
            <Button variant="outline" onClick={() => handleClose(false)}>
              {t("common.cancel")}
            </Button>
          )}

          {step === "preview" && (
            <div className="flex w-full justify-between gap-2">
              <Button variant="outline" onClick={() => setStep("upload")}>
                {t("deviceImportDialog.chooseAnotherFile")}
              </Button>
              <div className="flex gap-2">
                <Button variant="outline" onClick={() => handleClose(false)}>
                  {t("common.cancel")}
                </Button>
                <Button
                  disabled={hasInvalidRows || commitMutation.isPending}
                  onClick={() => preview?.previewId && commitMutation.mutate(preview.previewId)}
                >
                  {commitMutation.isPending ? (
                    t("deviceImportDialog.committingImport")
                  ) : (
                    <>
                      {t("deviceImportDialog.commitImport")}
                      <ArrowRight className="size-4" data-icon="inline-end" />
                    </>
                  )}
                </Button>
              </div>
            </div>
          )}

          {step === "done" && (
            <Button onClick={() => handleClose(false)}>{t("deviceImportDialog.done")}</Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
