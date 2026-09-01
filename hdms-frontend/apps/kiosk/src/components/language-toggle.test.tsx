import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { LocaleProvider } from "@hdms/i18n";
import { LanguageToggle } from "./language-toggle";
import { catalogue } from "@/i18n";

describe("LanguageToggle", () => {
  it("toggles locale between ja and en with appropriate label and lang attribute", async () => {
    const user = userEvent.setup();
    render(
      <LocaleProvider locale="ja" catalogue={catalogue}>
        <LanguageToggle />
      </LocaleProvider>
    );

    const toggle = screen.getByTestId("language-toggle");
    expect(toggle).toHaveTextContent("English");
    expect(toggle).toHaveAttribute("lang", "en");

    await user.click(toggle);

    expect(toggle).toHaveTextContent("日本語");
    expect(toggle).toHaveAttribute("lang", "ja");
  });
});
