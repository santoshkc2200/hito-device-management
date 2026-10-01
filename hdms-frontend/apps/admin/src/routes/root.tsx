import type { QueryClient } from "@tanstack/react-query";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet, useRouter, useRouterState } from "@tanstack/react-router";
import { DEFAULT_LOCALE, isLocale, LocaleProvider } from "@hdms/i18n";
import { EnvironmentBanner, useMaintenance } from "@hdms/ui";
import { MaintenanceNotice } from "@/components/maintenance-notice";
import { RouteErrorBoundary } from "@/components/states";
import { currentAdminQueryOptions } from "@/lib/auth";
import { useT } from "@/i18n";

export interface RouterContext {
  queryClient: QueryClient;
}

// Sign-in and the backup console are exempt from the API's maintenance gate,
// so an admin can still reach Backups while a restore runs.
const WORKS_DURING_MAINTENANCE = /\/(backups|login)(\/|$)/;

function AdminEnvironmentBanner() {
  const t = useT();
  return (
    <EnvironmentBanner
      labels={{ development: t("environment.development"), staging: t("environment.staging") }}
    />
  );
}

function RootComponent() {
  const { data: admin } = useQuery(currentAdminQueryOptions);
  const locale = isLocale(admin?.locale) ? admin.locale : DEFAULT_LOCALE;
  const router = useRouter();
  const queryClient = useQueryClient();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const underMaintenance = useMaintenance(() => {
    void queryClient.invalidateQueries();
    void router.invalidate();
  });
  const showNotice = underMaintenance && !WORKS_DURING_MAINTENANCE.test(pathname);

  return (
    <LocaleProvider locale={locale}>
      <AdminEnvironmentBanner />
      <div className="min-h-dvh">
        {showNotice ? (
          <MaintenanceNotice />
        ) : (
          <RouteErrorBoundary>
            <Outlet />
          </RouteErrorBoundary>
        )}
      </div>
    </LocaleProvider>
  );
}

export const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: RootComponent,
});
