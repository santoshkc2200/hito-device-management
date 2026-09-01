import { describe, expect, it } from "vitest";
import { translate, plural } from "./translate";
import type { Catalogues } from "./translate";

const ja = {
  greeting: "こんにちは、{name}さん",
  loans: { heading: "貸出中の機器", empty: "貸出中の機器はありません" },
} as const;

type Messages = typeof ja;

const en: Messages = {
  greeting: "Hello, {name}",
  loans: { heading: "Your active loans", empty: "No devices currently borrowed" },
};

const catalogues: Catalogues<Messages> = { ja, en };

describe("translate", () => {
  it("returns the string for the active locale", () => {
    expect(translate(catalogues, "en", "loans.heading")).toBe("Your active loans");
    expect(translate(catalogues, "ja", "loans.heading")).toBe("貸出中の機器");
  });

  it("interpolates named parameters", () => {
    expect(translate(catalogues, "en", "greeting", { name: "Sato" })).toBe("Hello, Sato");
  });

  it("falls back to the Japanese value rather than the key when a locale is missing a leaf", () => {
    const broken = { ja, en: { ...en, loans: { ...en.loans, empty: undefined } } };
    expect(
      translate(broken as unknown as Catalogues<Messages>, "en", "loans.empty")
    ).toBe("貸出中の機器はありません");
  });

  it("never returns a raw key", () => {
    const empty = { ja: {}, en: {} } as unknown as Catalogues<Messages>;
    expect(translate(empty, "en", "loans.empty")).toBe("");
  });

  it("leaves an unsupplied placeholder visible so it is caught in review", () => {
    expect(translate(catalogues, "en", "greeting")).toBe("Hello, {name}");
  });

  it("rejects a locale catalogue that is missing a key", () => {
    // @ts-expect-error — an incomplete English catalogue must not type-check.
    // This is the guarantee the whole typed-catalogue choice rests on, so it is
    // asserted rather than assumed: if the error ever stops occurring,
    // @ts-expect-error fails the build.
    const incomplete: Messages = { greeting: "Hello, {name}" };
    expect(incomplete).toBeDefined();
  });
});

describe("plural", () => {
  it("uses the singular form in English for one", () => {
    expect(plural("en", 1, { one: "1 device", other: "{count} devices" })).toBe("1 device");
  });

  it("uses the other form in English for anything else", () => {
    expect(plural("en", 3, { one: "1 device", other: "{count} devices" })).toBe("3 devices");
  });

  it("always uses the other form in Japanese, which has one plural category", () => {
    expect(plural("ja", 1, { other: "{count}台" })).toBe("1台");
    expect(plural("ja", 3, { other: "{count}台" })).toBe("3台");
  });
});
