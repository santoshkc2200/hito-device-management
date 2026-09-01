import { render, screen, act } from "@testing-library/react";
import { describe, expect, it, beforeEach } from "vitest";
import { LocaleProvider, useLocale, useTranslator, setDefaultLocaleFallback } from "./provider";
import type { Catalogues } from "./translate";

const ja = { hello: "こんにちは" } as const;
type Messages = typeof ja;
const en: Messages = { hello: "Hello" };
const catalogues: Catalogues<Messages> = { ja, en };

function Probe() {
  const t = useTranslator(catalogues);
  const { locale, setLocale } = useLocale();
  return (
    <div>
      <span data-testid="text">{t("hello")}</span>
      <span data-testid="locale">{locale}</span>
      <button onClick={() => setLocale(locale === "ja" ? "en" : "ja")}>toggle</button>
    </div>
  );
}

describe("LocaleProvider", () => {
  beforeEach(() => {
    setDefaultLocaleFallback("ja");
  });
  it("renders the active locale's copy", () => {
    render(
      <LocaleProvider locale="en">
        <Probe />
      </LocaleProvider>
    );
    expect(screen.getByTestId("text")).toHaveTextContent("Hello");
  });

  it("sets the document lang attribute, which a screen reader needs to pronounce anything", () => {
    render(
      <LocaleProvider locale="ja">
        <Probe />
      </LocaleProvider>
    );
    expect(document.documentElement.lang).toBe("ja");
  });

  it("re-renders consumers and updates lang when the locale changes", async () => {
    render(
      <LocaleProvider locale="ja">
        <Probe />
      </LocaleProvider>
    );
    await act(async () => {
      screen.getByText("toggle").click();
    });
    expect(screen.getByTestId("text")).toHaveTextContent("Hello");
    expect(document.documentElement.lang).toBe("en");
  });

  it("re-anchors to a new default when the prop changes, so an idle reset works", () => {
    const { rerender } = render(
      <LocaleProvider locale="ja">
        <Probe />
      </LocaleProvider>
    );
    rerender(
      <LocaleProvider locale="en">
        <Probe />
      </LocaleProvider>
    );
    expect(screen.getByTestId("locale")).toHaveTextContent("en");
  });

  it("falls back to the default locale outside a provider rather than throwing", () => {
    render(<Probe />);
    expect(screen.getByTestId("locale")).toHaveTextContent("ja");
  });
});
