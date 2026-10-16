import { render, screen, within } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { SessionTransactionList } from "./session-transaction-list";
import type { SessionTransaction } from "@/lib/session-transactions";

const originalScroll = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollIntoView");
afterEach(() => {
  if (originalScroll) Object.defineProperty(HTMLElement.prototype, "scrollIntoView", originalScroll);
  else Reflect.deleteProperty(HTMLElement.prototype, "scrollIntoView");
});

it("reveals newly scanned items and updates to earlier pending items", () => {
  const revealed: HTMLElement[] = [];
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
    configurable: true, value(this: HTMLElement) { revealed.push(this); },
  });
  const first: SessionTransaction = { id: 1, status: "pending", device: { id: "d1", assetTag: "LAPTOP-01", name: "Laptop" } };
  const second: SessionTransaction = { id: 2, status: "pending", device: { id: "d2", assetTag: "TABLET-02", name: "Tablet" } };
  const { rerender } = render(<SessionTransactionList transactions={[first]} />);
  rerender(<SessionTransactionList transactions={[first, second]} />);
  const rows = within(screen.getByRole("list", { name: "Scanned items" })).getAllByRole("listitem");
  expect(revealed.at(-1)).toBe(rows[1]);
  rerender(<SessionTransactionList transactions={[{ ...first, status: "returned" }, second]} />);
  expect(revealed.at(-1)).toBe(rows[0]);
  expect(rows[0]).toHaveTextContent("Returned");
});
