import { BookOpen, Wrench } from "lucide-react";
import { useTranslator } from "@/i18n";
import { ScreenFrame } from "./screen-frame";

export interface MaintenanceScreenProps {
  kioskName?: string;
  supportCode?: string | null;
  onOpenDiagnostics?: () => void;
}

// A restore is rewinding the data. Nothing scanned here would count, so the
// kiosk takes no scans and sends people to the paper register — the same
// fallback as offline, but without the "reconnecting" story, because the
// network is fine. It comes back by itself (packages/ui maintenance store).
export function MaintenanceScreen({
  kioskName = "HDMS Kiosk",
  supportCode = "KIOSK-01",
  onOpenDiagnostics,
}: MaintenanceScreenProps) {
  const t = useTranslator();

  return (
    <ScreenFrame
      kioskName={kioskName}
      supportCode={supportCode}
      scannerReady={false}
      scannerFresh={false}
      onOpenDiagnostics={onOpenDiagnostics}
    >
      <div
        data-testid="maintenance-screen"
        className="flex flex-col items-center justify-between h-full w-full max-w-3xl space-y-8 py-6 text-center"
      >
        <div className="space-y-4 flex flex-col items-center">
          <div className="p-5 rounded-full bg-muted text-muted-foreground shadow-md">
            <Wrench className="size-16 stroke-[2.5]" aria-hidden="true" />
          </div>
          <span className="font-mono text-xs font-bold uppercase tracking-wider px-3.5 py-1 rounded-full bg-muted text-muted-foreground">
            {t("maintenance.badge")}
          </span>
          <h2 data-testid="maintenance-title" className="text-kiosk-prompt text-foreground leading-tight">
            {t("maintenance.title")}
          </h2>
          <p data-testid="maintenance-subtitle" className="text-kiosk-body text-muted-foreground max-w-lg">
            {t("maintenance.subtitle")}
          </p>
        </div>

        <div className="w-full max-w-xl rounded-2xl border-2 border-primary/30 bg-primary/5 p-6 md:p-8 shadow-sm text-left">
          <div className="flex items-start gap-4">
            <div className="p-3 rounded-xl bg-primary/10 text-primary shrink-0 mt-0.5">
              <BookOpen className="size-7" aria-hidden="true" />
            </div>
            <div className="space-y-2 flex-1">
              <h3 className="text-lg font-bold text-foreground">{t("offline.paperFallbackTitle")}</h3>
              <p
                data-testid="maintenance-paper-instruction"
                className="text-base text-foreground/80 leading-relaxed font-medium"
              >
                {t("offline.paperFallbackInstruction")}
              </p>
            </div>
          </div>
        </div>

        <div className="w-full max-w-lg pt-2 text-xs text-muted-foreground">
          <p>{t("maintenance.notice")}</p>
        </div>
      </div>
    </ScreenFrame>
  );
}
