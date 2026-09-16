import { createRoute } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { useT } from "@/i18n";

export function IndexPage() {
  const t = useT();
  return (
    <main className="flex flex-col items-center justify-center p-4">
      <h1 className="text-xl font-bold">{t("appTitle")}</h1>
    </main>
  );
}

export const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: IndexPage,
});
