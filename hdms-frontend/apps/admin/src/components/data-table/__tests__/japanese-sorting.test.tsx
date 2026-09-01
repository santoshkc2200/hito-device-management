import { createColumnHelper } from "@tanstack/react-table";
import { render, renderHook, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { LocaleProvider } from "@hdms/i18n";
import { DataTable, DataTableColumnHeader, useDataTableColumns, useTextSortingFn } from "../index";

interface Borrower {
  id: string;
  fullName: string;
}

const borrowers: Borrower[] = [
  { id: "1", fullName: "さとう" },
  { id: "2", fullName: "あおき" },
  { id: "3", fullName: "たなか" },
];

// Hiragana and katakana of the same sound sit far apart in code point order, so
// this is the set that actually tells a collator from a naive string compare:
// by code point it is いとう, さとう, アオキ.
const mixedScriptBorrowers: Borrower[] = [
  { id: "1", fullName: "いとう" },
  { id: "2", fullName: "アオキ" },
  { id: "3", fullName: "さとう" },
];

const columnHelper = createColumnHelper<Borrower>();

function wrapper({ children }: { children: ReactNode }) {
  return <LocaleProvider locale="ja">{children}</LocaleProvider>;
}

function renderedNameOrder() {
  return screen
    .getAllByRole("row")
    .slice(1)
    .map((row) => row.querySelectorAll("td")[0]?.textContent);
}

describe("Japanese text sorting", () => {
  it("orders a text column through the table with a collator, not by code point", async () => {
    const user = userEvent.setup();

    const { result } = renderHook(
      () => {
        const sortText = useTextSortingFn<Borrower>();
        return useDataTableColumns<Borrower>(
          () => [
            columnHelper.accessor("fullName", {
              sortingFn: sortText,
              header: ({ column }) => <DataTableColumnHeader column={column} title="名前" />,
            }),
          ],
          [sortText]
        );
      },
      { wrapper }
    );

    render(
      <LocaleProvider locale="ja">
        <DataTable columns={result.current} data={borrowers} hideToolbar />
      </LocaleProvider>
    );

    await user.click(screen.getByRole("button", { name: /名前/ }));
    await user.click(await screen.findByText("Asc"));

    // The assertion is on the table, not the collator: the point is that the
    // column definition actually uses it.
    expect(renderedNameOrder()).toEqual(["あおき", "さとう", "たなか"]);
  });

  it("sorts katakana and hiragana by sound, which a code point compare cannot do", async () => {
    const user = userEvent.setup();

    const { result } = renderHook(
      () => {
        const sortText = useTextSortingFn<Borrower>();
        return useDataTableColumns<Borrower>(
          () => [
            columnHelper.accessor("fullName", {
              sortingFn: sortText,
              header: ({ column }) => <DataTableColumnHeader column={column} title="名前" />,
            }),
          ],
          [sortText]
        );
      },
      { wrapper }
    );

    render(
      <LocaleProvider locale="ja">
        <DataTable columns={result.current} data={mixedScriptBorrowers} hideToolbar />
      </LocaleProvider>
    );

    await user.click(screen.getByRole("button", { name: /名前/ }));
    await user.click(await screen.findByText("Asc"));

    expect(renderedNameOrder()).toEqual(["アオキ", "いとう", "さとう"]);
  });
});
