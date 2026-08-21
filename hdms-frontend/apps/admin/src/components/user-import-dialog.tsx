import {
  type ImportPreview,
  type ImportResult,
  type User,
  commitUserImport,
  getUser,
  issueCredential,
  previewUserImport,
} from "@hdms/api-client";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  CreditCard,
  Printer,
  Upload,
} from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { Barcode } from "@/components/barcode";
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

interface UserWithCredential {
  user: User;
  token?: string;
}

export function UserImportDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const [step, setStep] = useState<"upload" | "preview" | "done">("upload");
  const [fileName, setFileName] = useState("");
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [importResult, setImportResult] = useState<ImportResult | null>(null);
  const [issuedUsers, setIssuedUsers] = useState<UserWithCredential[]>([]);
  const [isIssuingCards, setIsIssuingCards] = useState(false);

  const reset = () => {
    setStep("upload");
    setFileName("");
    setPreview(null);
    setImportResult(null);
    setIssuedUsers([]);
    setIsIssuingCards(false);
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
      const resp = await previewUserImport({
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
      const resp = await commitUserImport({
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
      queryClient.invalidateQueries({ queryKey: ["users"] });
      toast.success(
        `Import completed: ${data.createdCount} created, ${data.updatedCount} updated`,
      );

      // Load newly created users info
      if (data.createdSubjectIds && data.createdSubjectIds.length > 0) {
        try {
          const loaded: UserWithCredential[] = [];
          for (const uid of data.createdSubjectIds) {
            const uResp = await getUser({ path: { id: uid } });
            if (uResp.data) {
              loaded.push({ user: uResp.data });
            }
          }
          setIssuedUsers(loaded);
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

  const handleIssueCards = async () => {
    if (!importResult?.createdSubjectIds?.length || isIssuingCards) return;
    setIsIssuingCards(true);

    try {
      const updated: UserWithCredential[] = [];
      for (const item of issuedUsers) {
        try {
          const credResp = await issueCredential({
            body: {
              subjectType: "user",
              subjectId: item.user.id,
              kind: "qr",
            },
          });
          if (credResp.data) {
            updated.push({
              user: item.user,
              token: credResp.data.token,
            });
          } else {
            updated.push(item);
          }
        } catch {
          updated.push(item);
        }
      }
      setIssuedUsers(updated);
      queryClient.invalidateQueries({ queryKey: ["credentials"] });
      toast.success("Cards issued to new borrowers");
    } catch {
      toast.error("Failed to issue some cards");
    } finally {
      setIsIssuingCards(false);
    }
  };

  const hasInvalidRows = useMemo(() => {
    return (preview?.summary.invalidCount ?? 0) > 0;
  }, [preview]);

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>
            {step === "upload" && "Import users from CSV"}
            {step === "preview" && "Preview user import"}
            {step === "done" && "Import summary & card distribution"}
          </DialogTitle>
          <DialogDescription>
            {step === "upload" &&
              "Upload a CSV file of hospital staff. Columns: employee_no and full_name are required. Optional: department, email, phone, notes."}
            {step === "preview" &&
              `Review validated rows from ${fileName} before writing to the database.`}
            {step === "done" &&
              "Import batch committed. Issue cards and print the distribution sheet for newly registered staff."}
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
                staff.csv with columns: employee_no, full_name, department, email, phone, notes
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
                    <th className="p-2">Employee #</th>
                    <th className="p-2">Full Name</th>
                    <th className="p-2">Department</th>
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
                        {row.values.employee_no || "—"}
                      </td>
                      <td className="p-2">{row.values.full_name || "—"}</td>
                      <td className="p-2">{row.values.department || "—"}</td>
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
                          <span className="text-muted-foreground">Existing live borrower</span>
                        ) : (
                          <span className="text-muted-foreground">New borrower</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {/* Step 3: Done & Distribution Sheet */}
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

            {issuedUsers.length > 0 && (
              <div className="flex flex-col gap-3">
                <div className="flex items-center justify-between">
                  <div>
                    <h4 className="text-sm font-semibold">Name-to-card distribution sheet</h4>
                    <p className="text-xs text-muted-foreground">
                      FR-77: Issue QR cards to the {issuedUsers.length} newly registered staff and print the handout sheet.
                    </p>
                  </div>
                  <div className="flex items-center gap-2">
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={isIssuingCards}
                      onClick={handleIssueCards}
                    >
                      <CreditCard className="size-4" data-icon="inline-start" />
                      {isIssuingCards ? "Issuing cards…" : "Issue cards to new staff"}
                    </Button>
                    <Button size="sm" onClick={() => window.print()}>
                      <Printer className="size-4" data-icon="inline-start" />
                      Print sheet
                    </Button>
                  </div>
                </div>

                {/* Printable distribution sheet area */}
                <div
                  id="user-distribution-sheet"
                  aria-label="Name-to-card distribution sheet"
                  className="rounded-md border border-border bg-white p-6 text-black shadow-xs"
                >
                  <div className="border-b pb-3 mb-3">
                    <h2 className="text-base font-bold tracking-tight">
                      HITO HOSPITAL — BORROWER CARD DISTRIBUTION SHEET
                    </h2>
                    <div className="flex justify-between text-xs text-gray-600 mt-1">
                      <span>Import Batch: {importResult.importId}</span>
                      <span>Date: {new Date().toLocaleDateString()}</span>
                      <span>Total Staff: {issuedUsers.length}</span>
                    </div>
                  </div>

                  <table className="w-full border-collapse text-xs">
                    <thead>
                      <tr className="border-b bg-gray-50">
                        <th className="p-1.5 text-left font-semibold">#</th>
                        <th className="p-1.5 text-left font-semibold">EMPLOYEE #</th>
                        <th className="p-1.5 text-left font-semibold">NAME</th>
                        <th className="p-1.5 text-left font-semibold">DEPARTMENT</th>
                        <th className="p-1.5 text-left font-semibold">CARD STATUS / TOKEN</th>
                        <th className="p-1.5 text-left font-semibold">SIGNATURE / ACKNOWLEDGMENT</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-gray-200">
                      {issuedUsers.map(({ user, token }, idx) => (
                        <tr key={user.id}>
                          <td className="p-1.5 font-mono">{idx + 1}</td>
                          <td className="p-1.5 font-mono font-medium">{user.employeeNo}</td>
                          <td className="p-1.5 font-medium">{user.fullName}</td>
                          <td className="p-1.5">{user.departmentId || "—"}</td>
                          <td className="p-1.5">
                            {token ? (
                              <div className="flex items-center gap-2">
                                <Barcode value={token} scale={1} className="size-6" />
                                <span className="font-mono text-[10px]">{token}</span>
                              </div>
                            ) : (
                              <span className="text-gray-500 italic">Card not issued</span>
                            )}
                          </td>
                          <td className="p-1.5 border-b border-gray-300 w-48" />
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
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
