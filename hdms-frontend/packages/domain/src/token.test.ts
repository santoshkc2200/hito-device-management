import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { canonicalScan, validateToken, parseToken, formatToken, toHalfWidth, TokenParseError } from "./token";

interface FixtureCase {
  token: string;
  valid: boolean;
  case: string;
}

// Same golden fixture the Go package's TestGoldenFixture reads
// (hdms-backend/internal/platform/tokens/tokens_test.go), so the two
// implementations cannot drift apart.
const fixturePath = fileURLToPath(
  new URL("../../../../fixtures/token-fixtures.json", import.meta.url),
);
const fixture: FixtureCase[] = JSON.parse(readFileSync(fixturePath, "utf-8"));

describe("golden fixture parity", () => {
  it("has 200 valid and 200 invalid cases", () => {
    const valid = fixture.filter((c) => c.valid).length;
    const invalid = fixture.filter((c) => !c.valid).length;
    expect(valid).toBe(200);
    expect(invalid).toBe(200);
  });

  it.each(fixture)("$case: $token", (tc) => {
    expect(validateToken(tc.token)).toBe(tc.valid);
  });
});

// A real, checksum-valid token pulled from the golden fixture — the format
// example in docs/05-credentials-and-labeling.md is illustrative only and
// does not itself carry a valid check character.
const sampleValid = fixture.find((c) => c.case === "generated" && c.valid)!.token;

describe("parseToken", () => {
  it("round-trips a valid token through formatToken", () => {
    const token = parseToken(sampleValid);
    expect(formatToken(token)).toBe(sampleValid);
  });

  it("accepts a token scanned through a full-width IME", () => {
    const fullWidth = [...sampleValid].map((c) =>
      c === " " ? "\u3000" : /[!-~]/.test(c) ? String.fromCharCode(c.charCodeAt(0) + 0xfee0) : c,
    ).join("");
    expect(fullWidth).not.toBe(sampleValid);
    expect(formatToken(parseToken(fullWidth))).toBe(sampleValid);
  });

  it("is case-insensitive", () => {
    expect(validateToken(sampleValid.toLowerCase())).toBe(true);
  });

  it("rejects an unrecognised namespace", () => {
    const bad = "XX" + sampleValid.slice(2);
    expect(() => parseToken(bad)).toThrow(TokenParseError);
    try {
      parseToken(bad);
    } catch (err) {
      expect((err as TokenParseError).reason).toBe("invalid-namespace");
    }
  });

  it("treats the subject hint as a hint, not authority", () => {
    // Flipping only the hint character must not, by itself, fail
    // validation — Resolve's stored subject_type is what's authoritative
    // (docs/05-credentials-and-labeling.md).
    const flippedHint = sampleValid[3] === "U" ? "D" : "U";
    const flipped =
      sampleValid.slice(0, 3) + flippedHint + sampleValid.slice(4);
    expect(validateToken(flipped)).toBe(true);
  });
});

describe("toHalfWidth", () => {
  it("folds full-width ASCII and the ideographic space", () => {
    expect(toHalfWidth("ＨＨ－１００１\u3000ａ")).toBe("HH-1001 a");
  });

  it("leaves half-width and other text alone", () => {
    expect(toHalfWidth("HH-1001 あ")).toBe("HH-1001 あ");
  });
});

describe("canonicalScan", () => {
  const valid = fixture.find((c) => c.valid)!.token;

  it("canonicalises an HDMS token", () => {
    expect(canonicalScan(` ${valid.toLowerCase()} `)).toBe(formatToken(parseToken(valid)));
  });

  it("rejects a damaged HDMS token rather than treating it as foreign", () => {
    const flipped = `${valid.slice(0, -1)}${valid.endsWith("Z") ? "Y" : "Z"}`;
    expect(() => canonicalScan(flipped)).toThrow(TokenParseError);
  });

  it("passes an employee barcode through trimmed, case intact", () => {
    expect(canonicalScan(" e-004217\n")).toBe("e-004217");
    expect(canonicalScan("0012345678")).toBe("0012345678");
  });

  it("folds a full-width employee barcode to half-width", () => {
    expect(canonicalScan("Ｅ００４２１７")).toBe("E004217");
  });

  it("rejects empty, spaced, non-ASCII and over-long values", () => {
    for (const bad of ["  ", "E 123", "社員1234", "A".repeat(65)]) {
      expect(() => canonicalScan(bad)).toThrow(TokenParseError);
    }
    expect(canonicalScan("A".repeat(64))).toBe("A".repeat(64));
  });
});
