import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { LocaleProvider } from "@hdms/i18n";
import { useDeviceColumns } from "@/routes/devices";

const noop = () => {};

function wrapper({ children }: { children: ReactNode }) {
  return <LocaleProvider locale="en">{children}</LocaleProvider>;
}

describe("column definitions", () => {
  it("returns the same array reference across renders in a fixed locale", () => {
    // The map is created once, exactly as the page's own useMemo hands it over.
    const categoryName = new Map<string, string>([["cat-1", "Tablets"]]);

    const { result, rerender } = renderHook(
      () =>
        useDeviceColumns({
          categoryName,
          onEditDevice: noop,
          onChangeStatus: noop,
          setLabelDevices: noop,
          setLabelSheetOpen: noop,
        }),
      { wrapper }
    );

    const first = result.current;
    rerender();

    // An unmemoized column array is a new reference every render, which sends
    // TanStack Table v8 into a silent 100% CPU loop with nothing in the console.
    expect(result.current).toBe(first);
  });
});
