import { globSync, readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

const ROOT = path.resolve(import.meta.dirname, "..");

/** Props whose value a person reads or hears, so they must be translated. */
const TRANSLATABLE_PROPS = ["aria-label", "title", "placeholder", "alt"];

/** A bare text node between JSX tags, with at least one letter in it. */
const JSX_TEXT = />\s*([A-Za-z][^<>{}\n]{2,})\s*</g;

function sourceFiles(): string[] {
  return globSync("**/*.tsx", { cwd: ROOT })
    .filter((f) => !f.endsWith(".test.tsx"))
    .filter((f) => !f.startsWith("i18n/"))
    .map((f) => path.join(ROOT, f));
}

describe("no user-visible string literals remain in recovery components", () => {
  it("finds no bare text nodes in JSX", () => {
    const offenders: string[] = [];
    for (const file of sourceFiles()) {
      const source = readFileSync(file, "utf8");
      const lines = source.split("\n");
      lines.forEach((line, index) => {
        if (line.includes("i18n-allow-literal")) return;
        for (const match of line.matchAll(JSX_TEXT)) {
          offenders.push(`${path.relative(ROOT, file)}:${index + 1}  ${match[1].trim()}`);
        }
      });
    }
    expect(offenders, offenders.join("\n")).toEqual([]);
  });

  it("finds no untranslated aria-label, title, placeholder or alt props", () => {
    const offenders: string[] = [];
    for (const file of sourceFiles()) {
      const source = readFileSync(file, "utf8");
      source.split("\n").forEach((line, index) => {
        if (line.includes("i18n-allow-literal")) return;
        for (const prop of TRANSLATABLE_PROPS) {
          const re = new RegExp(`${prop}\\s*=\\s*"[^"]{2,}"`);
          if (re.test(line)) {
            offenders.push(`${path.relative(ROOT, file)}:${index + 1}  ${line.trim()}`);
          }
        }
      });
    }
    expect(offenders, offenders.join("\n")).toEqual([]);
  });
});
