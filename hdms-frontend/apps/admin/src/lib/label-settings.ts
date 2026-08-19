import { useEffect, useState } from "react";

// Sheet layout is a client-side, per-admin-PC preference (docs/05 defers
// full template configurability to Phase 4's Settings screen) — persisted
// to localStorage so it survives a reload but needs no backend entity.
export interface LabelSheetSettings {
  pageWidthMm: number;
  pageHeightMm: number;
  columns: number;
  rows: number;
  labelWidthMm: number;
  labelHeightMm: number;
  marginTopMm: number;
  marginLeftMm: number;
  gapXMm: number;
  gapYMm: number;
}

export const defaultLabelSheetSettings: LabelSheetSettings = {
  pageWidthMm: 210,
  pageHeightMm: 297,
  columns: 3,
  rows: 8,
  labelWidthMm: 50,
  labelHeightMm: 25,
  marginTopMm: 15,
  marginLeftMm: 8,
  gapXMm: 4,
  gapYMm: 4,
};

const STORAGE_KEY = "hdms.admin.labelSheetSettings";

export function loadLabelSheetSettings(): LabelSheetSettings {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return defaultLabelSheetSettings;
    return { ...defaultLabelSheetSettings, ...JSON.parse(raw) };
  } catch {
    return defaultLabelSheetSettings;
  }
}

export function saveLabelSheetSettings(settings: LabelSheetSettings) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(settings));
}

export function useLabelSheetSettings() {
  const [settings, setSettings] = useState<LabelSheetSettings>(loadLabelSheetSettings);

  useEffect(() => {
    saveLabelSheetSettings(settings);
  }, [settings]);

  return [settings, setSettings] as const;
}
