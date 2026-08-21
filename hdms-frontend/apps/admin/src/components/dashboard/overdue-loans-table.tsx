import { type OverdueLoanSummary, forceReturnLoan } from "@hdms/api-client";
import { Link } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Clock, Copy, CornerDownLeft, ExternalLink } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Textarea } from "@/components/ui/textarea";
import { RoleGate } from "@/lib/use-role";
import { cn } from "@hdms/ui";

interface OverdueLoansTableProps {
  loans?: OverdueLoanSummary[];
}

function formatDate(iso: string): string {
  try {
    const d = new Date(iso);
    return d.toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return iso;
  }
}

export function OverdueLoansTable({ loans = [] }: OverdueLoansTableProps) {
  const queryClient = useQueryClient();
  const [returnLoanTarget, setReturnLoanTarget] = useState<OverdueLoanSummary | null>(null);
  const [returnReason, setReturnReason] = useState("");

  // Sort worst first (highest daysOverdue / earliest due date)
  const sortedLoans = [...loans].sort((a, b) => {
    if (b.daysOverdue !== a.daysOverdue) {
      return b.daysOverdue - a.daysOverdue;
    }
    return new Date(a.dueAt).getTime() - new Date(b.dueAt).getTime();
  });

  const forceReturnMutation = useMutation({
    mutationFn: async ({ loanId, reason }: { loanId: string; reason: string }) => {
      const { data, error } = await forceReturnLoan({
        path: { id: loanId },
        body: { reason },
      });
      if (error) throw error;
      return data;
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["dashboard"] }),
        queryClient.invalidateQueries({ queryKey: ["loans"] }),
        queryClient.invalidateQueries({ queryKey: ["devices"] }),
      ]);
      toast.success("Loan closed administratively");
      setReturnLoanTarget(null);
      setReturnReason("");
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || "Failed to force return loan");
    },
  });

  const handleCopyReminder = async (loan: OverdueLoanSummary) => {
    const formattedDue = formatDate(loan.dueAt);
    const message = `Hi ${loan.userFullName}, your loan for ${loan.deviceName} (${loan.assetTag}) was due on ${formattedDue}. Please return the device to any HDMS kiosk or admin station as soon as possible.`;
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(message);
      }
      toast.success("Reminder message copied to clipboard");
    } catch {
      toast.error("Could not copy reminder message to clipboard");
    }
  };

  return (
    <Card className="h-full flex flex-col">
      <CardHeader className="pb-3 flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-base font-semibold flex items-center gap-2">
            <Clock className="size-4 text-amber-500" />
            Overdue Loans
            {sortedLoans.length > 0 && (
              <span className="rounded-full bg-amber-100 px-2 py-0.5 text-xs font-semibold text-amber-800 dark:bg-amber-950 dark:text-amber-300">
                {sortedLoans.length}
              </span>
            )}
          </CardTitle>
          <p className="text-xs text-muted-foreground mt-0.5">
            Priority work queue — sorted worst first
          </p>
        </div>
        <Link
          to="/loans"
          search={{ status: "overdue" }}
          className="text-xs font-medium text-primary hover:underline"
        >
          View all loans →
        </Link>
      </CardHeader>

      <CardContent className="flex-1 p-0">
        {sortedLoans.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-12 px-4 text-center">
            <div className="flex size-12 items-center justify-center rounded-full bg-emerald-50 dark:bg-emerald-950/40 text-emerald-600 dark:text-emerald-400 mb-3">
              <CheckCircle2 className="size-6" />
            </div>
            <h3 className="text-sm font-semibold text-foreground">
              Nothing overdue
            </h3>
            <p className="mt-1 text-xs text-muted-foreground max-w-sm">
              All active loans are currently within their scheduled return period.
            </p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs border-collapse">
              <thead>
                <tr className="border-b border-t bg-muted/40 text-muted-foreground">
                  <th className="py-2.5 px-4 font-medium">Device</th>
                  <th className="py-2.5 px-4 font-medium">Borrower</th>
                  <th className="py-2.5 px-4 font-medium">Due Date</th>
                  <th className="py-2.5 px-4 font-medium">Overdue</th>
                  <th className="py-2.5 px-4 font-medium text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {sortedLoans.map((loan) => (
                  <tr
                    key={loan.loanId}
                    data-testid={`overdue-row-${loan.loanId}`}
                    className="hover:bg-muted/30 transition-colors"
                  >
                    <td className="py-3 px-4">
                      <div className="font-medium text-foreground">
                        {loan.deviceName}
                      </div>
                      <div className="font-identifier text-muted-foreground text-[11px]">
                        {loan.assetTag}
                      </div>
                    </td>
                    <td className="py-3 px-4">
                      <Link
                        to="/users/$userId"
                        params={{ userId: loan.userId }}
                        className="font-medium text-foreground hover:text-primary hover:underline"
                      >
                        {loan.userFullName}
                      </Link>
                      {loan.userEmployeeNo && (
                        <div className="text-muted-foreground text-[11px]">
                          {loan.userEmployeeNo}
                        </div>
                      )}
                    </td>
                    <td className="py-3 px-4 text-muted-foreground whitespace-nowrap">
                      {formatDate(loan.dueAt)}
                    </td>
                    <td className="py-3 px-4 whitespace-nowrap">
                      <span
                        className={cn(
                          "inline-flex items-center rounded px-1.5 py-0.5 text-[11px] font-semibold",
                          loan.daysOverdue >= 3
                            ? "bg-rose-100 text-rose-800 dark:bg-rose-950 dark:text-rose-300"
                            : "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300"
                        )}
                      >
                        {loan.daysOverdue === 0
                          ? "Due today"
                          : loan.daysOverdue === 1
                          ? "1 day late"
                          : `${loan.daysOverdue} days late`}
                      </span>
                    </td>
                    <td className="py-3 px-4 text-right whitespace-nowrap">
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 px-2 text-xs"
                          onClick={() => handleCopyReminder(loan)}
                          title="Copy prepared reminder message to clipboard"
                        >
                          <Copy className="size-3.5 mr-1" />
                          Remind
                        </Button>
                        <RoleGate minRole="technician">
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-7 px-2 text-xs border-amber-300 text-amber-800 dark:border-amber-800 dark:text-amber-300 hover:bg-amber-50 dark:hover:bg-amber-950/50"
                            onClick={() => {
                              setReturnLoanTarget(loan);
                              setReturnReason("");
                            }}
                          >
                            <CornerDownLeft className="size-3.5 mr-1" />
                            Force return
                          </Button>
                        </RoleGate>
                        <Link
                          to="/loans"
                          search={{ q: loan.assetTag }}
                          className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground hover:text-foreground hover:bg-muted"
                          title="View loan details"
                        >
                          <ExternalLink className="size-3.5" />
                        </Link>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>

      {/* Force Return Dialog */}
      <Dialog
        open={Boolean(returnLoanTarget)}
        onOpenChange={(open) => {
          if (!open) {
            setReturnLoanTarget(null);
            setReturnReason("");
          }
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Force Return Loan</DialogTitle>
            <DialogDescription>
              Administratively close the overdue loan for{" "}
              <strong className="text-foreground">
                {returnLoanTarget?.deviceName} ({returnLoanTarget?.assetTag})
              </strong>{" "}
              borrowed by{" "}
              <strong className="text-foreground">
                {returnLoanTarget?.userFullName}
              </strong>
              .
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-2 py-2">
            <label
              htmlFor="force-return-reason"
              className="text-xs font-medium text-foreground"
            >
              Reason for administrative return (required for audit)
            </label>
            <Textarea
              id="force-return-reason"
              placeholder="e.g. Device returned to nurse station without scanning, or confirmed found in department."
              value={returnReason}
              onChange={(e) => setReturnReason(e.target.value)}
              className="min-h-[80px]"
            />
          </div>

          <DialogFooter className="gap-2 sm:gap-0">
            <Button
              variant="outline"
              onClick={() => {
                setReturnLoanTarget(null);
                setReturnReason("");
              }}
              disabled={forceReturnMutation.isPending}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                if (returnLoanTarget && returnReason.trim()) {
                  forceReturnMutation.mutate({
                    loanId: returnLoanTarget.loanId,
                    reason: returnReason.trim(),
                  });
                }
              }}
              disabled={!returnReason.trim() || forceReturnMutation.isPending}
            >
              {forceReturnMutation.isPending ? "Returning…" : "Confirm return"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
