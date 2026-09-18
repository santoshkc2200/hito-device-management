import {
  type Device,
  type Reservation,
  type ReservationStatus,
  type User,
  cancelReservation,
  createReservation,
  listDevices,
  listReservations,
  listUsers,
} from "@hdms/api-client";
import { createColumnHelper } from "@tanstack/react-table";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute, Link, useNavigate } from "@tanstack/react-router";
import {
  CalendarClock,
  MoreHorizontal,
  Plus,
  XCircle,
} from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { z } from "zod";
import { DataTable, DataTableColumnHeader, useDataTableColumns } from "@/components/data-table";
import {
  StatusBadge,
  labelize,
  reservationStatusTone,
} from "@/components/status-badge";
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
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useT } from "@/i18n";
import { RoleGate } from "@/lib/use-role";
import { authenticatedRoute } from "./authenticated";

const RESERVATION_STATUS_OPTIONS = ["all", "active", "collected", "cancelled", "expired"] as const;

const reservationsSearchSchema = z.object({
  status: z.enum(RESERVATION_STATUS_OPTIONS).optional(),
  deviceId: z.string().optional(),
  userId: z.string().optional(),
  q: z.string().optional(),
});
export type ReservationsSearch = z.infer<typeof reservationsSearchSchema>;

function formatDate(iso?: string | null): string {
  if (!iso) return "—";
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

function CancelReservationDialog({
  open,
  onOpenChange,
  target,
  onConfirm,
  isPending,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  target: Reservation | null;
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
          <DialogTitle>{t("reservations.cancelTitle")}</DialogTitle>
          <DialogDescription>
            {t("reservations.cancelDescription", {
              device: target?.deviceName || t("reservations.deviceFallback"),
              assetTag: target?.deviceAssetTag || "",
              user: target?.userName || t("reservations.userFallback"),
            })}
          </DialogDescription>
        </DialogHeader>
        <div className="py-2">
          <label htmlFor="cancel-reason" className="block text-xs font-medium text-foreground mb-1.5">
            {t("reservations.cancelReasonLabel")}
          </label>
          <Textarea
            id="cancel-reason"
            placeholder={t("reservations.cancelReasonPlaceholder")}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            rows={3}
            autoFocus
          />
        </div>
        <DialogFooter className="gap-2 sm:gap-0">
          <Button variant="outline" onClick={handleClose} disabled={isPending}>
            {t("reservations.cancel")}
          </Button>
          <Button
            variant="destructive"
            disabled={!reason.trim() || isPending}
            onClick={() => onConfirm(reason.trim())}
          >
            {isPending ? t("reservations.submitting") : t("reservations.confirmCancel")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function CreateReservationDialog({
  open,
  onOpenChange,
  devices,
  users,
  onSuccess,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  devices: Device[];
  users: User[];
  onSuccess: () => void;
}) {
  const t = useT();
  const [deviceId, setDeviceId] = useState("");
  const [userId, setUserId] = useState("");
  const [startAt, setStartAt] = useState("");
  const [endAt, setEndAt] = useState("");
  const [formError, setFormError] = useState<string | null>(null);

  const handleClose = () => {
    setDeviceId("");
    setUserId("");
    setStartAt("");
    setEndAt("");
    setFormError(null);
    onOpenChange(false);
  };

  const createMutation = useMutation({
    mutationFn: async () => {
      if (!deviceId) throw new Error(t("reservations.selectDeviceRequired"));
      if (!userId) throw new Error(t("reservations.selectUserRequired"));
      if (!startAt) throw new Error(t("reservations.startRequired"));
      if (!endAt) throw new Error(t("reservations.endRequired"));
      const startDate = new Date(startAt);
      const endDate = new Date(endAt);
      if (startDate >= endDate) {
        throw new Error(t("reservations.endMustBeAfterStart"));
      }

      const res = await createReservation({
        body: {
          deviceId,
          userId,
          startAt: startDate.toISOString(),
          endAt: endDate.toISOString(),
        },
      });
      if (res.error) {
        throw res.error;
      }
      return res.data;
    },
    onSuccess: () => {
      toast.success(t("reservations.createSuccess"));
      handleClose();
      onSuccess();
    },
    onError: (err: any) => {
      const msg = err?.detail || err?.title || err?.message || t("reservations.createFailed");
      setFormError(msg);
      toast.error(msg);
    },
  });

  return (
    <Dialog open={open} onOpenChange={(o) => (!o ? handleClose() : onOpenChange(true))}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("reservations.createTitle")}</DialogTitle>
          <DialogDescription>{t("reservations.createDescription")}</DialogDescription>
        </DialogHeader>

        {formError && (
          <div className="rounded-md bg-destructive/10 p-3 text-xs text-destructive border border-destructive/20">
            {formError}
          </div>
        )}

        <div className="flex flex-col gap-3 py-2">
          <div>
            <label className="block text-xs font-medium text-foreground mb-1.5">
              {t("reservations.deviceSelectLabel")}
            </label>
            <Select value={deviceId} onValueChange={(val) => { setDeviceId(val); setFormError(null); }}>
              <SelectTrigger aria-label={t("reservations.deviceSelectLabel")}>
                <SelectValue placeholder={t("reservations.deviceSelectPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                {devices.map((d) => (
                  <SelectItem key={d.id} value={d.id}>
                    {d.name} ({d.assetTag})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div>
            <label className="block text-xs font-medium text-foreground mb-1.5">
              {t("reservations.userSelectLabel")}
            </label>
            <Select value={userId} onValueChange={(val) => { setUserId(val); setFormError(null); }}>
              <SelectTrigger aria-label={t("reservations.userSelectLabel")}>
                <SelectValue placeholder={t("reservations.userSelectPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                {users.map((u) => (
                  <SelectItem key={u.id} value={u.id}>
                    {u.fullName} ({u.employeeNo})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div>
            <label className="block text-xs font-medium text-foreground mb-1.5">
              {t("reservations.startAtLabel")}
            </label>
            <input
              type="datetime-local"
              className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
              value={startAt}
              onChange={(e) => { setStartAt(e.target.value); setFormError(null); }}
              aria-label={t("reservations.startAtLabel")}
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-foreground mb-1.5">
              {t("reservations.endAtLabel")}
            </label>
            <input
              type="datetime-local"
              className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
              value={endAt}
              onChange={(e) => { setEndAt(e.target.value); setFormError(null); }}
              aria-label={t("reservations.endAtLabel")}
            />
          </div>
        </div>

        <DialogFooter className="gap-2 sm:gap-0">
          <Button variant="outline" onClick={handleClose} disabled={createMutation.isPending}>
            {t("reservations.cancel")}
          </Button>
          <Button
            disabled={!deviceId || !userId || !startAt || !endAt || createMutation.isPending}
            onClick={() => createMutation.mutate()}
          >
            {createMutation.isPending ? t("reservations.submitting") : t("reservations.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const columnHelper = createColumnHelper<Reservation>();

export function ReservationsPage() {
  const t = useT();
  const navigate = useNavigate({ from: reservationsRoute.fullPath });
  const search = reservationsRoute.useSearch();
  const queryClient = useQueryClient();

  const [cancelTarget, setCancelTarget] = useState<Reservation | null>(null);
  const [createOpen, setCreateOpen] = useState(false);

  // Users and devices for lookup and create dialog
  const { data: usersData } = useQuery({
    queryKey: ["users", "lookup"],
    queryFn: async () => {
      const res = await listUsers({ query: { limit: 500 } });
      return res.data?.items ?? [];
    },
    staleTime: 60_000,
  });

  const { data: devicesData } = useQuery({
    queryKey: ["devices", "lookup"],
    queryFn: async () => {
      const res = await listDevices({ query: { limit: 500 } });
      return res.data?.items ?? [];
    },
    staleTime: 60_000,
  });

  const apiStatus = search.status && search.status !== "all"
    ? (search.status as ReservationStatus)
    : undefined;

  const reservationsQuery = useInfiniteQuery({
    queryKey: ["reservations", { status: apiStatus, userId: search.userId, deviceId: search.deviceId }],
    queryFn: async ({ pageParam }) => {
      const res = await listReservations({
        query: {
          status: apiStatus,
          userId: search.userId,
          deviceId: search.deviceId,
          cursor: pageParam || undefined,
          limit: 50,
        },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    initialPageParam: "",
    getNextPageParam: (lastPage) => lastPage?.nextCursor || undefined,
  });

  const cancelMutation = useMutation({
    mutationFn: async ({ reservationId, reason }: { reservationId: string; reason: string }) => {
      const res = await cancelReservation({
        path: { id: reservationId },
        body: { reason },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["reservations"] });
      toast.success(t("reservations.cancelSuccess"));
      setCancelTarget(null);
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || t("reservations.cancelFailed"));
    },
  });

  const filteredReservations = useMemo(() => {
    const pages = reservationsQuery.data?.pages ?? [];
    let items = pages.flatMap((page) => page?.items ?? []);

    if (search.q?.trim()) {
      const term = search.q.trim().toLowerCase();
      items = items.filter((r) =>
        r.id.toLowerCase().includes(term) ||
        r.deviceName.toLowerCase().includes(term) ||
        r.deviceAssetTag.toLowerCase().includes(term) ||
        r.userName.toLowerCase().includes(term) ||
        r.userEmployeeNo.toLowerCase().includes(term)
      );
    }

    return items;
  }, [reservationsQuery.data, search.q]);

  const columns = useDataTableColumns<Reservation>(
    () => [
      columnHelper.accessor("deviceName", {
        id: "device",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("reservations.columnDevice")} />,
        cell: ({ row }) => {
          const res = row.original;
          return (
            <div className="flex flex-col">
              <Link
                to="/devices/$deviceId"
                params={{ deviceId: res.deviceId }}
                className="font-medium text-foreground hover:text-primary hover:underline"
              >
                {res.deviceName || t("reservations.unknownDevice")}
              </Link>
              <span className="font-identifier text-muted-foreground text-xs">
                {res.deviceAssetTag || res.deviceId.slice(0, 8)}
              </span>
            </div>
          );
        },
      }),
      columnHelper.accessor("userName", {
        id: "reserver",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("reservations.columnReserver")} />,
        cell: ({ row }) => {
          const res = row.original;
          return (
            <div className="flex flex-col">
              <Link
                to="/users/$userId"
                params={{ userId: res.userId }}
                className="font-medium text-foreground hover:text-primary hover:underline"
              >
                {res.userName || t("reservations.unknownReserver")}
              </Link>
              <span className="text-muted-foreground text-xs">
                {res.userEmployeeNo}
              </span>
            </div>
          );
        },
      }),
      columnHelper.accessor("status", {
        id: "status",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("reservations.columnStatus")} />,
        cell: ({ row }) => {
          const res = row.original;
          return (
            <div className="flex flex-col gap-1">
              <StatusBadge
                label={labelize(res.status)}
                tone={reservationStatusTone[res.status] ?? "muted"}
              />
              {res.cancellationReason && (
                <span className="text-[11px] text-muted-foreground truncate max-w-[180px]">
                  {res.cancellationReason}
                </span>
              )}
            </div>
          );
        },
      }),
      columnHelper.accessor("startAt", {
        id: "window",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("reservations.columnWindow")} />,
        cell: ({ row }) => {
          const res = row.original;
          return (
            <div className="flex flex-col text-xs">
              <span className="font-medium text-foreground">
                {formatDate(res.startAt)}
              </span>
              <span className="text-muted-foreground">
                {formatDate(res.endAt)}
              </span>
            </div>
          );
        },
      }),
      columnHelper.accessor("createdAt", {
        id: "createdAt",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("reservations.columnCreated")} />,
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            {formatDate(row.original.createdAt)}
          </span>
        ),
      }),
      columnHelper.display({
        id: "actions",
        header: () => <span className="sr-only">{t("columns.actions")}</span>,
        cell: ({ row }) => {
          const res = row.original;
          if (res.status !== "active") return null;

          return (
            <div className="flex justify-end" onClick={(e) => e.stopPropagation()}>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon" className="size-8" aria-label={t("columns.openMenu")}>
                    <MoreHorizontal className="size-4" />
                    <span className="sr-only">{t("columns.openMenu")}</span>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <RoleGate minRole="technician">
                    <DropdownMenuItem
                      data-testid={`cancel-reservation-action-${res.id}`}
                      className="text-destructive focus:text-destructive"
                      onClick={() => setCancelTarget(res)}
                    >
                      <XCircle className="mr-2 size-4" />
                      {t("reservations.cancelAction")}
                    </DropdownMenuItem>
                  </RoleGate>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          );
        },
      }),
    ],
    [t],
  );

  const isFiltered = Boolean(search.q || search.status);

  function updateSearch(patch: Partial<ReservationsSearch>) {
    void navigate({ search: (prev) => ({ ...prev, ...patch }) });
  }

  return (
    <div className="flex flex-col gap-4" data-testid="reservations-page">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-foreground flex items-center gap-2">
            <CalendarClock className="size-5 text-primary" />
            {t("reservations.title")}
          </h1>
          <p className="text-sm text-muted-foreground">
            {t("reservations.subtitle")}
          </p>
        </div>

        <RoleGate minRole="technician">
          <Button
            size="sm"
            onClick={() => setCreateOpen(true)}
            data-testid="create-reservation-btn"
            className="self-start sm:self-auto"
          >
            <Plus className="mr-1.5 size-4" />
            {t("reservations.createReservation")}
          </Button>
        </RoleGate>
      </div>

      <DataTable
        tableId="reservations-table"
        columns={columns}
        data={filteredReservations}
        isLoading={reservationsQuery.isLoading}
        isError={reservationsQuery.isError}
        error={reservationsQuery.error}
        onRetry={() => reservationsQuery.refetch()}
        searchQuery={search.q ?? ""}
        onSearchChange={(q) => updateSearch({ q: q || undefined })}
        searchPlaceholder={t("reservations.searchPlaceholder")}
        isFiltered={isFiltered}
        onResetFilters={() => updateSearch({ q: undefined, status: undefined })}
        hasNextPage={reservationsQuery.hasNextPage}
        isFetchingNextPage={reservationsQuery.isFetchingNextPage}
        onFetchNextPage={() => reservationsQuery.fetchNextPage()}
        emptyTitle={t("reservations.emptyTitle")}
        emptyExplanation={
          isFiltered
            ? t("reservations.emptyExplanationFiltered")
            : t("reservations.emptyExplanation")
        }
        filterControls={
          <Select
            value={search.status || "all"}
            onValueChange={(val) => updateSearch({ status: val as any })}
          >
            <SelectTrigger className="w-[150px] h-8 text-xs" aria-label={t("reservations.filterByStatusAria")}>
              <SelectValue placeholder={t("reservations.statusPlaceholder")} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t("reservations.allStatuses")}</SelectItem>
              <SelectItem value="active">{t("reservations.statusActive")}</SelectItem>
              <SelectItem value="collected">{t("reservations.statusCollected")}</SelectItem>
              <SelectItem value="cancelled">{t("reservations.statusCancelled")}</SelectItem>
              <SelectItem value="expired">{t("reservations.statusExpired")}</SelectItem>
            </SelectContent>
          </Select>
        }
      />

      <CreateReservationDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        devices={devicesData ?? []}
        users={usersData ?? []}
        onSuccess={() => {
          queryClient.invalidateQueries({ queryKey: ["reservations"] });
        }}
      />

      <CancelReservationDialog
        open={Boolean(cancelTarget)}
        onOpenChange={(open) => !open && setCancelTarget(null)}
        target={cancelTarget}
        isPending={cancelMutation.isPending}
        onConfirm={(reason) => {
          if (cancelTarget) {
            cancelMutation.mutate({ reservationId: cancelTarget.id, reason });
          }
        }}
      />
    </div>
  );
}

export const reservationsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/reservations",
  validateSearch: reservationsSearchSchema,
  component: ReservationsPage,
});
