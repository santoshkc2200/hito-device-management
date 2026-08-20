export type ScanSourceType = "hid" | "camera" | "manual";

export interface KioskConfig {
  schemaVersion: number;
  kioskId: string;
  kioskName: string;
  token: string;
  muteEnabled: boolean;
  enabledSources: ScanSourceType[];
  attendantPinHash?: string;
}

export const CURRENT_SCHEMA_VERSION = 1;
export const KIOSK_CONFIG_STORAGE_KEY = "hdms_kiosk_config";
export const KIOSK_SESSION_STORAGE_KEY = "hdms_kiosk_session_id";

export function getKioskConfig(): KioskConfig | null {
  try {
    const raw = localStorage.getItem(KIOSK_CONFIG_STORAGE_KEY);
    if (!raw) {
      return null;
    }
    const parsed = JSON.parse(raw) as Partial<KioskConfig>;
    if (parsed.schemaVersion !== CURRENT_SCHEMA_VERSION) {
      localStorage.removeItem(KIOSK_CONFIG_STORAGE_KEY);
      return null;
    }
    if (!parsed.kioskId || !parsed.kioskName || !parsed.token) {
      localStorage.removeItem(KIOSK_CONFIG_STORAGE_KEY);
      return null;
    }
    return {
      schemaVersion: CURRENT_SCHEMA_VERSION,
      kioskId: parsed.kioskId,
      kioskName: parsed.kioskName,
      token: parsed.token,
      muteEnabled: parsed.muteEnabled ?? false,
      enabledSources: parsed.enabledSources ?? ["hid", "camera", "manual"],
      attendantPinHash: parsed.attendantPinHash,
    };
  } catch {
    localStorage.removeItem(KIOSK_CONFIG_STORAGE_KEY);
    return null;
  }
}

type ConfigListener = (config: KioskConfig | null) => void;
const configListeners = new Set<ConfigListener>();

export function subscribeKioskConfig(listener: ConfigListener): () => void {
  configListeners.add(listener);
  return () => {
    configListeners.delete(listener);
  };
}

function notifyConfigListeners(): void {
  const current = getKioskConfig();
  for (const listener of configListeners) {
    try {
      listener(current);
    } catch {
      // Ignore listener error
    }
  }
}

export function isKioskPaired(): boolean {
  return getKioskConfig() !== null;
}

export function setKioskConfig(
  updates: Partial<Omit<KioskConfig, "schemaVersion">> & {
    kioskId: string;
    kioskName: string;
    token: string;
  }
): KioskConfig {
  const existing = getKioskConfig();
  const next: KioskConfig = {
    schemaVersion: CURRENT_SCHEMA_VERSION,
    kioskId: updates.kioskId,
    kioskName: updates.kioskName,
    token: updates.token,
    muteEnabled: updates.muteEnabled ?? existing?.muteEnabled ?? false,
    enabledSources:
      updates.enabledSources ?? existing?.enabledSources ?? ["hid", "camera", "manual"],
    attendantPinHash: updates.attendantPinHash ?? existing?.attendantPinHash,
  };
  localStorage.setItem(KIOSK_CONFIG_STORAGE_KEY, JSON.stringify(next));
  notifyConfigListeners();
  return next;
}

export function clearKioskConfig(): void {
  // Wipe the token and config immediately
  localStorage.removeItem(KIOSK_CONFIG_STORAGE_KEY);
  notifyConfigListeners();
}

export function unpairKiosk(): void {
  clearKioskConfig();
  clearSessionId();
}

export function getSessionId(): string | null {
  try {
    return sessionStorage.getItem(KIOSK_SESSION_STORAGE_KEY);
  } catch {
    return null;
  }
}

export function setSessionId(sessionId: string | null): void {
  try {
    if (sessionId) {
      sessionStorage.setItem(KIOSK_SESSION_STORAGE_KEY, sessionId);
    } else {
      sessionStorage.removeItem(KIOSK_SESSION_STORAGE_KEY);
    }
  } catch {
    // Ignore storage quota or permission errors
  }
}

export function clearSessionId(): void {
  try {
    sessionStorage.removeItem(KIOSK_SESSION_STORAGE_KEY);
  } catch {
    // Ignore
  }
}
