import { useMutation } from "@tanstack/react-query";
import { createRoute, redirect, useRouter } from "@tanstack/react-router";
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
import { completeProfile, currentStaffQueryKey } from "@/lib/auth";
import { useLocalizedResolver } from "@/lib/localized-resolver";
import { queryClient } from "@/lib/query-client";
import { authenticatedRoute } from "./authenticated";

const completeProfileSchema = z.object({
  employeeNo: z.string().min(1, "validation.employeeNoRequired"),
});

type CompleteProfileFormValues = z.infer<typeof completeProfileSchema>;

export function CompleteProfilePage() {
  const t = useT();
  const router = useRouter();

  const form = useForm<CompleteProfileFormValues>({
    resolver: useLocalizedResolver(completeProfileSchema),
    defaultValues: { employeeNo: "" },
  });

  const mutation = useMutation(
    {
      mutationFn: async (values: CompleteProfileFormValues) => {
        return completeProfile({ employeeNo: values.employeeNo.trim() });
      },
      onSuccess: async () => {
        await queryClient.invalidateQueries({ queryKey: currentStaffQueryKey });
        await router.invalidate();
        await router.navigate({ to: "/" });
      },
      onError: (error: unknown) => {
        let message = t("completeProfile.genericError");
        if (error && typeof error === "object") {
          if ("status" in error && (error as { status: number }).status === 409) {
            message = t("completeProfile.employeeNoTaken");
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

  const handleSubmit = (values: CompleteProfileFormValues) => {
    mutation.mutate(values);
  };

  return (
    <main className="flex min-h-dvh items-center justify-center bg-secondary p-6">
      <div className="w-full max-w-sm rounded-lg border border-border bg-card p-8 shadow-sm">
        <div className="mb-6">
          <p className="text-xs font-medium tracking-widest text-muted-foreground uppercase">
            {t("login.hospitalName")}
          </p>
          <h1 className="mt-1 text-2xl font-semibold text-card-foreground">
            {t("completeProfile.title")}
          </h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {t("completeProfile.description")}
          </p>
        </div>

        <form onSubmit={form.handleSubmit(handleSubmit)}>
          <FieldGroup>
            <Field data-invalid={!!form.formState.errors.employeeNo}>
              <FieldLabel htmlFor="employeeNo">
                {t("completeProfile.employeeNoLabel")}
              </FieldLabel>
              <Input
                id="employeeNo"
                type="text"
                autoComplete="off"
                autoFocus
                placeholder={t("completeProfile.employeeNoPlaceholder")}
                aria-invalid={!!form.formState.errors.employeeNo}
                {...form.register("employeeNo")}
              />
              {form.formState.errors.employeeNo && (
                <FieldError>{form.formState.errors.employeeNo.message}</FieldError>
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
                ? t("completeProfile.submitting")
                : t("completeProfile.submit")}
            </Button>
          </FieldGroup>
        </form>
      </div>
    </main>
  );
}

export const completeProfileRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/complete-profile",
  beforeLoad: ({ context }) => {
    if (context.me.profileComplete) {
      throw redirect({ to: "/" });
    }
  },
  component: CompleteProfilePage,
});
