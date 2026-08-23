import { describe, expect, it } from "vitest";
import { generateAssetTag } from "./asset-tag";

describe("generateAssetTag", () => {
  it("uses a twelve-character Crockford Base32 random suffix", () => {
    const tag = generateAssetTag("Laptops");

    expect(tag).toMatch(/^LAPTOP-[0123456789ABCDEFGHJKMNPQRSTVWXYZ]{12}$/);
  });

  it("creates a different tag for each generation", () => {
    expect(generateAssetTag("Laptops")).not.toBe(generateAssetTag("Laptops"));
  });
});
