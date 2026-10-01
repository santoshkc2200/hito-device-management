import { Link } from "@tanstack/react-router";
import { Wrench } from "lucide-react";
import { buttonVariants } from "@/components/ui/button";
import { useT } from "@/i18n";

export function MaintenanceNotice() {
  const t = useT();
  return (
    <main
      role="status"
      data-testid="maintenance-notice"
      className="mx-auto flex min-h-dvh max-w-lg flex-col items-center justify-center gap-4 px-6 text-center"
    >
      <Wrench className="size-12 text-muted-foreground" aria-hidden="true" />
      <h1 className="text-2xl font-bold">{t("maintenance.title")}</h1>
      <p className="text-muted-foreground">{t("maintenance.body")}</p>
      <p className="text-sm text-muted-foreground">{t("maintenance.recheck")}</p>
      <Link to="/backups" className={buttonVariants({ variant: "outline" })}>
        {t("maintenance.openBackups")}
      </Link>
    </main>
  );
}
