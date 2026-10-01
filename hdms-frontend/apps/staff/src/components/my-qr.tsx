import bwipjs from "bwip-js/browser";
import { useEffect, useMemo, useState, type JSX } from "react";
import { Button } from "@/components/ui/button";
import { useT } from "@/i18n";
import { releaseScreenWakeLock, requestScreenWakeLock } from "@/lib/screen";

type Symbology = "qrcode" | "code128";

// Per-browser preference; barcode unless the staff member picked QR. Both
// encode the same token, so the kiosk resolves either identically.
const STORAGE_KEY = "hdms.staff.codeSymbology";

function loadSymbology(): Symbology {
  try {
    return localStorage.getItem(STORAGE_KEY) === "qrcode" ? "qrcode" : "code128";
  } catch {
    return "code128";
  }
}

// Linear codes need an explicit bar height (mm); 2D codes must not get one.
function symbologyOptions(token: string, symbology: Symbology) {
  return {
    bcid: symbology,
    text: token,
    scale: symbology === "code128" ? 4 : 6,
    includetext: false,
    ...(symbology === "code128" ? { height: 20 } : {}),
  };
}

/**
 * Render the token as a data-URL image rather than a bare canvas, so it carries
 * an accessible name and can be long-pressed and saved on iOS.
 */
function renderCode(token: string, symbology: Symbology): string {
  if (!token) return "";
  try {
    const canvas = document.createElement("canvas");
    bwipjs.toCanvas(canvas, { ...symbologyOptions(token, symbology), backgroundcolor: "FFFFFF" });
    return canvas.toDataURL("image/png");
  } catch {
    // jsdom and any browser without a 2D context: fall back to the vector form
    try {
      const svg = bwipjs.toSVG(symbologyOptions(token, symbology));
      return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`;
    } catch {
      return "";
    }
  }
}

export function MyQr(props: { token: string }): JSX.Element {
  const { token } = props;
  const t = useT();

  const [symbology, setSymbology] = useState<Symbology>(loadSymbology);
  const dataUrl = useMemo(() => renderCode(token, symbology), [token, symbology]);

  function choose(next: Symbology) {
    setSymbology(next);
    try {
      localStorage.setItem(STORAGE_KEY, next);
    } catch {
      // preference just won't persist
    }
  }

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
        alt={symbology === "code128" ? t("myQr.altBarcode") : t("myQr.alt")}
        className={
          symbology === "code128"
            ? "h-auto w-full max-w-sm rounded-xl border bg-white p-3 shadow-sm"
            : "size-64 rounded-xl border bg-white p-2 shadow-sm"
        }
      />
      <div className="flex gap-2" role="group" aria-label={t("myQr.codeType")}>
        {(["code128", "qrcode"] as const).map((s) => (
          <Button
            key={s}
            size="sm"
            variant={symbology === s ? "default" : "outline"}
            aria-pressed={symbology === s}
            onClick={() => choose(s)}
          >
            {s === "code128" ? t("myQr.barcode") : t("myQr.qr")}
          </Button>
        ))}
      </div>
      <p className="max-w-xs text-sm text-muted-foreground">{t("myQr.instructions")}</p>
    </div>
  );
}
