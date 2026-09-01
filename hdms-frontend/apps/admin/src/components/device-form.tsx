import {
  type Category,
  type Device,
  createDevice,
  updateDevice,
  zCreateDeviceRequest,
} from "@hdms/api-client";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { z } from "zod";
import { generateAssetTag } from "./asset-tag";
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
import { useT } from "@/i18n";

const deviceSchema = zCreateDeviceRequest.extend({
  assetTag: z.string().min(1, "validation.assetTagRequired"),
  name: z.string().min(1, "validation.nameRequired"),
  categoryId: z.string().min(1, "validation.categoryRequired"),
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
  const t = useT();
  const queryClient = useQueryClient();
  const form = useForm<DeviceFormValues>({
    resolver: useLocalizedResolver(deviceSchema),
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
      toast.success(device ? t("deviceForm.deviceUpdated") : t("deviceForm.deviceRegistered"));
      if (data) onDone(data);
    },
    onError: (error) => {
      const detail =
        error && typeof error === "object" && "detail" in error
          ? String((error as { detail?: string }).detail)
          : t("deviceForm.saveFailed");
      toast.error(detail);
    },
  });

  return (
    <form onSubmit={form.handleSubmit((v) => mutation.mutate(v))}>
      <FieldGroup>
        <div className="grid grid-cols-2 gap-3">
          <Field data-invalid={!!form.formState.errors.assetTag}>
            <div className="flex items-center justify-between gap-2">
              <FieldLabel htmlFor="assetTag">{t("columns.assetTag")}</FieldLabel>
              {!device && (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  title={t("deviceForm.generateAssetTag")}
                  aria-label={t("deviceForm.generateAssetTag")}
                  onClick={() => {
                    const category = categories.find((c) => c.id === form.getValues("categoryId"));
                    form.setValue("assetTag", generateAssetTag(category?.name), {
                      shouldDirty: true,
                      shouldValidate: true,
                    });
                  }}
                >
                  <RefreshCw />
                </Button>
              )}
            </div>
            <Input id="assetTag" className="font-identifier" autoFocus {...form.register("assetTag")} />
            {form.formState.errors.assetTag && (
              <FieldError>{form.formState.errors.assetTag.message}</FieldError>
            )}
          </Field>
          <Field data-invalid={!!form.formState.errors.categoryId}>
            <FieldLabel htmlFor="categoryId">{t("columns.category")}</FieldLabel>
            <Select
              value={form.watch("categoryId")}
              onValueChange={(v) => form.setValue("categoryId", v, { shouldValidate: true })}
            >
              <SelectTrigger id="categoryId" className="w-full">
                <SelectValue placeholder={t("deviceForm.selectCategoryPlaceholder")} />
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
          <FieldLabel htmlFor="name">{t("columns.name")}</FieldLabel>
          <Input id="name" {...form.register("name")} />
          {form.formState.errors.name && <FieldError>{form.formState.errors.name.message}</FieldError>}
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field>
            <FieldLabel htmlFor="manufacturer">{t("devices.csvHeaders.manufacturer")}</FieldLabel>
            <Input id="manufacturer" {...form.register("manufacturer")} />
          </Field>
          <Field>
            <FieldLabel htmlFor="model">{t("columns.model")}</FieldLabel>
            <Input id="model" {...form.register("model")} />
          </Field>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Field>
            <FieldLabel htmlFor="serialNo">{t("deviceForm.serialNoLabel")}</FieldLabel>
            <Input id="serialNo" className="font-identifier" {...form.register("serialNo")} />
          </Field>
          <Field>
            <FieldLabel htmlFor="homeLocation">{t("devices.csvHeaders.homeLocation")}</FieldLabel>
            <Input id="homeLocation" {...form.register("homeLocation")} />
          </Field>
        </div>
        <Field>
          <FieldLabel htmlFor="notes">{t("devices.csvHeaders.notes")}</FieldLabel>
          <Input id="notes" {...form.register("notes")} />
        </Field>
        <div className="flex justify-end gap-2 pt-2">
          <Button type="submit" disabled={mutation.isPending}>
            {device ? t("common.save") : t("deviceForm.registerDevice")}
          </Button>
        </div>
      </FieldGroup>
    </form>
  );
}
