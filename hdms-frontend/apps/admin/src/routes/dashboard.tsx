import { getBackupConfig, getDashboard } from "@hdms/api-client";
import { useQuery } from "@tanstack/react-query";
import { createRoute } from "@tanstack/react-router";
import { RefreshCw } from "lucide-react";
import { AttentionStrip } from "@/components/dashboard/attention-strip";
import { CategoryAvailabilityBars } from "@/components/dashboard/category-availability-bars";
import { OverdueLoansTable } from "@/components/dashboard/overdue-loans-table";
import { StatTiles } from "@/components/dashboard/stat-tiles";
import { ErrorState } from "@/components/states";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useRole } from "@/lib/use-role";
import { useT } from "@/i18n";
import { authenticatedRoute } from "./authenticated";

function DashboardSkeleton() {
  return (
    <div className="flex flex-col gap-6" data-testid="dashboard-loading">
      <div className="flex items-center justify-between">
        <div className="space-y-1">
          <Skeleton className="h-7 w-40" />
          <Skeleton className="h-4 w-64" />
        </div>
        <Skeleton className="h-8 w-24" />
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-28 rounded-xl" />
        ))}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-12 gap-6">
        <div className="lg:col-span-7 flex flex-col gap-6">
          <Skeleton className="h-72 rounded-xl" />
          <Skeleton className="h-56 rounded-xl" />
        </div>
        <div className="lg:col-span-5">
          <Skeleton className="h-[520px] rounded-xl" />
        </div>
      </div>
    </div>
  );
}

function DashboardPage() {
  const t = useT();
  const { isAdmin } = useRole();
  const backupQuery = useQuery({
    queryKey: ["backup", "config"],
    enabled: isAdmin,
    queryFn: async () => {
      const res = await getBackupConfig();
      if (res.error) throw res.error;
      return res.data;
    },
  });
  const {
    data: dashboard,
    isLoading,
    isError,
    error,
    refetch,
    isFetching,
  } = useQuery({
    queryKey: ["dashboard"],
    queryFn: async () => {
      const { data, error } = await getDashboard();
      if (error) throw error;
      return data;
    },
    refetchInterval: 30000, // Background poll every 30s as safety net
  });

  if (isLoading) {
    return <DashboardSkeleton />;
  }

  if (isError || !dashboard) {
    return (
      <div className="flex flex-col gap-4">
        <h1 className="text-xl font-semibold">{t("nav.dashboard")}</h1>
        <ErrorState
          error={error}
          title={t("dashboard.errorTitle")}
          detail={t("dashboard.errorDetail")}
          onRetry={() => refetch()}
        />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6" data-testid="dashboard-page">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight text-foreground">
            {t("dashboard.headerTitle")}
          </h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            {t("dashboard.headerSubtitle")}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => refetch()}
            disabled={isFetching}
            className="h-8 text-xs"
            title={t("dashboard.refreshTooltip")}
          >
            <RefreshCw className={`size-3.5 mr-1.5 ${isFetching ? "animate-spin" : ""}`} />
            {t("dashboard.refreshButton")}
          </Button>
        </div>
      </div>

      {/* Attention Strip */}
      <AttentionStrip
        turnedAwayCounts={dashboard.turnedAwayCounts}
        kiosks={dashboard.kiosks}
        backup={
          isAdmin && backupQuery.data
            ? { lastSuccessAt: backupQuery.data.lastSuccessAt ?? null, recoveryKeyStatus: backupQuery.data.recoveryKey.status }
            : undefined
        }
      />

      {/* 4 Stat Tiles */}
      <StatTiles
        availableCount={dashboard.availableCount}
        onLoanCount={dashboard.onLoanCount}
        overdueCount={dashboard.overdueCount}
        maintenanceCount={dashboard.maintenanceCount}
      />

      {/* Main Content */}
      <div className="flex flex-col gap-6">
        <OverdueLoansTable loans={dashboard.overdueLoans} />
        <CategoryAvailabilityBars categories={dashboard.availabilityByCategory} />
      </div>
    </div>
  );
}

export const dashboardRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/dashboard",
  component: DashboardPage,
});
