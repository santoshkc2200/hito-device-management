import { useMutation, useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { useNavigate, useRouter } from "@tanstack/react-router";
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
  currentAdminQueryOptions,
  loginAdmin,
  logoutAdmin,
  onSessionExpired,
} from "@/lib/auth";
import { toast } from "sonner";
import { useT } from "@/i18n";

const reauthSchema = z.object({
  email: z.string().min(1, "validation.emailRequired").email("validation.emailInvalid"),
  password: z.string().min(1, "validation.passwordRequired"),
  totpCode: z.string().optional(),
  recoveryCode: z.string().optional(),
});

type ReauthFormValues = z.infer<typeof reauthSchema>;

export function ReauthDialog() {
  const t = useT();
  const [isOpen, setIsOpen] = useState(false);
  const [useRecoveryCode, setUseRecoveryCode] = useState(false);
  const { data: admin } = useQuery(currentAdminQueryOptions);
  const router = useRouter();
  const navigate = useNavigate();

  const form = useForm<ReauthFormValues>({
    resolver: useLocalizedResolver(reauthSchema),
    defaultValues: {
      email: admin?.email || "",
      password: "",
      totpCode: "",
      recoveryCode: "",
    },
  });

  useEffect(() => {
    if (admin?.email) {
      form.setValue("email", admin.email);
    }
  }, [admin?.email, form]);

  useEffect(() => {
    const unsubscribe = onSessionExpired(() => {
      setIsOpen(true);
    });
    return unsubscribe;
  }, []);

  const mutation = useMutation({
    mutationFn: loginAdmin,
    onSuccess: async () => {
      setIsOpen(false);
      form.reset({
        email: admin?.email || "",
        password: "",
        totpCode: "",
        recoveryCode: "",
      });
      toast.success(t("reauthDialog.sessionRestored"));
      await router.invalidate();
    },
    onError: (error: unknown) => {
      let message = t("reauthDialog.invalidCredentials");
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

  const handleSubmit = (values: ReauthFormValues) => {
    if (useRecoveryCode) {
      if (!values.recoveryCode || values.recoveryCode.trim().length === 0) {
        form.setError("recoveryCode", { message: t("reauthDialog.enterRecoveryCode") });
        return;
      }
      mutation.mutate({
        email: values.email,
        password: values.password,
        recoveryCode: values.recoveryCode.trim(),
      });
    } else {
      if (!values.totpCode || values.totpCode.trim().length < 6) {
        form.setError("totpCode", { message: t("reauthDialog.enterTotpCode") });
        return;
      }
      mutation.mutate({
        email: values.email,
        password: values.password,
        totpCode: values.totpCode.trim(),
      });
    }
  };

  const handleSignOut = async () => {
    setIsOpen(false);
    await logoutAdmin();
    await router.invalidate();
    await navigate({ to: "/login" });
  };

  return (
    <Dialog open={isOpen} onOpenChange={(open) => {
      // Prevent accidental backdrop closing to protect form state
      if (!open && mutation.isPending) return;
      setIsOpen(open);
    }}>
      <DialogContent
        className="sm:max-w-md"
        onEscapeKeyDown={(e) => e.preventDefault()}
        onPointerDownOutside={(e) => e.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle>{t("reauthDialog.title")}</DialogTitle>
          <DialogDescription>{t("reauthDialog.description")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit(handleSubmit)}>
          <FieldGroup className="mt-4">
            <Field data-invalid={!!form.formState.errors.email}>
              <FieldLabel htmlFor="reauth-email">{t("login.emailLabel")}</FieldLabel>
              <Input
                id="reauth-email"
                type="email"
                autoComplete="username"
                aria-invalid={!!form.formState.errors.email}
                {...form.register("email")}
              />
              {form.formState.errors.email && (
                <FieldError>{form.formState.errors.email.message}</FieldError>
              )}
            </Field>

            <Field data-invalid={!!form.formState.errors.password}>
              <FieldLabel htmlFor="reauth-password">{t("login.passwordLabel")}</FieldLabel>
              <Input
                id="reauth-password"
                type="password"
                autoComplete="current-password"
                autoFocus
                aria-invalid={!!form.formState.errors.password}
                {...form.register("password")}
              />
              {form.formState.errors.password && (
                <FieldError>{form.formState.errors.password.message}</FieldError>
              )}
            </Field>

            {!useRecoveryCode ? (
              <Field data-invalid={!!form.formState.errors.totpCode}>
                <FieldLabel htmlFor="reauth-totp">{t("login.totpLabel")}</FieldLabel>
                <Input
                  id="reauth-totp"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  maxLength={6}
                  placeholder={t("login.totpPlaceholder")}
                  className="font-mono tracking-widest"
                  aria-invalid={!!form.formState.errors.totpCode}
                  {...form.register("totpCode")}
                />
                {form.formState.errors.totpCode && (
                  <FieldError>{form.formState.errors.totpCode.message}</FieldError>
                )}
              </Field>
            ) : (
              <Field data-invalid={!!form.formState.errors.recoveryCode}>
                <FieldLabel htmlFor="reauth-recovery">{t("login.recoveryCodeLabel")}</FieldLabel>
                <Input
                  id="reauth-recovery"
                  type="text"
                  autoComplete="off"
                  placeholder={t("reauthDialog.singleUseCodePlaceholder")}
                  className="font-mono uppercase tracking-wider"
                  aria-invalid={!!form.formState.errors.recoveryCode}
                  {...form.register("recoveryCode")}
                />
                {form.formState.errors.recoveryCode && (
                  <FieldError>{form.formState.errors.recoveryCode.message}</FieldError>
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
                  ? t("reauthDialog.useAuthenticatorCode")
                  : t("reauthDialog.useRecoveryCode")}
              </button>
            </div>

            {form.formState.errors.root && (
              <p className="text-sm text-destructive" role="alert">
                {form.formState.errors.root.message}
              </p>
            )}

            <div className="mt-4 flex items-center justify-between gap-3">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={handleSignOut}
              >
                {t("nav.signOut")}
              </Button>
              <Button
                type="submit"
                disabled={mutation.isPending}
              >
                {mutation.isPending
                  ? t("reauthDialog.reauthenticating")
                  : t("reauthDialog.resumeSession")}
              </Button>
            </div>
          </FieldGroup>
        </form>
      </DialogContent>
    </Dialog>
  );
}
