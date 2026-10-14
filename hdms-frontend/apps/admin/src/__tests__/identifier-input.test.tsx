import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Input } from "@/components/ui/input";

describe("identifier input", () => {
  it("folds pasted identifiers and reports half-width text to the form", () => {
    const onChange = vi.fn((e) => e.currentTarget.value);
    render(<Input halfWidth aria-label="asset tag" onChange={onChange} />);
    fireEvent.change(screen.getByLabelText("asset tag"), { target: { value: "ＨＤ－５２０５" } });
    expect(screen.getByLabelText("asset tag")).toHaveValue("HD-5205");
    expect(onChange).toHaveReturnedWith("HD-5205");
  });
  it("waits for IME composition to finish and preserves Japanese name inputs", () => {
    const onChange = vi.fn((e) => e.currentTarget.value);
    render(<><Input halfWidth aria-label="id" onChange={onChange} /><Input aria-label="name" /></>);
    const input = screen.getByLabelText("id");
    fireEvent.compositionStart(input);
    fireEvent.change(input, { target: { value: "５２０５" } });
    expect(input).toHaveValue("５２０５");
    fireEvent.compositionEnd(input);
    expect(input).toHaveValue("5205");
    expect(onChange).toHaveLastReturnedWith("5205");
    fireEvent.change(screen.getByLabelText("name"), { target: { value: "山田　太郎" } });
    expect(screen.getByLabelText("name")).toHaveValue("山田　太郎");
  });
});
