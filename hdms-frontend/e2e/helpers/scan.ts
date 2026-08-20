import type { Page } from "@playwright/test";

/**
 * Simulates a hardware barcode/QR scanner (HID Keyboard Wedge) emitting keystrokes.
 * Scanners send rapid keystrokes (~8ms interval) followed by an Enter key.
 */
export async function simulateScan(
  page: Page,
  token: string,
  interKeyDelayMs = 8
): Promise<void> {
  for (const char of token) {
    await page.keyboard.press(char, { delay: interKeyDelayMs });
  }
  // Trailing Enter character suffix
  await page.keyboard.press("Enter", { delay: interKeyDelayMs });
}
