import type { Page } from "@playwright/test";

/**
 * Simulates a hardware barcode/QR scanner (HID Keyboard Wedge) emitting keystrokes.
 * Scanners send rapid keystrokes (~8ms interval) followed by an Enter key.
 */
export async function simulateScan(
  page: Page,
  token: string,
  interKeyDelayMs = 0
): Promise<void> {
  await page.evaluate(
    async ({ token, delay }) => {
      // Warm up event listener and clear any partial buffer
      document.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "Escape",
          bubbles: true,
          cancelable: true,
          composed: true,
        })
      );

      for (const char of token) {
        document.dispatchEvent(
          new KeyboardEvent("keydown", {
            key: char,
            bubbles: true,
            cancelable: true,
            composed: true,
          })
        );
        if (delay > 0) {
          await new Promise((r) => setTimeout(r, delay));
        }
      }
      document.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "Enter",
          bubbles: true,
          cancelable: true,
          composed: true,
        })
      );
    },
    { token, delay: interKeyDelayMs }
  );
}

/**
 * Enters a token through the Attendant Manual Entry modal with the Crockford Base32 keypad.
 */
export async function typeKeypad(
  page: Page,
  token: string,
  pin = "1234"
): Promise<void> {
  // Open attendant modal if not already open
  const modal = page.getByTestId("attendant-modal");
  if (!(await modal.isVisible())) {
    await page.getByRole("button", { name: /attendant|pin|keypad/i }).click();
  }

  // If PIN form is visible, submit PIN
  const pinInput = page.getByTestId("attendant-pin-input");
  if (await pinInput.isVisible()) {
    await pinInput.fill(pin);
    await page.getByTestId("pin-submit-button").click();
  }

  // Clear existing input
  await page.getByTestId("keypad-key-clear").click();

  let remaining = token;
  // Set prefix or type each character
  if (remaining.startsWith("HD-D-")) {
    await page.getByRole("button", { name: /HD-D-/ }).click();
    remaining = remaining.slice(5);
  } else if (remaining.startsWith("HD-U-")) {
    await page.getByRole("button", { name: /HD-U-/ }).click();
    remaining = remaining.slice(5);
  }

  for (const char of remaining) {
    if (char === "-") {
      await page.getByTestId("keypad-key-hyphen").click();
    } else {
      await page.getByTestId(`keypad-key-${char.toUpperCase()}`).click();
    }
  }

  // Submit token
  await page.getByTestId("token-submit-button").click();
}
