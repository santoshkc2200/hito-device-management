import { createRoute } from "@tanstack/react-router";
import { Printer } from "lucide-react";
import { DeviceStickerLabel, LabelSheet } from "@/components/label-templates";
import { RegisterSlip } from "@/components/register-slip";
import { Button } from "@/components/ui/button";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useT } from "@/i18n";
import { defaultLabelSheetSettings, useLabelSheetSettings } from "@/lib/label-settings";
import { authenticatedRoute } from "./authenticated";

const SAMPLE_TOKENS = ["HD-D-7K3M9QXA2F-4", "HD-D-2B8N4RSD6H-1", "HD-D-9X1F5TVWM3-7"];

function SettingsField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number;
  onChange: (v: number) => void;
}) {
  return (
    <Field>
      <FieldLabel>{label}</FieldLabel>
      <Input
        type="number"
        min={0}
        step="any"
        value={value}
        onChange={(e) => onChange(Number(e.target.value) || 0)}
      />
    </Field>
  );
}

function SheetSettingsTab() {
  const t = useT();
  const [settings, setSettings] = useLabelSheetSettings();
  const previewCount = settings.columns * Math.min(settings.rows, 3);

  return (
    <div className="grid grid-cols-[280px_1fr] gap-6">
      <div className="flex flex-col gap-4">
        <FieldGroup>
          <div className="grid grid-cols-2 gap-3">
            <SettingsField
              label={t("labelsPage.columnsLabel")}
              value={settings.columns}
              onChange={(v) => setSettings({ ...settings, columns: v })}
            />
            <SettingsField
              label={t("labelsPage.rowsLabel")}
              value={settings.rows}
              onChange={(v) => setSettings({ ...settings, rows: v })}
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <SettingsField
              label={t("labelsPage.labelWidthLabel")}
              value={settings.labelWidthMm}
              onChange={(v) => setSettings({ ...settings, labelWidthMm: v })}
            />
            <SettingsField
              label={t("labelsPage.labelHeightLabel")}
              value={settings.labelHeightMm}
              onChange={(v) => setSettings({ ...settings, labelHeightMm: v })}
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <SettingsField
              label={t("labelsPage.topMarginLabel")}
              value={settings.marginTopMm}
              onChange={(v) => setSettings({ ...settings, marginTopMm: v })}
            />
            <SettingsField
              label={t("labelsPage.leftMarginLabel")}
              value={settings.marginLeftMm}
              onChange={(v) => setSettings({ ...settings, marginLeftMm: v })}
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <SettingsField
              label={t("labelsPage.columnGapLabel")}
              value={settings.gapXMm}
              onChange={(v) => setSettings({ ...settings, gapXMm: v })}
            />
            <SettingsField
              label={t("labelsPage.rowGapLabel")}
              value={settings.gapYMm}
              onChange={(v) => setSettings({ ...settings, gapYMm: v })}
            />
          </div>
        </FieldGroup>
        <Button variant="outline" size="sm" onClick={() => setSettings(defaultLabelSheetSettings)}>
          {t("labelsPage.resetToDefault")}
        </Button>
      </div>
      <div className="flex flex-col gap-3">
        <div className="flex items-center justify-between">
          <p className="text-sm font-medium">{t("labelsPage.previewHint")}</p>
          <Button size="sm" onClick={() => window.print()}>
            <Printer className="size-4" data-icon="inline-start" />
            {t("labelsPage.printTestSheet")}
          </Button>
        </div>
        <div className="overflow-auto rounded-md border border-border bg-secondary p-4">
          <LabelSheet settings={settings}>
            {Array.from({ length: previewCount }, (_, i) => (
              <DeviceStickerLabel
                key={i}
                assetTag={`LAPTOP-0${(i % 9) + 1}`}
                name="Dell Latitude"
                model="5420"
                token={SAMPLE_TOKENS[i % SAMPLE_TOKENS.length]!}
                widthMm={settings.labelWidthMm}
                heightMm={settings.labelHeightMm}
              />
            ))}
          </LabelSheet>
        </div>
      </div>
    </div>
  );
}

function RegisterSlipTab() {
  const t = useT();
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <p className="text-sm font-medium">{t("labelsPage.registerSlipHint")}</p>
        <Button size="sm" onClick={() => window.print()}>
          <Printer className="size-4" data-icon="inline-start" />
          {t("labelsPage.printPage")}
        </Button>
      </div>
      <div className="overflow-auto rounded-md border border-border bg-secondary p-4">
        <RegisterSlip />
      </div>
    </div>
  );
}

function LabelsPage() {
  const t = useT();
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">{t("labelsPage.title")}</h1>
      <Tabs defaultValue="sheet">
        <TabsList>
          <TabsTrigger value="sheet">{t("labelsPage.sheetTab")}</TabsTrigger>
          <TabsTrigger value="slip">{t("labelsPage.slipTab")}</TabsTrigger>
        </TabsList>
        <TabsContent value="sheet">
          <SheetSettingsTab />
        </TabsContent>
        <TabsContent value="slip">
          <RegisterSlipTab />
        </TabsContent>
      </Tabs>
    </div>
  );
}

export const labelsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/labels",
  component: LabelsPage,
});
