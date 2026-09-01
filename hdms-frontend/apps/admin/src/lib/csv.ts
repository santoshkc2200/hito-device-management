/**
 * Excel on Japanese Windows treats a BOM-less UTF-8 file as Shift_JIS, so
 * every exported kanji arrives as mojibake. The mark costs three bytes and is
 * ignored by everything else that reads CSV.
 */
export function csvBlob(lines: string[]): Blob {
  return new Blob(["﻿", lines.join("\n")], { type: "text/csv;charset=utf-8;" });
}
