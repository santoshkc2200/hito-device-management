import { type User } from "@hdms/api-client";
import { Printer } from "lucide-react";
import { LabelSheet, StaffCardLabel } from "@/components/label-templates";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { SymbologyToggle } from "@/components/symbology-toggle";
import { useCodeSymbology } from "@/lib/code-symbology";
import { cardSheetSettings } from "@/lib/label-settings";
import { useT } from "@/i18n";

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
  const [symbology] = useCodeSymbology();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>{t("userCardSheetDialog.title")}</DialogTitle>
          <DialogDescription>
            {users.length === 1
              ? t("userCardSheetDialog.readyOne", { count: users.length })
              : t("userCardSheetDialog.readyOther", { count: users.length })}
          </DialogDescription>
        </DialogHeader>
        <SymbologyToggle />
        <div className="max-h-[70vh] overflow-auto rounded-md border border-border bg-secondary p-4">
          <LabelSheet settings={cardSheetSettings}>
            {users.map((user) => (
              <StaffCardLabel
                key={user.id}
                fullName={user.fullName}
                employeeNo={user.employeeNo}
                department={user.departmentId ? departmentName.get(user.departmentId) : undefined}
                token={user.employeeNo}
                symbology={symbology}
              />
            ))}
          </LabelSheet>
        </div>
        <div className="flex justify-end">
          <Button disabled={users.length === 0} onClick={() => window.print()}>
            <Printer className="size-4" data-icon="inline-start" />
            {t("userCardSheetDialog.printSheet")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
