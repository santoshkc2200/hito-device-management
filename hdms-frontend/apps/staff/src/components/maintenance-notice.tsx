import type { QueryClient } from "@tanstack/react-query";
import type { AnyRouter } from "@tanstack/react-router";
import { Wrench } from "lucide-react";
import { useMaintenance } from "@hdms/ui";
import { useTranslator } from "@/i18n";

export function MaintenanceNotice() {
  const t = useTranslator();
  return (
    <main
      role="status"
      data-testid="maintenance-notice"
      className="fixed inset-0 z-50 flex flex-col items-center justify-center gap-4 bg-background px-6 text-center text-foreground"
    >
      <Wrench className="size-12 text-muted-foreground" aria-hidden="true" />
      <h1 className="max-w-md text-2xl font-bold">{t("maintenance.title")}</h1>
      <p className="max-w-md text-muted-foreground">{t("maintenance.body")}</p>
      <p className="max-w-md text-sm text-muted-foreground">{t("maintenance.recheck")}</p>
    </main>
  );
}

// Rendered beside the router, not inside it: while a restore runs the auth
// guard waits (routes/authenticated.tsx), and the router renders nothing until
// it is done. When the restore is over, load everything again — the data may
// be a different day's.
export function MaintenanceOverlay({ router, queryClient }: { router: AnyRouter; queryClient: QueryClient }) {
  const underMaintenance = useMaintenance(() => {
    void queryClient.invalidateQueries();
    void router.invalidate();
  });
  return underMaintenance ? <MaintenanceNotice /> : null;
}
