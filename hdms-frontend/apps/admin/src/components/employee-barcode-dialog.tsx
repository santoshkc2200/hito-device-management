import { issueCredential } from "@hdms/api-client";
import { useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useT } from "@/i18n";

// Adopts the barcode already printed on a borrower's employee ID as a
// second, manual credential. The field takes a scanner's keyboard input, so
// the admin scans the ID card into it rather than typing.
export function EmployeeBarcodeDialog({
  open,
  onOpenChange,
  userId,
  onIssued,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  userId: string;
  onIssued(): Promise<unknown>;
}) {
  const t = useT();
  const [value, setValue] = useState("");

  const close = (next: boolean) => {
    if (!next) setValue("");
    onOpenChange(next);
  };

  const mutation = useMutation({
    mutationFn: async (manualToken: string) => {
      const { error, response } = await issueCredential({
        body: {
          subjectType: "user",
          subjectId: userId,
          kind: "manual",
          manualToken,
          label: "Employee ID",
        },
      });
      if (error) throw Object.assign(new Error("issue failed"), { status: response?.status });
    },
    onSuccess: async () => {
      await onIssued();
      toast.success(t("credentialsPanel.employeeBarcodeAdded"));
      close(false);
    },
    onError: (err: Error & { status?: number }) =>
      toast.error(
        err.status === 409
          ? t("credentialsPanel.employeeBarcodeTaken")
          : t("credentialsPanel.employeeBarcodeFailed"),
      ),
  });

  const trimmed = value.trim();

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (trimmed && !mutation.isPending) mutation.mutate(trimmed);
          }}
        >
          <DialogHeader>
            <DialogTitle>{t("credentialsPanel.employeeBarcodeTitle")}</DialogTitle>
            <DialogDescription>{t("credentialsPanel.employeeBarcodeDescription")}</DialogDescription>
          </DialogHeader>
          <Input
            autoFocus
            autoComplete="off"
            aria-label={t("credentialsPanel.employeeBarcodeField")}
            placeholder={t("credentialsPanel.employeeBarcodePlaceholder")}
            value={value}
            maxLength={64}
            onChange={(e) => setValue(e.target.value)}
          />
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => close(false)} disabled={mutation.isPending}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={!trimmed || mutation.isPending}>
              {t("credentialsPanel.employeeBarcodeSave")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
