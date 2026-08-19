import { zLoginRequest } from "@hdms/api-client";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import { createRoute, useNavigate, useRouter } from "@tanstack/react-router";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { loginAdmin } from "@/lib/auth";
import { rootRoute } from "./root";

const loginSchema = zLoginRequest.extend({
  email: z.string().min(1, "Email is required").email("Enter a valid email"),
  password: z.string().min(1, "Password is required"),
  totpCode: z.string().min(6, "Enter the 6-digit code"),
});

type LoginFormValues = z.infer<typeof loginSchema>;

function LoginPage() {
  const router = useRouter();
  const navigate = useNavigate();
  const form = useForm<LoginFormValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: { email: "", password: "", totpCode: "" },
  });

  const mutation = useMutation({
    mutationFn: loginAdmin,
    onSuccess: async () => {
      await router.invalidate();
      await navigate({ to: "/" });
    },
    onError: (error: unknown) => {
      form.setError("root", {
        message:
          error && typeof error === "object" && "detail" in error
            ? String((error as { detail?: string }).detail)
            : "Invalid email, password or code.",
      });
    },
  });

  return (
    <main className="flex min-h-dvh items-center justify-center bg-secondary p-6">
      <div className="w-full max-w-sm rounded-lg border border-border bg-card p-8 shadow-sm">
        <div className="mb-6">
          <p className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
            Hito Hospital
          </p>
          <h1 className="mt-1 text-2xl font-semibold text-card-foreground">
            Device management
          </h1>
        </div>
        <form
          onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
        >
          <FieldGroup>
            <Field data-invalid={!!form.formState.errors.email}>
              <FieldLabel htmlFor="email">Email</FieldLabel>
              <Input
                id="email"
                type="email"
                autoComplete="username"
                autoFocus
                aria-invalid={!!form.formState.errors.email}
                {...form.register("email")}
              />
              {form.formState.errors.email && (
                <FieldError>{form.formState.errors.email.message}</FieldError>
              )}
            </Field>
            <Field data-invalid={!!form.formState.errors.password}>
              <FieldLabel htmlFor="password">Password</FieldLabel>
              <Input
                id="password"
                type="password"
                autoComplete="current-password"
                aria-invalid={!!form.formState.errors.password}
                {...form.register("password")}
              />
              {form.formState.errors.password && (
                <FieldError>
                  {form.formState.errors.password.message}
                </FieldError>
              )}
            </Field>
            <Field data-invalid={!!form.formState.errors.totpCode}>
              <FieldLabel htmlFor="totpCode">Authenticator code</FieldLabel>
              <Input
                id="totpCode"
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={6}
                className="font-identifier"
                aria-invalid={!!form.formState.errors.totpCode}
                {...form.register("totpCode")}
              />
              {form.formState.errors.totpCode && (
                <FieldError>
                  {form.formState.errors.totpCode.message}
                </FieldError>
              )}
            </Field>
            {form.formState.errors.root && (
              <p className="text-sm text-destructive" role="alert">
                {form.formState.errors.root.message}
              </p>
            )}
            <Button type="submit" disabled={mutation.isPending} className="mt-2">
              {mutation.isPending ? "Signing in…" : "Sign in"}
            </Button>
          </FieldGroup>
        </form>
      </div>
    </main>
  );
}

export const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  component: LoginPage,
});
