import {
  type Device,
  type Loan,
  type LoanOrigin,
  type LoanStatus,
  type User,
  forceReturnLoan,
  listDevices,
  listLoans,
  listUsers,
  writeOffLoan,
} from "@hdms/api-client";
import { createColumnHelper } from "@tanstack/react-table";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute, Link, useNavigate } from "@tanstack/react-router";
import {
  AlertCircle,
  Copy,
  CornerDownLeft,
  ExternalLink,
  FileCheck2,
  MoreHorizontal,
  Trash2,
} from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { z } from "zod";
import { DataTable, DataTableColumnHeader, useDataTableColumns } from "@/components/data-table";
import {
  LoanOriginBadge,
  loanStatusTone,
  StatusBadge,
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

const LOAN_STATUS_OPTIONS = ["all", "open", "overdue", "returned", "written_off"] as const;
const LOAN_ORIGIN_OPTIONS = ["all", "kiosk", "paper", "admin", "import"] as const;

const loansSearchSchema = z.object({
  status: z.enum(LOAN_STATUS_OPTIONS).optional(),
  origin: z.enum(LOAN_ORIGIN_OPTIONS).optional(),
  userId: z.string().optional(),
  deviceId: z.string().optional(),
  q: z.string().optional(),
});
export type LoansSearch = z.infer<typeof loansSearchSchema>;

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

function getDaysOverdue(dueAt?: string | null): number {
  if (!dueAt) return 0;
  const due = new Date(dueAt).getTime();
  const now = Date.now();
  if (due >= now) return 0;
  return Math.max(1, Math.floor((now - due) / (1000 * 60 * 60 * 24)));
}

function ReasonOverrideDialog({
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
          <label htmlFor="override-reason" className="block text-xs font-medium text-foreground mb-1.5">
            {t("loans.overrideReasonLabel")}
          </label>
          <Textarea
            id="override-reason"
            placeholder={t("loans.overrideReasonPlaceholder")}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            rows={3}
            autoFocus
          />
        </div>
        <DialogFooter className="gap-2 sm:gap-0">
          <Button variant="outline" onClick={handleClose} disabled={isPending}>
            {t("loans.cancel")}
          </Button>
          <Button
            variant={confirmVariant}
            disabled={!reason.trim() || isPending}
            onClick={() => onConfirm(reason.trim())}
          >
            {isPending ? t("loans.processing") : confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const columnHelper = createColumnHelper<Loan>();

export function LoansPage() {
  const t = useT();
  const navigate = useNavigate({ from: loansRoute.fullPath });
  const search = loansRoute.useSearch();
  const queryClient = useQueryClient();

  const [forceReturnTarget, setForceReturnTarget] = useState<Loan | null>(null);
  const [writeOffTarget, setWriteOffTarget] = useState<Loan | null>(null);

  // Queries
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

  const userMap = useMemo(() => {
    const map = new Map<string, User>();
    for (const u of usersData ?? []) {
      map.set(u.id, u);
    }
    return map;
  }, [usersData]);

  const deviceMap = useMemo(() => {
    const map = new Map<string, Device>();
    for (const d of devicesData ?? []) {
      map.set(d.id, d);
    }
    return map;
  }, [devicesData]);

  const apiStatus = search.status && search.status !== "all" && search.status !== "overdue"
    ? (search.status as LoanStatus)
    : undefined;

  const apiOrigin = search.origin && search.origin !== "all"
    ? (search.origin as LoanOrigin)
    : undefined;

  const loansQuery = useInfiniteQuery({
    queryKey: ["loans", { status: apiStatus, origin: apiOrigin, userId: search.userId, deviceId: search.deviceId }],
    queryFn: async ({ pageParam }) => {
      const res = await listLoans({
        query: {
          status: apiStatus,
          origin: apiOrigin,
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

  // Mutations
  const forceReturnMutation = useMutation({
    mutationFn: async ({ loanId, reason }: { loanId: string; reason: string }) => {
      const res = await forceReturnLoan({
        path: { id: loanId },
        body: { reason },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["loans"] }),
        queryClient.invalidateQueries({ queryKey: ["dashboard"] }),
        queryClient.invalidateQueries({ queryKey: ["devices"] }),
      ]);
      toast.success(t("loans.forceReturnSuccess"));
      setForceReturnTarget(null);
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || t("loans.forceReturnFailed"));
    },
  });

  const writeOffMutation = useMutation({
    mutationFn: async ({ loanId, reason }: { loanId: string; reason: string }) => {
      const res = await writeOffLoan({
        path: { id: loanId },
        body: { reason },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["loans"] }),
        queryClient.invalidateQueries({ queryKey: ["dashboard"] }),
        queryClient.invalidateQueries({ queryKey: ["devices"] }),
      ]);
      toast.success(t("loans.writeOffSuccess"));
      setWriteOffTarget(null);
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || t("loans.writeOffFailed"));
    },
  });

  const handleCopyReminder = async (loan: Loan) => {
    const user = userMap.get(loan.userId);
    const device = deviceMap.get(loan.deviceId);
    const formattedDue = formatDate(loan.dueAt);
    const userName = user?.fullName || t("loans.reminderUserFallback");
    const devName = device?.name || t("loans.reminderDeviceFallback");
    const assetTag = device?.assetTag || "";

    const message = t("loans.reminderMessage", {
      user: userName,
      device: devName,
      assetTag,
      dueAt: formattedDue,
    });
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(message);
      }
      toast.success(t("loans.reminderCopied"));
    } catch {
      toast.error(t("loans.reminderCopyFailed"));
    }
  };

  // Filter and sort items
  const filteredLoans = useMemo(() => {
    const raw = loansQuery.data?.pages.flatMap((p) => p?.items ?? []) ?? [];
    let items = [...raw];

    // Filter by overdue if status === "overdue"
    if (search.status === "overdue") {
      items = items.filter(
        (l) => l.status === "open" && l.dueAt && Date.now() > new Date(l.dueAt).getTime(),
      );
      // Sort worst first (highest days overdue / earliest due date)
      items.sort((a, b) => {
        const dueA = a.dueAt ? new Date(a.dueAt).getTime() : 0;
        const dueB = b.dueAt ? new Date(b.dueAt).getTime() : 0;
        return dueA - dueB;
      });
    }

    // Client-side text search (q)
    if (search.q?.trim()) {
      const term = search.q.trim().toLowerCase();
      items = items.filter((l) => {
        const user = userMap.get(l.userId);
        const device = deviceMap.get(l.deviceId);
        return (
          l.id.toLowerCase().includes(term) ||
          (user?.fullName && user.fullName.toLowerCase().includes(term)) ||
          (user?.employeeNo && user.employeeNo.toLowerCase().includes(term)) ||
          (device?.name && device.name.toLowerCase().includes(term)) ||
          (device?.assetTag && device.assetTag.toLowerCase().includes(term)) ||
          (l.paperRef && l.paperRef.toLowerCase().includes(term))
        );
      });
    }

    return items;
  }, [loansQuery.data, search.status, search.q, userMap, deviceMap]);

  // Memoized columns
  const columns = useDataTableColumns<Loan>(
    () => [
      columnHelper.accessor("deviceId", {
        id: "device",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("loans.columnDevice")} />,
        cell: ({ row }) => {
          const loan = row.original;
          const device = deviceMap.get(loan.deviceId);
          return (
            <div className="flex flex-col">
              <Link
                to="/devices/$deviceId"
                params={{ deviceId: loan.deviceId }}
                className="font-medium text-foreground hover:text-primary hover:underline"
              >
                {device?.name || t("loans.unknownDevice")}
              </Link>
              <span className="font-identifier text-muted-foreground text-xs">
                {device?.assetTag || loan.deviceId.slice(0, 8)}
              </span>
            </div>
          );
        },
      }),
      columnHelper.accessor("userId", {
        id: "borrower",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("loans.columnBorrower")} />,
        cell: ({ row }) => {
          const loan = row.original;
          const user = userMap.get(loan.userId);
          return (
            <div className="flex flex-col">
              <Link
                to="/users/$userId"
                params={{ userId: loan.userId }}
                className="font-medium text-foreground hover:text-primary hover:underline"
              >
                {user?.fullName || t("loans.unknownBorrower")}
              </Link>
              {user?.employeeNo && (
                <span className="text-muted-foreground text-xs">
                  {user.employeeNo}
                </span>
              )}
            </div>
          );
        },
      }),
      columnHelper.accessor("status", {
        id: "status",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("loans.columnStatusOrigin")} />,
        cell: ({ row }) => {
          const loan = row.original;
          const isOverdue =
            loan.status === "open" &&
            loan.dueAt &&
            new Date(loan.dueAt).getTime() < Date.now();

          const tone = isOverdue ? "warning" : loanStatusTone[loan.status] || "muted";
          const statusLabel = isOverdue
            ? t("loans.statusOverdue")
            : loan.status === "written_off"
              ? t("loans.statusWrittenOff")
              : loan.status;

          return (
            <div className="flex items-center gap-2 flex-wrap">
              <StatusBadge label={statusLabel} tone={tone} />
              <LoanOriginBadge origin={loan.origin} disputed={loan.disputed} />
            </div>
          );
        },
      }),
      columnHelper.accessor("borrowedAt", {
        id: "borrowedAt",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("loans.columnBorrowed")} />,
        cell: ({ getValue }) => (
          <span className="text-xs text-muted-foreground whitespace-nowrap">
            {formatDate(getValue())}
          </span>
        ),
      }),
      columnHelper.accessor("dueAt", {
        id: "dueAt",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("loans.columnDueDate")} />,
        cell: ({ row }) => {
          const loan = row.original;
          if (!loan.dueAt) return <span className="text-xs text-muted-foreground">—</span>;

          const isOverdue =
            loan.status === "open" && new Date(loan.dueAt).getTime() < Date.now();
          const days = isOverdue ? getDaysOverdue(loan.dueAt) : 0;

          return (
            <div className="flex flex-col whitespace-nowrap">
              <span className="text-xs text-foreground">{formatDate(loan.dueAt)}</span>
              {isOverdue && (
                <span className="text-[11px] font-semibold text-rose-600 dark:text-rose-400">
                  {days === 1 ? t("loans.oneDayLate") : t("loans.daysLate", { days })}
                </span>
              )}
            </div>
          );
        },
      }),
      columnHelper.accessor("returnedAt", {
        id: "returnedAt",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("loans.columnReturned")} />,
        cell: ({ getValue }) => (
          <span className="text-xs text-muted-foreground whitespace-nowrap">
            {formatDate(getValue())}
          </span>
        ),
      }),
      columnHelper.display({
        id: "actions",
        header: () => <span className="sr-only">{t("columns.actions")}</span>,
        cell: ({ row }) => {
          const loan = row.original;
          const isOpen = loan.status === "open";

          return (
            <div className="flex items-center justify-end gap-1">
              {isOpen && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 px-2 text-xs"
                  onClick={() => handleCopyReminder(loan)}
                  title={t("loans.remindTooltip")}
                  data-testid={`remind-btn-${loan.id}`}
                >
                  <Copy className="size-3.5 mr-1" />
                  {t("loans.remind")}
                </Button>
              )}
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon" className="size-8" data-testid={`loan-actions-${loan.id}`}>
                    <MoreHorizontal className="size-4" />
                    <span className="sr-only">{t("columns.openMenu")}</span>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem asChild>
                    <Link to="/loans/$loanId" params={{ loanId: loan.id }}>
                      <ExternalLink className="size-4 mr-2" />
                      {t("loans.viewLoanDetail")}
                    </Link>
                  </DropdownMenuItem>

                  {isOpen && (
                    <>
                      <RoleGate minRole="technician">
                        <DropdownMenuItem
                          onClick={() => setForceReturnTarget(loan)}
                          className="text-amber-800 dark:text-amber-300"
                        >
                          <CornerDownLeft className="size-4 mr-2" />
                          {t("loans.forceReturnAction")}
                        </DropdownMenuItem>
                      </RoleGate>

                      <RoleGate minRole="admin">
                        <DropdownMenuItem
                          onClick={() => setWriteOffTarget(loan)}
                          className="text-destructive focus:text-destructive"
                        >
                          <Trash2 className="size-4 mr-2" />
                          {t("loans.writeOffAction")}
                        </DropdownMenuItem>
                      </RoleGate>
                    </>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          );
        },
      }),
    ],
    [userMap, deviceMap, t],
  );

  const isFiltered = Boolean(search.q || search.status || search.origin);

  function updateSearch(patch: Partial<LoansSearch>) {
    void navigate({ search: (prev) => ({ ...prev, ...patch }) });
  }

  return (
    <div className="flex flex-col gap-4" data-testid="loans-page">
      {/* Header */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-foreground flex items-center gap-2">
            <FileCheck2 className="size-5 text-primary" />
            {t("loans.title")}
          </h1>
          <p className="text-sm text-muted-foreground">
            {t("loans.subtitle")}
          </p>
        </div>

        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" asChild>
            <Link
              to="/disputed"
              className="inline-flex items-center gap-1.5 text-xs text-rose-800 dark:text-rose-300"
            >
              <AlertCircle className="size-3.5 text-rose-600" />
              {t("loans.disputedLoans")}
            </Link>
          </Button>
        </div>
      </div>

      <DataTable
        tableId="loans-table"
        columns={columns}
        data={filteredLoans}
        isLoading={loansQuery.isLoading}
        isError={loansQuery.isError}
        error={loansQuery.error}
        onRetry={() => loansQuery.refetch()}
        searchQuery={search.q ?? ""}
        onSearchChange={(q) => updateSearch({ q: q || undefined })}
        searchPlaceholder={t("loans.searchPlaceholder")}
        isFiltered={isFiltered}
        onResetFilters={() => updateSearch({ q: undefined, status: undefined, origin: undefined })}
        onRowClick={(row) => void navigate({ to: "/loans/$loanId", params: { loanId: row.id } })}
        hasNextPage={loansQuery.hasNextPage}
        isFetchingNextPage={loansQuery.isFetchingNextPage}
        onFetchNextPage={() => loansQuery.fetchNextPage()}
        emptyTitle={t("loans.emptyTitle")}
        emptyExplanation={
          isFiltered
            ? t("loans.emptyExplanationFiltered")
            : t("loans.emptyExplanation")
        }
        filterControls={
          <>
            <Select
              value={search.status || "all"}
              onValueChange={(val) => updateSearch({ status: val as any })}
            >
              <SelectTrigger className="w-[140px] h-8 text-xs" aria-label={t("loans.filterByStatusAria")}>
                <SelectValue placeholder={t("loans.statusPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t("loans.allStatuses")}</SelectItem>
                <SelectItem value="open">{t("loans.statusOpen")}</SelectItem>
                <SelectItem value="overdue">{t("loans.statusOverdue")}</SelectItem>
                <SelectItem value="returned">{t("loans.statusReturned")}</SelectItem>
                <SelectItem value="written_off">{t("loans.statusWrittenOff")}</SelectItem>
              </SelectContent>
            </Select>

            <Select
              value={search.origin || "all"}
              onValueChange={(val) => updateSearch({ origin: val as any })}
            >
              <SelectTrigger className="w-[130px] h-8 text-xs" aria-label={t("loans.filterByOriginAria")}>
                <SelectValue placeholder={t("loans.originPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t("loans.allOrigins")}</SelectItem>
                <SelectItem value="kiosk">{t("loans.originKiosk")}</SelectItem>
                <SelectItem value="paper">{t("loans.originPaper")}</SelectItem>
                <SelectItem value="admin">{t("loans.originAdmin")}</SelectItem>
                <SelectItem value="import">{t("loans.originImport")}</SelectItem>
              </SelectContent>
            </Select>
          </>
        }
      />

      {/* Force Return Dialog */}
      <ReasonOverrideDialog
        open={Boolean(forceReturnTarget)}
        onOpenChange={(open) => !open && setForceReturnTarget(null)}
        title={t("loans.forceReturnTitle")}
        description={t("loans.forceReturnDescription", {
          device:
            deviceMap.get(forceReturnTarget?.deviceId || "")?.name || t("loans.deviceFallback"),
          assetTag: deviceMap.get(forceReturnTarget?.deviceId || "")?.assetTag || "",
          borrower:
            userMap.get(forceReturnTarget?.userId || "")?.fullName || t("loans.borrowerFallback"),
        })}
        confirmLabel={t("loans.confirmReturn")}
        confirmVariant="default"
        isPending={forceReturnMutation.isPending}
        onConfirm={(reason) => {
          if (forceReturnTarget) {
            forceReturnMutation.mutate({ loanId: forceReturnTarget.id, reason });
          }
        }}
      />

      {/* Write Off Dialog */}
      <ReasonOverrideDialog
        open={Boolean(writeOffTarget)}
        onOpenChange={(open) => !open && setWriteOffTarget(null)}
        title={t("loans.writeOffTitle")}
        description={t("loans.writeOffDescription", {
          device: deviceMap.get(writeOffTarget?.deviceId || "")?.name || t("loans.deviceFallback"),
          assetTag: deviceMap.get(writeOffTarget?.deviceId || "")?.assetTag || "",
        })}
        confirmLabel={t("loans.confirmWriteOff")}
        confirmVariant="destructive"
        isPending={writeOffMutation.isPending}
        onConfirm={(reason) => {
          if (writeOffTarget) {
            writeOffMutation.mutate({ loanId: writeOffTarget.id, reason });
          }
        }}
      />
    </div>
  );
}

export const loansRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/loans",
  validateSearch: loansSearchSchema,
  component: LoansPage,
});
