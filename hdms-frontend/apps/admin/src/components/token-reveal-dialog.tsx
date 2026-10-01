import bwipjs from "bwip-js/browser";
import { Download, Printer } from "lucide-react";
import { barcodeOptions, barcodeSvg, type Symbology } from "@/components/barcode";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { DeviceStickerLabel, StaffCardLabel } from "@/components/label-templates";
import { SymbologyToggle } from "@/components/symbology-toggle";
import { useT } from "@/i18n";
import { useCodeSymbology } from "@/lib/code-symbology";

function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

// PNG/SVG export of an individual code, for pasting into other documents
// (docs/05-credentials-and-labeling.md) — independent of the print path.
function exportPng(token: string, symbology: Symbology) {
  const canvas = document.createElement("canvas");
  bwipjs.toCanvas(canvas, { ...barcodeOptions(token, symbology), scale: 6 });
  canvas.toBlob((blob) => blob && downloadBlob(blob, `${token}.png`));
}

function exportSvg(token: string, symbology: Symbology) {
  const svg = barcodeSvg(token, symbology);
  downloadBlob(new Blob([svg], { type: "image/svg+xml" }), `${token}.svg`);
}

export type TokenRevealSubject =
  | { type: "device"; assetTag: string; name: string; model?: string }
  | { type: "user"; fullName: string; employeeNo: string; department?: string };

// The plaintext token is disclosed exactly once by the server (docs/05) —
// this dialog is the only place it's ever shown, so it doubles as the
// single-label print/export surface for that moment.
export function TokenRevealDialog({
  open,
  onOpenChange,
  token,
  subject,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  token: string | undefined;
  subject: TokenRevealSubject;
}) {
  const t = useT();
  const [symbology] = useCodeSymbology();
  if (!token) return null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md" aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle>{t("tokenRevealDialog.title")}</DialogTitle>
        </DialogHeader>
        <div className="flex justify-center">
          <SymbologyToggle />
        </div>
        <style>{`@page { size: ${subject.type === "device" ? "60mm 30mm" : "85.6mm 54mm"}; margin: 0; }`}</style>
        <div className="print-area flex justify-center py-2">
          {subject.type === "device" ? (
            <DeviceStickerLabel
              assetTag={subject.assetTag}
              name={subject.name}
              model={subject.model}
              token={token}
              symbology={symbology}
              widthMm={60}
              heightMm={30}
            />
          ) : (
            <StaffCardLabel
              fullName={subject.fullName}
              employeeNo={subject.employeeNo}
              department={subject.department}
              token={token}
              symbology={symbology}
            />
          )}
        </div>
        <p data-testid="revealed-token" className="text-center font-identifier text-xs text-muted-foreground">{token}</p>
        <DialogFooter className="gap-2 sm:justify-center">
          <Button variant="outline" onClick={() => exportPng(token, symbology)}>
            <Download className="size-4" data-icon="inline-start" />
            {/* i18n-allow-literal: PNG is a file-format acronym, not translatable prose */}
            PNG
          </Button>
          <Button variant="outline" onClick={() => exportSvg(token, symbology)}>
            <Download className="size-4" data-icon="inline-start" />
            {/* i18n-allow-literal: SVG is a file-format acronym, not translatable prose */}
            SVG
          </Button>
          <Button onClick={() => window.print()}>
            <Printer className="size-4" data-icon="inline-start" />
            {t("tokenRevealDialog.print")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
