import "@testing-library/jest-dom/vitest";
import * as matchers from "vitest-axe/matchers";
import { afterEach, expect } from "vitest";

expect.extend(matchers);

afterEach(() => {
  localStorage.clear();
  sessionStorage.clear();
});
