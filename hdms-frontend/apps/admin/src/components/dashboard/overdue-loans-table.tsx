import { type OverdueLoanSummary, forceReturnLoan, remindLoan } from "@hdms/api-client";
import { Link } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Bell, CheckCircle2, Clock, CornerDownLeft, ExternalLink } from "lucide-react";
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
import { useT } from "@/i18n";
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
  const t = useT();
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
      toast.success(t("dashboard.overdue.toastSuccess"));
      setReturnLoanTarget(null);
      setReturnReason("");
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || t("dashboard.overdue.toastError"));
    },
  });

  const [remindingId, setRemindingId] = useState<string | null>(null);

  const remindMutation = useMutation({
    mutationFn: async (loanId: string) => {
      setRemindingId(loanId);
      const { data, error, response } = await remindLoan({
        path: { id: loanId },
      });
      if (error) {
        throw { ...error, status: response?.status };
      }
      return data;
    },
    onSuccess: (data) => {
      if (data?.outcome === "sent") {
        toast.success(t("dashboard.overdue.remindSuccess"));
      } else if (data?.outcome === "queued_quiet_hours") {
        toast.info(t("dashboard.overdue.remindQueuedQuietHours"));
      } else if (data?.outcome === "refused") {
        toast.warning(t("dashboard.overdue.remindRefused", { reason: data.reason || "" }));
      }
    },
    onError: (err: any) => {
      if (err?.status === 429) {
        toast.error(t("dashboard.overdue.remindRateLimited"));
      } else {
        toast.error(err?.detail || err?.title || t("dashboard.overdue.remindFailed"));
      }
    },
    onSettled: () => {
      setRemindingId(null);
    },
  });

  return (
    <Card className="h-full flex flex-col">
      <CardHeader className="pb-3 flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-base font-semibold flex items-center gap-2">
            <Clock className="size-4 text-amber-500" />
            {t("dashboard.overdue.title")}
            {sortedLoans.length > 0 && (
              <span className="rounded-full bg-amber-100 px-2 py-0.5 text-xs font-semibold text-amber-800 dark:bg-amber-950 dark:text-amber-300">
                {sortedLoans.length}
              </span>
            )}
          </CardTitle>
          <p className="text-xs text-muted-foreground mt-0.5">
            {t("dashboard.overdue.subtitle")}
          </p>
        </div>
        <Link
          to="/loans"
          search={{ status: "overdue" }}
          className="text-xs font-medium text-primary hover:underline"
        >
          {t("dashboard.overdue.viewAllLoans")}
        </Link>
      </CardHeader>

      <CardContent className="flex-1 p-0">
        {sortedLoans.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-12 px-4 text-center">
            <div className="flex size-12 items-center justify-center rounded-full bg-emerald-50 dark:bg-emerald-950/40 text-emerald-600 dark:text-emerald-400 mb-3">
              <CheckCircle2 className="size-6" />
            </div>
            <h3 className="text-sm font-semibold text-foreground">
              {t("dashboard.overdue.emptyTitle")}
            </h3>
            <p className="mt-1 text-xs text-muted-foreground max-w-sm">
              {t("dashboard.overdue.emptyDesc")}
            </p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs border-collapse">
              <thead>
                <tr className="border-b border-t bg-muted/40 text-muted-foreground">
                  <th className="py-2.5 px-4 font-medium">{t("dashboard.overdue.columnDevice")}</th>
                  <th className="py-2.5 px-4 font-medium">{t("dashboard.overdue.columnBorrower")}</th>
                  <th className="py-2.5 px-4 font-medium">{t("dashboard.overdue.columnDueDate")}</th>
                  <th className="py-2.5 px-4 font-medium">{t("dashboard.overdue.columnOverdue")}</th>
                  <th className="py-2.5 px-4 font-medium text-right">{t("dashboard.overdue.columnActions")}</th>
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
                          ? t("dashboard.overdue.dueToday")
                          : loan.daysOverdue === 1
                          ? t("dashboard.overdue.oneDayLate")
                          : t("dashboard.overdue.daysLate", { count: loan.daysOverdue })}
                      </span>
                    </td>
                    <td className="py-3 px-4 text-right whitespace-nowrap">
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 px-2 text-xs"
                          onClick={() => remindMutation.mutate(loan.loanId)}
                          disabled={remindingId === loan.loanId}
                          title={t("dashboard.overdue.remindTooltip")}
                        >
                          <Bell className={`size-3.5 mr-1 ${remindingId === loan.loanId ? "animate-pulse" : ""}`} />
                          {remindingId === loan.loanId
                            ? t("dashboard.overdue.remindSending")
                            : t("dashboard.overdue.remindButton")}
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
                            {t("dashboard.overdue.forceReturnButton")}
                          </Button>
                        </RoleGate>
                        <Link
                          to="/loans"
                          search={{ q: loan.assetTag }}
                          className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground hover:text-foreground hover:bg-muted"
                          title={t("dashboard.overdue.viewDetailsTooltip")}
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
            <DialogTitle>{t("dashboard.overdue.dialogTitle")}</DialogTitle>
            <DialogDescription>
              {t("dashboard.overdue.dialogDescPrefix")}{" "}
              <strong className="text-foreground">
                {returnLoanTarget?.deviceName} ({returnLoanTarget?.assetTag})
              </strong>{" "}
              {t("dashboard.overdue.dialogDescBorrower")}{" "}
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
              {t("dashboard.overdue.reasonLabel")}
            </label>
            <Textarea
              id="force-return-reason"
              placeholder={t("dashboard.overdue.reasonPlaceholder")}
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
              {t("common.cancel")}
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
              {forceReturnMutation.isPending ? t("dashboard.overdue.returning") : t("dashboard.overdue.confirmReturn")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
