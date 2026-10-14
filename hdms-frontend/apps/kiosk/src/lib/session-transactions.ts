import type { ScanSource, SessionDevice, SessionMessage } from "@hdms/api-client";
import type { KioskProblem } from "./problem";

export interface SessionTransaction {
  id: number;
  status: "pending" | "borrowed" | "returned" | "failed";
  device?: SessionDevice | null;
  message?: SessionMessage | null;
  problem?: KioskProblem | null;
  failureReason?: "user-required" | "result-unconfirmed" | "session-expired";
  dueAt?: string | null;
  loanId?: string;
}

export interface PendingScan {
  rowId: number;
  token: string;
  source: ScanSource;
  device: SessionDevice;
}

const STORAGE_KEY = "hdms-kiosk-transactions";

export function restoreTransactions(sessionId: string | null): {
  transactions: SessionTransaction[];
  pending: PendingScan[];
} {
  try {
    const saved = JSON.parse(sessionStorage.getItem(STORAGE_KEY) ?? "null");
    if (sessionId && saved?.sessionId === sessionId && Array.isArray(saved.transactions) && Array.isArray(saved.pending)) {
      return saved;
    }
  } catch { /* A damaged or unavailable browser store starts empty. */ }
  return { transactions: [], pending: [] };
}

export function saveTransactions(sessionId: string | null, transactions: SessionTransaction[], pending: PendingScan[]) {
  try {
    if (sessionId) sessionStorage.setItem(STORAGE_KEY, JSON.stringify({ sessionId, transactions, pending }));
    else sessionStorage.removeItem(STORAGE_KEY);
  } catch { /* Scanning still works when browser storage is unavailable. */ }
}
