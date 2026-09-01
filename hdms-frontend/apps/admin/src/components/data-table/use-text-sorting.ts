import { collator, useLocale } from "@hdms/i18n";
import type { Row, SortingFn } from "@tanstack/react-table";
import { useMemo } from "react";

/**
 * A locale-aware comparator for text columns.
 *
 * TanStack Table's default string sort compares code points, which orders
 * Japanese text by the accident of its encoding — さとう before あおき. A
 * collator sorts it the way a reader expects, and the comparator is memoized
 * on the locale so it stays referentially stable inside a column definition.
 */
export function useTextSortingFn<TData>(): SortingFn<TData> {
  const { locale } = useLocale();

  return useMemo(() => {
    const compare = collator(locale).compare;
    return (a: Row<TData>, b: Row<TData>, columnId: string) =>
      compare(String(a.getValue(columnId) ?? ""), String(b.getValue(columnId) ?? ""));
  }, [locale]);
}
