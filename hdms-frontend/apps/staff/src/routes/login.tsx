import { useMutation } from "@tanstack/react-query";
import { createRoute } from "@tanstack/react-router";
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
import { useT } from "@/i18n";
import { loginWithPassword } from "@/lib/auth";
import { useLocalizedResolver } from "@/lib/localized-resolver";
import { queryClient } from "@/lib/query-client";
import { rootRoute } from "./root";

const loginSchema = z.object({
  employeeNo: z.string().min(1, "validation.employeeNoRequired"),
  password: z.string().min(1, "validation.passwordRequired"),
});

type LoginFormValues = z.infer<typeof loginSchema>;

export interface LoginPageProps {
  onSubmit?: (values: { employeeNo: string; password: string }) => Promise<unknown>;
  error?: string;
}

export function LoginPage({ onSubmit, error: errorProp }: LoginPageProps = {}) {
  const t = useT();

  const [urlError, setUrlError] = useState<string | undefined>(() => {
    if (errorProp) return errorProp;
    if (typeof window !== "undefined" && window.location?.search) {
      const params = new URLSearchParams(window.location.search);
      return params.get("error") || undefined;
    }
    return undefined;
  });

  const form = useForm<LoginFormValues>({
    resolver: useLocalizedResolver(loginSchema),
    defaultValues: { employeeNo: "", password: "" },
  });

  const mutation = useMutation(
    {
      mutationFn: loginWithPassword,
      onSuccess: async () => {
        if (typeof window !== "undefined") {
          // Base-aware: production serves under /staff (5.3a), dev at /.
          const base = (import.meta as any).env?.VITE_BASE_PATH ?? "/";
          window.location.href = base === "/" ? "/" : `${base.replace(/\/$/, "")}/`;
        }
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
    },
    queryClient,
  );

  const getUrlErrorMessage = (code: string) => {
    switch (code) {
      case "tenant_not_allowed":
        return t("login.tenantNotAllowed");
      case "domain_not_allowed":
        return t("login.domainNotAllowed");
      case "state_unknown":
        return t("login.stateUnknown");
      default:
        return t("login.genericError");
    }
  };

  const handleSubmit = async (values: LoginFormValues) => {
    form.clearErrors("root");
    setUrlError(undefined);

    if (onSubmit) {
      try {
        await onSubmit(values);
      } catch {
        form.setError("root", { message: t("login.invalidCredentials") });
      }
      return;
    }

    mutation.mutate(values);
  };

  const isPending = mutation.isPending || form.formState.isSubmitting;
  const activeAlert = form.formState.errors.root?.message || (urlError ? getUrlErrorMessage(urlError) : null);

  return (
    <main className="flex min-h-dvh items-center justify-center bg-secondary p-6">
      <div className="w-full max-w-sm rounded-lg border border-border bg-card p-8 shadow-sm">
        <div className="mb-6">
          <div className="flex items-center justify-between">
            <p className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
              {t("login.hospitalName")}
            </p>
            <span className="text-xs font-bold tracking-wider text-muted-foreground">
              {t("appTitle")}
            </span>
          </div>
          <h1 className="mt-1 text-2xl font-semibold text-card-foreground">
            {t("login.title")}
          </h1>
        </div>

        <form onSubmit={form.handleSubmit(handleSubmit)}>
          <FieldGroup>
            <Field data-invalid={!!form.formState.errors.employeeNo}>
              <FieldLabel htmlFor="employeeNo">{t("login.employeeNoLabel")}</FieldLabel>
              <Input
                id="employeeNo"
                type="text"
                autoComplete="username"
                autoFocus
                placeholder={t("login.employeeNoPlaceholder")}
                aria-invalid={!!form.formState.errors.employeeNo}
                {...form.register("employeeNo")}
              />
              {form.formState.errors.employeeNo && (
                <FieldError>{form.formState.errors.employeeNo.message}</FieldError>
              )}
            </Field>

            <Field data-invalid={!!form.formState.errors.password}>
              <FieldLabel htmlFor="password">{t("login.passwordLabel")}</FieldLabel>
              <Input
                id="password"
                type="password"
                autoComplete="current-password"
                placeholder={t("login.passwordPlaceholder")}
                aria-invalid={!!form.formState.errors.password}
                {...form.register("password")}
              />
              {form.formState.errors.password && (
                <FieldError>{form.formState.errors.password.message}</FieldError>
              )}
            </Field>

            {activeAlert && (
              <p className="text-sm text-destructive" role="alert">
                {activeAlert}
              </p>
            )}

            <Button type="submit" disabled={isPending} className="mt-2 w-full">
              {isPending ? t("login.signingIn") : t("login.signIn")}
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
