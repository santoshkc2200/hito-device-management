import * as React from "react";
import { SwitchCamera } from "lucide-react";
import { type CameraFacingMode, type CameraSource } from "@hdms/scan";
import { Button } from "@/components/ui/button";
import { useTranslator } from "@/i18n";

export interface CameraOverlayProps {
  isOpen: boolean;
  onClose: () => void;
  onScan?: (token: string) => void;
  onManualEntry?: () => void;
  cameraSource?: CameraSource | null;
}

function CameraOverlayContent({
  onClose,
  onScan,
  onManualEntry,
  cameraSource,
}: Omit<CameraOverlayProps, "isOpen">) {
  const t = useTranslator();
  const [hasPermissionError, setHasPermissionError] = React.useState(false);
  const [torchEnabled, setTorchEnabled] = React.useState(false);
  const [hasTorchCapability, setHasTorchCapability] = React.useState(false);
  const [facingMode, setFacingMode] = React.useState<CameraFacingMode>("user");
  const [isSwitching, setIsSwitching] = React.useState(false);
  const containerRef = React.useRef<HTMLDivElement | null>(null);

  React.useEffect(() => {
    let isMounted = true;
    const source = cameraSource;

    const initCamera = async () => {
      if (!source) return;

      try {
        await source.start((scannedToken) => {
          if (!isMounted) return;
          const tokenStr =
            typeof scannedToken === "string" ? scannedToken : scannedToken.token;
          onScan?.(tokenStr);
          onClose();
        });

        if (!isMounted) return;
        setFacingMode(source.getFacingMode());
        setHasTorchCapability(source.hasTorch());

        // Attach video element to preview container
        const video = source.getVideoElement();
        if (video && containerRef.current) {
          video.className = "w-full h-full object-cover rounded-lg";
          if (!containerRef.current.contains(video)) {
            containerRef.current.innerHTML = "";
            containerRef.current.appendChild(video);
          }
        }
      } catch {
        if (!isMounted) return;
        setHasPermissionError(true);
      }
    };

    void initCamera();

    return () => {
      isMounted = false;
      if (source) {
        void source.stop();
      }
    };
  }, [cameraSource, onScan, onClose]);

  const handleToggleTorch = async () => {
    if (!cameraSource) return;
    const nextState = !torchEnabled;
    const success = await cameraSource.setTorch(nextState);
    if (success) {
      setTorchEnabled(nextState);
    }
  };

  const handleSwitchCamera = async () => {
    if (!cameraSource || isSwitching) return;
    setIsSwitching(true);
    try {
      const nextMode = await cameraSource.switchCamera();
      setFacingMode(nextMode);
      setHasTorchCapability(cameraSource.hasTorch());
      setTorchEnabled(cameraSource.isTorchOn());
    } catch {
      // Keep current state on error
    } finally {
      setIsSwitching(false);
    }
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="camera-viewfinder-title"
      className="fixed inset-0 z-50 flex flex-col items-center justify-between bg-black/90 p-6 text-white backdrop-blur-md animate-in fade-in duration-200"
    >
      {/* Modal Top Bar */}
      <div className="w-full max-w-xl flex items-center justify-between pt-2">
        <h2
          id="camera-viewfinder-title"
          className="text-2xl font-bold tracking-tight text-white"
        >
          {t("camera.title")}
        </h2>
        <div className="flex items-center gap-2">
          {!hasPermissionError && (
            <Button
              type="button"
              variant="outline"
              size="lg"
              className="border-white/40 bg-white/10 text-white hover:bg-white/20"
              onClick={handleSwitchCamera}
              data-testid="camera-switch-button"
              disabled={isSwitching}
            >
              <SwitchCamera className="mr-2 h-5 w-5" aria-hidden="true" />
              {facingMode === "user" ? t("camera.backCamera") : t("camera.frontCamera")}
            </Button>
          )}
          {hasTorchCapability && !hasPermissionError && (
            <Button
              type="button"
              variant="outline"
              size="lg"
              className="border-white/40 bg-white/10 text-white hover:bg-white/20"
              onClick={handleToggleTorch}
              aria-pressed={torchEnabled}
            >
              {torchEnabled ? t("camera.torchOff") : t("camera.torchOn")}
            </Button>
          )}
        </div>
      </div>

      {/* Main Viewfinder Area */}
      <div className="relative flex flex-1 w-full max-w-xl items-center justify-center my-6">
        {hasPermissionError ? (
          <div className="w-full rounded-2xl border border-destructive/40 bg-destructive/10 p-8 text-center space-y-6">
            <h3 className="text-2xl font-semibold text-white">
              {t("camera.permissionBlockedTitle")}
            </h3>
            <p className="text-lg text-white/80 leading-relaxed">
              {t("camera.permissionBlockedDetail")}
            </p>
            {onManualEntry && (
              <div className="pt-2">
                <Button
                  type="button"
                  variant="secondary"
                  size="lg"
                  className="min-h-14 w-full text-lg font-semibold"
                  onClick={() => {
                    onClose();
                    onManualEntry();
                  }}
                >
                  {t("camera.manualEntryButton")}
                </Button>
              </div>
            )}
          </div>
        ) : (
          <div className="relative w-full aspect-4/3 max-h-[60vh] overflow-hidden rounded-2xl border-2 border-white/20 bg-black/40 shadow-2xl flex items-center justify-center">
            {/* Video Feed Mount Point */}
            <div
              ref={containerRef}
              data-testid="camera-preview-feed"
              className="absolute inset-0 flex items-center justify-center"
            />

            {/* Target Reticle / Scanning Frame */}
            <div
              aria-hidden="true"
              className="relative z-10 w-3/4 h-1/2 rounded-xl border-4 border-dashed border-primary/90 bg-primary/5 flex items-center justify-center pointer-events-none shadow-[0_0_0_9999px_rgba(0,0,0,0.4)]"
            >
              <div className="w-full h-0.5 bg-primary/80 animate-pulse" />
            </div>

            {/* Instructions Prompt */}
            <div className="absolute bottom-4 z-20 rounded-full bg-black/75 px-6 py-2 text-center">
              <p className="text-base font-medium text-white">
                {t("camera.instruction")}
              </p>
            </div>
          </div>
        )}
      </div>

      {/* Modal Actions */}
      <div className="w-full max-w-xl pb-2">
        <Button
          type="button"
          variant="outline"
          className="min-h-16 w-full text-xl font-bold bg-white text-black hover:bg-white/90 border-transparent shadow-lg"
          onClick={onClose}
        >
          {t("common.cancel")}
        </Button>
      </div>
    </div>
  );
}

export function CameraOverlay({ isOpen, ...props }: CameraOverlayProps) {
  if (!isOpen) return null;
  return <CameraOverlayContent {...props} />;
}
