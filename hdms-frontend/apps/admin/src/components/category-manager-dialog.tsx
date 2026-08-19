import {
  type Category,
  createCategory,
  listCategories,
  updateCategory,
  zCreateCategoryRequest,
} from "@hdms/api-client";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { PencilLine, Plus } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
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

const categorySchema = zCreateCategoryRequest.extend({
  name: z.string().min(1, "Name is required"),
  defaultLoanPeriodSeconds: z.number().int().min(0).optional(),
});
type CategoryFormValues = z.infer<typeof categorySchema>;

function CategoryForm({
  category,
  onDone,
}: {
  category?: Category;
  onDone: () => void;
}) {
  const queryClient = useQueryClient();
  const form = useForm<CategoryFormValues>({
    resolver: zodResolver(categorySchema),
    defaultValues: {
      name: category?.name ?? "",
      defaultLoanPeriodSeconds: category?.defaultLoanPeriodSeconds
        ? category.defaultLoanPeriodSeconds / 86400
        : undefined,
      requiresApproval: category?.requiresApproval ?? false,
    },
  });

  const mutation = useMutation({
    mutationFn: async (values: CategoryFormValues) => {
      const body = {
        name: values.name,
        defaultLoanPeriodSeconds: values.defaultLoanPeriodSeconds
          ? values.defaultLoanPeriodSeconds * 86400
          : undefined,
        requiresApproval: values.requiresApproval,
      };
      const { data, error } = category
        ? await updateCategory({ path: { id: category.id }, body })
        : await createCategory({ body });
      if (error) throw error;
      return data;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["categories"] });
      toast.success(category ? "Category updated" : "Category created");
      onDone();
    },
    onError: () => toast.error("Could not save the category"),
  });

  return (
    <form onSubmit={form.handleSubmit((v) => mutation.mutate(v))}>
      <FieldGroup>
        <Field data-invalid={!!form.formState.errors.name}>
          <FieldLabel htmlFor="cat-name">Name</FieldLabel>
          <Input id="cat-name" autoFocus {...form.register("name")} />
          {form.formState.errors.name && (
            <FieldError>{form.formState.errors.name.message}</FieldError>
          )}
        </Field>
        <Field>
          <FieldLabel htmlFor="cat-loan-days">Default loan period (days)</FieldLabel>
          <Input
            id="cat-loan-days"
            type="number"
            min={0}
            {...form.register("defaultLoanPeriodSeconds", { valueAsNumber: true })}
          />
        </Field>
        <Field orientation="horizontal">
          <input
            id="cat-approval"
            type="checkbox"
            className="size-4"
            {...form.register("requiresApproval")}
          />
          <FieldLabel htmlFor="cat-approval" className="font-normal">
            Requires approval to borrow
          </FieldLabel>
        </Field>
        <div className="flex justify-end gap-2 pt-2">
          <Button type="button" variant="outline" onClick={onDone}>
            Cancel
          </Button>
          <Button type="submit" disabled={mutation.isPending}>
            {category ? "Save" : "Create"}
          </Button>
        </div>
      </FieldGroup>
    </form>
  );
}

export function CategoryManagerDialog() {
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Category | "new" | null>(null);
  const { data } = useQuery({
    queryKey: ["categories"],
    queryFn: async () => {
      const { data, error } = await listCategories();
      if (error) throw error;
      return data.items;
    },
    enabled: open,
  });

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">
          Manage categories
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Device categories</DialogTitle>
          <DialogDescription>
            Categories set the default loan period new devices inherit.
          </DialogDescription>
        </DialogHeader>
        {editing ? (
          <CategoryForm
            category={editing === "new" ? undefined : editing}
            onDone={() => setEditing(null)}
          />
        ) : (
          <>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Loan period</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {data?.map((c) => (
                  <TableRow key={c.id}>
                    <TableCell>{c.name}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {c.defaultLoanPeriodSeconds
                        ? `${Math.round(c.defaultLoanPeriodSeconds / 86400)} days`
                        : "—"}
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => setEditing(c)}
                        aria-label={`Edit ${c.name}`}
                      >
                        <PencilLine className="size-4" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
                {data?.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={3} className="text-center text-muted-foreground">
                      No categories yet.
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
            <Button
              variant="outline"
              size="sm"
              className="self-start"
              onClick={() => setEditing("new")}
            >
              <Plus className="size-4" data-icon="inline-start" />
              New category
            </Button>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
