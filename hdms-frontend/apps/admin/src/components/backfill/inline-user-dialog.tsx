import {
  listDepartments,
  type BackfillNewUser,
} from "@hdms/api-client";
import { useQuery } from "@tanstack/react-query";
import { UserPlus } from "lucide-react";
import { useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export interface CreatedPerson {
  /** newUser payload the backfill row will carry */
  newUser: BackfillNewUser;
  /** Display label shown in the staged table */
  displayName: string;
}

interface InlineUserDialogProps {
  open: boolean;
  onClose: () => void;
  /** Called when the admin fills the form; does NOT call the API — the
   *  server creates the person atomically with the batch. */
  onConfirm: (person: CreatedPerson) => void;
  /** Pre-filled name from a typeahead search that returned no match */
  initialName?: string;
}

export function InlineUserDialog({
  open,
  onClose,
  onConfirm,
  initialName = "",
}: InlineUserDialogProps) {
  const [fullName, setFullName] = useState(initialName);
  const [employeeNo, setEmployeeNo] = useState("");
  const [departmentId, setDepartmentId] = useState<string | undefined>();
  const nameRef = useRef<HTMLInputElement>(null);

  const { data: departments } = useQuery({
    queryKey: ["departments"],
    queryFn: async () => {
      const { data, error } = await listDepartments();
      if (error) throw error;
      return data.items;
    },
    staleTime: 5 * 60_000,
  });

  // Reset when re-opened
  const handleOpenChange = (v: boolean) => {
    if (!v) {
      onClose();
    } else {
      setFullName(initialName);
      setEmployeeNo("");
      setDepartmentId(undefined);
    }
  };

  const handleConfirm = () => {
    const name = fullName.trim();
    const empNo = employeeNo.trim();
    if (!name) {
      nameRef.current?.focus();
      return;
    }
    onConfirm({
      newUser: {
        fullName: name,
        employeeNo: empNo,
        departmentId,
      },
      displayName: `${name}${empNo ? ` (${empNo})` : ""}`,
    });
    onClose();
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <UserPlus className="h-4 w-4" />
            Create new person
          </DialogTitle>
          <DialogDescription>
            The person will be created atomically when this batch is saved.
            Staged rows are not affected.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-4 py-2">
          <Field>
            <FieldLabel htmlFor="iud-fullname">
              Full name
            </FieldLabel>
            <Input
              id="iud-fullname"
              ref={nameRef}
              value={fullName}
              onChange={(e) => setFullName(e.target.value)}
              placeholder="Tanaka Hiroshi"
              onKeyDown={(e) => {
                if (e.key === "Enter") handleConfirm();
              }}
              autoFocus
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="iud-empno">Employee no.</FieldLabel>
            <Input
              id="iud-empno"
              value={employeeNo}
              onChange={(e) => setEmployeeNo(e.target.value)}
              placeholder="E-1234"
              onKeyDown={(e) => {
                if (e.key === "Enter") handleConfirm();
              }}
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="iud-dept">Department</FieldLabel>
            <Select value={departmentId ?? ""} onValueChange={(val) => setDepartmentId(val || undefined)}>
              <SelectTrigger id="iud-dept">
                <SelectValue placeholder="Select department (optional)" />
              </SelectTrigger>
              <SelectContent>
                {departments?.map((d) => (
                  <SelectItem key={d.id} value={d.id}>
                    {d.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={handleConfirm} disabled={!fullName.trim()}>
            Add to batch
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
