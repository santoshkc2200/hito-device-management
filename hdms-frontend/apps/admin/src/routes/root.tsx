import type { QueryClient } from "@tanstack/react-query";
import { useQuery } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";
import { DEFAULT_LOCALE, isLocale, LocaleProvider } from "@hdms/i18n";
import { EnvironmentBanner } from "@hdms/ui";
import { RouteErrorBoundary } from "@/components/states";
import { currentAdminQueryOptions } from "@/lib/auth";
import { useT } from "@/i18n";

export interface RouterContext {
  queryClient: QueryClient;
}

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

  return (
    <LocaleProvider locale={locale}>
      <AdminEnvironmentBanner />
      <div className="min-h-dvh">
        <RouteErrorBoundary>
          <Outlet />
        </RouteErrorBoundary>
      </div>
    </LocaleProvider>
  );
}

export const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: RootComponent,
});

