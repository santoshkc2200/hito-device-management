import { toHalfWidth } from "@hdms/domain";
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
 * Replaces the field's value with its half-width form. Returns whether it
 * changed anything.
 */
function foldToHalfWidth(el: HTMLInputElement): boolean {
  const folded = toHalfWidth(el.value);
  if (folded === el.value) return false;
  el.value = folded;
  return true;
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
export function ScanInput({ onKeyDown, onChange, onCompositionEnd, ...props }: React.ComponentProps<"input">) {
  return (
    <Input
      {...props}
      onChange={(e) => {
        if (!(e.nativeEvent as InputEvent).isComposing) foldToHalfWidth(e.currentTarget);
        onChange?.(e);
      }}
      onCompositionEnd={(e) => {
        onCompositionEnd?.(e);
        // The committed text arrives with no further change event, so tell
        // the form about the folded value ourselves.
        if (foldToHalfWidth(e.currentTarget)) {
          onChange?.(e as unknown as React.ChangeEvent<HTMLInputElement>);
        }
      }}
      onKeyDown={(e) => {
        onKeyDown?.(e);
        if (e.defaultPrevented || e.key !== "Enter" || e.nativeEvent.isComposing) return;
        e.preventDefault();
        focusNext(e.currentTarget);
      }}
    />
  );
}
