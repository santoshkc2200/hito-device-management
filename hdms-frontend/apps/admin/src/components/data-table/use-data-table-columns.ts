import { type ColumnDef } from "@tanstack/react-table";
import { type DependencyList, useMemo } from "react";

/**
 * useDataTableColumns guarantees that column definitions passed to TanStack Table
 * are strictly memoized.
 *
 * Background: Passing unmemoized column arrays directly to useReactTable causes
 * TanStack Table v8 to enter an infinite render loop (100% CPU burn with no console error)
 * because column reference changes trigger internal state resets.
 */
export function useDataTableColumns<TData, TValue = unknown>(
  factory: () => ColumnDef<TData, TValue>[],
  deps: DependencyList
): ColumnDef<TData, TValue>[] {
  // eslint-disable-next-line react-hooks/exhaustive-deps
  return useMemo(factory, deps);
}
