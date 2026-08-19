import {
  type Category,
  type Device,
  createDevice,
  updateDevice,
  zCreateDeviceRequest,
} from "@hdms/api-client";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

const deviceSchema = zCreateDeviceRequest.extend({
  assetTag: z.string().min(1, "Asset tag is required"),
  name: z.string().min(1, "Name is required"),
  categoryId: z.string().min(1, "Category is required"),
});
export type DeviceFormValues = z.infer<typeof deviceSchema>;

export function DeviceForm({
  device,
  categories,
  onDone,
}: {
  device?: Device;
  categories: Category[];
  onDone: (device: Device) => void;
}) {
  const queryClient = useQueryClient();
  const form = useForm<DeviceFormValues>({
    resolver: zodResolver(deviceSchema),
    defaultValues: {
      assetTag: device?.assetTag ?? "",
      name: device?.name ?? "",
      categoryId: device?.categoryId ?? "",
      manufacturer: device?.manufacturer ?? "",
      model: device?.model ?? "",
      serialNo: device?.serialNo ?? "",
      homeLocation: device?.homeLocation ?? "",
      notes: device?.notes ?? "",
    },
  });

  const mutation = useMutation({
    mutationFn: async (values: DeviceFormValues) => {
      const { data, error } = device
        ? await updateDevice({ path: { id: device.id }, body: values })
        : await createDevice({ body: values });
      if (error) throw error;
      return data;
    },
    onSuccess: async (data) => {
      await queryClient.invalidateQueries({ queryKey: ["devices"] });
      toast.success(device ? "Device updated" : "Device registered");
      if (data) onDone(data);
    },
    onError: (error) => {
      const detail =
        error && typeof error === "object" && "detail" in error
          ? String((error as { detail?: string }).detail)
          : "Could not save the device";
      toast.error(detail);
    },
  });

  return (
    <form onSubmit={form.handleSubmit((v) => mutation.mutate(v))}>
      <FieldGroup>
        <div className="grid grid-cols-2 gap-3">
          <Field data-invalid={!!form.formState.errors.assetTag}>
            <FieldLabel htmlFor="assetTag">Asset tag</FieldLabel>
            <Input id="assetTag" className="font-identifier" autoFocus {...form.register("assetTag")} />
            {form.formState.errors.assetTag && (
              <FieldError>{form.formState.errors.assetTag.message}</FieldError>
            )}
          </Field>
          <Field data-invalid={!!form.formState.errors.categoryId}>
            <FieldLabel htmlFor="categoryId">Category</FieldLabel>
            <Select
              value={form.watch("categoryId")}
              onValueChange={(v) => form.setValue("categoryId", v, { shouldValidate: true })}
            >
              <SelectTrigger id="categoryId" className="w-full">
                <SelectValue placeholder="Select a category" />
              </SelectTrigger>
              <SelectContent>
                {categories.map((c) => (
                  <SelectItem key={c.id} value={c.id}>
                    {c.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {form.formState.errors.categoryId && (
              <FieldError>{form.formState.errors.categoryId.message}</FieldError>
            )}
          </Field>
        </div>
        <Field data-invalid={!!form.formState.errors.name}>
          <FieldLabel htmlFor="name">Name</FieldLabel>
          <Input id="name" {...form.register("name")} />
          {form.formState.errors.name && <FieldError>{form.formState.errors.name.message}</FieldError>}
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field>
            <FieldLabel htmlFor="manufacturer">Manufacturer</FieldLabel>
            <Input id="manufacturer" {...form.register("manufacturer")} />
          </Field>
          <Field>
            <FieldLabel htmlFor="model">Model</FieldLabel>
            <Input id="model" {...form.register("model")} />
          </Field>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Field>
            <FieldLabel htmlFor="serialNo">Serial no.</FieldLabel>
            <Input id="serialNo" className="font-identifier" {...form.register("serialNo")} />
          </Field>
          <Field>
            <FieldLabel htmlFor="homeLocation">Home location</FieldLabel>
            <Input id="homeLocation" {...form.register("homeLocation")} />
          </Field>
        </div>
        <Field>
          <FieldLabel htmlFor="notes">Notes</FieldLabel>
          <Input id="notes" {...form.register("notes")} />
        </Field>
        <div className="flex justify-end gap-2 pt-2">
          <Button type="submit" disabled={mutation.isPending}>
            {device ? "Save" : "Register device"}
          </Button>
        </div>
      </FieldGroup>
    </form>
  );
}
