import type { CSSProperties, ReactNode } from "react";
import { Barcode, type Symbology } from "@/components/barcode";
import { useT } from "@/i18n";
import type { LabelSheetSettings } from "@/lib/label-settings";

// Device sticker — docs/05-credentials-and-labeling.md: QR (or Data Matrix
// for small items) at left, asset tag large enough to read aloud, token
// printed small underneath for the manual-entry fallback.
export function DeviceStickerLabel({
  assetTag,
  name,
  model,
  token,
  symbology = "qrcode",
  widthMm,
  heightMm,
}: {
  assetTag: string;
  name: string;
  model?: string;
  token: string;
  symbology?: Symbology;
  widthMm?: number;
  heightMm?: number;
}) {
  const t = useT();
  return (
    <div
      className="label-template flex items-center gap-2 overflow-hidden border border-dashed border-border bg-white p-1.5 text-black"
      style={{ width: widthMm ? `${widthMm}mm` : "50mm", height: heightMm ? `${heightMm}mm` : "25mm" }}
    >
      <Barcode value={token} symbology={symbology} scale={2} className="h-full shrink-0" />
      <div className="flex min-w-0 flex-col justify-center gap-0.5 leading-tight">
        <div className="truncate font-identifier text-[11px] font-bold">{assetTag}</div>
        <div className="truncate text-[8px]">{[name, model].filter(Boolean).join(" · ")}</div>
        <div className="text-[7px] text-neutral-500">{t("labelTemplates.organizationName")}</div>
        <div className="truncate font-identifier text-[6px] text-neutral-500">{token}</div>
      </div>
    </div>
  );
}

// Staff card — credit-card-size (CR80, 85.6×54mm) sheet for lamination when
// there's no existing ID card to sticker.
export function StaffCardLabel({
  fullName,
  employeeNo,
  department,
  token,
}: {
  fullName: string;
  employeeNo: string;
  department?: string;
  token: string;
}) {
  const t = useT();
  return (
    <div
      className="label-template flex flex-col items-center justify-center gap-2 border border-dashed border-border bg-white p-3 text-center text-black"
      style={{ width: "85.6mm", height: "54mm" }}
    >
      <div className="text-[10px] font-medium tracking-widest text-neutral-500 uppercase">
        {t("labelTemplates.organizationName")}
      </div>
      <Barcode value={token} symbology="qrcode" scale={3} />
      <div className="text-sm font-semibold">{fullName}</div>
      <div className="font-identifier text-xs">{employeeNo}</div>
      {department && <div className="text-[10px] text-neutral-500">{department}</div>}
    </div>
  );
}

// Blank card stock — a real, printable credential with nobody bound to it
// yet (INV-12); the physical card that fills the drawer at the equipment
// desk (docs/05).
export function BlankCardLabel({ token }: { token: string }) {
  const t = useT();
  return (
    <div
      className="label-template flex flex-col items-center justify-center gap-2 border border-dashed border-border bg-white p-3 text-center text-black"
      style={{ width: "85.6mm", height: "54mm" }}
    >
      <div className="text-[10px] font-medium tracking-widest text-neutral-500 uppercase">
        {t("labelTemplates.organizationName")}
      </div>
      <Barcode value={token} symbology="qrcode" scale={3} />
      <div className="text-[9px] text-neutral-500">{t("labelTemplates.blankCardInstruction")}</div>
      <div className="font-identifier text-[9px]">{token}</div>
    </div>
  );
}

// A4/Letter sheet of repeated labels via CSS Grid, laid out at the exact
// physical size that prints (docs/05: "what is previewed is exactly what
// prints") — no separate PDF pipeline.
export function LabelSheet({
  settings,
  children,
}: {
  settings: LabelSheetSettings;
  children: ReactNode;
}) {
  const style: CSSProperties = {
    width: `${settings.pageWidthMm}mm`,
    minHeight: `${settings.pageHeightMm}mm`,
    paddingTop: `${settings.marginTopMm}mm`,
    paddingLeft: `${settings.marginLeftMm}mm`,
    display: "grid",
    gridTemplateColumns: `repeat(${settings.columns}, ${settings.labelWidthMm}mm)`,
    gridAutoRows: `${settings.labelHeightMm}mm`,
    columnGap: `${settings.gapXMm}mm`,
    rowGap: `${settings.gapYMm}mm`,
  };
  return (
    <>
      <style>{`@page { size: ${settings.pageWidthMm}mm ${settings.pageHeightMm}mm; margin: 0; }`}</style>
      <div className="print-area bg-white" style={style}>
        {children}
      </div>
    </>
  );
}
