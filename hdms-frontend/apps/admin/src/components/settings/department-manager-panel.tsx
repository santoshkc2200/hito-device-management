import {
  type Department,
  createDepartment,
  deleteDepartment,
  listDepartments,
  updateDepartment,
} from "@hdms/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { PencilLine, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useT } from "@/i18n";

function errorDetail(error: unknown) {
  if (error && typeof error === "object" && "detail" in error) {
    return String((error as { detail?: string }).detail ?? "");
  }
  return "";
}

function DepartmentForm({
  department,
  onDone,
}: {
  department?: Department;
  onDone: () => void;
}) {
  const t = useT();
  const queryClient = useQueryClient();
  const [name, setName] = useState(department?.name ?? "");
  const [error, setError] = useState("");
  const mutation = useMutation({
    mutationFn: async () => {
      const result = department
        ? await updateDepartment({ path: { id: department.id }, body: { name } })
        : await createDepartment({ body: { name } });
      if (result.error) throw result.error;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["departments"] });
      toast.success(department ? t("departmentManagerPanel.departmentUpdated") : t("departmentManagerPanel.departmentCreated"));
      onDone();
    },
    onError: (cause) => {
      const detail = errorDetail(cause);
      setError(detail || t("departmentManagerPanel.saveFailed"));
    },
  });

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        setError("");
        mutation.mutate();
      }}
    >
      <FieldGroup>
        <Field data-invalid={!!error}>
          <FieldLabel htmlFor="department-name">{t("columns.name")}</FieldLabel>
          <Input
            id="department-name"
            autoFocus
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
          {error && <FieldError>{error}</FieldError>}
        </Field>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onDone} disabled={mutation.isPending}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" disabled={!name.trim() || mutation.isPending}>
            {department ? t("common.save") : t("departmentManagerPanel.create")}
          </Button>
        </DialogFooter>
      </FieldGroup>
    </form>
  );
}

export function DepartmentManagerPanel() {
  const t = useT();
  const queryClient = useQueryClient();
  const [editor, setEditor] = useState<Department | "new" | null>(null);
  const [deleting, setDeleting] = useState<Department | null>(null);
  const { data: departments = [], isLoading } = useQuery({
    queryKey: ["departments"],
    queryFn: async () => {
      const result = await listDepartments();
      if (result.error) throw result.error;
      return result.data.items;
    },
  });
  const deleteMutation = useMutation({
    mutationFn: async (department: Department) => {
      const result = await deleteDepartment({ path: { id: department.id } });
      if (result.error) throw result.error;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["departments"] });
      toast.success(t("departmentManagerPanel.departmentDeleted"));
      setDeleting(null);
    },
    onError: (cause) => {
      toast.error(errorDetail(cause) || t("departmentManagerPanel.deleteFailed"));
      setDeleting(null);
    },
  });

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-lg font-semibold">{t("departmentManagerPanel.title")}</h2>
          <p className="text-sm text-muted-foreground">{t("departmentManagerPanel.subtitle")}</p>
        </div>
        <Button size="sm" onClick={() => setEditor("new")}>
          <Plus className="size-4" data-icon="inline-start" />
          {t("departmentManagerPanel.newDepartment")}
        </Button>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("columns.name")}</TableHead>
            <TableHead className="w-24 text-right">{t("columns.actions")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {departments.map((department) => (
            <TableRow key={department.id}>
              <TableCell>{department.name}</TableCell>
              <TableCell>
                <div className="flex justify-end gap-1">
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => setEditor(department)}
                    aria-label={t("departmentManagerPanel.editAria", { name: department.name })}
                  >
                    <PencilLine className="size-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="text-destructive hover:text-destructive"
                    onClick={() => setDeleting(department)}
                    aria-label={t("departmentManagerPanel.deleteAria", { name: department.name })}
                  >
                    <Trash2 className="size-4" />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
          {!isLoading && departments.length === 0 && (
            <TableRow>
              <TableCell colSpan={2} className="text-center text-muted-foreground">
                {t("departmentManagerPanel.noDepartmentsYet")}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>

      <Dialog open={editor !== null} onOpenChange={(open) => !open && setEditor(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{editor === "new" ? t("departmentManagerPanel.newDepartment") : t("departmentManagerPanel.editDepartment")}</DialogTitle>
            <DialogDescription>{t("departmentManagerPanel.dialogDescription")}</DialogDescription>
          </DialogHeader>
          {editor && (
            <DepartmentForm
              department={editor === "new" ? undefined : editor}
              onDone={() => setEditor(null)}
            />
          )}
        </DialogContent>
      </Dialog>

      <AlertDialog open={deleting !== null} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("departmentManagerPanel.deleteConfirmTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("departmentManagerPanel.deleteConfirmDescription", {
                name: deleting?.name ?? t("departmentManagerPanel.thisDepartmentFallback"),
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={deleteMutation.isPending || !deleting}
              onClick={(event) => {
                event.preventDefault();
                if (deleting) deleteMutation.mutate(deleting);
              }}
            >
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
