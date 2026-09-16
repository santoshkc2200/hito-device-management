import { changeStaffPassword } from "@hdms/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute, Link, redirect, useRouter } from "@tanstack/react-router";
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
import { currentStaffQueryKey, currentStaffQueryOptions } from "@/lib/auth";
import { useLocalizedResolver } from "@/lib/localized-resolver";
import { authenticatedRoute } from "./authenticated";

const changePasswordSchema = z.object({
  currentPassword: z.string().min(1, "validation.currentPasswordRequired"),
  newPassword: z.string().min(12, "validation.passwordMinLength"),
});

type ChangePasswordFormValues = z.infer<typeof changePasswordSchema>;

export function ChangePasswordPage() {
  const t = useT();
  const router = useRouter();
  const queryClient = useQueryClient();
  const { data: meData } = useQuery(currentStaffQueryOptions);
  const context = changePasswordRoute.useRouteContext();
  const me = meData ?? context.me;

  const isForced = Boolean(me?.mustChangePassword);

  const form = useForm<ChangePasswordFormValues>({
    resolver: useLocalizedResolver(changePasswordSchema),
    defaultValues: { currentPassword: "", newPassword: "" },
  });

  const mutation = useMutation(
    {
      mutationFn: async (values: ChangePasswordFormValues) => {
        const { error } = await changeStaffPassword({
          body: {
            currentPassword: values.currentPassword,
            newPassword: values.newPassword,
          },
        });
        if (error) throw error;
      },
      onSuccess: async () => {
        await queryClient.invalidateQueries({ queryKey: currentStaffQueryKey });
        await router.invalidate();
        await router.navigate({ to: isForced ? "/" : "/settings" });
      },
      onError: (error: unknown) => {
        let message = t("changePassword.genericError");
        if (error && typeof error === "object") {
          if ("status" in error && (error as { status: number }).status === 400) {
            message = t("changePassword.invalidCurrentPassword");
          } else if ("detail" in error && typeof (error as { detail: string }).detail === "string") {
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

  const handleSubmit = (values: ChangePasswordFormValues) => {
    mutation.mutate(values);
  };

  return (
    <main className="flex min-h-dvh items-center justify-center bg-secondary p-6">
      <div className="w-full max-w-sm rounded-lg border border-border bg-card p-8 shadow-sm">
        <div className="mb-6">
          <p className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
            {t("changePassword.badge")}
          </p>
          <h1 className="mt-1 text-2xl font-semibold text-card-foreground">
            {isForced ? t("changePassword.forcedTitle") : t("changePassword.title")}
          </h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {isForced
              ? t("changePassword.forcedExplanation")
              : t("changePassword.description")}
          </p>
        </div>

        <form onSubmit={form.handleSubmit(handleSubmit)}>
          <FieldGroup>
            <Field data-invalid={!!form.formState.errors.currentPassword}>
              <FieldLabel htmlFor="currentPassword">
                {t("changePassword.currentPasswordLabel")}
              </FieldLabel>
              <Input
                id="currentPassword"
                type="password"
                autoComplete="current-password"
                placeholder={t("changePassword.currentPasswordPlaceholder")}
                aria-invalid={!!form.formState.errors.currentPassword}
                {...form.register("currentPassword")}
              />
              {form.formState.errors.currentPassword && (
                <FieldError>{form.formState.errors.currentPassword.message}</FieldError>
              )}
            </Field>

            <Field data-invalid={!!form.formState.errors.newPassword}>
              <FieldLabel htmlFor="newPassword">
                {t("changePassword.newPasswordLabel")}
              </FieldLabel>
              <Input
                id="newPassword"
                type="password"
                autoComplete="new-password"
                placeholder={t("changePassword.newPasswordPlaceholder")}
                aria-invalid={!!form.formState.errors.newPassword}
                {...form.register("newPassword")}
              />
              {form.formState.errors.newPassword && (
                <FieldError>{form.formState.errors.newPassword.message}</FieldError>
              )}
            </Field>

            {form.formState.errors.root && (
              <p className="text-sm text-destructive" role="alert">
                {form.formState.errors.root.message}
              </p>
            )}

            <Button
              type="submit"
              disabled={mutation.isPending || form.formState.isSubmitting}
              className="mt-2 w-full"
            >
              {mutation.isPending
                ? t("changePassword.saving")
                : t("changePassword.save")}
            </Button>

            {!isForced && (
              <div className="mt-4 text-center">
                <Link
                  to="/settings"
                  className="text-xs font-medium text-muted-foreground hover:underline"
                >
                  {t("changePassword.backToSettings")}
                </Link>
              </div>
            )}
          </FieldGroup>
        </form>
      </div>
    </main>
  );
}

export const changePasswordRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/change-password",
  beforeLoad: ({ context }) => {
    if (!context.me.hasPassword) {
      throw redirect({ to: "/" });
    }
  },
  component: ChangePasswordPage,
});
