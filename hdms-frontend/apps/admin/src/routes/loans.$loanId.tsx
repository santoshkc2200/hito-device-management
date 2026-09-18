import {
  type DeviceCondition,
  correctLoanAttribution,
  forceReturnLoan,
  getDevice,
  getLoan,
  getUser,
  listAuditEvents,
  listUsers,
  remindLoan,
  writeOffLoan,
} from "@hdms/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute, Link, useNavigate } from "@tanstack/react-router";
import {
  AlertCircle,
  ArrowLeft,
  Bell,
  Clock,
  CornerDownLeft,
  FileText,
  History,
  Laptop,
  Trash2,
  UserCheck,
  Users,
} from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { ErrorState, LoadingState } from "@/components/states";
import { LoanOriginBadge, loanStatusTone, StatusBadge } from "@/components/status-badge";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { RoleGate } from "@/lib/use-role";
import { cn } from "@hdms/ui";
import { useT } from "@/i18n";
import { authenticatedRoute } from "./authenticated";

function formatDate(iso?: string | null): string {
  if (!iso) return "—";
  try {
    const d = new Date(iso);
    return d.toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return iso;
  }
}

function getDaysOverdue(dueAt?: string | null): number {
  if (!dueAt) return 0;
  const due = new Date(dueAt).getTime();
  const now = Date.now();
  if (due >= now) return 0;
  return Math.max(1, Math.floor((now - due) / (1000 * 60 * 60 * 24)));
}

export function LoanDetailPage() {
  const t = useT();
  const { loanId } = loanDetailRoute.useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [forceReturnOpen, setForceReturnOpen] = useState(false);
  const [forceReturnReason, setForceReturnReason] = useState("");
  const [forceReturnCondition, setForceReturnCondition] = useState<DeviceCondition | "">("");

  const [writeOffOpen, setWriteOffOpen] = useState(false);
  const [writeOffReason, setWriteOffReason] = useState("");

  const [correctAttributionOpen, setCorrectAttributionOpen] = useState(false);
  const [correctUserId, setCorrectUserId] = useState("");
  const [correctReason, setCorrectReason] = useState("");

  // 1. Fetch Loan
  const { data: loan, isLoading: loanLoading, isError: loanError, refetch: refetchLoan } = useQuery({
    queryKey: ["loans", loanId],
    queryFn: async () => {
      const res = await getLoan({ path: { id: loanId } });
      if (res.error) throw res.error;
      return res.data;
    },
  });

  // 2. Fetch Borrower User
  const { data: borrower } = useQuery({
    queryKey: ["users", loan?.userId],
    queryFn: async () => {
      if (!loan?.userId) return null;
      const res = await getUser({ path: { id: loan.userId } });
      return res.data ?? null;
    },
    enabled: Boolean(loan?.userId),
  });

  // 3. Fetch Device
  const { data: device } = useQuery({
    queryKey: ["devices", loan?.deviceId],
    queryFn: async () => {
      if (!loan?.deviceId) return null;
      const res = await getDevice({ path: { id: loan.deviceId } });
      return res.data ?? null;
    },
    enabled: Boolean(loan?.deviceId),
  });

  // 4. Fetch Users List for Correction Dialog
  const { data: allUsers } = useQuery({
    queryKey: ["users", "lookup"],
    queryFn: async () => {
      const res = await listUsers({ query: { limit: 200 } });
      return res.data?.items ?? [];
    },
    enabled: correctAttributionOpen,
    staleTime: 60_000,
  });

  // 5. Fetch Audit Events for this loan
  const { data: auditEvents } = useQuery({
    queryKey: ["audit", "loan", loanId],
    queryFn: async () => {
      const res = await listAuditEvents({ query: { subject: `loan:${loanId}` } });
      return res.data?.items ?? [];
    },
    enabled: Boolean(loanId),
  });

  // Overrides Mutations
  const forceReturnMutation = useMutation({
    mutationFn: async () => {
      if (!loanId || !forceReturnReason.trim()) return;
      const res = await forceReturnLoan({
        path: { id: loanId },
        body: {
          reason: forceReturnReason.trim(),
          conditionIn: (forceReturnCondition || undefined) as DeviceCondition | undefined,
        },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["loans", loanId] }),
        queryClient.invalidateQueries({ queryKey: ["loans"] }),
        queryClient.invalidateQueries({ queryKey: ["devices"] }),
        queryClient.invalidateQueries({ queryKey: ["audit", "loan", loanId] }),
      ]);
      toast.success(t("loans.forceReturnSuccess"));
      setForceReturnOpen(false);
      setForceReturnReason("");
      setForceReturnCondition("");
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || t("loans.forceReturnFailed"));
    },
  });

  const writeOffMutation = useMutation({
    mutationFn: async () => {
      if (!loanId || !writeOffReason.trim()) return;
      const res = await writeOffLoan({
        path: { id: loanId },
        body: { reason: writeOffReason.trim() },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["loans", loanId] }),
        queryClient.invalidateQueries({ queryKey: ["loans"] }),
        queryClient.invalidateQueries({ queryKey: ["devices"] }),
        queryClient.invalidateQueries({ queryKey: ["audit", "loan", loanId] }),
      ]);
      toast.success(t("loans.writeOffSuccess"));
      setWriteOffOpen(false);
      setWriteOffReason("");
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || t("loans.writeOffFailed"));
    },
  });

  const correctAttributionMutation = useMutation({
    mutationFn: async () => {
      if (!loanId || !correctUserId || !correctReason.trim()) return;
      const res = await correctLoanAttribution({
        path: { id: loanId },
        body: {
          userId: correctUserId,
          reason: correctReason.trim(),
        },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: async (newLoan) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["loans"] }),
        queryClient.invalidateQueries({ queryKey: ["loans", loanId] }),
        queryClient.invalidateQueries({ queryKey: ["audit", "loan", loanId] }),
      ]);
      toast.success(t("loanDetail.attributionCorrected"));
      setCorrectAttributionOpen(false);
      setCorrectUserId("");
      setCorrectReason("");
      if (newLoan?.id) {
        navigate({ to: "/loans/$loanId", params: { loanId: newLoan.id } });
      }
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || t("loanDetail.attributionFailed"));
    },
  });

  const remindMutation = useMutation({
    mutationFn: async () => {
      if (!loan) return;
      const { data, error, response } = await remindLoan({
        path: { id: loan.id },
      });
      if (error) {
        throw { ...error, status: response?.status };
      }
      return data;
    },
    onSuccess: (data) => {
      if (data?.outcome === "sent") {
        toast.success(t("loanDetail.remindSuccess"));
      } else if (data?.outcome === "queued_quiet_hours") {
        toast.info(t("loanDetail.remindQueuedQuietHours"));
      } else if (data?.outcome === "refused") {
        toast.warning(t("loanDetail.remindRefused", { reason: data.reason || "" }));
      }
    },
    onError: (err: any) => {
      if (err?.status === 429) {
        toast.error(t("loanDetail.remindRateLimited"));
      } else {
        toast.error(err?.detail || err?.title || t("loanDetail.remindFailed"));
      }
    },
  });

  if (loanLoading) {
    return <LoadingState message={t("loanDetail.loadingLoan")} />;
  }

  if (loanError || !loan) {
    return (
      <ErrorState
        title={t("loanDetail.notFoundTitle")}
        detail={t("loanDetail.notFoundDetail", { loanId })}
        onRetry={() => refetchLoan()}
      />
    );
  }

  const isOpen = loan.status === "open";
  const isOverdue = isOpen && loan.dueAt && new Date(loan.dueAt).getTime() < Date.now();
  const daysOverdue = isOverdue ? getDaysOverdue(loan.dueAt) : 0;
  const tone = isOverdue ? "warning" : loanStatusTone[loan.status] || "muted";
  const statusLabel = isOverdue
    ? t("loans.statusOverdue")
    : loan.status === "written_off"
      ? t("loans.statusWrittenOff")
      : loan.status;

  return (
    <div className="flex flex-col gap-6" data-testid="loan-detail-page">
      {/* Top breadcrumb & actions */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3">
          <Button variant="ghost" size="icon" asChild className="size-8">
            <Link to="/loans">
              <ArrowLeft className="size-4" />
              <span className="sr-only">{t("loanDetail.backToLoans")}</span>
            </Link>
          </Button>
          <div>
            <div className="flex items-center gap-2 flex-wrap">
              <h1 className="text-xl font-semibold tracking-tight text-foreground font-identifier">
                {t("loanDetail.loanHeading", { id: loan.id.slice(0, 8) })}
              </h1>
              <StatusBadge label={statusLabel} tone={tone} />
              <LoanOriginBadge origin={loan.origin} disputed={loan.disputed} />
            </div>
            <p className="text-xs text-muted-foreground mt-0.5">
              {t("loanDetail.subtitle")}
            </p>
          </div>
        </div>

        {/* Action buttons */}
        <div className="flex items-center gap-2 flex-wrap">
          {isOpen && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => remindMutation.mutate()}
              disabled={remindMutation.isPending}
              title={t("loanDetail.remindTooltip")}
              data-testid="remind-button"
            >
              <Bell className={`size-3.5 mr-1.5 ${remindMutation.isPending ? "animate-pulse" : ""}`} />
              {remindMutation.isPending ? t("loanDetail.remindSending") : t("loanDetail.remind")}
            </Button>
          )}

          {isOpen && (
            <RoleGate minRole="technician">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setForceReturnOpen(true)}
                className="border-amber-300 text-amber-800 dark:border-amber-800 dark:text-amber-300 hover:bg-amber-50 dark:hover:bg-amber-950/50"
                data-testid="force-return-button"
              >
                <CornerDownLeft className="size-3.5 mr-1.5" />
                {t("loanDetail.forceReturnAction")}
              </Button>
            </RoleGate>
          )}

          {isOpen && (
            <RoleGate minRole="admin">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setWriteOffOpen(true)}
                className="border-destructive/40 text-destructive hover:bg-destructive/10"
                data-testid="write-off-button"
              >
                <Trash2 className="size-3.5 mr-1.5" />
                {t("loanDetail.writeOffAction")}
              </Button>
            </RoleGate>
          )}

          <RoleGate minRole="admin">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setCorrectAttributionOpen(true)}
              data-testid="correct-attribution-button"
            >
              <UserCheck className="size-3.5 mr-1.5" />
              {t("loanDetail.correctAttributionAction")}
            </Button>
          </RoleGate>
        </div>
      </div>

      {/* Disputed Banner */}
      {loan.disputed && (
        <div className="rounded-lg border border-rose-200 bg-rose-50 p-4 text-xs text-rose-900 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-300 flex items-start gap-3">
          <AlertCircle className="size-4 shrink-0 mt-0.5 text-rose-600" />
          <div>
            <p className="font-semibold">{t("loanDetail.disputedTitle")}</p>
            <p className="mt-0.5 text-rose-800 dark:text-rose-400">
              {t("loanDetail.disputedExplanation")}
            </p>
          </div>
        </div>
      )}

      {/* Grid of info cards */}
      <div className="grid gap-6 md:grid-cols-2">
        {/* Borrower Card */}
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-semibold flex items-center justify-between">
              <span className="flex items-center gap-2">
                <Users className="size-4 text-primary" />
                {t("loanDetail.borrowerHeading")}
              </span>
              {borrower && (
                <Link
                  to="/users/$userId"
                  params={{ userId: borrower.id }}
                  className="text-xs text-primary hover:underline font-normal"
                >
                  {t("loanDetail.viewProfile")}
                </Link>
              )}
            </CardTitle>
          </CardHeader>
          <CardContent className="text-xs space-y-2.5">
            <div className="flex justify-between border-b pb-1.5">
              <span className="text-muted-foreground">{t("loanDetail.fullNameLabel")}</span>
              <span className="font-medium text-foreground" data-testid="borrower-name">
                {borrower?.fullName || "—"}
              </span>
            </div>
            <div className="flex justify-between border-b pb-1.5">
              <span className="text-muted-foreground">{t("loanDetail.employeeNoLabel")}</span>
              <span className="font-identifier text-foreground">
                {borrower?.employeeNo || "—"}
              </span>
            </div>
            <div className="flex justify-between border-b pb-1.5">
              <span className="text-muted-foreground">{t("loanDetail.departmentLabel")}</span>
              <span className="text-foreground">{borrower?.departmentId || "—"}</span>
            </div>
            <div className="flex justify-between border-b pb-1.5">
              <span className="text-muted-foreground">{t("loanDetail.userStatusLabel")}</span>
              <span className="capitalize text-foreground">{borrower?.status || "—"}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">{t("loanDetail.emailPhoneLabel")}</span>
              <span className="text-foreground">
                {borrower?.email || borrower?.phone || "—"}
              </span>
            </div>
          </CardContent>
        </Card>

        {/* Device Card */}
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-semibold flex items-center justify-between">
              <span className="flex items-center gap-2">
                <Laptop className="size-4 text-primary" />
                {t("loanDetail.deviceHeading")}
              </span>
              {device && (
                <Link
                  to="/devices/$deviceId"
                  params={{ deviceId: device.id }}
                  className="text-xs text-primary hover:underline font-normal"
                >
                  {t("loanDetail.viewDevice")}
                </Link>
              )}
            </CardTitle>
          </CardHeader>
          <CardContent className="text-xs space-y-2.5">
            <div className="flex justify-between border-b pb-1.5">
              <span className="text-muted-foreground">{t("loanDetail.deviceNameLabel")}</span>
              <span className="font-medium text-foreground" data-testid="device-name">
                {device?.name || "—"}
              </span>
            </div>
            <div className="flex justify-between border-b pb-1.5">
              <span className="text-muted-foreground">{t("loanDetail.assetTagLabel")}</span>
              <span className="font-identifier text-foreground">
                {device?.assetTag || loan.deviceId}
              </span>
            </div>
            <div className="flex justify-between border-b pb-1.5">
              <span className="text-muted-foreground">{t("loanDetail.manufacturerModelLabel")}</span>
              <span className="text-foreground">
                {device ? `${device.manufacturer || "—"} / ${device.model || "—"}` : "—"}
              </span>
            </div>
            <div className="flex justify-between border-b pb-1.5">
              <span className="text-muted-foreground">{t("loanDetail.conditionOutLabel")}</span>
              <span className="capitalize text-foreground">{loan.conditionOut || "—"}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">{t("loanDetail.conditionInLabel")}</span>
              <span className="capitalize text-foreground">{loan.conditionIn || "—"}</span>
            </div>
          </CardContent>
        </Card>

        {/* Timeline & Scan Sources Card */}
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-semibold flex items-center gap-2">
              <Clock className="size-4 text-primary" />
              {t("loanDetail.timelineHeading")}
            </CardTitle>
          </CardHeader>
          <CardContent className="text-xs space-y-3">
            <div className="rounded-md border p-2.5 bg-muted/20 space-y-1.5">
              <div className="flex items-center justify-between font-medium">
                <span className="text-foreground">{t("loanDetail.borrowedLabel")}</span>
                <span className="text-muted-foreground">{formatDate(loan.borrowedAt)}</span>
              </div>
              <div className="flex justify-between text-[11px] text-muted-foreground">
                <span>{t("loanDetail.scanSourceLabel")}</span>
                <span className="font-medium text-foreground capitalize" data-testid="borrow-source">
                  {loan.borrowSource || "—"}
                </span>
              </div>
              <div className="flex justify-between text-[11px] text-muted-foreground">
                <span>{t("loanDetail.kioskActorLabel")}</span>
                <span className="font-identifier text-foreground">
                  {loan.borrowKioskId
                    ? t("loanDetail.kioskPrefix", { id: loan.borrowKioskId.slice(0, 8) })
                    : loan.borrowActor}
                </span>
              </div>
            </div>

            <div className="rounded-md border p-2.5 bg-muted/20 space-y-1.5">
              <div className="flex items-center justify-between font-medium">
                <span className="text-foreground">{t("loanDetail.dueDateLabel")}</span>
                <span className={cn(isOverdue ? "text-rose-600 font-semibold" : "text-muted-foreground")}>
                  {formatDate(loan.dueAt)}
                  {isOverdue && t("loanDetail.daysLateSuffix", { days: daysOverdue })}
                </span>
              </div>
            </div>

            <div className="rounded-md border p-2.5 bg-muted/20 space-y-1.5">
              <div className="flex items-center justify-between font-medium">
                <span className="text-foreground">{t("loanDetail.returnedLabel")}</span>
                <span className="text-muted-foreground">{formatDate(loan.returnedAt)}</span>
              </div>
              {loan.returnedAt && (
                <>
                  <div className="flex justify-between text-[11px] text-muted-foreground">
                    <span>{t("loanDetail.scanSourceLabel")}</span>
                    <span className="font-medium text-foreground capitalize" data-testid="return-source">
                      {loan.returnSource || "—"}
                    </span>
                  </div>
                  <div className="flex justify-between text-[11px] text-muted-foreground">
                    <span>{t("loanDetail.kioskActorLabel")}</span>
                    <span className="font-identifier text-foreground">
                      {loan.returnKioskId
                        ? t("loanDetail.kioskPrefix", { id: loan.returnKioskId.slice(0, 8) })
                        : loan.returnActor || "—"}
                    </span>
                  </div>
                </>
              )}
            </div>
          </CardContent>
        </Card>

        {/* Provenance & Notes Card */}
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-semibold flex items-center gap-2">
              <FileText className="size-4 text-primary" />
              {t("loanDetail.provenanceHeading")}
            </CardTitle>
          </CardHeader>
          <CardContent className="text-xs space-y-2.5">
            <div className="flex justify-between border-b pb-1.5">
              <span className="text-muted-foreground">{t("loanDetail.recordOriginLabel")}</span>
              <span className="capitalize font-medium text-foreground">{loan.origin}</span>
            </div>

            {loan.origin === "paper" && (
              <>
                <div className="flex justify-between border-b pb-1.5">
                  <span className="text-muted-foreground">{t("loanDetail.paperRefLabel")}</span>
                  <span className="font-identifier text-foreground font-medium" data-testid="paper-ref">
                    {loan.paperRef || "—"}
                  </span>
                </div>
                <div className="flex justify-between border-b pb-1.5">
                  <span className="text-muted-foreground">{t("loanDetail.typedInByLabel")}</span>
                  <span className="text-foreground" data-testid="recorded-by">
                    {loan.recordedBy || "—"}
                  </span>
                </div>
                <div className="flex justify-between border-b pb-1.5">
                  <span className="text-muted-foreground">{t("loanDetail.typedInTimestampLabel")}</span>
                  <span className="text-foreground" data-testid="recorded-at">
                    {formatDate(loan.recordedAt)}
                  </span>
                </div>
              </>
            )}

            {loan.backfillNote && (
              <div className="border-b pb-1.5">
                <span className="text-muted-foreground block mb-0.5">{t("loanDetail.backfillNoteLabel")}</span>
                <p className="text-foreground italic">{loan.backfillNote}</p>
              </div>
            )}

            {loan.notes && (
              <div className="pb-1">
                <span className="text-muted-foreground block mb-0.5">{t("loanDetail.notesLabel")}</span>
                <p className="text-foreground bg-muted/30 p-2 rounded">{loan.notes}</p>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Inline Audit Trail */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm font-semibold flex items-center gap-2">
            <History className="size-4 text-primary" />
            {t("loanDetail.auditHeading")}
          </CardTitle>
        </CardHeader>
        <CardContent className="text-xs">
          {(!auditEvents || auditEvents.length === 0) ? (
            <p className="text-muted-foreground py-2">{t("loanDetail.noAuditEvents")}</p>
          ) : (
            <div className="divide-y">
              {auditEvents.map((ev) => (
                <div key={ev.id} className="py-2.5 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-1">
                  <div className="flex items-center gap-2">
                    <span className="font-mono text-[11px] font-semibold text-primary">
                      {ev.action}
                    </span>
                    <span className="text-muted-foreground">{t("loanDetail.auditBy")}</span>
                    <span className="font-medium text-foreground">{ev.actor}</span>
                  </div>
                  <div className="flex items-center gap-3 text-muted-foreground text-[11px]">
                    {ev.payload && typeof ev.payload === "object" && (
                      <span className="max-w-xs truncate text-muted-foreground/80 font-mono text-[10px]">
                        {JSON.stringify(ev.payload)}
                      </span>
                    )}
                    <span className="whitespace-nowrap">{formatDate(ev.at)}</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Force Return Dialog */}
      <Dialog open={forceReturnOpen} onOpenChange={setForceReturnOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("loanDetail.forceReturnTitle")}</DialogTitle>
            <DialogDescription>
              {t("loanDetail.forceReturnDescriptionPrefix")}{" "}
              <strong className="text-foreground">{device?.name || t("loans.deviceFallback")}</strong>
              {t("loanDetail.forceReturnDescriptionSuffix")}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3 py-2 text-xs">
            <div>
              <label htmlFor="condition-select" className="block font-medium text-foreground mb-1">
                {t("loanDetail.conditionLabel")}
              </label>
              <Select
                value={forceReturnCondition}
                onValueChange={(val) => setForceReturnCondition(val as any)}
              >
                <SelectTrigger id="condition-select" className="h-9 text-xs">
                  <SelectValue placeholder={t("loanDetail.conditionPlaceholder")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="good">{t("loanDetail.conditionGood")}</SelectItem>
                  <SelectItem value="fair">{t("loanDetail.conditionFair")}</SelectItem>
                  <SelectItem value="damaged">{t("loanDetail.conditionDamaged")}</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div>
              <label htmlFor="force-reason" className="block font-medium text-foreground mb-1">
                {t("loanDetail.forceReturnReasonLabel")}
              </label>
              <Textarea
                id="force-reason"
                placeholder={t("loanDetail.forceReturnReasonPlaceholder")}
                value={forceReturnReason}
                onChange={(e) => setForceReturnReason(e.target.value)}
                rows={3}
                autoFocus
              />
            </div>
          </div>

          <DialogFooter className="gap-2 sm:gap-0">
            <Button
              variant="outline"
              onClick={() => setForceReturnOpen(false)}
              disabled={forceReturnMutation.isPending}
            >
              {t("loanDetail.cancel")}
            </Button>
            <Button
              disabled={!forceReturnReason.trim() || forceReturnMutation.isPending}
              onClick={() => forceReturnMutation.mutate()}
            >
              {forceReturnMutation.isPending ? t("loanDetail.returning") : t("loanDetail.confirmReturn")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Write Off Dialog */}
      <Dialog open={writeOffOpen} onOpenChange={setWriteOffOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("loanDetail.writeOffTitle")}</DialogTitle>
            <DialogDescription>
              {t("loanDetail.writeOffDescriptionPrefix")}{" "}
              <strong className="text-foreground">{device?.name || t("loans.deviceFallback")}</strong>
              {t("loanDetail.writeOffDescriptionSuffix")}
            </DialogDescription>
          </DialogHeader>

          <div className="py-2 text-xs">
            <label htmlFor="writeoff-reason" className="block font-medium text-foreground mb-1">
              {t("loanDetail.writeOffReasonLabel")}
            </label>
            <Textarea
              id="writeoff-reason"
              placeholder={t("loanDetail.writeOffReasonPlaceholder")}
              value={writeOffReason}
              onChange={(e) => setWriteOffReason(e.target.value)}
              rows={3}
              autoFocus
            />
          </div>

          <DialogFooter className="gap-2 sm:gap-0">
            <Button
              variant="outline"
              onClick={() => setWriteOffOpen(false)}
              disabled={writeOffMutation.isPending}
            >
              {t("loanDetail.cancel")}
            </Button>
            <Button
              variant="destructive"
              disabled={!writeOffReason.trim() || writeOffMutation.isPending}
              onClick={() => writeOffMutation.mutate()}
            >
              {writeOffMutation.isPending ? t("loanDetail.writingOff") : t("loanDetail.confirmWriteOff")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Correct Attribution Dialog */}
      <Dialog open={correctAttributionOpen} onOpenChange={setCorrectAttributionOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("loanDetail.correctTitle")}</DialogTitle>
            <DialogDescription>
              {t("loanDetail.correctDescription")}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3 py-2 text-xs">
            <div>
              <label htmlFor="user-select" className="block font-medium text-foreground mb-1">
                {t("loanDetail.correctBorrowerLabel")}
              </label>
              <Select
                value={correctUserId}
                onValueChange={setCorrectUserId}
              >
                <SelectTrigger id="user-select" className="h-9 text-xs">
                  <SelectValue placeholder={t("loanDetail.correctBorrowerPlaceholder")} />
                </SelectTrigger>
                <SelectContent className="max-h-60">
                  {(allUsers ?? []).map((u) => (
                    <SelectItem key={u.id} value={u.id}>
                      {u.fullName} ({u.employeeNo || u.id.slice(0, 8)})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div>
              <label htmlFor="correct-reason" className="block font-medium text-foreground mb-1">
                {t("loanDetail.correctReasonLabel")}
              </label>
              <Textarea
                id="correct-reason"
                placeholder={t("loanDetail.correctReasonPlaceholder")}
                value={correctReason}
                onChange={(e) => setCorrectReason(e.target.value)}
                rows={3}
              />
            </div>
          </div>

          <DialogFooter className="gap-2 sm:gap-0">
            <Button
              variant="outline"
              onClick={() => setCorrectAttributionOpen(false)}
              disabled={correctAttributionMutation.isPending}
            >
              {t("loanDetail.cancel")}
            </Button>
            <Button
              disabled={!correctUserId || !correctReason.trim() || correctAttributionMutation.isPending}
              onClick={() => correctAttributionMutation.mutate()}
            >
              {correctAttributionMutation.isPending ? t("loanDetail.correcting") : t("loanDetail.saveCorrection")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

export const loanDetailRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/loans/$loanId",
  component: LoanDetailPage,
});

