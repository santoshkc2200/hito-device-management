import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LoginPage } from "@/routes/login";

describe("staff login", () => {
  it("offers Microsoft sign-in and an employee-number form on the same screen", () => {
    render(<LoginPage />);
    expect(screen.getByRole("link", { name: /continue with microsoft/i })).toHaveAttribute(
      "href",
      "/v1/staff/auth/microsoft/start",
    );
    expect(screen.getByLabelText(/employee number/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
  });

  it("shows one message for a wrong employee number and a wrong password", async () => {
    const submit = vi.fn().mockRejectedValue({ status: 401 });
    render(<LoginPage onSubmit={submit} />);

    await userEvent.type(screen.getByLabelText(/employee number/i), "E-1");
    await userEvent.type(screen.getByLabelText(/password/i), "wrong-password");
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
