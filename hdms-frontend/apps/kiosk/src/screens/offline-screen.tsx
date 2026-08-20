import { BookOpen, RefreshCw, WifiOff } from "lucide-react";
import { ScreenFrame } from "./screen-frame";

export interface OfflineScreenProps {
  kioskName?: string;
  supportCode?: string | null;
  onToggleCamera?: () => void;
  onOpenDiagnostics?: () => void;
}

export function OfflineScreen({
  kioskName = "HDMS Kiosk",
  supportCode = "KIOSK-01",
  onToggleCamera,
  onOpenDiagnostics,
}: OfflineScreenProps) {
  return (
    <ScreenFrame
      kioskName={kioskName}
      supportCode={supportCode}
      scannerReady={false}
      scannerFresh={false}
      onToggleCamera={onToggleCamera}
      onOpenDiagnostics={onOpenDiagnostics}
    >
      <div
        data-testid="offline-screen"
        className="flex flex-col items-center justify-between h-full w-full max-w-3xl space-y-8 py-6 text-center"
      >
        {/* Animated Reconnection Status */}
        <div className="space-y-4 flex flex-col items-center">
          <div
            data-testid="offline-icon-container"
            className="p-5 rounded-full bg-muted text-muted-foreground shadow-md relative"
          >
            <WifiOff className="size-16 stroke-[2.5]" aria-hidden="true" />
            <span className="absolute -bottom-1 -right-1 p-1.5 rounded-full bg-background border-2 border-border">
              <RefreshCw className="size-5 text-primary animate-spin" aria-hidden="true" />
            </span>
          </div>

          <span
            data-testid="offline-status-badge"
            className="font-mono text-xs font-bold uppercase tracking-wider px-3.5 py-1 rounded-full bg-muted text-muted-foreground"
          >
            Reconnecting
          </span>

          {/* Primary Prompt: ≥ 36px font size */}
          <h2
            data-testid="offline-title"
            className="text-4xl md:text-5xl font-extrabold tracking-tight text-foreground"
            style={{ fontSize: "clamp(2rem, 5vw, 2.75rem)" }}
          >
            Reconnecting to hospital network
          </h2>

          <p data-testid="offline-subtitle" className="text-lg md:text-xl text-muted-foreground max-w-lg">
            This kiosk is temporarily offline and will resume automatically as soon as the connection is restored.
          </p>
        </div>

        {/* Paper Register Fallback Card */}
        <div
          data-testid="offline-paper-card"
          className="w-full max-w-xl rounded-2xl border-2 border-primary/30 bg-primary/5 p-6 md:p-8 shadow-sm space-y-4 text-left"
        >
          <div className="flex items-start gap-4">
            <div className="p-3 rounded-xl bg-primary/10 text-primary shrink-0 mt-0.5">
              <BookOpen className="size-7" aria-hidden="true" />
            </div>
            <div className="space-y-2 flex-1">
              <h3 className="text-lg font-bold text-foreground">
                Paper Register Fallback
              </h3>
              <p
                data-testid="offline-fallback-instruction"
                className="text-base text-foreground/80 leading-relaxed font-medium"
              >
                The attendant can record your device loan or return on the paper register in the meantime. You do not need to wait.
              </p>
            </div>
          </div>
        </div>

        {/* Quiet footer notice */}
        <div className="w-full max-w-lg pt-2 text-xs text-muted-foreground">
          <p>Automatic background reconnect is in progress.</p>
        </div>
      </div>
    </ScreenFrame>
  );
}
