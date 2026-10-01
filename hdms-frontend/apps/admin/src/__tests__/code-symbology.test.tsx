import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@hdms/i18n";
import { DeviceStickerLabel, StaffCardLabel } from "../components/label-templates";
import { TokenRevealDialog } from "../components/token-reveal-dialog";

const { toCanvas, toSVG } = vi.hoisted(() => ({ toCanvas: vi.fn(), toSVG: vi.fn(() => "<svg/>") }));
vi.mock("bwip-js/browser", () => ({ default: { toCanvas, toSVG } }));

const TOKEN = "HD-D-7K3M9QXA2F-4";

function wrap(ui: React.ReactNode) {
  return render(
    <LocaleProvider locale="en">
      {ui}
    </LocaleProvider>,
  );
}

const device = { type: "device", assetTag: "LAPTOP-01", name: "Dell", model: "5420" } as const;

describe("label code type", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
  });

  it("renders QR by default, with no bar height", () => {
    wrap(<StaffCardLabel fullName="A B" employeeNo="E1" token={TOKEN} />);
    const opts = toCanvas.mock.calls[0]![1];
    expect(opts).toMatchObject({ bcid: "qrcode", text: TOKEN });
    expect(opts).not.toHaveProperty("height");
  });

  it("renders Code 128 with a bar height on sticker and card", () => {
    wrap(
      <>
        <DeviceStickerLabel assetTag="A" name="n" token={TOKEN} symbology="code128" />
        <StaffCardLabel fullName="A B" employeeNo="E1" token={TOKEN} symbology="code128" />
      </>,
    );
    for (const [, opts] of toCanvas.mock.calls) {
      expect(opts).toMatchObject({ bcid: "code128", text: TOKEN, height: 15 });
    }
    expect(toCanvas).toHaveBeenCalledTimes(2);
  });

  it("toggle switches the label and export to Code 128 and persists per browser", async () => {
    const user = userEvent.setup();
    const { unmount } = wrap(
      <TokenRevealDialog open onOpenChange={() => {}} token={TOKEN} subject={device} />,
    );
    expect(toCanvas.mock.calls.at(-1)![1]).toMatchObject({ bcid: "qrcode" });

    await user.click(screen.getByRole("tab", { name: "Barcode" }));
    expect(toCanvas.mock.calls.at(-1)![1]).toMatchObject({ bcid: "code128" });
    expect(localStorage.getItem("hdms.admin.codeSymbology")).toBe("code128");

    URL.createObjectURL = vi.fn(() => "blob:x");
    URL.revokeObjectURL = vi.fn();
    await user.click(screen.getByRole("button", { name: "SVG" }));
    expect(toSVG).toHaveBeenLastCalledWith(expect.objectContaining({ bcid: "code128" }));

    unmount();
    wrap(<TokenRevealDialog open onOpenChange={() => {}} token={TOKEN} subject={device} />);
    expect(screen.getByRole("tab", { name: "Barcode" })).toHaveAttribute("data-state", "active");
  });
});
