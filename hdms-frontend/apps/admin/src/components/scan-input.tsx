import { Input } from "@/components/ui/input";

const FOCUSABLE = "input, select, textarea, button";

function focusNext(current: HTMLElement) {
  const form = current.closest("form");
  if (!form) return;
  const fields = Array.from(form.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
    (el) => !el.hasAttribute("disabled") && el.tabIndex >= 0
  );
  fields[fields.indexOf(current) + 1]?.focus();
}

/**
 * Text input that accepts a barcode/QR scanner in keyboard mode. The scanner
 * types the code into the focused field and ends it with Enter, which would
 * otherwise submit the form before the rest is filled in; Enter moves focus to
 * the next field instead. Works for any code length.
 *
 * A Japanese IME left on full-width makes the scanner type full-width
 * characters; they are folded back to half-width as they arrive, so the form
 * only ever sees the half-width value. Folding waits for a composition to
 * finish so it never disturbs one in progress.
 */
export function ScanInput({ onKeyDown, ...props }: React.ComponentProps<"input">) {
  return (
    <Input
      {...props}
      halfWidth
      onKeyDown={(e) => {
        onKeyDown?.(e);
        if (e.defaultPrevented || e.key !== "Enter" || e.nativeEvent.isComposing) return;
        e.preventDefault();
        focusNext(e.currentTarget);
      }}
    />
  );
}
