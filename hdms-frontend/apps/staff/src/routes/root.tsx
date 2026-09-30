import type { QueryClient } from "@tanstack/react-query";
import { Outlet, createRootRouteWithContext } from "@tanstack/react-router";
import { EnvironmentBanner } from "@hdms/ui";
import { useTranslator } from "@/i18n";

export interface RouterContext {
  queryClient: QueryClient;
}

function RootComponent() {
  const t = useTranslator();
  return (
    <>
      <EnvironmentBanner
        labels={{ development: t("environment.development"), staging: t("environment.staging") }}
      />
      <div className="min-h-dvh bg-background text-foreground">
        <Outlet />
      </div>
    </>
  );
}

export const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: RootComponent,
});
