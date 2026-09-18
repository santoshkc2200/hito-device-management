import {
  type QuarantinedDelivery,
  listQuarantinedDeliveries,
} from "@hdms/api-client";
import { createColumnHelper } from "@tanstack/react-table";
import { useQuery } from "@tanstack/react-query";
import { createRoute, Link } from "@tanstack/react-router";
import {
  AlertTriangle,
  Bell,
  ExternalLink,
  RefreshCw,
} from "lucide-react";
import { useMemo, useState } from "react";
import { DataTable, DataTableColumnHeader, useDataTableColumns } from "@/components/data-table";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { RoleGate } from "@/lib/use-role";
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

const columnHelper = createColumnHelper<QuarantinedDelivery>();

export function NotificationsPage() {
  const t = useT();
  const [searchFilter, setSearchFilter] = useState("");

  const deliveriesQuery = useQuery({
    queryKey: ["notifications", "quarantined"],
    queryFn: async () => {
      const res = await listQuarantinedDeliveries();
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
    refetchInterval: 30000,
  });

  const filteredDeliveries = useMemo(() => {
    const items = deliveriesQuery.data ?? [];
    if (!searchFilter.trim()) return items;
    const term = searchFilter.toLowerCase();
    return items.filter(
      (d) =>
        d.recipient.toLowerCase().includes(term) ||
        d.template.toLowerCase().includes(term) ||
        (d.lastError && d.lastError.toLowerCase().includes(term))
    );
  }, [deliveriesQuery.data, searchFilter]);

  const columns = useDataTableColumns<QuarantinedDelivery>(
    () => [
      columnHelper.accessor("recipient", {
        id: "recipient",
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t("notifications.columns.recipient")} />
        ),
        cell: ({ row }) => (
          <div className="flex flex-col">
            <span className="font-medium text-foreground">{row.original.recipient}</span>
            {row.original.userId && (
              <Link
                to="/users/$userId"
                params={{ userId: row.original.userId }}
                className="text-xs text-primary hover:underline inline-flex items-center gap-0.5"
              >
                <span>{row.original.userId.slice(0, 8)}…</span>
                <ExternalLink className="size-3" />
              </Link>
            )}
          </div>
        ),
      }),
      columnHelper.accessor("template", {
        id: "template",
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t("notifications.columns.template")} />
        ),
        cell: ({ row }) => {
          const tmpl = row.original.template;
          let label = tmpl;
          if (tmpl === "overdue_reminder") {
            label = t("notifications.templates.overdueReminder");
          } else if (tmpl === "weekly_digest") {
            label = t("notifications.templates.weeklyDigest");
          } else if (tmpl === "return_confirmation") {
            label = t("notifications.templates.returnConfirmation");
          }
          return (
            <div className="flex flex-col">
              <span className="text-xs font-medium text-foreground">{label}</span>
              {row.original.loanId && (
                <Link
                  to="/loans/$loanId"
                  params={{ loanId: row.original.loanId }}
                  className="text-xs text-muted-foreground hover:text-primary hover:underline inline-flex items-center gap-0.5"
                >
                  <span>Loan: {row.original.loanId.slice(0, 8)}…</span>
                  <ExternalLink className="size-3" />
                </Link>
              )}
            </div>
          );
        },
      }),
      columnHelper.accessor("status", {
        id: "status",
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t("notifications.columns.status")} />
        ),
        cell: () => (
          <StatusBadge
            label={t("notifications.status.quarantined")}
            tone="destructive"
          />
        ),
      }),
      columnHelper.accessor("escalationStep", {
        id: "escalationStep",
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t("notifications.columns.escalationStep")} />
        ),
        cell: ({ row }) => (
          <span className="font-identifier text-xs">
            {row.original.escalationStep != null ? `Step ${row.original.escalationStep}` : "—"}
          </span>
        ),
      }),
      columnHelper.accessor("attemptCount", {
        id: "attemptCount",
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t("notifications.columns.attempts")} />
        ),
        cell: ({ row }) => (
          <span className="font-identifier text-xs text-foreground font-semibold">
            {row.original.attemptCount}
          </span>
        ),
      }),
      columnHelper.accessor("lastError", {
        id: "lastError",
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t("notifications.columns.lastError")} />
        ),
        cell: ({ row }) => (
          <span
            className="text-xs text-destructive max-w-xs truncate block"
            title={row.original.lastError ?? ""}
          >
            {row.original.lastError || "—"}
          </span>
        ),
      }),
      columnHelper.accessor("createdAt", {
        id: "createdAt",
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t("notifications.columns.createdAt")} />
        ),
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            {formatDate(row.original.createdAt)}
          </span>
        ),
      }),
    ],
    [t]
  );

  return (
    <RoleGate minRole="admin">
      <div className="flex flex-col gap-6 p-6">
        {/* Page Header */}
        <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
          <div>
            <div className="flex items-center gap-2">
              <div className="rounded-lg bg-primary/10 p-2 text-primary">
                <Bell className="size-5" />
              </div>
              <h1 className="text-2xl font-bold tracking-tight text-foreground">
                {t("notifications.title")}
              </h1>
            </div>
            <p className="mt-1 text-sm text-muted-foreground">
              {t("notifications.subtitle")}
            </p>
          </div>

          <Button
            variant="outline"
            size="sm"
            onClick={() => deliveriesQuery.refetch()}
            disabled={deliveriesQuery.isFetching}
          >
            <RefreshCw
              className={`size-4 mr-2 ${deliveriesQuery.isFetching ? "animate-spin" : ""}`}
            />
            {t("notifications.refresh")}
          </Button>
        </div>

        {/* Informational banner about quarantined deliveries */}
        <div className="flex items-start gap-3 rounded-lg border border-amber-500/20 bg-amber-500/10 p-4 text-amber-900 dark:text-amber-200">
          <AlertTriangle className="size-5 text-amber-600 dark:text-amber-400 shrink-0 mt-0.5" />
          <div className="text-sm">
            <h3 className="font-semibold text-amber-900 dark:text-amber-100">
              {t("notifications.quarantinedTitle")}
            </h3>
            <p className="mt-0.5 text-xs text-amber-800 dark:text-amber-300">
              {t("notifications.quarantinedDescription")}
            </p>
          </div>
        </div>

        {/* Deliveries Table */}
        <DataTable
          tableId="quarantined-notifications-table"
          columns={columns}
          data={filteredDeliveries}
          isLoading={deliveriesQuery.isPending}
          isError={deliveriesQuery.isError}
          error={deliveriesQuery.error}
          onRetry={() => deliveriesQuery.refetch()}
          searchQuery={searchFilter}
          onSearchChange={setSearchFilter}
          searchPlaceholder={t("table.searchPlaceholder")}
          isFiltered={Boolean(searchFilter)}
          onResetFilters={() => setSearchFilter("")}
          emptyTitle={t("notifications.emptyTitle")}
          emptyExplanation={t("notifications.emptyDescription")}
        />
      </div>
    </RoleGate>
  );
}

export const notificationsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/notifications",
  component: NotificationsPage,
});
