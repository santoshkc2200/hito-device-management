import "vitest";
import type { AxeResults } from "vitest-axe";

interface CustomAxeMatchers<R = unknown> {
  toHaveNoViolations(): R;
}

declare module "vitest" {
  interface Assertion<T = any> extends CustomAxeMatchers<T> {}
  interface AsymmetricMatchersContaining extends CustomAxeMatchers {}
}
