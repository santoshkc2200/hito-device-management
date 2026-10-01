import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MyQr } from "@/components/my-qr";

describe("MyQr component", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("shows a barcode by default, as an image with an accessible name, not a bare canvas", async () => {
    render(<MyQr token="HDMS-TEST-TOKEN" />);
    expect(await screen.findByRole("img", { name: /your barcode/i })).toBeInTheDocument();
  });

  it("switches to the QR code and remembers the choice", async () => {
    const user = userEvent.setup();
    const { unmount } = render(<MyQr token="HDMS-TEST-TOKEN" />);
    await user.click(screen.getByRole("button", { name: /qr code/i }));
    expect(await screen.findByRole("img", { name: /your qr code/i })).toBeInTheDocument();

    unmount();
    render(<MyQr token="HDMS-TEST-TOKEN" />);
    expect(await screen.findByRole("img", { name: /your qr code/i })).toBeInTheDocument();
  });

  it("tells the user what to do with it", async () => {
    render(<MyQr token="HDMS-TEST-TOKEN" />);
    expect(await screen.findByText(/hold this up to the kiosk scanner/i)).toBeInTheDocument();
  });

  it("asks for a wake lock while the code is on screen and releases it on unmount", async () => {
    const release = vi.fn().mockResolvedValue(undefined);
    const request = vi.fn().mockResolvedValue({ release });
    vi.stubGlobal("navigator", { ...navigator, wakeLock: { request } });

    const { unmount } = render(<MyQr token="HDMS-TEST-TOKEN" />);
    await waitFor(() => expect(request).toHaveBeenCalledWith("screen"));
    unmount();
    await waitFor(() => expect(release).toHaveBeenCalled());
  });

  it("still renders when wake lock is unavailable", async () => {
    vi.stubGlobal("navigator", { ...navigator, wakeLock: undefined });
    render(<MyQr token="HDMS-TEST-TOKEN" />);
    expect(await screen.findByRole("img", { name: /your barcode/i })).toBeInTheDocument();
  });
});
