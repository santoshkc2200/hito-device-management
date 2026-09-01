import {
  type Department,
  type User,
  createUser,
  updateUser,
  zCreateUserRequest,
} from "@hdms/api-client";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { useLocalizedResolver } from "@/lib/localized-resolver";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

const userSchema = zCreateUserRequest.extend({
  employeeNo: z.string().min(1, "validation.employeeNoRequired"),
  fullName: z.string().min(1, "validation.fullNameRequired"),
  email: z.string().email("validation.emailInvalid").optional().or(z.literal("")),
});
export type UserFormValues = z.infer<typeof userSchema>;

export function UserForm({
  user,
  departments,
  onDone,
}: {
  user?: User;
  departments: Department[];
  onDone: (user: User) => void;
}) {
  const queryClient = useQueryClient();
  const form = useForm<UserFormValues>({
    resolver: useLocalizedResolver(userSchema),
    defaultValues: {
      employeeNo: user?.employeeNo ?? "",
      fullName: user?.fullName ?? "",
      departmentId: user?.departmentId ?? "",
      email: user?.email ?? "",
      phone: user?.phone ?? "",
      notes: user?.notes ?? "",
    },
  });

  const mutation = useMutation({
    mutationFn: async (values: UserFormValues) => {
      const { data, error } = user
        ? await updateUser({
            path: { id: user.id },
            body: {
              fullName: values.fullName,
              departmentId: values.departmentId,
              email: values.email,
              phone: values.phone,
              notes: values.notes,
            },
          })
        : await createUser({ body: values });
      if (error) throw error;
      return data;
    },
    onSuccess: async (data) => {
      await queryClient.invalidateQueries({ queryKey: ["users"] });
      toast.success(user ? "User updated" : "User registered");
      if (data) onDone(data);
    },
    onError: (error) => {
      const detail =
        error && typeof error === "object" && "detail" in error
          ? String((error as { detail?: string }).detail)
          : "Could not save the user";
      toast.error(detail);
    },
  });

  return (
    <form onSubmit={form.handleSubmit((v) => mutation.mutate(v))}>
      <FieldGroup>
        <div className="grid grid-cols-2 gap-3">
          <Field data-invalid={!!form.formState.errors.employeeNo}>
            <FieldLabel htmlFor="employeeNo">Employee no.</FieldLabel>
            <Input
              id="employeeNo"
              className="font-identifier"
              autoFocus
              disabled={!!user}
              {...form.register("employeeNo")}
            />
            {form.formState.errors.employeeNo && (
              <FieldError>{form.formState.errors.employeeNo.message}</FieldError>
            )}
          </Field>
          <Field data-invalid={!!form.formState.errors.fullName}>
            <FieldLabel htmlFor="fullName">Full name</FieldLabel>
            <Input id="fullName" {...form.register("fullName")} />
            {form.formState.errors.fullName && (
              <FieldError>{form.formState.errors.fullName.message}</FieldError>
            )}
          </Field>
        </div>
        <Field>
          <FieldLabel htmlFor="departmentId">Department</FieldLabel>
          <Select
            value={form.watch("departmentId") || undefined}
            onValueChange={(v) => form.setValue("departmentId", v)}
          >
            <SelectTrigger id="departmentId" className="w-full">
              <SelectValue placeholder="Select a department" />
            </SelectTrigger>
            <SelectContent>
              {departments.map((d) => (
                <SelectItem key={d.id} value={d.id}>
                  {d.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field data-invalid={!!form.formState.errors.email}>
            <FieldLabel htmlFor="email">Email</FieldLabel>
            <Input id="email" type="email" {...form.register("email")} />
            {form.formState.errors.email && (
              <FieldError>{form.formState.errors.email.message}</FieldError>
            )}
          </Field>
          <Field>
            <FieldLabel htmlFor="phone">Phone</FieldLabel>
            <Input id="phone" {...form.register("phone")} />
          </Field>
        </div>
        <Field>
          <FieldLabel htmlFor="notes">Notes</FieldLabel>
          <Input id="notes" {...form.register("notes")} />
        </Field>
        <div className="flex justify-end gap-2 pt-2">
          <Button type="submit" disabled={mutation.isPending}>
            {user ? "Save" : "Register"}
          </Button>
        </div>
      </FieldGroup>
    </form>
  );
}
