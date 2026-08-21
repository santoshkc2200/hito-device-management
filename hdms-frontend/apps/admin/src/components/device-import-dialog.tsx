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
            "Failed to parse CSV file",
        );
      }
      return resp.data as ImportPreview;
    },
    onSuccess: (data) => {
      setPreview(data);
      setStep("preview");
    },
    onError: (err: Error) => {
      toast.error(err.message || "Failed to validate CSV");
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
            "Failed to commit import",
        );
      }
      return resp.data as ImportResult;
    },
    onSuccess: async (data) => {
      setImportResult(data);
      setStep("done");
      await queryClient.invalidateQueries({ queryKey: ["devices"] });
      toast.success(
        `Import completed: ${data.createdCount} created, ${data.updatedCount} updated`,
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
      toast.error(err.message || "Failed to commit import");
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
            {step === "upload" && "Import devices from CSV"}
            {step === "preview" && "Preview device import"}
            {step === "done" && "Import summary & label generation"}
          </DialogTitle>
          <DialogDescription>
            {step === "upload" &&
              "Upload a CSV file of equipment. Columns: asset_tag, name, and category are required. Optional: manufacturer, model, serial_no, home_location, notes, acquired_on (YYYY-MM-DD)."}
            {step === "preview" &&
              `Review validated rows from ${fileName} before writing to the catalog.`}
            {step === "done" &&
              "Import batch committed. Review created devices and generate labels for newly added equipment."}
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
              <p className="text-sm font-medium">Click to select CSV file</p>
              <p className="text-xs text-muted-foreground">
                devices.csv with columns: asset_tag, name, category, manufacturer, model, serial_no, home_location, notes, acquired_on
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
                Validating CSV rows…
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
                <span className="text-muted-foreground">Total Rows</span>
                <p className="text-lg font-semibold">{preview.summary.totalRows}</p>
              </div>
              <div className="rounded-md border border-emerald-200 bg-emerald-50/50 p-2 text-emerald-900 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-300">
                <span className="text-muted-foreground">To Create</span>
                <p className="text-lg font-semibold">{preview.summary.createCount}</p>
              </div>
              <div className="rounded-md border border-blue-200 bg-blue-50/50 p-2 text-blue-900 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-300">
                <span className="text-muted-foreground">To Update</span>
                <p className="text-lg font-semibold">{preview.summary.updateCount}</p>
              </div>
              <div className="rounded-md border border-destructive/30 bg-destructive/10 p-2 text-destructive">
                <span className="text-muted-foreground">Invalid</span>
                <p className="text-lg font-semibold">{preview.summary.invalidCount}</p>
              </div>
            </div>

            {hasInvalidRows && (
              <div className="flex items-center gap-2 rounded-md border border-destructive/50 bg-destructive/10 p-3 text-xs text-destructive">
                <AlertTriangle className="size-4 shrink-0" />
                <span>
                  This file contains {preview.summary.invalidCount} invalid row(s). Please fix the errors and re-upload before committing.
                </span>
              </div>
            )}

            {/* Preview table */}
            <div className="max-h-72 overflow-auto rounded-md border text-xs">
              <table className="w-full text-left">
                <thead className="sticky top-0 bg-muted font-medium text-muted-foreground">
                  <tr>
                    <th className="p-2">Line</th>
                    <th className="p-2">Asset Tag</th>
                    <th className="p-2">Name</th>
                    <th className="p-2">Category</th>
                    <th className="p-2">Action</th>
                    <th className="p-2">Validation Notes</th>
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
                            Create
                          </Badge>
                        )}
                        {row.action === "update" && (
                          <Badge variant="outline" className="border-blue-500 text-blue-600">
                            Update
                          </Badge>
                        )}
                        {row.action === "invalid" && (
                          <Badge variant="destructive">Invalid</Badge>
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
                          <span className="text-muted-foreground">Existing device (will update fields)</span>
                        ) : (
                          <span className="text-muted-foreground">New device</span>
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
                <p className="font-semibold">Import batch committed</p>
                <p className="text-xs text-muted-foreground">
                  Batch ID: <span className="font-mono">{importResult.importId}</span> ·{" "}
                  {importResult.createdCount} created, {importResult.updatedCount} updated.
                </p>
              </div>
            </div>

            {createdDevices.length > 0 && (
              <div className="flex items-center justify-between rounded-lg border border-border p-4">
                <div>
                  <h4 className="text-sm font-semibold">Print barcode / QR labels</h4>
                  <p className="text-xs text-muted-foreground">
                    Print adhesive labels for the {createdDevices.length} newly registered devices.
                  </p>
                </div>
                <Button size="sm" onClick={handlePrintCreatedLabels}>
                  <Printer className="size-4" data-icon="inline-start" />
                  Print labels ({createdDevices.length})
                </Button>
              </div>
            )}
          </div>
        )}

        <DialogFooter>
          {step === "upload" && (
            <Button variant="outline" onClick={() => handleClose(false)}>
              Cancel
            </Button>
          )}

          {step === "preview" && (
            <div className="flex w-full justify-between gap-2">
              <Button variant="outline" onClick={() => setStep("upload")}>
                Choose another file
              </Button>
              <div className="flex gap-2">
                <Button variant="outline" onClick={() => handleClose(false)}>
                  Cancel
                </Button>
                <Button
                  disabled={hasInvalidRows || commitMutation.isPending}
                  onClick={() => preview?.previewId && commitMutation.mutate(preview.previewId)}
                >
                  {commitMutation.isPending ? (
                    "Committing import…"
                  ) : (
                    <>
                      Commit import
                      <ArrowRight className="size-4" data-icon="inline-end" />
                    </>
                  )}
                </Button>
              </div>
            </div>
          )}

          {step === "done" && (
            <Button onClick={() => handleClose(false)}>Done</Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
