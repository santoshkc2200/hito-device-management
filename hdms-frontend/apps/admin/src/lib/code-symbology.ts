import { useCallback, useSyncExternalStore } from "react";
import type { Symbology } from "@/components/barcode";

// Which code the admin prints on labels is a per-browser preference, not
// stored state: the token string is identical either way (docs/05 — the
// scan path is symbology-agnostic). Data Matrix stays a template-level
// choice for small items, so it is not offered here.
export type CodeSymbology = Extract<Symbology, "qrcode" | "code128">;

const STORAGE_KEY = "hdms.admin.codeSymbology";
const listeners = new Set<() => void>();

function read(): CodeSymbology {
  try {
    return localStorage.getItem(STORAGE_KEY) === "code128" ? "code128" : "qrcode";
  } catch {
    return "qrcode";
  }
}

function subscribe(onChange: () => void) {
  listeners.add(onChange);
  window.addEventListener("storage", onChange);
  return () => {
    listeners.delete(onChange);
    window.removeEventListener("storage", onChange);
  };
}

// Shared across every mounted label surface and across tabs, so flipping the
// toggle in one dialog never leaves another showing a stale choice.
export function useCodeSymbology() {
  const symbology = useSyncExternalStore(subscribe, read, () => "qrcode" as CodeSymbology);
  const setSymbology = useCallback((next: CodeSymbology) => {
    try {
      localStorage.setItem(STORAGE_KEY, next);
    } catch {
      return;
    }
    listeners.forEach((l) => l());
  }, []);
  return [symbology, setSymbology] as const;
}
