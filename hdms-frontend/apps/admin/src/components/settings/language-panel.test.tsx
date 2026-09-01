import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@hdms/i18n";
import { LanguagePanel } from "./language-panel";

describe("LanguagePanel", () => {
  it("persists the choice to the account and switches immediately", async () => {
    const updateMyLocale = vi.fn().mockResolvedValue({ locale: "en" });
    render(
      <LocaleProvider locale="ja">
        <LanguagePanel updateMyLocale={updateMyLocale} />
      </LocaleProvider>
    );

    await userEvent.click(screen.getByRole("radio", { name: "English" }));

    expect(updateMyLocale).toHaveBeenCalledWith({ locale: "en" });
    expect(document.documentElement.lang).toBe("en");
  });

  it("reverts the display if the save fails, so what is shown matches what is stored", async () => {
    const updateMyLocale = vi.fn().mockRejectedValue(new Error("network"));
    render(
      <LocaleProvider locale="ja">
        <LanguagePanel updateMyLocale={updateMyLocale} />
      </LocaleProvider>
    );

    await userEvent.click(screen.getByRole("radio", { name: "English" }));

    expect(document.documentElement.lang).toBe("ja");
  });
});
