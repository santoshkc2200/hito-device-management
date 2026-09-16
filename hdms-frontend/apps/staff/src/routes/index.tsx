import { createRoute } from "@tanstack/react-router";
import { useT } from "@/i18n";
import { authenticatedRoute } from "./authenticated";

export function IndexPage() {
  const t = useT();
  return (
    <main className="flex flex-col items-center justify-center p-4">
      <h1 className="text-xl font-bold">{t("appTitle")}</h1>
    </main>
  );
}

export const indexRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/",
  component: IndexPage,
});
