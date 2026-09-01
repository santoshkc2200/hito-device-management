import { useState } from "react";
import { Check, Copy, Printer, ShieldAlert } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";
import { useT } from "@/i18n";

export interface RecoveryCodesDialogProps {
  open: boolean;
  codes: string[];
  title?: string;
  description?: string;
  onDismiss: () => void;
}

export function RecoveryCodesDialog({
  open,
  codes,
  title,
  description,
  onDismiss,
}: RecoveryCodesDialogProps) {
  const t = useT();
  const [copied, setCopied] = useState(false);
  const [confirmed, setConfirmed] = useState(false);
  const resolvedTitle = title ?? t("recoveryCodesDialog.defaultTitle");
  const resolvedDescription = description ?? t("recoveryCodesDialog.defaultDescription");

  const handleCopy = async () => {
    try {
      const text = [
        t("recoveryCodesDialog.clipboardHeader"),
        t("recoveryCodesDialog.clipboardGenerated", { date: new Date().toISOString() }),
        "",
        ...codes.map((c, i) => `${i + 1}. ${c}`),
        "",
        t("recoveryCodesDialog.clipboardFooter"),
      ].join("\n");
      await navigator.clipboard.writeText(text);
      setCopied(true);
      toast.success(t("recoveryCodesDialog.copiedToast"));
      setTimeout(() => setCopied(false), 3000);
    } catch {
      toast.error(t("recoveryCodesDialog.copyFailedToast"));
    }
  };

  const handlePrint = () => {
    window.print();
  };

  const handleClose = () => {
    if (!confirmed) {
      toast.error(t("recoveryCodesDialog.confirmBeforeClose"));
      return;
    }
    setConfirmed(false);
    onDismiss();
  };

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => {
      if (!nextOpen && confirmed) {
        handleClose();
      }
    }}>
      <DialogContent
        className="sm:max-w-lg"
        onEscapeKeyDown={(e) => {
          if (!confirmed) e.preventDefault();
        }}
        onPointerDownOutside={(e) => {
          if (!confirmed) e.preventDefault();
        }}
      >
        <DialogHeader>
          <DialogTitle>{resolvedTitle}</DialogTitle>
          <DialogDescription>{resolvedDescription}</DialogDescription>
        </DialogHeader>

        <div className="my-2 rounded-md border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-900 dark:text-amber-200 flex items-start gap-2.5">
          <ShieldAlert className="size-5 shrink-0 text-amber-600 dark:text-amber-400 mt-0.5" />
          <div className="space-y-1 text-xs leading-relaxed">
            <p className="font-semibold text-amber-950 dark:text-amber-100">
              {t("recoveryCodesDialog.shownOnceWarning")}
            </p>
            <p>{t("recoveryCodesDialog.storeSecurelyHint")}</p>
          </div>
        </div>

        <div className="rounded-lg border border-border bg-muted/40 p-4">
          <div className="grid grid-cols-2 gap-2.5 font-mono text-sm tracking-wider sm:grid-cols-4">
            {codes.map((code, idx) => (
              <div
                key={idx}
                className="flex items-center justify-center rounded border border-border/80 bg-background px-2.5 py-1.5 font-medium shadow-xs select-all"
              >
                {code}
              </div>
            ))}
          </div>
        </div>

        <div className="flex items-center justify-between gap-2 pt-1">
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={handleCopy}
              className="gap-1.5"
            >
              {copied ? (
                <>
                  <Check className="size-4 text-green-600" />
                  {t("forcedTotpDialog.copied")}
                </>
              ) : (
                <>
                  <Copy className="size-4" />
                  {t("recoveryCodesDialog.copyAllCodes")}
                </>
              )}
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={handlePrint}
              className="gap-1.5"
            >
              <Printer className="size-4" />
              {t("recoveryCodesDialog.printCodes")}
            </Button>
          </div>
        </div>

        <div className="mt-3 flex items-start gap-2.5 rounded-md border border-border bg-card p-3">
          <input
            id="confirm-saved-codes"
            type="checkbox"
            checked={confirmed}
            onChange={(e) => setConfirmed(e.target.checked)}
            className="mt-0.5 size-4 rounded border-border text-primary cursor-pointer focus:ring-ring"
          />
          <label
            htmlFor="confirm-saved-codes"
            className="text-xs font-medium text-foreground cursor-pointer leading-snug"
          >
            {t("recoveryCodesDialog.confirmCheckboxLabel")}
          </label>
        </div>

        <DialogFooter className="mt-2">
          <Button
            type="button"
            disabled={!confirmed}
            onClick={handleClose}
            className="w-full sm:w-auto"
          >
            {t("recoveryCodesDialog.doneAndClose")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
