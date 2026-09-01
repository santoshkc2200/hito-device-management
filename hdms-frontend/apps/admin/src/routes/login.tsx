import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation } from "@tanstack/react-query";
import { createRoute, useNavigate, useRouter } from "@tanstack/react-router";
import { useState } from "react";
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
import { useT } from "@/i18n";
import { rootRoute } from "./root";

const loginSchema = z.object({
  email: z.string().min(1, "Email is required").email("Enter a valid email"),
  password: z.string().min(1, "Password is required"),
  totpCode: z.string().optional(),
  recoveryCode: z.string().optional(),
});

type LoginFormValues = z.infer<typeof loginSchema>;

export function LoginPage() {
  const t = useT();
  const router = useRouter();
  const navigate = useNavigate();
  const [useRecoveryCode, setUseRecoveryCode] = useState(false);

  const form = useForm<LoginFormValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: { email: "", password: "", totpCode: "", recoveryCode: "" },
  });

  const mutation = useMutation({
    mutationFn: loginAdmin,
    onSuccess: async () => {
      await router.invalidate();
      await navigate({ to: "/" });
    },
    onError: (error: unknown) => {
      let message = t("login.invalidCredentials");
      if (error && typeof error === "object") {
        if ("detail" in error && typeof (error as { detail: string }).detail === "string") {
          message = (error as { detail: string }).detail;
        } else if ("title" in error && typeof (error as { title: string }).title === "string") {
          message = (error as { title: string }).title;
        }
      }
      form.setError("root", { message });
    },
  });

  const handleSubmit = (values: LoginFormValues) => {
    if (useRecoveryCode) {
      if (!values.recoveryCode || values.recoveryCode.trim().length === 0) {
        form.setError("recoveryCode", { message: t("login.enterRecoveryCode") });
        return;
      }
      mutation.mutate({
        email: values.email,
        password: values.password,
        recoveryCode: values.recoveryCode.trim(),
      });
    } else {
      if (!values.totpCode || values.totpCode.trim().length < 6) {
        form.setError("totpCode", { message: t("login.enterTotpCode") });
        return;
      }
      mutation.mutate({
        email: values.email,
        password: values.password,
        totpCode: values.totpCode.trim(),
      });
    }
  };

  return (
    <main className="flex min-h-dvh items-center justify-center bg-secondary p-6">
      <div className="w-full max-w-sm rounded-lg border border-border bg-card p-8 shadow-sm">
        <div className="mb-6">
          <p className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
            {t("login.hospitalName")}
          </p>
          <h1 className="mt-1 text-2xl font-semibold text-card-foreground">
            {t("login.systemTitle")}
          </h1>
        </div>
        <form onSubmit={form.handleSubmit(handleSubmit)}>
          <FieldGroup>
            <Field data-invalid={!!form.formState.errors.email}>
              <FieldLabel htmlFor="email">{t("login.emailLabel")}</FieldLabel>
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
              <FieldLabel htmlFor="password">{t("login.passwordLabel")}</FieldLabel>
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

            {!useRecoveryCode ? (
              <Field data-invalid={!!form.formState.errors.totpCode}>
                <FieldLabel htmlFor="totpCode">{t("login.totpLabel")}</FieldLabel>
                <Input
                  id="totpCode"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  maxLength={6}
                  placeholder={t("login.totpPlaceholder")}
                  className="font-mono tracking-widest"
                  aria-invalid={!!form.formState.errors.totpCode}
                  {...form.register("totpCode")}
                />
                {form.formState.errors.totpCode && (
                  <FieldError>
                    {form.formState.errors.totpCode.message}
                  </FieldError>
                )}
              </Field>
            ) : (
              <Field data-invalid={!!form.formState.errors.recoveryCode}>
                <FieldLabel htmlFor="recoveryCode">{t("login.recoveryCodeLabel")}</FieldLabel>
                <Input
                  id="recoveryCode"
                  type="text"
                  autoComplete="off"
                  placeholder={t("login.recoveryCodePlaceholder")}
                  className="font-mono uppercase tracking-wider"
                  aria-invalid={!!form.formState.errors.recoveryCode}
                  {...form.register("recoveryCode")}
                />
                {form.formState.errors.recoveryCode && (
                  <FieldError>
                    {form.formState.errors.recoveryCode.message}
                  </FieldError>
                )}
              </Field>
            )}

            <div className="flex justify-end">
              <button
                type="button"
                className="text-xs text-muted-foreground hover:text-foreground underline underline-offset-2 transition-colors cursor-pointer"
                onClick={() => {
                  setUseRecoveryCode(!useRecoveryCode);
                  form.clearErrors(["totpCode", "recoveryCode"]);
                  form.clearErrors("root");
                }}
              >
                {useRecoveryCode
                  ? t("login.useTotpInstead")
                  : t("login.useRecoveryInstead")}
              </button>
            </div>

            {form.formState.errors.root && (
              <p className="text-sm text-destructive" role="alert">
                {form.formState.errors.root.message}
              </p>
            )}

            <Button type="submit" disabled={mutation.isPending} className="mt-2 w-full">
              {mutation.isPending ? t("login.signingIn") : t("login.signIn")}
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
