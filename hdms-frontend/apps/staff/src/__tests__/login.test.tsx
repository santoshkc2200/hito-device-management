import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LoginPage } from "@/routes/login";

describe("staff login", () => {
  it("reveals and hides the password without submitting the form", async () => {
    const submit = vi.fn();
    render(<LoginPage onSubmit={submit} />);
    const password = screen.getByLabelText(/^password$/i);
    await userEvent.type(password, "abcdefgh");
    await userEvent.click(screen.getByRole("button", { name: /show password/i }));
    expect(password).toHaveAttribute("type", "text");
    expect(password).toHaveValue("abcdefgh");
    await userEvent.click(screen.getByRole("button", { name: /hide password/i }));
    expect(password).toHaveAttribute("type", "password");
    expect(submit).not.toHaveBeenCalled();
  });

  it("submits a full-width employee number as half-width", async () => {
    const submit = vi.fn().mockResolvedValue(undefined);
    render(<LoginPage onSubmit={submit} />);
    await userEvent.type(screen.getByLabelText(/employee number/i), "５２０５");
    await userEvent.type(screen.getByLabelText(/^password$/i), "abcdefgh");
    await userEvent.click(screen.getByRole("button", { name: /^sign in$/i }));
    expect(submit).toHaveBeenCalledWith({ employeeNo: "5205", password: "abcdefgh" });
  });

  it("offers an employee-number form and hides Microsoft sign-in", () => {
    render(<LoginPage />);
    expect(screen.queryByRole("link", { name: /continue with microsoft/i })).not.toBeInTheDocument();
    expect(screen.getByLabelText(/employee number/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/^password$/i)).toBeInTheDocument();
  });

  it("shows one message for a wrong employee number and a wrong password", async () => {
    const submit = vi.fn().mockRejectedValue({ status: 401 });
    render(<LoginPage onSubmit={submit} />);

    await userEvent.type(screen.getByLabelText(/employee number/i), "E-1");
    await userEvent.type(screen.getByLabelText(/^password$/i), "wrong-password");
    await userEvent.click(screen.getByRole("button", { name: /sign in/i }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      /employee number or password is incorrect/i,
    );
  });

  it("displays an error alert when ?error=tenant_not_allowed is present", () => {
    render(<LoginPage error="tenant_not_allowed" />);
    expect(screen.getByRole("alert")).toHaveTextContent(/not permitted/i);
  });

  it("displays an error alert when ?error=domain_not_allowed is present", () => {
    render(<LoginPage error="domain_not_allowed" />);
    expect(screen.getByRole("alert")).toHaveTextContent(/domain/i);
  });

  it("displays an error alert when ?error=state_unknown is present", () => {
    render(<LoginPage error="state_unknown" />);
    expect(screen.getByRole("alert")).toHaveTextContent(/expired/i);
  });

  it("displays a fallback error alert when an unknown ?error= is present", () => {
    render(<LoginPage error="other_unknown_failure" />);
    expect(screen.getByRole("alert")).toHaveTextContent(/failed/i);
  });
});
