import {
  type Device,
  type Loan,
  type User,
  listDevices,
  listDisputedLoans,
  listUsers,
} from "@hdms/api-client";
import { createColumnHelper } from "@tanstack/react-table";
import { useQuery } from "@tanstack/react-query";
import { createRoute, Link } from "@tanstack/react-router";
import {
  AlertCircle,
  ExternalLink,
  FileCheck2,
  HelpCircle,
} from "lucide-react";
import { useMemo, useState } from "react";
import { DataTable, DataTableColumnHeader, useDataTableColumns } from "@/components/data-table";
import { LoanOriginBadge, StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { useT } from "@/i18n";
import { authenticatedRoute } from "./authenticated";

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

const columnHelper = createColumnHelper<Loan>();

export function DisputedLoansPage() {
  const t = useT();
  const [searchFilter, setSearchFilter] = useState("");

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

  const disputedQuery = useQuery({
    queryKey: ["loans", "disputed"],
    queryFn: async () => {
      const res = await listDisputedLoans({ query: { limit: 100 } });
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
  });

  const items = useMemo(() => {
    const raw = disputedQuery.data ?? [];
    if (!searchFilter.trim()) return raw;
    const term = searchFilter.trim().toLowerCase();
    return raw.filter((l) => {
      const u = userMap.get(l.userId);
      const d = deviceMap.get(l.deviceId);
      return (
        l.id.toLowerCase().includes(term) ||
        (u?.fullName && u.fullName.toLowerCase().includes(term)) ||
        (u?.employeeNo && u.employeeNo.toLowerCase().includes(term)) ||
        (d?.name && d.name.toLowerCase().includes(term)) ||
        (d?.assetTag && d.assetTag.toLowerCase().includes(term)) ||
        (l.notes && l.notes.toLowerCase().includes(term)) ||
        (l.paperRef && l.paperRef.toLowerCase().includes(term))
      );
    });
  }, [disputedQuery.data, searchFilter, userMap, deviceMap]);

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
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("disputed.columnRecordedBorrower")} />,
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
      columnHelper.accessor("origin", {
        id: "origin",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("disputed.columnOriginStatus")} />,
        cell: ({ row }) => {
          const loan = row.original;
          return (
            <div className="flex items-center gap-2 flex-wrap">
              <LoanOriginBadge origin={loan.origin} disputed={true} />
              <StatusBadge label={loan.status} tone="muted" />
            </div>
          );
        },
      }),
      columnHelper.accessor("borrowedAt", {
        id: "borrowedAt",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("disputed.columnClaimedWindow")} />,
        cell: ({ row }) => {
          const loan = row.original;
          return (
            <div className="flex flex-col text-xs text-muted-foreground">
              <span>{t("disputed.outPrefix")} {formatDate(loan.borrowedAt)}</span>
              {loan.returnedAt && <span>{t("disputed.inPrefix")} {formatDate(loan.returnedAt)}</span>}
            </div>
          );
        },
      }),
      columnHelper.accessor("notes", {
        id: "conflictNotes",
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("disputed.columnConflictNotes")} />,
        cell: ({ getValue, row }) => {
          const val =
            getValue() || row.original.backfillNote || t("disputed.fallbackConflictNote");
          return (
            <div className="max-w-md text-xs text-muted-foreground truncate" title={val}>
              {val}
            </div>
          );
        },
      }),
      columnHelper.display({
        id: "actions",
        header: () => <span className="sr-only">{t("columns.actions")}</span>,
        cell: ({ row }) => {
          const loan = row.original;
          return (
            <div className="flex items-center justify-end">
              <Button variant="ghost" size="sm" asChild className="h-8 px-2 text-xs">
                <Link to="/loans/$loanId" params={{ loanId: loan.id }}>
                  <ExternalLink className="size-3.5 mr-1" />
                  {t("disputed.viewDetail")}
                </Link>
              </Button>
            </div>
          );
        },
      }),
    ],
    [userMap, deviceMap, t],
  );

  return (
    <div className="flex flex-col gap-6" data-testid="disputed-loans-page">
      {/* Header */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground flex items-center gap-2">
            <AlertCircle className="size-6 text-rose-600 dark:text-rose-400" />
            {t("disputed.title")}
          </h1>
          <p className="text-sm text-muted-foreground">
            {t("disputed.subtitle")}
          </p>
        </div>

        <div className="flex items-center gap-2">
          <Link
            to="/loans"
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md border text-xs font-medium text-foreground hover:bg-muted"
          >
            <FileCheck2 className="size-3.5" />
            {t("disputed.backToLoans")}
          </Link>
        </div>
      </div>

      {/* Info notice */}
      <div className="rounded-lg border border-rose-200 bg-rose-50/50 p-4 text-xs text-rose-900 dark:border-rose-900/50 dark:bg-rose-950/30 dark:text-rose-300 flex items-start gap-3">
        <HelpCircle className="size-4 shrink-0 mt-0.5" />
        <div>
          <p className="font-semibold">{t("disputed.whyTitle")}</p>
          <p className="mt-0.5 text-rose-800 dark:text-rose-400 leading-relaxed">
            {t("disputed.whyExplanation")}
          </p>
        </div>
      </div>

      <DataTable
        tableId="disputed-loans-table"
        columns={columns}
        data={items}
        isLoading={disputedQuery.isLoading}
        isError={disputedQuery.isError}
        error={disputedQuery.error}
        onRetry={() => disputedQuery.refetch()}
        searchQuery={searchFilter}
        onSearchChange={setSearchFilter}
        searchPlaceholder={t("disputed.searchPlaceholder")}
        isFiltered={Boolean(searchFilter)}
        onResetFilters={() => setSearchFilter("")}
        emptyTitle={t("disputed.emptyTitle")}
        emptyExplanation={t("disputed.emptyExplanation")}
      />
    </div>
  );
}

export const disputedRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/disputed",
  component: DisputedLoansPage,
});

