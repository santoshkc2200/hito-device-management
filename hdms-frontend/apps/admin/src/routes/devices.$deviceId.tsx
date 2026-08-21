import {
  type Category,
  type Device,
  type DeviceStatus,
  type Loan,
  forceReturnLoan,
  getDevice,
  getUser,
  listCategories,
  listDeviceLoans,
  setDeviceStatus,
} from "@hdms/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute, Link } from "@tanstack/react-router";
import {
  ArrowLeft,
  Calendar,
  Clock,
  Edit3,
  ExternalLink,
  History,
  Laptop,
  Printer,
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
            placeholder="Reason (required)"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            rows={3}
            autoFocus
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleClose} disabled={isPending}>
            Cancel
          </Button>
          <Button
            variant={confirmVariant}
            disabled={!reason.trim() || isPending}
            onClick={() => onConfirm(reason.trim())}
          >
            {isPending ? "Processing…" : confirmLabel}
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
      toast.success("Device status updated");
      handleClose();
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || "Could not change status");
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
          <DialogTitle>Change status: {device.assetTag}</DialogTitle>
          <DialogDescription>
            Current status is <strong>{labelize(device.status)}</strong>. A mandatory reason is required
            for audit tracking.
          </DialogDescription>
        </DialogHeader>

        {options.length === 0 ? (
          <p className="text-sm text-muted-foreground py-2">
            Retired devices have no further transitions.
          </p>
        ) : (
          <div className="flex flex-col gap-3 py-2">
            <div>
              <label className="text-xs font-semibold text-muted-foreground uppercase">
                Target status
              </label>
              <div className="mt-1">
                <Select
                  value={targetStatus}
                  onValueChange={(v) => setTargetStatus(v as DeviceStatus)}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="Select new status…" />
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
                Reason (required)
              </label>
              <div className="mt-1">
                <Textarea
                  placeholder="State reason for this transition…"
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
            Cancel
          </Button>
          <Button
            disabled={!targetStatus || !reason.trim() || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending ? "Applying…" : "Apply status change"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function DeviceDetailPage() {
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
      toast.success("Device force-returned successfully");
      setForceReturnOpen(false);
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || "Could not force return device");
    },
  });

  if (isDeviceLoading) {
    return <LoadingState message="Loading device details…" />;
  }

  if (isDeviceError || !device) {
    return (
      <ErrorState
        error={deviceError}
        title="Device not found"
        detail="Could not load details for this equipment item."
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
          <span>Devices</span>
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
              {device.manufacturer ? `${device.manufacturer} · ` : ""}
              {device.model ? `Model ${device.model} · ` : ""}
              Registered on {new Date(device.createdAt).toLocaleDateString()}
            </p>
          </div>
        </div>

        {/* Action Buttons */}
        <div className="flex flex-wrap items-center gap-2">
          <RoleGate minRole="technician">
            <Button size="sm" variant="outline" onClick={() => setEditOpen(true)}>
              <Edit3 className="size-4" data-icon="inline-start" />
              Edit
            </Button>
            <Button size="sm" variant="outline" onClick={() => setStatusOpen(true)}>
              <RotateCw className="size-4" data-icon="inline-start" />
              Change status
            </Button>
            <Button size="sm" variant="outline" onClick={() => setLabelSheetOpen(true)}>
              <Tag className="size-4" data-icon="inline-start" />
              Print label
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
            <h2 className="text-base font-semibold text-foreground">Equipment Attributes</h2>
            <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">Asset Tag</span>
                <p className="font-identifier text-sm font-semibold text-foreground mt-0.5">
                  {device.assetTag}
                </p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">Name</span>
                <p className="text-sm font-medium text-foreground mt-0.5">{device.name}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">Category</span>
                <p className="text-sm text-foreground mt-0.5">{categoryName || "—"}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">Condition</span>
                <p className="text-sm text-foreground mt-0.5">{labelize(device.condition)}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">Manufacturer</span>
                <p className="text-sm text-foreground mt-0.5">{device.manufacturer || "—"}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">Model</span>
                <p className="text-sm text-foreground mt-0.5">{device.model || "—"}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">Serial Number</span>
                <p className="font-identifier text-sm text-foreground mt-0.5">{device.serialNo || "—"}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">Home Location</span>
                <p className="text-sm text-foreground mt-0.5">{device.homeLocation || "—"}</p>
              </div>
              {device.acquiredOn && (
                <div>
                  <span className="text-xs font-medium text-muted-foreground uppercase">Acquired On</span>
                  <p className="text-sm text-foreground mt-0.5">{device.acquiredOn}</p>
                </div>
              )}
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">Status</span>
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
                <span className="text-xs font-medium text-muted-foreground uppercase">Notes</span>
                <p className="text-sm text-foreground mt-0.5 whitespace-pre-wrap">{device.notes}</p>
              </div>
            )}

            <div className="mt-6 border-t border-border pt-4">
              <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                Lifecycle & Timestamps
              </h3>
              <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 text-xs">
                <div>
                  <span className="text-muted-foreground">Created:</span>
                  <p className="font-medium text-foreground mt-0.5">
                    {new Date(device.createdAt).toLocaleString()}
                  </p>
                </div>
                <div>
                  <span className="text-muted-foreground">Last updated:</span>
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
                <h2 className="text-base font-semibold text-foreground">Current Holder & Custody</h2>
              </div>
              {activeLoan && (
                <Badge variant="outline" className="border-primary text-primary text-xs">
                  On Loan
                </Badge>
              )}
            </div>

            {isLoansLoading && (
              <div className="mt-4">
                <LoadingState message="Checking custody status…" />
              </div>
            )}

            {!isLoansLoading && !activeLoan && (
              <p className="mt-4 text-sm text-muted-foreground">
                Device is currently available / not on loan.
              </p>
            )}

            {!isLoansLoading && activeLoan && (
              <div className="mt-4 rounded-md border border-border bg-muted/30 p-4">
                <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
                  <div>
                    <div className="flex items-center gap-2">
                      <span className="text-xs font-medium text-muted-foreground uppercase">Borrower:</span>
                      {isBorrowerLoading ? (
                        <span className="text-sm text-muted-foreground">Loading borrower…</span>
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
                          User {activeLoan.userId}
                        </span>
                      )}
                    </div>

                    <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                      <span className="flex items-center gap-1">
                        <Clock className="size-3.5" />
                        Borrowed: {new Date(activeLoan.borrowedAt).toLocaleString()}
                      </span>
                      {activeLoan.dueAt && (
                        <span className="flex items-center gap-1">
                          <Calendar className="size-3.5" />
                          Due: {new Date(activeLoan.dueAt).toLocaleString()}
                          {new Date(activeLoan.dueAt) < new Date() && (
                            <Badge variant="destructive" className="ml-1 text-[10px] py-0 px-1">
                              Overdue
                            </Badge>
                          )}
                        </span>
                      )}
                      {activeLoan.paperRef && <span>Paper slip: {activeLoan.paperRef}</span>}
                    </div>
                  </div>

                  <div className="flex items-center gap-2">
                    <Button size="sm" variant="ghost" asChild>
                      <Link to="/loans/$loanId" params={{ loanId: activeLoan.id }} className="text-xs">
                        <ExternalLink className="mr-1 size-3.5" />
                        View loan
                      </Link>
                    </Button>
                    <RoleGate minRole="technician">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => setForceReturnOpen(true)}
                        className="text-xs text-destructive hover:text-destructive"
                      >
                        Force return
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
                <h2 className="text-base font-semibold text-foreground">Loan History</h2>
              </div>
              <span className="text-xs text-muted-foreground">{loans.length} total</span>
            </div>

            {isLoansLoading && (
              <div className="mt-4">
                <LoadingState message="Loading loan history…" />
              </div>
            )}

            {!isLoansLoading && loans.length === 0 && (
              <p className="mt-4 text-sm text-muted-foreground">
                No loan history for this device.
              </p>
            )}

            {!isLoansLoading && loans.length > 0 && (
              <div className="mt-4 overflow-x-auto">
                <table className="w-full text-left text-sm">
                  <thead className="border-b border-border text-xs text-muted-foreground uppercase">
                    <tr>
                      <th className="pb-2 font-medium">Borrower / User ID</th>
                      <th className="pb-2 font-medium">Status</th>
                      <th className="pb-2 font-medium">Origin</th>
                      <th className="pb-2 font-medium">Borrowed</th>
                      <th className="pb-2 font-medium">Returned / Due</th>
                      <th className="pb-2 font-medium text-right">Action</th>
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
                            View
                          </Link>
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
            <DialogTitle>Edit device</DialogTitle>
            <DialogDescription>Update equipment attributes and location.</DialogDescription>
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
        title="Force return device"
        description={`Administratively close the open loan for ${device.name} (${device.assetTag}).`}
        confirmLabel="Force return"
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
