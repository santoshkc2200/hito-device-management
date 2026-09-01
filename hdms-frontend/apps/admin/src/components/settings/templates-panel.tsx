import { useState, useEffect } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { useLocalizedResolver } from "@/lib/localized-resolver";
import { toast } from "sonner";
import {
  getSettings,
  updateSettings,
} from "@hdms/api-client";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { LoadingState } from "@/components/states";
import { useRole } from "@/lib/use-role";
import {
  Save,
  Printer,
  FileText,
  Grid,
  Plus,
  Trash2,
  AlertTriangle,
  Sparkles,
} from "lucide-react";

import { saveLabelSheetSettings } from "@/lib/label-settings";
import { useT } from "@/i18n";

const labelTemplateSchema = z.object({
  sheetWidthMm: z.number().min(50).max(1000),
  sheetHeightMm: z.number().min(50).max(1000),
  columns: z.number().int().min(1).max(20),
  rows: z.number().int().min(1).max(50),
  marginTopMm: z.number().min(0).max(200),
  marginLeftMm: z.number().min(0).max(200),
  gutterXMm: z.number().min(0).max(100),
  gutterYMm: z.number().min(0).max(100),
  labelWidthMm: z.number().min(10).max(500),
  labelHeightMm: z.number().min(5).max(500),
});

type LabelTemplateFormValues = z.infer<typeof labelTemplateSchema>;

const slipTemplateSchema = z.object({
  hospitalName: z.string().min(1, "validation.hospitalNameRequired"),
  pageRefFormat: z.string().min(1, "validation.pageRefFormatRequired"),
  rowsPerPage: z.number().int().min(5, "validation.rowsPerPageMin").max(100, "validation.rowsPerPageMax"),
  columns: z.array(z.string().min(1, "validation.columnNameRequired")).min(1, "validation.columnsMin"),
});

type SlipTemplateFormValues = z.infer<typeof slipTemplateSchema>;

const PRESETS: Record<string, { values: LabelTemplateFormValues }> = {
  "a4-3x8": {
    values: {
      sheetWidthMm: 210,
      sheetHeightMm: 297,
      columns: 3,
      rows: 8,
      marginTopMm: 15,
      marginLeftMm: 8,
      gutterXMm: 4,
      gutterYMm: 4,
      labelWidthMm: 60,
      labelHeightMm: 30,
    },
  },
  "a4-4x10": {
    values: {
      sheetWidthMm: 210,
      sheetHeightMm: 297,
      columns: 4,
      rows: 10,
      marginTopMm: 10,
      marginLeftMm: 7,
      gutterXMm: 3,
      gutterYMm: 3,
      labelWidthMm: 45,
      labelHeightMm: 25,
    },
  },
  "a4-2x5": {
    values: {
      sheetWidthMm: 210,
      sheetHeightMm: 297,
      columns: 2,
      rows: 5,
      marginTopMm: 15,
      marginLeftMm: 10,
      gutterXMm: 5,
      gutterYMm: 5,
      labelWidthMm: 90,
      labelHeightMm: 50,
    },
  },
};

function presetLabel(t: ReturnType<typeof useT>, key: string): string {
  switch (key) {
    case "a4-3x8":
      return t("templatesPanel.presetA4Standard");
    case "a4-4x10":
      return t("templatesPanel.presetA4Compact");
    case "a4-2x5":
      return t("templatesPanel.presetA4LargeTag");
    default:
      return key;
  }
}

export function TemplatesPanel() {
  const t = useT();
  const queryClient = useQueryClient();
  const { role } = useRole();
  const isAdmin = role === "admin";

  const [newColumnInput, setNewColumnInput] = useState("");

  // 1. Fetch Settings
  const {
    data: settingsData,
    isLoading,
    error,
  } = useQuery({
    queryKey: ["settings"],
    queryFn: async () => {
      const res = await getSettings();
      if (res.error) throw res.error;
      return res.data;
    },
  });

  // 2. Label Template Form
  const labelForm = useForm<LabelTemplateFormValues>({
    resolver: useLocalizedResolver(labelTemplateSchema),
    defaultValues: {
      sheetWidthMm: 210,
      sheetHeightMm: 297,
      columns: 3,
      rows: 8,
      marginTopMm: 15,
      marginLeftMm: 8,
      gutterXMm: 4,
      gutterYMm: 4,
      labelWidthMm: 60,
      labelHeightMm: 30,
    },
  });

  // 3. Slip Template Form
  const slipForm = useForm<SlipTemplateFormValues>({
    resolver: useLocalizedResolver(slipTemplateSchema),
    defaultValues: {
      hospitalName: "HITO HOSPITAL",
      pageRefFormat: "REF-YYYY-MM-pNN",
      rowsPerPage: 25,
      columns: ["#", "Asset Tag", "Device Name", "Employee ID", "Borrow Date", "Return Date", "Sign / Note"],
    },
  });

  useEffect(() => {
    if (settingsData) {
      if (settingsData.labelTemplate) {
        labelForm.reset({
          sheetWidthMm: Number(settingsData.labelTemplate.sheetWidthMm),
          sheetHeightMm: Number(settingsData.labelTemplate.sheetHeightMm),
          columns: settingsData.labelTemplate.columns,
          rows: settingsData.labelTemplate.rows,
          marginTopMm: Number(settingsData.labelTemplate.marginTopMm),
          marginLeftMm: Number(settingsData.labelTemplate.marginLeftMm),
          gutterXMm: Number(settingsData.labelTemplate.gutterXMm),
          gutterYMm: Number(settingsData.labelTemplate.gutterYMm),
          labelWidthMm: Number(settingsData.labelTemplate.labelWidthMm),
          labelHeightMm: Number(settingsData.labelTemplate.labelHeightMm),
        });

        // Sync to client-side localStorage helper as well for fallback
        saveLabelSheetSettings({
          pageWidthMm: Number(settingsData.labelTemplate.sheetWidthMm),
          pageHeightMm: Number(settingsData.labelTemplate.sheetHeightMm),
          columns: settingsData.labelTemplate.columns,
          rows: settingsData.labelTemplate.rows,
          marginTopMm: Number(settingsData.labelTemplate.marginTopMm),
          marginLeftMm: Number(settingsData.labelTemplate.marginLeftMm),
          gapXMm: Number(settingsData.labelTemplate.gutterXMm),
          gapYMm: Number(settingsData.labelTemplate.gutterYMm),
          labelWidthMm: Number(settingsData.labelTemplate.labelWidthMm),
          labelHeightMm: Number(settingsData.labelTemplate.labelHeightMm),
        });
      }

      if (settingsData.slipTemplate) {
        slipForm.reset({
          hospitalName: settingsData.slipTemplate.hospitalName,
          pageRefFormat: settingsData.slipTemplate.pageRefFormat,
          rowsPerPage: settingsData.slipTemplate.rowsPerPage,
          columns: settingsData.slipTemplate.columns,
        });
      }
    }
  }, [settingsData, labelForm, slipForm]);

  // Mutations
  const updateLabelTemplateMutation = useMutation({
    mutationFn: async (values: LabelTemplateFormValues) => {
      const res = await updateSettings({
        body: {
          labelTemplate: values,
        },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (data) => {
      queryClient.setQueryData(["settings"], data);
      if (data.labelTemplate) {
        saveLabelSheetSettings({
          pageWidthMm: Number(data.labelTemplate.sheetWidthMm),
          pageHeightMm: Number(data.labelTemplate.sheetHeightMm),
          columns: data.labelTemplate.columns,
          rows: data.labelTemplate.rows,
          marginTopMm: Number(data.labelTemplate.marginTopMm),
          marginLeftMm: Number(data.labelTemplate.marginLeftMm),
          gapXMm: Number(data.labelTemplate.gutterXMm),
          gapYMm: Number(data.labelTemplate.gutterYMm),
          labelWidthMm: Number(data.labelTemplate.labelWidthMm),
          labelHeightMm: Number(data.labelTemplate.labelHeightMm),
        });
      }
      toast.success(t("templatesPanel.labelTemplateSaved"));
    },
    onError: (err: any) => {
      toast.error(err?.detail || t("templatesPanel.labelTemplateSaveFailed"));
    },
  });

  const updateSlipTemplateMutation = useMutation({
    mutationFn: async (values: SlipTemplateFormValues) => {
      const res = await updateSettings({
        body: {
          slipTemplate: values,
        },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (data) => {
      queryClient.setQueryData(["settings"], data);
      toast.success(t("templatesPanel.slipTemplateSaved"));
    },
    onError: (err: any) => {
      toast.error(err?.detail || t("templatesPanel.slipTemplateSaveFailed"));
    },
  });

  const watchedLabelValues = labelForm.watch();
  const currentColumns = slipForm.watch("columns") || [];

  const handleApplyPreset = (presetKey: string) => {
    const preset = PRESETS[presetKey];
    if (preset) {
      labelForm.reset(preset.values);
      toast.info(t("templatesPanel.appliedPresetToast", { preset: presetLabel(t, presetKey) }));
    }
  };

  const handleAddColumn = () => {
    const trimmed = newColumnInput.trim();
    if (!trimmed) return;
    if (currentColumns.includes(trimmed)) {
      toast.error(t("templatesPanel.columnAlreadyExists"));
      return;
    }
    slipForm.setValue("columns", [...currentColumns, trimmed], { shouldDirty: true });
    setNewColumnInput("");
  };

  const handleRemoveColumn = (index: number) => {
    if (currentColumns.length <= 1) {
      toast.error(t("templatesPanel.mustHaveOneColumn"));
      return;
    }
    const updated = currentColumns.filter((_, i) => i !== index);
    slipForm.setValue("columns", updated, { shouldDirty: true });
  };

  const handlePrintTestSheet = () => {
    const { sheetWidthMm, sheetHeightMm, columns, rows, marginTopMm, marginLeftMm, gutterXMm, gutterYMm, labelWidthMm, labelHeightMm } = watchedLabelValues;
    const printWindow = window.open("", "_blank");
    if (!printWindow) {
      toast.error(t("templatesPanel.popupBlocked"));
      return;
    }

    const totalLabels = columns * rows;
    const sampleEquipmentLabel = t("templatesPanel.sampleEquipmentName");
    const testLabelSub = t("templatesPanel.testLabelSub");
    let labelBoxes = "";
    for (let i = 1; i <= totalLabels; i++) {
      labelBoxes += `
        <div class="label-box">
          <div class="qr-mock"></div>
          <div class="label-text">
            <div class="tag">DEV-TEST-${String(i).padStart(3, "0")}</div>
            <div class="name">${sampleEquipmentLabel} ${i}</div>
            <div class="sub">${testLabelSub}</div>
          </div>
        </div>
      `;
    }

    printWindow.document.write(`
      <!DOCTYPE html>
      <html>
      <head>
        <title>${t("templatesPanel.testLabelSheetTitle")}</title>
        <style>
          @page {
            size: ${sheetWidthMm}mm ${sheetHeightMm}mm;
            margin: 0;
          }
          body {
            margin: 0;
            padding: 0;
            background: white;
            font-family: system-ui, -apple-system, sans-serif;
          }
          .sheet {
            width: ${sheetWidthMm}mm;
            height: ${sheetHeightMm}mm;
            padding-top: ${marginTopMm}mm;
            padding-left: ${marginLeftMm}mm;
            box-sizing: border-box;
            display: grid;
            grid-template-columns: repeat(${columns}, ${labelWidthMm}mm);
            grid-auto-rows: ${labelHeightMm}mm;
            column-gap: ${gutterXMm}mm;
            row-gap: ${gutterYMm}mm;
          }
          .label-box {
            width: ${labelWidthMm}mm;
            height: ${labelHeightMm}mm;
            box-sizing: border-box;
            border: 1px dashed #cbd5e1;
            padding: 2mm;
            display: flex;
            align-items: center;
            gap: 2mm;
            overflow: hidden;
          }
          .qr-mock {
            width: 16mm;
            height: 16mm;
            background: #0f172a;
            flex-shrink: 0;
          }
          .label-text {
            display: flex;
            flex-direction: column;
            justify-content: center;
            line-height: 1.1;
          }
          .tag { font-size: 8pt; font-weight: bold; font-family: monospace; }
          .name { font-size: 6.5pt; color: #334155; }
          .sub { font-size: 5.5pt; color: #64748b; margin-top: 1mm; }
        </style>
      </head>
      <body>
        <div class="sheet">
          ${labelBoxes}
        </div>
        <script>
          window.onload = function() { window.print(); };
        </script>
      </body>
      </html>
    `);
    printWindow.document.close();
  };

  const handlePrintBlankRegisterPad = () => {
    const slipValues = slipForm.getValues();
    const printWindow = window.open("", "_blank");
    if (!printWindow) {
      toast.error(t("templatesPanel.popupBlocked"));
      return;
    }

    const rowsCount = slipValues.rowsPerPage || 25;
    let tableRows = "";
    for (let r = 1; r <= rowsCount; r++) {
      tableRows += "<tr>";
      slipValues.columns.forEach((_, idx) => {
        if (idx === 0) {
          tableRows += `<td style="text-align: center; width: 8mm; font-weight: 500;">${r}</td>`;
        } else {
          tableRows += `<td style="height: 8.5mm;"></td>`;
        }
      });
      tableRows += "</tr>";
    }


    const headerCells = slipValues.columns
      .map((col) => `<th style="padding: 2mm 1mm; border: 1px solid #334155; background: #f8fafc; font-size: 8pt; text-align: center;">${col}</th>`)
      .join("");

    printWindow.document.write(`
      <!DOCTYPE html>
      <html>
      <head>
        <title>${t("templatesPanel.registerLogSheetTitle")}</title>
        <style>
          @page {
            size: A4 portrait;
            margin: 10mm;
          }
          body {
            font-family: "Helvetica Neue", Arial, sans-serif;
            color: #0f172a;
            margin: 0;
            padding: 0;
          }
          .header {
            display: flex;
            justify-content: space-between;
            align-items: flex-end;
            border-bottom: 2px solid #0f172a;
            padding-bottom: 3mm;
            margin-bottom: 4mm;
          }
          .title { font-size: 14pt; font-weight: bold; }
          .subtitle { font-size: 9pt; color: #475569; }
          .meta-box {
            display: flex;
            gap: 6mm;
            font-size: 8.5pt;
            margin-bottom: 4mm;
            background: #f1f5f9;
            padding: 2.5mm 4mm;
            border-radius: 2px;
          }
          table {
            width: 100%;
            border-collapse: collapse;
            font-size: 8.5pt;
          }
          th, td {
            border: 1px solid #94a3b8;
          }
          .footer {
            margin-top: 5mm;
            display: flex;
            justify-content: space-between;
            font-size: 7.5pt;
            color: #64748b;
          }
        </style>
      </head>
      <body>
        <div class="header">
          <div>
            <div class="title">${slipValues.hospitalName}</div>
            <div class="subtitle">${t("templatesPanel.registerLogSubtitle")}</div>
          </div>
          <div style="text-align: right; font-family: monospace; font-size: 10pt; font-weight: bold;">
            ${slipValues.pageRefFormat}
          </div>
        </div>

        <div class="meta-box">
          <div><strong>${t("templatesPanel.wardDepartmentLabel")}</strong> ________________________</div>
          <div><strong>${t("templatesPanel.dateLabel")}</strong> 20____ / ____ / ____</div>
          <div><strong>${t("templatesPanel.supervisorLabel")}</strong> ________________________</div>
        </div>

        <table>
          <thead>
            <tr>${headerCells}</tr>
          </thead>
          <tbody>
            ${tableRows}
          </tbody>
        </table>

        <div class="footer">
          <div>${t("templatesPanel.transcribeHint")}</div>
          <div>${t("templatesPanel.footerVersionTag")}</div>
        </div>

        <script>
          window.onload = function() { window.print(); };
        </script>
      </body>
      </html>
    `);
    printWindow.document.close();
  };

  if (isLoading) {
    return <LoadingState message={t("templatesPanel.loadingTemplates")} />;
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center p-8 text-center text-destructive">
        <AlertTriangle className="size-8 mb-2" />
        <p className="font-semibold">{t("templatesPanel.loadFailedTitle")}</p>
        <p className="text-xs text-muted-foreground mt-1">{t("kiosksPanel.tryRefreshing")}</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* 1. Label Sheet Geometry Card */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
          <div>
            <CardTitle>{t("templatesPanel.labelSheetLayoutTitle")}</CardTitle>
            <CardDescription>{t("templatesPanel.labelSheetLayoutDescription")}</CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={handlePrintTestSheet}
              className="gap-1.5"
            >
              <Printer className="size-4" />
              {t("templatesPanel.printCalibrationTestSheet")}
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {/* Quick Presets */}
          <div className="mb-6 flex flex-wrap items-center gap-2 rounded-lg bg-muted/60 p-3">
            <span className="text-xs font-semibold text-foreground flex items-center gap-1.5 mr-2">
              <Sparkles className="size-3.5 text-primary" />
              {t("templatesPanel.standardPresetsLabel")}
            </span>
            {Object.entries(PRESETS).map(([key]) => (
              <Button
                key={key}
                type="button"
                variant="secondary"
                size="sm"
                onClick={() => handleApplyPreset(key)}
                className="text-xs h-7"
              >
                {presetLabel(t, key)}
              </Button>
            ))}
          </div>

          <form
            onSubmit={labelForm.handleSubmit((v) => updateLabelTemplateMutation.mutate(v))}
            className="space-y-6"
          >
            <div className="grid gap-6 lg:grid-cols-3">
              {/* Form Input Columns */}
              <div className="lg:col-span-2 grid gap-4 sm:grid-cols-2 md:grid-cols-3">
                <div className="space-y-1.5">
                  <label htmlFor="sheetWidthMm" className="text-xs font-medium">{t("templatesPanel.sheetWidthLabel")}</label>
                  <Input
                    id="sheetWidthMm"
                    type="number"
                    step="0.1"
                    disabled={!isAdmin}
                    {...labelForm.register("sheetWidthMm", { valueAsNumber: true })}
                  />
                </div>

                <div className="space-y-1.5">
                  <label htmlFor="sheetHeightMm" className="text-xs font-medium">{t("templatesPanel.sheetHeightLabel")}</label>
                  <Input
                    id="sheetHeightMm"
                    type="number"
                    step="0.1"
                    disabled={!isAdmin}
                    {...labelForm.register("sheetHeightMm", { valueAsNumber: true })}
                  />
                </div>

                <div className="space-y-1.5">
                  <label htmlFor="columns" className="text-xs font-medium">{t("templatesPanel.gridColumnsLabel")}</label>
                  <Input
                    id="columns"
                    type="number"
                    min={1}
                    max={20}
                    disabled={!isAdmin}
                    {...labelForm.register("columns", { valueAsNumber: true })}
                  />
                </div>

                <div className="space-y-1.5">
                  <label htmlFor="rows" className="text-xs font-medium">{t("templatesPanel.gridRowsLabel")}</label>
                  <Input
                    id="rows"
                    type="number"
                    min={1}
                    max={50}
                    disabled={!isAdmin}
                    {...labelForm.register("rows", { valueAsNumber: true })}
                  />
                </div>

                <div className="space-y-1.5">
                  <label htmlFor="marginTopMm" className="text-xs font-medium">{t("templatesPanel.marginTopLabel")}</label>
                  <Input
                    id="marginTopMm"
                    type="number"
                    step="0.1"
                    disabled={!isAdmin}
                    {...labelForm.register("marginTopMm", { valueAsNumber: true })}
                  />
                </div>

                <div className="space-y-1.5">
                  <label htmlFor="marginLeftMm" className="text-xs font-medium">{t("templatesPanel.marginLeftLabel")}</label>
                  <Input
                    id="marginLeftMm"
                    type="number"
                    step="0.1"
                    disabled={!isAdmin}
                    {...labelForm.register("marginLeftMm", { valueAsNumber: true })}
                  />
                </div>

                <div className="space-y-1.5">
                  <label htmlFor="gutterXMm" className="text-xs font-medium">{t("templatesPanel.horizontalGapLabel")}</label>
                  <Input
                    id="gutterXMm"
                    type="number"
                    step="0.1"
                    disabled={!isAdmin}
                    {...labelForm.register("gutterXMm", { valueAsNumber: true })}
                  />
                </div>

                <div className="space-y-1.5">
                  <label htmlFor="gutterYMm" className="text-xs font-medium">{t("templatesPanel.verticalGapLabel")}</label>
                  <Input
                    id="gutterYMm"
                    type="number"
                    step="0.1"
                    disabled={!isAdmin}
                    {...labelForm.register("gutterYMm", { valueAsNumber: true })}
                  />
                </div>

                <div className="space-y-1.5">
                  <label htmlFor="labelWidthMm" className="text-xs font-medium">{t("templatesPanel.labelWidthLabel")}</label>
                  <Input
                    id="labelWidthMm"
                    type="number"
                    step="0.1"
                    disabled={!isAdmin}
                    {...labelForm.register("labelWidthMm", { valueAsNumber: true })}
                  />
                </div>

                <div className="space-y-1.5">
                  <label htmlFor="labelHeightMm" className="text-xs font-medium">{t("templatesPanel.labelHeightLabel")}</label>
                  <Input
                    id="labelHeightMm"
                    type="number"
                    step="0.1"
                    disabled={!isAdmin}
                    {...labelForm.register("labelHeightMm", { valueAsNumber: true })}
                  />
                </div>
              </div>

              {/* Dynamic Visual Mini-Preview */}
              <div className="flex flex-col items-center justify-center rounded-xl border bg-muted/30 p-4">
                <span className="text-xs font-medium text-muted-foreground mb-2 flex items-center gap-1">
                  <Grid className="size-3.5" />
                  {t("templatesPanel.liveLayoutPreview", {
                    columns: watchedLabelValues.columns || 3,
                    rows: watchedLabelValues.rows || 8,
                    total: (watchedLabelValues.columns || 3) * (watchedLabelValues.rows || 8),
                  })}
                </span>
                <div
                  className="relative border-2 border-primary/40 bg-white dark:bg-zinc-900 rounded shadow-xs overflow-hidden"
                  style={{
                    width: "160px",
                    height: `${(160 * (watchedLabelValues.sheetHeightMm || 297)) / (watchedLabelValues.sheetWidthMm || 210)}px`,
                    maxHeight: "230px",
                  }}
                >
                  <div
                    className="w-full h-full grid p-1 gap-0.5"
                    style={{
                      gridTemplateColumns: `repeat(${watchedLabelValues.columns || 3}, 1fr)`,
                      gridTemplateRows: `repeat(${watchedLabelValues.rows || 8}, 1fr)`,
                    }}
                  >
                    {Array.from({ length: Math.min(60, (watchedLabelValues.columns || 3) * (watchedLabelValues.rows || 8)) }).map((_, i) => (
                      <div
                        key={i}
                        className="border border-primary/30 bg-primary/10 rounded-[1px]"
                      />
                    ))}
                  </div>
                </div>
              </div>
            </div>

            {isAdmin && (
              <div className="flex justify-end pt-4 border-t">
                <Button
                  type="submit"
                  disabled={updateLabelTemplateMutation.isPending || !labelForm.formState.isDirty}
                  className="gap-2"
                >
                  <Save className="size-4" />
                  {updateLabelTemplateMutation.isPending ? t("templatesPanel.savingLabelTemplate") : t("templatesPanel.saveLabelTemplate")}
                </Button>
              </div>
            )}
          </form>
        </CardContent>
      </Card>

      {/* 2. Paper Register Slip Template Card */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
          <div>
            <CardTitle>{t("templatesPanel.slipPadTitle")}</CardTitle>
            <CardDescription>{t("templatesPanel.slipPadDescription")}</CardDescription>
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={handlePrintBlankRegisterPad}
            className="gap-1.5"
          >
            <FileText className="size-4" />
            {t("templatesPanel.printBlankRegisterPad")}
          </Button>
        </CardHeader>
        <CardContent>
          <form
            onSubmit={slipForm.handleSubmit((v) => updateSlipTemplateMutation.mutate(v))}
            className="space-y-6"
          >
            <div className="grid gap-6 sm:grid-cols-3">
              <div className="space-y-1.5">
                <label htmlFor="hospitalName" className="text-xs font-medium">{t("templatesPanel.hospitalHeaderNameLabel")}</label>
                <Input
                  id="hospitalName"
                  disabled={!isAdmin}
                  {...slipForm.register("hospitalName")}
                />
                {slipForm.formState.errors.hospitalName && (
                  <p className="text-xs text-destructive">{slipForm.formState.errors.hospitalName.message}</p>
                )}
              </div>

              <div className="space-y-1.5">
                <label htmlFor="pageRefFormat" className="text-xs font-medium">{t("templatesPanel.pageRefFormatLabel")}</label>
                <Input
                  id="pageRefFormat"
                  disabled={!isAdmin}
                  {...slipForm.register("pageRefFormat")}
                />
                {slipForm.formState.errors.pageRefFormat && (
                  <p className="text-xs text-destructive">{slipForm.formState.errors.pageRefFormat.message}</p>
                )}
              </div>

              <div className="space-y-1.5">
                <label htmlFor="rowsPerPage" className="text-xs font-medium">{t("templatesPanel.rowsPerPageLabel")}</label>
                <Input
                  id="rowsPerPage"
                  type="number"
                  min={5}
                  max={100}
                  disabled={!isAdmin}
                  {...slipForm.register("rowsPerPage", { valueAsNumber: true })}
                />
                {slipForm.formState.errors.rowsPerPage && (
                  <p className="text-xs text-destructive">{slipForm.formState.errors.rowsPerPage.message}</p>
                )}
              </div>
            </div>

            {/* Column List Editor */}
            <div className="space-y-3 pt-2">
              <label className="text-xs font-medium">{t("templatesPanel.printedTableColumnsLabel")}</label>
              <div className="flex flex-wrap gap-2 p-3 border rounded-lg bg-muted/20">
                {currentColumns.map((col, index) => (
                  <Badge key={index} variant="secondary" className="gap-1.5 py-1 px-2.5 text-xs font-normal">
                    <span>{col}</span>
                    {isAdmin && (
                      <button
                        type="button"
                        onClick={() => handleRemoveColumn(index)}
                        className="hover:text-destructive text-muted-foreground ml-1"
                        title={t("templatesPanel.removeColumn")}
                      >
                        <Trash2 className="size-3" />
                      </button>
                    )}
                  </Badge>
                ))}
              </div>

              {isAdmin && (
                <div className="flex items-center gap-2 pt-1 max-w-sm">
                  <Input
                    placeholder={t("templatesPanel.newColumnNamePlaceholder")}
                    value={newColumnInput}
                    onChange={(e) => setNewColumnInput(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        e.preventDefault();
                        handleAddColumn();
                      }
                    }}
                    className="h-8 text-xs"
                  />
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={handleAddColumn}
                    className="h-8 text-xs gap-1"
                  >
                    <Plus className="size-3.5" />
                    {t("templatesPanel.addColumn")}
                  </Button>
                </div>
              )}
            </div>

            {isAdmin && (
              <div className="flex justify-end pt-4 border-t">
                <Button
                  type="submit"
                  disabled={updateSlipTemplateMutation.isPending || !slipForm.formState.isDirty}
                  className="gap-2"
                >
                  <Save className="size-4" />
                  {updateSlipTemplateMutation.isPending ? t("templatesPanel.savingSlipTemplate") : t("templatesPanel.saveSlipTemplate")}
                </Button>
              </div>
            )}
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
