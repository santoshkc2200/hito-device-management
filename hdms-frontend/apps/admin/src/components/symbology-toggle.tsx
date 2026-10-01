import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useT } from "@/i18n";
import { type CodeSymbology, useCodeSymbology } from "@/lib/code-symbology";

// Sits outside .print-area, so it never reaches the printed page.
export function SymbologyToggle() {
  const t = useT();
  const [symbology, setSymbology] = useCodeSymbology();
  return (
    <Tabs value={symbology} onValueChange={(v) => setSymbology(v as CodeSymbology)}>
      <TabsList aria-label={t("labelTemplates.codeType")}>
        <TabsTrigger value="qrcode">{t("labelTemplates.codeTypeQr")}</TabsTrigger>
        <TabsTrigger value="code128">{t("labelTemplates.codeTypeBarcode")}</TabsTrigger>
      </TabsList>
    </Tabs>
  );
}
