import bwipjs from "bwip-js/browser";
import { useEffect, useRef } from "react";

// bcid values, not credential kinds — docs/05-credentials-and-labeling.md:
// "qr"/"code128" map straight through; Data Matrix is a print-time symbology
// choice for small items, not a stored credential kind.
export type Symbology = "qrcode" | "code128" | "datamatrix";

// Linear codes need an explicit bar height (mm); 2D codes must not get one or
// bwip-js stretches the modules.
export function barcodeOptions(text: string, symbology: Symbology) {
  return {
    bcid: symbology,
    text,
    includetext: false,
    ...(symbology === "code128" ? { height: 15 } : {}),
  };
}

export function Barcode({
  value,
  symbology = "qrcode",
  scale = 3,
  className,
}: {
  value: string;
  symbology?: Symbology;
  scale?: number;
  className?: string;
}) {
  const ref = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = ref.current;
    if (!canvas || !value) return;
    try {
      bwipjs.toCanvas(canvas, {
        ...barcodeOptions(value, symbology),
        scale,
        backgroundcolor: "FFFFFF",
      });
    } catch (err) {
      // eslint-disable-next-line no-console
      console.error("barcode render failed", err);
    }
  }, [value, symbology, scale]);

  return <canvas ref={ref} className={className} role="img" aria-label={value} />;
}

export function barcodeSvg(value: string, symbology: Symbology = "qrcode"): string {
  return bwipjs.toSVG(barcodeOptions(value, symbology));
}
