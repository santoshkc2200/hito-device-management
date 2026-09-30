import { type User, listCredentialsBySubject, revealCredential } from "@hdms/api-client";
import { useMutation } from "@tanstack/react-query";
import { Printer } from "lucide-react";
import { useEffect } from "react";
import { LabelSheet, StaffCardLabel } from "@/components/label-templates";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { cardSheetSettings } from "@/lib/label-settings";
import { useT } from "@/i18n";

// Bulk counterpart of the per-user "View QR" action in CredentialsPanel:
// reveals each selected user's existing active card token (audited
// server-side as credential.revealed) rather than minting a new one, so the
// printed cards match the ones already bound to these users.
async function loadUserCard(user: User) {
  const { data, error } = await listCredentialsBySubject({
    query: { subjectType: "user", subjectId: user.id },
  });
  if (error) throw error;
  const active = data.items.find((c) => c.status === "active");
  if (!active) return { user, token: null as string | null };
  const reveal = await revealCredential({ path: { id: active.id } });
  // A card whose token was never stored reversibly can't be reprinted; skip
  // it like a user with no card rather than failing the whole sheet.
  if (reveal.error) return { user, token: null as string | null };
  return { user, token: reveal.data?.token ?? null };
}

export function UserCardSheetDialog({
  users,
  departmentName,
  open,
  onOpenChange,
}: {
  users: User[];
  departmentName: Map<string, string>;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const t = useT();

  // A manual one-shot mutation, not a query: every reveal writes an audit
  // event, so this must run exactly once per dialog open, never silently
  // again on refetch/refocus.
  const mutation = useMutation({
    mutationFn: () => Promise.all(users.map(loadUserCard)),
  });

  useEffect(() => {
    if (open && users.length > 0) mutation.mutate();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const results = mutation.data ?? [];
  const withToken = results.filter((r) => r.token);
  const withoutToken = results.filter((r) => !r.token);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>{t("userCardSheetDialog.title")}</DialogTitle>
          <DialogDescription>
            {mutation.isPending
              ? t("userCardSheetDialog.loading")
              : mutation.isError
                ? t("userCardSheetDialog.loadFailed")
                : [
                    withToken.length === 1
                      ? t("userCardSheetDialog.readyOne", { count: withToken.length })
                      : t("userCardSheetDialog.readyOther", { count: withToken.length }),
                    withoutToken.length > 0
                      ? withoutToken.length === 1
                        ? t("userCardSheetDialog.skippedOne", { count: withoutToken.length })
                        : t("userCardSheetDialog.skippedOther", { count: withoutToken.length })
                      : null,
                  ]
                    .filter(Boolean)
                    .join(" ")}
          </DialogDescription>
        </DialogHeader>
        {mutation.isPending ? (
          <Skeleton className="h-64 w-full" />
        ) : (
          <div className="max-h-[70vh] overflow-auto rounded-md border border-border bg-secondary p-4">
            <LabelSheet settings={cardSheetSettings}>
              {withToken.map(({ user, token }) => (
                <StaffCardLabel
                  key={user.id}
                  fullName={user.fullName}
                  employeeNo={user.employeeNo}
                  department={user.departmentId ? departmentName.get(user.departmentId) : undefined}
                  token={token!}
                />
              ))}
            </LabelSheet>
          </div>
        )}
        <div className="flex justify-end">
          <Button disabled={mutation.isPending || withToken.length === 0} onClick={() => window.print()}>
            <Printer className="size-4" data-icon="inline-start" />
            {t("userCardSheetDialog.printSheet")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
