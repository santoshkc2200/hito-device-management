import { useMutation, useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
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
import {
  currentAdminQueryOptions,
  loginAdmin,
  logoutAdmin,
  onSessionExpired,
} from "@/lib/auth";
import { toast } from "sonner";

const reauthSchema = z.object({
  email: z.string().min(1, "Email is required").email("Enter a valid email"),
  password: z.string().min(1, "Password is required"),
  totpCode: z.string().optional(),
  recoveryCode: z.string().optional(),
});

type ReauthFormValues = z.infer<typeof reauthSchema>;

export function ReauthDialog() {
  const [isOpen, setIsOpen] = useState(false);
  const [useRecoveryCode, setUseRecoveryCode] = useState(false);
  const { data: admin } = useQuery(currentAdminQueryOptions);
  const router = useRouter();
  const navigate = useNavigate();

  const form = useForm<ReauthFormValues>({
    resolver: zodResolver(reauthSchema),
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
      toast.success("Session restored. You can continue where you left off.");
      await router.invalidate();
    },
    onError: (error: unknown) => {
      let message = "Invalid credentials or code.";
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
        form.setError("recoveryCode", { message: "Enter your recovery code" });
        return;
      }
      mutation.mutate({
        email: values.email,
        password: values.password,
        recoveryCode: values.recoveryCode.trim(),
      });
    } else {
      if (!values.totpCode || values.totpCode.trim().length < 6) {
        form.setError("totpCode", { message: "Enter the 6-digit authenticator code" });
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
          <DialogTitle>Session expired</DialogTitle>
          <DialogDescription>
            Your session has timed out. Re-authenticate below to keep working without losing any form state.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit(handleSubmit)}>
          <FieldGroup className="mt-4">
            <Field data-invalid={!!form.formState.errors.email}>
              <FieldLabel htmlFor="reauth-email">Email</FieldLabel>
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
              <FieldLabel htmlFor="reauth-password">Password</FieldLabel>
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
                <FieldLabel htmlFor="reauth-totp">Authenticator code</FieldLabel>
                <Input
                  id="reauth-totp"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  maxLength={6}
                  placeholder="6-digit code"
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
                <FieldLabel htmlFor="reauth-recovery">Recovery code</FieldLabel>
                <Input
                  id="reauth-recovery"
                  type="text"
                  autoComplete="off"
                  placeholder="Single-use code"
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
                  ? "Use authenticator code"
                  : "Use recovery code"}
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
                Sign out
              </Button>
              <Button
                type="submit"
                disabled={mutation.isPending}
              >
                {mutation.isPending ? "Re-authenticating…" : "Resume session"}
              </Button>
            </div>
          </FieldGroup>
        </form>
      </DialogContent>
    </Dialog>
  );
}
