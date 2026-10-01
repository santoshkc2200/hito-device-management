import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@hdms/i18n";
import { MismatchScreen } from "./mismatch-screen";

describe("MismatchScreen", () => {
  it("blocks the restore and says what IT must run", async () => {
    const onBack = vi.fn();
    render(
      <LocaleProvider locale="en">
        <MismatchScreen onBack={onBack} />
      </LocaleProvider>,
    );
    expect(screen.getByRole("heading", { name: "This server was set up with different keys" })).toBeInTheDocument();
    expect(screen.getByText(/install\.sh --restore/)).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Back" }));
    expect(onBack).toHaveBeenCalledTimes(1);
  });
});
