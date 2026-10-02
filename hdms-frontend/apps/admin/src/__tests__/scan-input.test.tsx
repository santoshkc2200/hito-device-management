import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ScanInput } from "@/components/scan-input";

function renderForm(onSubmit = vi.fn()) {
  render(
    <form
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit();
      }}
    >
      <input aria-label="before" />
      <ScanInput aria-label="id" />
      <input aria-label="hidden-select" tabIndex={-1} />
      <input aria-label="disabled" disabled />
      <input aria-label="next" />
      <button type="submit">go</button>
    </form>
  );
  return onSubmit;
}

describe("ScanInput", () => {
  it("Enter after a scanned value moves focus to the next field without submitting", async () => {
    const onSubmit = renderForm();
    const user = userEvent.setup();
    await user.click(screen.getByLabelText("id"));
    await user.keyboard("HH-1001{Enter}");

    expect(screen.getByLabelText("id")).toHaveValue("HH-1001");
    expect(screen.getByLabelText("next")).toHaveFocus();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("treats a single-character scan the same way", async () => {
    const onSubmit = renderForm();
    const user = userEvent.setup();
    await user.click(screen.getByLabelText("id"));
    await user.keyboard("7{Enter}");

    expect(screen.getByLabelText("next")).toHaveFocus();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("leaves Enter alone while an IME composition is active", () => {
    const onSubmit = renderForm();
    const input = screen.getByLabelText("id");
    input.focus();
    const notCancelled = fireEvent.keyDown(input, { key: "Enter", isComposing: true });

    expect(notCancelled).toBe(true);
    expect(input).toHaveFocus();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("calls a caller-supplied onKeyDown and honours its preventDefault", () => {
    const onKeyDown = vi.fn((e: React.KeyboardEvent) => e.preventDefault());
    render(
      <form>
        <ScanInput aria-label="id" onKeyDown={onKeyDown} />
        <input aria-label="next" />
      </form>
    );
    const input = screen.getByLabelText("id");
    input.focus();
    fireEvent.keyDown(input, { key: "Enter" });

    expect(onKeyDown).toHaveBeenCalled();
    expect(input).toHaveFocus();
  });

  it("folds a full-width scan to half-width and reports the folded value", async () => {
    const onChange = vi.fn((e: React.ChangeEvent<HTMLInputElement>) => e.target.value);
    render(<ScanInput aria-label="id" onChange={onChange} />);
    const user = userEvent.setup();
    await user.click(screen.getByLabelText("id"));
    await user.keyboard("ＨＨ－１００１");

    expect(screen.getByLabelText("id")).toHaveValue("HH-1001");
    expect(onChange.mock.results.at(-1)?.value).toBe("HH-1001");
  });

  it("folds text committed at the end of an IME composition", () => {
    const seen: string[] = [];
    render(<ScanInput aria-label="id" onChange={(e) => seen.push(e.target.value)} />);
    const input = screen.getByLabelText("id") as HTMLInputElement;
    fireEvent.compositionStart(input);
    fireEvent.input(input, { target: { value: "ＨＨ１００１" }, isComposing: true });
    expect(input).toHaveValue("ＨＨ１００１");
    fireEvent.compositionEnd(input);

    expect(input).toHaveValue("HH1001");
    expect(seen.at(-1)).toBe("HH1001");
  });
});
