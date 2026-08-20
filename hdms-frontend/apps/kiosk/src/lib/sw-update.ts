import * as React from "react";
import type { SessionState } from "@hdms/domain";

let updatePending = false;
let reloadHandler: (() => void) | null = null;

export function setServiceWorkerUpdatePending(
  pending: boolean,
  reloadFn?: () => void
): void {
  updatePending = pending;
  if (reloadFn) {
    reloadHandler = reloadFn;
  }
}

export function isServiceWorkerUpdatePending(): boolean {
  return updatePending;
}

export function applyPendingServiceWorkerUpdate(): boolean {
  if (updatePending && reloadHandler) {
    updatePending = false;
    reloadHandler();
    return true;
  }
  return false;
}

export function resetServiceWorkerUpdateForTesting(): void {
  updatePending = false;
  reloadHandler = null;
}

export function useDeferredServiceWorkerUpdate(currentState: SessionState) {
  React.useEffect(() => {
    // When returning to idle, if an update was waiting, apply it now
    if (currentState === "idle" && updatePending) {
      applyPendingServiceWorkerUpdate();
    }
  }, [currentState]);
}
