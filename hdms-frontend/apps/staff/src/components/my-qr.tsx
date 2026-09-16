import bwipjs from "bwip-js/browser";
import { useEffect, useMemo, type JSX } from "react";
import { useT } from "@/i18n";
import { releaseScreenWakeLock, requestScreenWakeLock } from "@/lib/screen";

/**
 * Render the token as a data-URL image rather than a bare canvas, so it carries
 * an accessible name and can be long-pressed and saved on iOS.
 */
function renderQr(token: string): string {
  if (!token) return "";
  try {
    const canvas = document.createElement("canvas");
    bwipjs.toCanvas(canvas, {
      bcid: "qrcode",
      text: token,
      scale: 6,
      includetext: false,
      backgroundcolor: "FFFFFF",
    });
    return canvas.toDataURL("image/png");
  } catch {
    // jsdom and any browser without a 2D context: fall back to the vector form
    try {
      const svg = bwipjs.toSVG({
        bcid: "qrcode",
        text: token,
        scale: 6,
        includetext: false,
      });
      return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`;
    } catch {
      return "";
    }
  }
}

export function MyQr(props: { token: string }): JSX.Element {
  const { token } = props;
  const t = useT();

  const dataUrl = useMemo(() => renderQr(token), [token]);

  useEffect(() => {
    let sentinel: WakeLockSentinel | null = null;
    let unmounted = false;

    async function acquire() {
      const lock = await requestScreenWakeLock();
      if (unmounted) {
        void releaseScreenWakeLock(lock);
      } else {
        sentinel = lock;
      }
    }

    void acquire();

    // iOS drops the lock when the app backgrounds, so take it again on return.
    const handleVisibility = () => {
      if (document.visibilityState === "visible" && !sentinel) void acquire();
    };
    document.addEventListener("visibilitychange", handleVisibility);

    return () => {
      unmounted = true;
      document.removeEventListener("visibilitychange", handleVisibility);
      void releaseScreenWakeLock(sentinel);
    };
  }, []);

  return (
    <div className="flex flex-col items-center gap-4 text-center">
      <img
        src={dataUrl || undefined}
        alt={t("myQr.alt")}
        className="size-64 rounded-xl border bg-white p-2 shadow-sm"
      />
      <p className="max-w-xs text-sm text-muted-foreground">{t("myQr.instructions")}</p>
    </div>
  );
}
