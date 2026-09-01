import { useMutation, useQuery } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { useRouter } from "@tanstack/react-router";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { useLocalizedResolver } from "@/lib/localized-resolver";
import {
  changeOwnPasswordAdmin,
  currentAdminQueryOptions,
} from "@/lib/auth";
import { toast } from "sonner";
import { KeyRound, ShieldAlert } from "lucide-react";

const passwordChangeSchema = z
  .object({
    currentPassword: z.string().min(1, "validation.currentPasswordRequired"),
    newPassword: z
      .string()
      .min(12, "validation.newPasswordMin"),
    confirmPassword: z.string().min(1, "validation.confirmPasswordRequired"),
  })
  .refine((data) => data.newPassword === data.confirmPassword, {
    message: "validation.passwordsMismatch",
    path: ["confirmPassword"],
  });

type PasswordChangeFormValues = z.infer<typeof passwordChangeSchema>;

export function ForcedPasswordChangeDialog() {
  const { data: admin } = useQuery(currentAdminQueryOptions);
  const router = useRouter();

  const isOpen = Boolean(admin?.mustChangePassword);

  const form = useForm<PasswordChangeFormValues>({
    resolver: useLocalizedResolver(passwordChangeSchema),
    defaultValues: {
      currentPassword: "",
      newPassword: "",
      confirmPassword: "",
    },
  });

  const mutation = useMutation({
    mutationFn: changeOwnPasswordAdmin,
    onSuccess: async () => {
      form.reset();
      toast.success("Password changed successfully.");
      await router.invalidate();
    },
    onError: (error: unknown) => {
      let message = "Failed to update password.";
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

  const handleSubmit = (values: PasswordChangeFormValues) => {
    mutation.mutate({
      currentPassword: values.currentPassword,
      newPassword: values.newPassword,
    });
  };

  return (
    <Dialog open={isOpen}>
      <DialogContent
        className="sm:max-w-md"
        onEscapeKeyDown={(e) => e.preventDefault()}
        onPointerDownOutside={(e) => e.preventDefault()}
      >
        <DialogHeader>
          <div className="flex items-center gap-2">
            <div className="flex size-8 items-center justify-center rounded-full bg-amber-500/10 text-amber-600">
              <KeyRound className="size-4" />
            </div>
            <DialogTitle>Password change required</DialogTitle>
          </div>
          <DialogDescription>
            An administrator or system security policy requires you to set a new password before continuing.
          </DialogDescription>
        </DialogHeader>

        <div className="my-1 rounded-md border border-border bg-muted/40 p-3 text-xs text-muted-foreground flex items-start gap-2">
          <ShieldAlert className="size-4 shrink-0 text-muted-foreground mt-0.5" />
          <span>Passwords must be at least 12 characters in length.</span>
        </div>

        <form onSubmit={form.handleSubmit(handleSubmit)}>
          <FieldGroup className="mt-3">
            <Field data-invalid={!!form.formState.errors.currentPassword}>
              <FieldLabel htmlFor="forced-current-password">Current password</FieldLabel>
              <Input
                id="forced-current-password"
                type="password"
                autoComplete="current-password"
                autoFocus
                aria-invalid={!!form.formState.errors.currentPassword}
                {...form.register("currentPassword")}
              />
              {form.formState.errors.currentPassword && (
                <FieldError>{form.formState.errors.currentPassword.message}</FieldError>
              )}
            </Field>

            <Field data-invalid={!!form.formState.errors.newPassword}>
              <FieldLabel htmlFor="forced-new-password">New password</FieldLabel>
              <Input
                id="forced-new-password"
                type="password"
                autoComplete="new-password"
                aria-invalid={!!form.formState.errors.newPassword}
                {...form.register("newPassword")}
              />
              {form.formState.errors.newPassword && (
                <FieldError>{form.formState.errors.newPassword.message}</FieldError>
              )}
            </Field>

            <Field data-invalid={!!form.formState.errors.confirmPassword}>
              <FieldLabel htmlFor="forced-confirm-password">Confirm new password</FieldLabel>
              <Input
                id="forced-confirm-password"
                type="password"
                autoComplete="new-password"
                aria-invalid={!!form.formState.errors.confirmPassword}
                {...form.register("confirmPassword")}
              />
              {form.formState.errors.confirmPassword && (
                <FieldError>{form.formState.errors.confirmPassword.message}</FieldError>
              )}
            </Field>

            {form.formState.errors.root && (
              <p className="text-sm text-destructive" role="alert">
                {form.formState.errors.root.message}
              </p>
            )}

            <Button
              type="submit"
              disabled={mutation.isPending}
              className="mt-2 w-full"
            >
              {mutation.isPending ? "Updating password…" : "Set new password & continue"}
            </Button>
          </FieldGroup>
        </form>
      </DialogContent>
    </Dialog>
  );
}
