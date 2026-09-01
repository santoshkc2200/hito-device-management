import { describe, expect, it } from "vitest";
import { csvBlob } from "./csv";

describe("csvBlob", () => {
  it("starts with a UTF-8 byte-order mark", async () => {
    const bytes = new Uint8Array(await csvBlob(["資産番号,名称"]).arrayBuffer());
    // Excel on Japanese Windows reads a BOM-less UTF-8 file as Shift_JIS and
    // renders every kanji as mojibake.
    expect([bytes[0], bytes[1], bytes[2]]).toEqual([0xef, 0xbb, 0xbf]);
  });

  it("keeps the content intact after the mark", async () => {
    const text = await csvBlob(["a,b", "1,2"]).text();
    expect(text.replace(/^﻿/, "")).toBe("a,b\n1,2");
  });
});
