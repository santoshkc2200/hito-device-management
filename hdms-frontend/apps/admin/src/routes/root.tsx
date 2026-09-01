import type { QueryClient } from "@tanstack/react-query";
import { useQuery } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";
import { DEFAULT_LOCALE, isLocale, LocaleProvider } from "@hdms/i18n";
import { RouteErrorBoundary } from "@/components/states";
import { currentAdminQueryOptions } from "@/lib/auth";

export interface RouterContext {
  queryClient: QueryClient;
}

function RootComponent() {
  const { data: admin } = useQuery(currentAdminQueryOptions);
  const locale = isLocale(admin?.locale) ? admin.locale : DEFAULT_LOCALE;

  return (
    <LocaleProvider locale={locale}>
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

