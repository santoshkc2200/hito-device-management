import { toHalfWidth } from "@hdms/domain";
import { useRef, type ComponentProps, type ChangeEvent } from "react";

export type HalfWidthInputProps = ComponentProps<"input"> & { halfWidth?: boolean };

/** Normalize identifier text after IME composition, before notifying the form. */
export function useHalfWidthInput({ halfWidth, ...props }: HalfWidthInputProps): ComponentProps<"input"> {
  const composing = useRef(false);
  if (!halfWidth) return props;
  const fold = (input: HTMLInputElement) => {
    const value = toHalfWidth(input.value);
    if (value === input.value) return false;
    input.value = value;
    return true;
  };
  return {
    ...props,
    inputMode: props.inputMode ?? "text",
    autoCapitalize: "none",
    autoCorrect: "off",
    spellCheck: false,
    onCompositionStart: (event) => {
      composing.current = true;
      props.onCompositionStart?.(event);
    },
    onChange: (event) => {
      if (!composing.current && !(event.nativeEvent as InputEvent).isComposing) fold(event.currentTarget);
      props.onChange?.(event);
    },
    onCompositionEnd: (event) => {
      composing.current = false;
      props.onCompositionEnd?.(event);
      if (fold(event.currentTarget)) props.onChange?.(event as unknown as ChangeEvent<HTMLInputElement>);
    },
  };
}
