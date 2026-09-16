/**
 * Screen and display utilities for the staff app.
 */

/**
 * Ask the browser to keep the screen awake. Returns null when the platform has
 * no wake lock or refuses one — iOS drops it in several contexts — so callers
 * treat the lock as an enhancement and never as a precondition.
 */
export async function requestScreenWakeLock(): Promise<WakeLockSentinel | null> {
  if (typeof navigator === "undefined" || !navigator.wakeLock?.request) return null;
  try {
    return await navigator.wakeLock.request("screen");
  } catch {
    return null;
  }
}

export async function releaseScreenWakeLock(sentinel: WakeLockSentinel | null): Promise<void> {
  if (!sentinel?.release) return;
  try {
    await sentinel.release();
  } catch {
    // releasing a lock the platform already dropped is not an error worth raising
  }
}

/** Run `run` with the screen held awake, releasing the lock afterwards. */
export async function withMaxBrightness<T>(run: () => Promise<T>): Promise<T> {
  const sentinel = await requestScreenWakeLock();
  try {
    return await run();
  } finally {
    await releaseScreenWakeLock(sentinel);
  }
}
