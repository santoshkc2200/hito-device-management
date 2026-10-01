import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ja } from "@/i18n/ja";
import { AttentionStrip } from "@/components/dashboard/attention-strip";

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    Link: ({ children, to, ...props }: any) => (
      <a href={to} {...props}>
        {children}
      </a>
    ),
  };
});

const recent = new Date(Date.now() - 3_600_000).toISOString();
const a = ja.dashboard.attention;

describe("recovery key attention items", () => {
  it("warns when no recovery key is printed", () => {
    render(<AttentionStrip backup={{ lastSuccessAt: recent, recoveryKeyStatus: "missing" }} />);
    expect(screen.getByText(a.recoveryKeyMissingTitle)).toBeInTheDocument();
  });

  it("treats an unconfirmed key as not printed", () => {
    render(<AttentionStrip backup={{ lastSuccessAt: recent, recoveryKeyStatus: "unconfirmed" }} />);
    expect(screen.getByText(a.recoveryKeyMissingTitle)).toBeInTheDocument();
  });

  it("warns when the key is out of date", () => {
    render(<AttentionStrip backup={{ lastSuccessAt: recent, recoveryKeyStatus: "outdated" }} />);
    expect(screen.getByText(a.recoveryKeyOutdatedTitle)).toBeInTheDocument();
  });

  it("stays quiet when ready or when the status is unknown", () => {
    const { rerender } = render(<AttentionStrip backup={{ lastSuccessAt: recent, recoveryKeyStatus: "ready" }} />);
    expect(screen.queryByText(a.recoveryKeyMissingTitle)).not.toBeInTheDocument();
    expect(screen.queryByText(a.recoveryKeyOutdatedTitle)).not.toBeInTheDocument();
    rerender(<AttentionStrip backup={{ lastSuccessAt: recent }} />);
    expect(screen.queryByText(a.recoveryKeyMissingTitle)).not.toBeInTheDocument();
  });
});
