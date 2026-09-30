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

// CR80 cards (85.6×54mm), two across an A4 page — fixed by the card stock,
// so unlike the sticker sheet it isn't a per-PC preference.
export const cardSheetSettings: LabelSheetSettings = {
  pageWidthMm: 210,
  pageHeightMm: 297,
  columns: 2,
  rows: 5,
  labelWidthMm: 85.6,
  labelHeightMm: 54,
  marginTopMm: 12,
  marginLeftMm: 15,
  gapXMm: 8,
  gapYMm: 6,
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
