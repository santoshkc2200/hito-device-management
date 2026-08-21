import { useMutation, useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
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
import {
  beginTotpReenrolmentAdmin,
  confirmTotpReenrolmentAdmin,
  currentAdminQueryOptions,
} from "@/lib/auth";
import { toast } from "sonner";
import { Barcode } from "@/components/barcode";
import { Check, Copy, QrCode, ShieldCheck } from "lucide-react";
import type { TotpEnrolment } from "@hdms/api-client";

const totpConfirmSchema = z.object({
  totpCode: z
    .string()
    .min(6, "Enter the 6-digit verification code from your authenticator app")
    .max(6, "Code must be 6 digits"),
});

type TotpConfirmFormValues = z.infer<typeof totpConfirmSchema>;

export function ForcedTotpDialog() {
  const { data: admin } = useQuery(currentAdminQueryOptions);
  const router = useRouter();
  const [enrolment, setEnrolment] = useState<TotpEnrolment | null>(null);
  const [copiedSecret, setCopiedSecret] = useState(false);

  const isOpen = Boolean(admin?.mustReenrolTotp);

  const beginMutation = useMutation({
    mutationFn: beginTotpReenrolmentAdmin,
    onSuccess: (data) => {
      setEnrolment(data);
    },
    onError: () => {
      toast.error("Failed to initiate TOTP re-enrolment. Please try again.");
    },
  });

  const form = useForm<TotpConfirmFormValues>({
    resolver: zodResolver(totpConfirmSchema),
    defaultValues: { totpCode: "" },
  });

  useEffect(() => {
    if (isOpen && !enrolment && !beginMutation.isPending) {
      beginMutation.mutate();
    }
  }, [isOpen, enrolment, beginMutation]);

  const confirmMutation = useMutation({
    mutationFn: confirmTotpReenrolmentAdmin,
    onSuccess: async () => {
      form.reset();
      setEnrolment(null);
      toast.success("Authenticator re-enrolled successfully.");
      await router.invalidate();
    },
    onError: (error: unknown) => {
      let message = "Invalid authenticator code. Please try again.";
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

  const handleCopySecret = async () => {
    if (!enrolment?.totpSecret) return;
    try {
      await navigator.clipboard.writeText(enrolment.totpSecret);
      setCopiedSecret(true);
      toast.success("Secret copied to clipboard");
      setTimeout(() => setCopiedSecret(false), 3000);
    } catch {
      toast.error("Failed to copy secret");
    }
  };

  const handleConfirm = (values: TotpConfirmFormValues) => {
    confirmMutation.mutate({ totpCode: values.totpCode });
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
            <div className="flex size-8 items-center justify-center rounded-full bg-primary/10 text-primary">
              <QrCode className="size-4" />
            </div>
            <DialogTitle>Authenticator re-enrolment required</DialogTitle>
          </div>
          <DialogDescription>
            You must configure a new 2-factor authenticator app before accessing the admin console.
          </DialogDescription>
        </DialogHeader>

        {beginMutation.isPending && (
          <div className="py-8 text-center text-sm text-muted-foreground">
            Generating authenticator credentials…
          </div>
        )}

        {enrolment && (
          <div className="space-y-4">
            <div className="flex flex-col items-center justify-center rounded-lg border border-border bg-white p-4 text-black shadow-xs">
              <div className="size-44 flex items-center justify-center">
                <Barcode
                  value={enrolment.otpauthUrl}
                  symbology="qrcode"
                  scale={3}
                  className="max-h-full max-w-full"
                />
              </div>
              <p className="mt-2 text-[11px] text-zinc-500">
                Scan with Google Authenticator, 1Password, or compatible app
              </p>
            </div>

            <div className="rounded-md border border-border bg-muted/40 p-3">
              <div className="flex items-center justify-between text-xs text-muted-foreground mb-1">
                <span>Manual entry secret</span>
                <button
                  type="button"
                  onClick={handleCopySecret}
                  className="flex items-center gap-1 font-medium text-foreground hover:underline cursor-pointer"
                >
                  {copiedSecret ? (
                    <>
                      <Check className="size-3 text-green-600" />
                      Copied
                    </>
                  ) : (
                    <>
                      <Copy className="size-3" />
                      Copy secret
                    </>
                  )}
                </button>
              </div>
              <p className="font-mono text-xs font-semibold tracking-wider text-foreground select-all break-all">
                {enrolment.totpSecret}
              </p>
            </div>

            <form onSubmit={form.handleSubmit(handleConfirm)}>
              <FieldGroup>
                <Field data-invalid={!!form.formState.errors.totpCode}>
                  <FieldLabel htmlFor="forced-totp-code">Verification code</FieldLabel>
                  <Input
                    id="forced-totp-code"
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    maxLength={6}
                    placeholder="Enter 6-digit code"
                    className="font-mono tracking-widest"
                    autoFocus
                    aria-invalid={!!form.formState.errors.totpCode}
                    {...form.register("totpCode")}
                  />
                  {form.formState.errors.totpCode && (
                    <FieldError>{form.formState.errors.totpCode.message}</FieldError>
                  )}
                </Field>

                {form.formState.errors.root && (
                  <p className="text-sm text-destructive" role="alert">
                    {form.formState.errors.root.message}
                  </p>
                )}

                <Button
                  type="submit"
                  disabled={confirmMutation.isPending}
                  className="w-full gap-2 mt-2"
                >
                  <ShieldCheck className="size-4" />
                  {confirmMutation.isPending ? "Verifying…" : "Confirm & activate"}
                </Button>
              </FieldGroup>
            </form>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
