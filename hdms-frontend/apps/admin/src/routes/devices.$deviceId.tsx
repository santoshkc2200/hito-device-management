import {
  type Device,
  type DeviceStatus,
  forceReturnLoan,
  getDevice,
  getUser,
  listCategories,
  listDeviceLoans,
  listDeviceReservations,
  setDeviceStatus,
} from "@hdms/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute, Link } from "@tanstack/react-router";
import {
  ArrowLeft,
  Calendar,
  CalendarClock,
  Clock,
  Edit3,
  ExternalLink,
  History,
  Laptop,
  RotateCw,
  Tag,
  User as UserIcon,
} from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { CredentialsPanel } from "@/components/credentials-panel";
import { DeviceForm } from "@/components/device-form";
import { DeviceLabelSheetDialog } from "@/components/device-label-sheet-dialog";
import { ErrorState, LoadingState } from "@/components/states";
import {
  deviceStatusTone,
  labelize,
  reservationStatusTone,
  StatusBadge,
} from "@/components/status-badge";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { RoleGate } from "@/lib/use-role";
import { useT } from "@/i18n";
import { authenticatedRoute } from "./authenticated";

const DEVICE_TRANSITIONS: Record<DeviceStatus, DeviceStatus[]> = {
  available: ["on_loan", "maintenance", "lost", "retired"],
  on_loan: ["available", "lost"],
  maintenance: ["available", "retired"],
  lost: ["available", "retired"],
  retired: [],
};

function loanOriginTone(origin: string): "default" | "secondary" | "outline" {
  switch (origin) {
    case "kiosk":
      return "default";
    case "paper":
      return "secondary";
    default:
      return "outline";
  }
}

function ReasonActionDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  confirmVariant = "default",
  onConfirm,
  isPending,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  confirmLabel: string;
  confirmVariant?: "default" | "destructive";
  onConfirm: (reason: string) => void;
  isPending: boolean;
}) {
  const t = useT();
  const [reason, setReason] = useState("");

  const handleClose = () => {
    setReason("");
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={(o) => (!o ? handleClose() : onOpenChange(true))}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <div className="py-2">
          <Textarea
            placeholder={t("deviceDetail.reasonPlaceholder")}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            rows={3}
            autoFocus
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleClose} disabled={isPending}>
            {t("deviceDetail.cancel")}
          </Button>
          <Button
            variant={confirmVariant}
            disabled={!reason.trim() || isPending}
            onClick={() => onConfirm(reason.trim())}
          >
            {isPending ? t("deviceDetail.processing") : confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function StatusChangeDialog({
  device,
  open,
  onOpenChange,
}: {
  device: Device;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  const [targetStatus, setTargetStatus] = useState<DeviceStatus | "">("");
  const [reason, setReason] = useState("");

  const options = DEVICE_TRANSITIONS[device.status] ?? [];

  const mutation = useMutation({
    mutationFn: async () => {
      if (!targetStatus) return;
      const res = await setDeviceStatus({
        path: { id: device.id },
        body: { status: targetStatus as DeviceStatus, reason: reason.trim() },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["devices"] });
      toast.success(t("devices.statusDialog.updated"));
      handleClose();
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || t("devices.statusDialog.updateFailed"));
    },
  });

  const handleClose = () => {
    setTargetStatus("");
    setReason("");
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={(o) => (!o ? handleClose() : onOpenChange(true))}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("deviceDetail.statusDialog.title", { assetTag: device.assetTag })}</DialogTitle>
          <DialogDescription>
            {t("deviceDetail.statusDialog.descriptionPrefix")}{" "}
            <strong>{labelize(device.status)}</strong>
            {t("deviceDetail.statusDialog.descriptionSuffix")}
          </DialogDescription>
        </DialogHeader>

        {options.length === 0 ? (
          <p className="text-sm text-muted-foreground py-2">
            {t("deviceDetail.statusDialog.noTransitions")}
          </p>
        ) : (
          <div className="flex flex-col gap-3 py-2">
            <div>
              <label className="text-xs font-semibold text-muted-foreground uppercase">
                {t("deviceDetail.statusDialog.targetLabel")}
              </label>
              <div className="mt-1">
                <Select
                  value={targetStatus}
                  onValueChange={(v) => setTargetStatus(v as DeviceStatus)}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder={t("deviceDetail.statusDialog.selectNewStatusPlaceholder")} />
                  </SelectTrigger>
                  <SelectContent>
                    {options.map((s) => (
                      <SelectItem key={s} value={s}>
                        {labelize(s)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div>
              <label className="text-xs font-semibold text-muted-foreground uppercase">
                {t("deviceDetail.statusDialog.reasonLabel")}
              </label>
              <div className="mt-1">
                <Textarea
                  placeholder={t("deviceDetail.statusDialog.reasonPlaceholder")}
                  value={reason}
                  onChange={(e) => setReason(e.target.value)}
                  rows={3}
                />
              </div>
            </div>
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={handleClose} disabled={mutation.isPending}>
            {t("deviceDetail.statusDialog.cancel")}
          </Button>
          <Button
            disabled={!targetStatus || !reason.trim() || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending
              ? t("deviceDetail.statusDialog.applying")
              : t("deviceDetail.statusDialog.apply")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function DeviceDetailPage() {
  const t = useT();
  const { deviceId } = deviceDetailRoute.useParams();
  const queryClient = useQueryClient();

  const [editOpen, setEditOpen] = useState(false);
  const [statusOpen, setStatusOpen] = useState(false);
  const [labelSheetOpen, setLabelSheetOpen] = useState(false);
  const [forceReturnOpen, setForceReturnOpen] = useState(false);

  const {
    data: device,
    isLoading: isDeviceLoading,
    isError: isDeviceError,
    error: deviceError,
    refetch: refetchDevice,
  } = useQuery({
    queryKey: ["devices", deviceId],
    queryFn: async () => {
      const { data, error } = await getDevice({ path: { id: deviceId } });
      if (error) throw error;
      return data;
    },
  });

  const { data: categories } = useQuery({
    queryKey: ["categories"],
    queryFn: async () => {
      const { data, error } = await listCategories();
      if (error) throw error;
      return data.items;
    },
  });

  const categoryName = useMemo(() => {
    if (!device?.categoryId || !categories) return undefined;
    return categories.find((c) => c.id === device.categoryId)?.name;
  }, [device, categories]);

  const {
    data: loansData,
    isLoading: isLoansLoading,
  } = useQuery({
    queryKey: ["devices", deviceId, "loans"],
    queryFn: async () => {
      const { data, error } = await listDeviceLoans({ path: { id: deviceId } });
      if (error) throw error;
      return data;
    },
    enabled: Boolean(deviceId),
  });

  const loans = useMemo(() => loansData?.items ?? [], [loansData]);
  const activeLoan = useMemo(() => loans.find((l) => l.status === "open"), [loans]);

  const {
    data: reservationsData,
    isLoading: isReservationsLoading,
  } = useQuery({
    queryKey: ["devices", deviceId, "reservations"],
    queryFn: async () => {
      const { data, error } = await listDeviceReservations({ path: { id: deviceId } });
      if (error) throw error;
      return data;
    },
    enabled: Boolean(deviceId),
  });

  const reservations = useMemo(() => reservationsData?.items ?? [], [reservationsData]);

  // Fetch borrower details if device has active loan
  const { data: activeBorrower, isLoading: isBorrowerLoading } = useQuery({
    queryKey: ["users", activeLoan?.userId],
    queryFn: async () => {
      if (!activeLoan?.userId) return null;
      const { data, error } = await getUser({ path: { id: activeLoan.userId } });
      if (error) throw error;
      return data;
    },
    enabled: Boolean(activeLoan?.userId),
  });

  const forceReturnMutation = useMutation({
    mutationFn: async (reason: string) => {
      if (!activeLoan) return;
      const res = await forceReturnLoan({
        path: { id: activeLoan.id },
        body: { reason },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["devices"] });
      await queryClient.invalidateQueries({ queryKey: ["loans"] });
      toast.success(t("deviceDetail.forceReturnSuccess"));
      setForceReturnOpen(false);
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || t("deviceDetail.forceReturnFailed"));
    },
  });

  if (isDeviceLoading) {
    return <LoadingState message={t("deviceDetail.loadingDevice")} />;
  }

  if (isDeviceError || !device) {
    return (
      <ErrorState
        error={deviceError}
        title={t("deviceDetail.notFoundTitle")}
        detail={t("deviceDetail.notFoundDetail")}
        onRetry={() => refetchDevice()}
      />
    );
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Navigation Breadcrumb */}
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Link to="/devices" className="flex items-center gap-1.5 hover:text-foreground">
          <ArrowLeft className="size-4" />
          <span>{t("deviceDetail.breadcrumbDevices")}</span>
        </Link>
        <span>/</span>
        <span className="font-identifier text-foreground">{device.assetTag}</span>
      </div>

      {/* Header Banner */}
      <div className="flex flex-col justify-between gap-4 rounded-lg border border-border bg-card p-6 sm:flex-row sm:items-center">
        <div className="flex items-start gap-4">
          <div className="rounded-full bg-primary/10 p-3 text-primary">
            <Laptop className="size-6" />
          </div>
          <div>
            <div className="flex flex-wrap items-center gap-3">
              <h1 className="text-2xl font-bold tracking-tight text-foreground">{device.name}</h1>
              <span className="font-identifier text-lg text-muted-foreground">{device.assetTag}</span>
              <StatusBadge
                label={labelize(device.status)}
                tone={deviceStatusTone[device.status] ?? "muted"}
              />
              <Badge variant="outline" className="text-xs">
                {labelize(device.condition)}
              </Badge>
              {categoryName && (
                <Badge variant="secondary" className="text-xs">
                  {categoryName}
                </Badge>
              )}
            </div>
            <p className="mt-1 text-sm text-muted-foreground">
              {device.manufacturer
                ? t("deviceDetail.manufacturerPrefix", { manufacturer: device.manufacturer })
                : ""}
              {device.model ? t("deviceDetail.modelPrefix", { model: device.model }) : ""}
              {t("deviceDetail.registeredOn", {
                date: new Date(device.createdAt).toLocaleDateString(),
              })}
            </p>
          </div>
        </div>

        {/* Action Buttons */}
        <div className="flex flex-wrap items-center gap-2">
          <RoleGate minRole="technician">
            <Button size="sm" variant="outline" onClick={() => setEditOpen(true)}>
              <Edit3 className="size-4" data-icon="inline-start" />
              {t("deviceDetail.edit")}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setStatusOpen(true)}>
              <RotateCw className="size-4" data-icon="inline-start" />
              {t("deviceDetail.changeStatus")}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setLabelSheetOpen(true)}>
              <Tag className="size-4" data-icon="inline-start" />
              {t("deviceDetail.printLabel")}
            </Button>
          </RoleGate>
        </div>
      </div>

      {/* Main Grid Layout */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        {/* Left Column (2 spans): Details + Current Custody + History */}
        <div className="flex flex-col gap-6 lg:col-span-2">
          {/* Attributes Card */}
          <div className="rounded-lg border border-border bg-card p-6">
            <h2 className="text-base font-semibold text-foreground">{t("deviceDetail.attributesHeading")}</h2>
            <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("deviceDetail.assetTagLabel")}</span>
                <p className="font-identifier text-sm font-semibold text-foreground mt-0.5">
                  {device.assetTag}
                </p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("deviceDetail.nameLabel")}</span>
                <p className="text-sm font-medium text-foreground mt-0.5">{device.name}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("deviceDetail.categoryLabel")}</span>
                <p className="text-sm text-foreground mt-0.5">{categoryName || "—"}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("deviceDetail.conditionLabel")}</span>
                <p className="text-sm text-foreground mt-0.5">{labelize(device.condition)}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("deviceDetail.manufacturerLabel")}</span>
                <p className="text-sm text-foreground mt-0.5">{device.manufacturer || "—"}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("deviceDetail.modelLabel")}</span>
                <p className="text-sm text-foreground mt-0.5">{device.model || "—"}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("deviceDetail.serialNumberLabel")}</span>
                <p className="font-identifier text-sm text-foreground mt-0.5">{device.serialNo || "—"}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("deviceDetail.homeLocationLabel")}</span>
                <p className="text-sm text-foreground mt-0.5">{device.homeLocation || "—"}</p>
              </div>
              {device.acquiredOn && (
                <div>
                  <span className="text-xs font-medium text-muted-foreground uppercase">{t("deviceDetail.acquiredOnLabel")}</span>
                  <p className="text-sm text-foreground mt-0.5">{device.acquiredOn}</p>
                </div>
              )}
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("deviceDetail.statusLabel")}</span>
                <div className="mt-0.5">
                  <StatusBadge
                    label={labelize(device.status)}
                    tone={deviceStatusTone[device.status] ?? "muted"}
                  />
                </div>
              </div>
            </div>

            {device.notes && (
              <div className="mt-4 border-t border-border pt-3">
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("deviceDetail.notesLabel")}</span>
                <p className="text-sm text-foreground mt-0.5 whitespace-pre-wrap">{device.notes}</p>
              </div>
            )}

            <div className="mt-6 border-t border-border pt-4">
              <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                {t("deviceDetail.lifecycleHeading")}
              </h3>
              <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 text-xs">
                <div>
                  <span className="text-muted-foreground">{t("deviceDetail.createdLabel")}</span>
                  <p className="font-medium text-foreground mt-0.5">
                    {new Date(device.createdAt).toLocaleString()}
                  </p>
                </div>
                <div>
                  <span className="text-muted-foreground">{t("deviceDetail.lastUpdatedLabel")}</span>
                  <p className="font-medium text-foreground mt-0.5">
                    {new Date(device.updatedAt).toLocaleString()}
                  </p>
                </div>
              </div>
            </div>
          </div>

          {/* Currently Held / Live Custody Card */}
          <div className="rounded-lg border border-border bg-card p-6">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <UserIcon className="size-4 text-primary" />
                <h2 className="text-base font-semibold text-foreground">{t("deviceDetail.custodyHeading")}</h2>
              </div>
              {activeLoan && (
                <Badge variant="outline" className="border-primary text-primary text-xs">
                  {t("deviceDetail.onLoan")}
                </Badge>
              )}
            </div>

            {isLoansLoading && (
              <div className="mt-4">
                <LoadingState message={t("deviceDetail.checkingCustody")} />
              </div>
            )}

            {!isLoansLoading && !activeLoan && (
              <p className="mt-4 text-sm text-muted-foreground">
                {t("deviceDetail.notOnLoan")}
              </p>
            )}

            {!isLoansLoading && activeLoan && (
              <div className="mt-4 rounded-md border border-border bg-muted/30 p-4">
                <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
                  <div>
                    <div className="flex items-center gap-2">
                      <span className="text-xs font-medium text-muted-foreground uppercase">{t("deviceDetail.borrowerLabel")}</span>
                      {isBorrowerLoading ? (
                        <span className="text-sm text-muted-foreground">{t("deviceDetail.loadingBorrower")}</span>
                      ) : activeBorrower ? (
                        <Link
                          to="/users/$userId"
                          params={{ userId: activeBorrower.id }}
                          className="text-sm font-semibold text-primary hover:underline"
                        >
                          {activeBorrower.fullName} ({activeBorrower.employeeNo})
                        </Link>
                      ) : (
                        <span className="font-identifier text-sm font-semibold">
                          {t("deviceDetail.userFallback", { userId: activeLoan.userId })}
                        </span>
                      )}
                    </div>

                    <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                      <span className="flex items-center gap-1">
                        <Clock className="size-3.5" />
                        {t("deviceDetail.borrowedAt", { date: new Date(activeLoan.borrowedAt).toLocaleString() })}
                      </span>
                      {activeLoan.dueAt && (
                        <span className="flex items-center gap-1">
                          <Calendar className="size-3.5" />
                          {t("deviceDetail.dueAt", { date: new Date(activeLoan.dueAt).toLocaleString() })}
                          {new Date(activeLoan.dueAt) < new Date() && (
                            <Badge variant="destructive" className="ml-1 text-[10px] py-0 px-1">
                              {t("deviceDetail.overdue")}
                            </Badge>
                          )}
                        </span>
                      )}
                      {activeLoan.paperRef && <span>{t("deviceDetail.paperSlip", { ref: activeLoan.paperRef })}</span>}
                    </div>
                  </div>

                  <div className="flex items-center gap-2">
                    <Button size="sm" variant="ghost" asChild>
                      <Link to="/loans/$loanId" params={{ loanId: activeLoan.id }} className="text-xs">
                        <ExternalLink className="mr-1 size-3.5" />
                        {t("deviceDetail.viewLoan")}
                      </Link>
                    </Button>
                    <RoleGate minRole="technician">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => setForceReturnOpen(true)}
                        className="text-xs text-destructive hover:text-destructive"
                      >
                        {t("deviceDetail.forceReturn")}
                      </Button>
                    </RoleGate>
                  </div>
                </div>
              </div>
            )}
          </div>

          {/* Loan History Card */}
          <div className="rounded-lg border border-border bg-card p-6">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <History className="size-4 text-primary" />
                <h2 className="text-base font-semibold text-foreground">{t("deviceDetail.historyHeading")}</h2>
              </div>
              <span className="text-xs text-muted-foreground">{t("deviceDetail.totalCount", { count: loans.length })}</span>
            </div>

            {isLoansLoading && (
              <div className="mt-4">
                <LoadingState message={t("deviceDetail.loadingHistory")} />
              </div>
            )}

            {!isLoansLoading && loans.length === 0 && (
              <p className="mt-4 text-sm text-muted-foreground">
                {t("deviceDetail.noHistory")}
              </p>
            )}

            {!isLoansLoading && loans.length > 0 && (
              <div className="mt-4 overflow-x-auto">
                <table className="w-full text-left text-sm">
                  <thead className="border-b border-border text-xs text-muted-foreground uppercase">
                    <tr>
                      <th className="pb-2 font-medium">{t("deviceDetail.colBorrower")}</th>
                      <th className="pb-2 font-medium">{t("deviceDetail.colStatus")}</th>
                      <th className="pb-2 font-medium">{t("deviceDetail.colOrigin")}</th>
                      <th className="pb-2 font-medium">{t("deviceDetail.colBorrowed")}</th>
                      <th className="pb-2 font-medium">{t("deviceDetail.colReturnedDue")}</th>
                      <th className="pb-2 font-medium text-right">{t("deviceDetail.colAction")}</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {loans.map((loan) => (
                      <tr key={loan.id} className="hover:bg-muted/50">
                        <td className="py-2.5 font-identifier text-xs">
                          <Link
                            to="/users/$userId"
                            params={{ userId: loan.userId }}
                            className="hover:underline font-semibold text-primary"
                          >
                            {loan.userId}
                          </Link>
                        </td>
                        <td className="py-2.5">
                          <StatusBadge
                            label={labelize(loan.status)}
                            tone={
                              loan.status === "open"
                                ? "primary"
                                : loan.status === "returned"
                                  ? "success"
                                  : "destructive"
                            }
                          />
                        </td>
                        <td className="py-2.5">
                          <Badge variant={loanOriginTone(loan.origin)} className="text-xs font-normal">
                            {loan.origin}
                          </Badge>
                        </td>
                        <td className="py-2.5 text-xs text-muted-foreground">
                          {new Date(loan.borrowedAt).toLocaleDateString()}
                        </td>
                        <td className="py-2.5 text-xs text-muted-foreground">
                          {loan.returnedAt
                            ? new Date(loan.returnedAt).toLocaleDateString()
                            : loan.dueAt
                              ? new Date(loan.dueAt).toLocaleDateString()
                              : "—"}
                        </td>
                        <td className="py-2.5 text-right">
                          <Link
                            to="/loans/$loanId"
                            params={{ loanId: loan.id }}
                            className="text-xs font-medium text-primary hover:underline"
                          >
                            {t("deviceDetail.view")}
                          </Link>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          {/* Reservation History Card */}
          <div className="rounded-lg border border-border bg-card p-6" data-testid="device-reservations-card">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <CalendarClock className="size-4 text-primary" />
                <h2 className="text-base font-semibold text-foreground">{t("deviceDetail.reservationsHeading")}</h2>
              </div>
              <span className="text-xs text-muted-foreground">{t("deviceDetail.totalReservationsCount", { count: reservations.length })}</span>
            </div>

            {isReservationsLoading && (
              <div className="mt-4">
                <LoadingState message={t("deviceDetail.loadingReservations")} />
              </div>
            )}

            {!isReservationsLoading && reservations.length === 0 && (
              <p className="mt-4 text-sm text-muted-foreground">
                {t("deviceDetail.noReservations")}
              </p>
            )}

            {!isReservationsLoading && reservations.length > 0 && (
              <div className="mt-4 overflow-x-auto">
                <table className="w-full text-left text-sm">
                  <thead className="border-b border-border text-xs text-muted-foreground uppercase">
                    <tr>
                      <th className="pb-2 font-medium">{t("deviceDetail.colReserver")}</th>
                      <th className="pb-2 font-medium">{t("deviceDetail.colReservationStatus")}</th>
                      <th className="pb-2 font-medium">{t("deviceDetail.colReservedWindow")}</th>
                      <th className="pb-2 font-medium">{t("deviceDetail.colReservationCreated")}</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {reservations.map((res) => (
                      <tr key={res.id} className="hover:bg-muted/50">
                        <td className="py-2.5 font-identifier text-xs">
                          <Link
                            to="/users/$userId"
                            params={{ userId: res.userId }}
                            className="hover:underline font-semibold text-primary"
                          >
                            {res.userName || res.userId}
                          </Link>
                        </td>
                        <td className="py-2.5">
                          <StatusBadge
                            label={labelize(res.status)}
                            tone={reservationStatusTone[res.status] ?? "muted"}
                          />
                        </td>
                        <td className="py-2.5 text-xs text-muted-foreground">
                          {new Date(res.startAt).toLocaleString()} — {new Date(res.endAt).toLocaleString()}
                        </td>
                        <td className="py-2.5 text-xs text-muted-foreground">
                          {new Date(res.createdAt).toLocaleDateString()}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>

        {/* Right Column: Credentials Panel */}
        <div className="flex flex-col gap-6">
          <div className="rounded-lg border border-border bg-card p-6">
            <CredentialsPanel
              subjectType="device"
              subjectId={device.id}
              subject={{
                type: "device",
                assetTag: device.assetTag,
                name: device.name,
                model: device.model,
              }}
            />
          </div>
        </div>
      </div>

      {/* Edit Device Dialog */}
      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{t("deviceDetail.editTitle")}</DialogTitle>
            <DialogDescription>{t("deviceDetail.editDescription")}</DialogDescription>
          </DialogHeader>
          <DeviceForm
            device={device}
            categories={categories ?? []}
            onDone={() => setEditOpen(false)}
          />
        </DialogContent>
      </Dialog>

      {/* Status Change Dialog with Mandatory Reason */}
      <StatusChangeDialog
        device={device}
        open={statusOpen}
        onOpenChange={setStatusOpen}
      />

      {/* Single Device Label Sheet Dialog */}
      <DeviceLabelSheetDialog
        devices={[device]}
        open={labelSheetOpen}
        onOpenChange={setLabelSheetOpen}
      />

      {/* Force Return Reason Dialog */}
      <ReasonActionDialog
        open={forceReturnOpen}
        onOpenChange={setForceReturnOpen}
        title={t("deviceDetail.forceReturnTitle")}
        description={t("deviceDetail.forceReturnDescription", {
          name: device.name,
          assetTag: device.assetTag,
        })}
        confirmLabel={t("deviceDetail.forceReturn")}
        confirmVariant="destructive"
        onConfirm={(reason) => forceReturnMutation.mutate(reason)}
        isPending={forceReturnMutation.isPending}
      />
    </div>
  );
}

export const deviceDetailRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/devices/$deviceId",
  component: DeviceDetailPage,
});
