import "@testing-library/jest-dom/vitest";
import * as matchers from "vitest-axe/matchers";
import { afterEach, expect, vi } from "vitest";
import { setDefaultLocaleFallback } from "@hdms/i18n";

expect.extend(matchers);
setDefaultLocaleFallback("en");

afterEach(() => {
  vi.restoreAllMocks();
});
