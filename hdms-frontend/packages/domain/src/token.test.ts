import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { validateToken, parseToken, formatToken, TokenParseError } from "./token";

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
