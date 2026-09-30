import { Outlet, createRootRoute } from "@tanstack/react-router";
import { EnvironmentBanner } from "@hdms/ui";
import { useTranslator } from "@/i18n";

function RootComponent() {
  const t = useTranslator();
  return (
    <>
      <EnvironmentBanner
        labels={{ development: t("environment.development"), staging: t("environment.staging") }}
      />
      <div className="min-h-dvh">
        <Outlet />
      </div>
    </>
  );
}

export const rootRoute = createRootRoute({
  component: RootComponent,
});
