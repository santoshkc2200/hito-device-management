import * as React from "react";
import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { z } from "zod";
import { LocaleProvider } from "@hdms/i18n";
import { useLocalizedResolver } from "./localized-resolver";

const schema = z.object({ name: z.string().min(1, "validation.required") });

// This file stays .ts (not .tsx) per the resolver's own file layout, so the
// wrapper is built with createElement instead of JSX.
function makeWrapper() {
  // createElement (not JSX, since this file is .ts) needs children in the
  // props object because LocaleProviderProps declares it as required; this
  // trips oxlint's no-children-prop warning, which is a JSX-authoring rule
  // that does not apply to this non-JSX call.
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(LocaleProvider, { locale: "en", children });
}

describe("useLocalizedResolver", () => {
  it("resolves a message key through the catalogue", async () => {
    const { result } = renderHook(() => useLocalizedResolver(schema), {
      wrapper: makeWrapper(),
    });
    const outcome = await result.current({ name: "" }, undefined, {
      fields: {},
      shouldUseNativeValidation: false,
    });
    expect(outcome.errors.name?.message).toBe("This field is required");
  });

  it("passes a message through unchanged when it is not a catalogue key", async () => {
    const plain = z.object({ name: z.string().min(1, "Name is required") });
    const { result } = renderHook(() => useLocalizedResolver(plain), {
      wrapper: makeWrapper(),
    });
    const outcome = await result.current({ name: "" }, undefined, {
      fields: {},
      shouldUseNativeValidation: false,
    });
    expect(outcome.errors.name?.message).toBe("Name is required");
  });

  it("resolves a message key nested inside a per-item array error", async () => {
    // Mirrors templates-panel.tsx's slipTemplateSchema.columns shape: a
    // violation on one array item lands at errors.columns[i].message, not
    // errors.columns.message — a depth-1-only walk would miss this.
    const withArray = z.object({
      columns: z.array(z.string().min(1, "validation.columnNameRequired")),
    });
    const { result } = renderHook(() => useLocalizedResolver(withArray), {
      wrapper: makeWrapper(),
    });
    const outcome = await result.current({ columns: ["", "ok"] }, undefined, {
      fields: {},
      shouldUseNativeValidation: false,
    });
    expect(outcome.errors.columns?.[0]?.message).toBe("Column name cannot be empty");
  });
});
