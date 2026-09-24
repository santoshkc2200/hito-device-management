import { useState, useEffect } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm, Controller } from "react-hook-form";
import { z } from "zod";
import { useLocalizedResolver } from "@/lib/localized-resolver";
import { toast } from "sonner";
import {
  getSettings,
  updateSettings,
  listCategories,
  createCategory,
  updateCategory,
  type Category,
} from "@hdms/api-client";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { LoadingState } from "@/components/states";
import { useRole } from "@/lib/use-role";
import { PencilLine, Plus, Save, Clock, AlertTriangle } from "lucide-react";
import { useT } from "@/i18n";


const policyFormSchema = z.object({
  blockOnOverdue: z.boolean(),
  sessionIdleTimeoutSeconds: z.number().int().min(5, "validation.sessionTimeoutMin").max(600, "validation.sessionTimeoutMax"),
  kioskSoundEnabled: z.boolean(),
  lowStockThreshold: z.number().int().min(0, "validation.thresholdMin").max(10000, "validation.thresholdMax"),
  paperBacklogHours: z.number().int().min(1, "validation.backlogThresholdMin").max(720, "validation.backlogThresholdMax"),
});

type PolicyFormValues = z.infer<typeof policyFormSchema>;

const bookingPolicySchema = z.object({
  advanceDays: z.number().int().min(1, "validation.bookingDaysRange").max(365, "validation.bookingDaysRange"),
  maxDurationDays: z.number().int().min(1, "validation.bookingDaysRange").max(365, "validation.bookingDaysRange"),
  returnBufferMinutes: z.number().int().min(0, "validation.bookingBufferRange").max(1440, "validation.bookingBufferRange"),
});

type BookingPolicyFormValues = z.infer<typeof bookingPolicySchema>;

const categoryFormSchema = z.object({
  name: z.string().min(1, "validation.nameRequired"),
  defaultLoanPeriodDays: z.number().int().min(0).optional(),
  requiresApproval: z.boolean(),
});

type CategoryFormValues = z.infer<typeof categoryFormSchema>;

export function PolicyPanel() {
  const t = useT();
  const queryClient = useQueryClient();
  const { role } = useRole();
  const isAdmin = role === "admin";

  const [categoryModalOpen, setCategoryModalOpen] = useState(false);
  const [editingCategory, setEditingCategory] = useState<Category | null>(null);

  // 1. Fetch Settings
  const {
    data: settingsData,
    isLoading: isSettingsLoading,
    error: settingsError,
  } = useQuery({
    queryKey: ["settings"],
    queryFn: async () => {
      const res = await getSettings();
      if (res.error) throw res.error;
      return res.data;
    },
  });

  // 2. Fetch Categories
  const {
    data: categoriesData,
    isLoading: isCategoriesLoading,
  } = useQuery({
    queryKey: ["categories"],
    queryFn: async () => {
      const res = await listCategories();
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
  });

  // 3. Policy Form setup
  const {
    register,
    handleSubmit,
    control,
    reset,
    formState: { errors, isDirty },
  } = useForm<PolicyFormValues>({
    resolver: useLocalizedResolver(policyFormSchema),
    defaultValues: {
      blockOnOverdue: false,
      sessionIdleTimeoutSeconds: 45,
      kioskSoundEnabled: true,
      lowStockThreshold: 10,
      paperBacklogHours: 48,
    },
  });

  const bookingForm = useForm<BookingPolicyFormValues>({
    resolver: useLocalizedResolver(bookingPolicySchema),
    defaultValues: { advanceDays: 90, maxDurationDays: 30, returnBufferMinutes: 60 },
  });

  useEffect(() => {
    if (settingsData?.policy) {
      reset({
        blockOnOverdue: settingsData.policy.blockOnOverdue,
        sessionIdleTimeoutSeconds: settingsData.policy.sessionIdleTimeoutSeconds,
        kioskSoundEnabled: settingsData.policy.kioskSoundEnabled,
        lowStockThreshold: settingsData.policy.lowStockThreshold,
        paperBacklogHours: settingsData.policy.paperBacklogHours,
      });
    }
  }, [settingsData, reset]);

  useEffect(() => {
    if (settingsData?.bookingPolicy) bookingForm.reset(settingsData.bookingPolicy);
  }, [settingsData, bookingForm.reset]);

  // 4. Update Policy Mutation
  const updatePolicyMutation = useMutation({
    mutationFn: async (values: PolicyFormValues) => {
      const res = await updateSettings({
        body: {
          policy: values,
        },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (data) => {
      queryClient.setQueryData(["settings"], data);
      toast.success(t("policyPanel.policyUpdated"));
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || t("policyPanel.policyUpdateFailed"));
    },
  });

  const updateBookingPolicyMutation = useMutation({
    mutationFn: async (values: BookingPolicyFormValues) => {
      const res = await updateSettings({ body: { bookingPolicy: values } });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (data) => {
      queryClient.setQueryData(["settings"], data);
      toast.success(t("policyPanel.bookingPolicyUpdated"));
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || t("policyPanel.bookingPolicyUpdateFailed"));
    },
  });

  // 5. Category Form setup
  const categoryForm = useForm<CategoryFormValues>({
    resolver: useLocalizedResolver(categoryFormSchema),
    defaultValues: {
      name: "",
      defaultLoanPeriodDays: 7,
      requiresApproval: false,
    },
  });

  const openNewCategoryModal = () => {
    setEditingCategory(null);
    categoryForm.reset({
      name: "",
      defaultLoanPeriodDays: 7,
      requiresApproval: false,
    });
    setCategoryModalOpen(true);
  };

  const openEditCategoryModal = (cat: Category) => {
    setEditingCategory(cat);
    categoryForm.reset({
      name: cat.name,
      defaultLoanPeriodDays: cat.defaultLoanPeriodSeconds
        ? Math.round(cat.defaultLoanPeriodSeconds / 86400)
        : undefined,
      requiresApproval: cat.requiresApproval ?? false,
    });
    setCategoryModalOpen(true);
  };

  const saveCategoryMutation = useMutation({
    mutationFn: async (values: CategoryFormValues) => {
      const defaultLoanPeriodSeconds =
        values.defaultLoanPeriodDays && values.defaultLoanPeriodDays > 0
          ? values.defaultLoanPeriodDays * 86400
          : undefined;

      if (editingCategory) {
        const res = await updateCategory({
          path: { id: editingCategory.id },
          body: {
            name: values.name,
            defaultLoanPeriodSeconds,
            requiresApproval: values.requiresApproval,
          },
        });
        if (res.error) throw res.error;
        return res.data;
      } else {
        const res = await createCategory({
          body: {
            name: values.name,
            defaultLoanPeriodSeconds,
            requiresApproval: values.requiresApproval,
          },
        });
        if (res.error) throw res.error;
        return res.data;
      }
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["categories"] });
      toast.success(editingCategory ? t("categoryManagerDialog.categoryUpdated") : t("categoryManagerDialog.categoryCreated"));
      setCategoryModalOpen(false);
    },
    onError: (err: any) => {
      toast.error(err?.detail || t("policyPanel.categorySaveFailed"));
    },
  });

  if (isSettingsLoading || isCategoriesLoading) {
    return <LoadingState message={t("policyPanel.loadingSettings")} />;
  }

  if (settingsError) {
    return (
      <div className="flex flex-col items-center justify-center p-8 text-center text-destructive">
        <AlertTriangle className="size-8 mb-2" />
        <p className="font-semibold">{t("policyPanel.loadFailedTitle")}</p>
        <p className="text-xs text-muted-foreground mt-1">{t("kiosksPanel.tryRefreshing")}</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Category Management Card */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
          <div>
            <CardTitle>{t("policyPanel.deviceCategoriesTitle")}</CardTitle>
            <CardDescription>{t("policyPanel.deviceCategoriesDescription")}</CardDescription>
          </div>
          {isAdmin && (
            <Button size="sm" onClick={openNewCategoryModal} className="gap-1.5">
              <Plus className="size-4" />
              {t("policyPanel.newCategory")}
            </Button>
          )}
        </CardHeader>
        <CardContent>
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("policyPanel.colCategoryName")}</TableHead>
                  <TableHead>{t("policyPanel.colDefaultLoanPeriod")}</TableHead>
                  <TableHead>{t("policyPanel.colApprovalPolicy")}</TableHead>
                  {isAdmin && <TableHead className="w-16 text-right">{t("policyPanel.colAction")}</TableHead>}
                </TableRow>
              </TableHeader>
              <TableBody>
                {categoriesData && categoriesData.length > 0 ? (
                  categoriesData.map((cat) => (
                    <TableRow key={cat.id}>
                      <TableCell className="font-medium text-foreground">{cat.name}</TableCell>
                      <TableCell className="text-muted-foreground">
                        {cat.defaultLoanPeriodSeconds ? (
                          <span className="flex items-center gap-1.5">
                            <Clock className="size-3.5 text-muted-foreground" />
                            {t("categoryManagerDialog.daysSuffix", {
                              days: Math.round(cat.defaultLoanPeriodSeconds / 86400),
                            })}
                          </span>
                        ) : (
                          <span className="text-xs text-muted-foreground italic">{t("policyPanel.indefiniteNoLimit")}</span>
                        )}
                      </TableCell>
                      <TableCell>
                        {cat.requiresApproval ? (
                          <Badge variant="outline" className="text-amber-600 border-amber-300 bg-amber-50 dark:bg-amber-950/30">
                            {t("policyPanel.requiresApprovalBadge")}
                          </Badge>
                        ) : (
                          <Badge variant="secondary" className="text-muted-foreground">
                            {t("policyPanel.standardBadge")}
                          </Badge>
                        )}
                      </TableCell>
                      {isAdmin && (
                        <TableCell className="text-right">
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            onClick={() => openEditCategoryModal(cat)}
                            title={t("categoryManagerDialog.editAria", { name: cat.name })}
                          >
                            <PencilLine className="size-4" />
                          </Button>
                        </TableCell>
                      )}
                    </TableRow>
                  ))
                ) : (
                  <TableRow>
                    <TableCell colSpan={isAdmin ? 4 : 3} className="text-center py-6 text-muted-foreground">
                      {t("policyPanel.noCategoriesConfigured")}
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>

      {/* System Policy & Thresholds Card */}
      <Card>
        <CardHeader>
          <CardTitle>{t("policyPanel.systemPolicyTitle")}</CardTitle>
          <CardDescription>{t("policyPanel.systemPolicyDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit((v) => updatePolicyMutation.mutate(v))} className="space-y-6">
            <div className="grid gap-6 md:grid-cols-2">
              {/* Overdue Borrow Blocking */}
              <div className="flex items-start space-x-3 rounded-lg border p-4 shadow-2xs">
                <Controller
                  name="blockOnOverdue"
                  control={control}
                  render={({ field }) => (
                    <Checkbox
                      id="blockOnOverdue"
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={!isAdmin}
                    />
                  )}
                />
                <div className="space-y-1 leading-none">
                  <label
                    htmlFor="blockOnOverdue"
                    className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70 cursor-pointer"
                  >
                    {t("policyPanel.strictOverdueEnforcementLabel")}
                  </label>
                  <p className="text-xs text-muted-foreground">{t("policyPanel.strictOverdueEnforcementHint")}</p>
                </div>
              </div>

              {/* Kiosk Sound Effects */}
              <div className="flex items-start space-x-3 rounded-lg border p-4 shadow-2xs">
                <Controller
                  name="kioskSoundEnabled"
                  control={control}
                  render={({ field }) => (
                    <Checkbox
                      id="kioskSoundEnabled"
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={!isAdmin}
                    />
                  )}
                />
                <div className="space-y-1 leading-none">
                  <label
                    htmlFor="kioskSoundEnabled"
                    className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70 cursor-pointer"
                  >
                    {t("policyPanel.kioskSoundLabel")}
                  </label>
                  <p className="text-xs text-muted-foreground">{t("policyPanel.kioskSoundHint")}</p>
                </div>
              </div>
            </div>

            <div className="grid gap-6 md:grid-cols-3">
              {/* Session Timeout */}
              <div className="space-y-2">
                <label htmlFor="sessionIdleTimeoutSeconds" className="text-sm font-medium">
                  {t("policyPanel.sessionTimeoutLabel")}
                </label>
                <Input
                  id="sessionIdleTimeoutSeconds"
                  type="number"
                  min={5}
                  max={600}
                  disabled={!isAdmin}
                  {...register("sessionIdleTimeoutSeconds", { valueAsNumber: true })}
                />
                <p className="text-xs text-muted-foreground">{t("policyPanel.sessionTimeoutHint")}</p>
                {errors.sessionIdleTimeoutSeconds && (
                  <p className="text-xs text-destructive">{errors.sessionIdleTimeoutSeconds.message}</p>
                )}
              </div>

              {/* Blank Stock Threshold */}
              <div className="space-y-2">
                <label htmlFor="lowStockThreshold" className="text-sm font-medium">
                  {t("policyPanel.lowStockThresholdLabel")}
                </label>
                <Input
                  id="lowStockThreshold"
                  type="number"
                  min={0}
                  max={10000}
                  disabled={!isAdmin}
                  {...register("lowStockThreshold", { valueAsNumber: true })}
                />
                <p className="text-xs text-muted-foreground">{t("policyPanel.lowStockThresholdHint")}</p>
                {errors.lowStockThreshold && (
                  <p className="text-xs text-destructive">{errors.lowStockThreshold.message}</p>
                )}
              </div>

              {/* Paper Backlog Hours */}
              <div className="space-y-2">
                <label htmlFor="paperBacklogHours" className="text-sm font-medium">
                  {t("policyPanel.paperBacklogHoursLabel")}
                </label>
                <Input
                  id="paperBacklogHours"
                  type="number"
                  min={1}
                  max={720}
                  disabled={!isAdmin}
                  {...register("paperBacklogHours", { valueAsNumber: true })}
                />
                <p className="text-xs text-muted-foreground">{t("policyPanel.paperBacklogHoursHint")}</p>
                {errors.paperBacklogHours && (
                  <p className="text-xs text-destructive">{errors.paperBacklogHours.message}</p>
                )}
              </div>
            </div>

            {isAdmin && (
              <div className="flex items-center justify-between pt-4 border-t">
                <div className="text-xs text-muted-foreground">
                  {settingsData?.updatedAt && (
                    <span>
                      {t("policyPanel.lastModified", {
                        date: new Date(settingsData.updatedAt).toLocaleString(),
                      })}
                      {settingsData.updatedBy && t("policyPanel.byActor", { actor: settingsData.updatedBy })}
                    </span>
                  )}
                </div>
                <Button
                  type="submit"
                  disabled={updatePolicyMutation.isPending || !isDirty}
                  className="gap-2"
                >
                  <Save className="size-4" />
                  {updatePolicyMutation.isPending ? t("policyPanel.savingPolicy") : t("policyPanel.savePolicySettings")}
                </Button>
              </div>
            )}
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("policyPanel.bookingPolicyTitle")}</CardTitle>
          <CardDescription>{t("policyPanel.bookingPolicyDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={bookingForm.handleSubmit((v) => updateBookingPolicyMutation.mutate(v))} className="space-y-6">
            <div className="grid gap-6 md:grid-cols-3">
              {([
                ["advanceDays", "bookingAdvanceDaysLabel", 1, 365],
                ["maxDurationDays", "bookingMaxDurationDaysLabel", 1, 365],
                ["returnBufferMinutes", "bookingReturnBufferMinutesLabel", 0, 1440],
              ] as const).map(([name, label, min, max]) => (
                <div key={name} className="space-y-2">
                  <label htmlFor={name} className="text-sm font-medium">{t(`policyPanel.${label}`)}</label>
                  <Input id={name} type="number" min={min} max={max} disabled={!isAdmin}
                    {...bookingForm.register(name, { valueAsNumber: true })} />
                  {bookingForm.formState.errors[name] && (
                    <p className="text-xs text-destructive">{bookingForm.formState.errors[name]?.message}</p>
                  )}
                </div>
              ))}
            </div>
            {isAdmin && (
              <div className="flex justify-end border-t pt-4">
                <Button type="submit" disabled={updateBookingPolicyMutation.isPending || !bookingForm.formState.isDirty} className="gap-2">
                  <Save className="size-4" />
                  {updateBookingPolicyMutation.isPending ? t("policyPanel.savingPolicy") : t("policyPanel.saveBookingPolicy")}
                </Button>
              </div>
            )}
          </form>
        </CardContent>
      </Card>

      {/* Category Modal (Create / Edit) */}
      <Dialog open={categoryModalOpen} onOpenChange={setCategoryModalOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{editingCategory ? t("policyPanel.editCategory") : t("policyPanel.newCategory")}</DialogTitle>
            <DialogDescription>{t("policyPanel.categoryModalDescription")}</DialogDescription>
          </DialogHeader>
          <form
            onSubmit={categoryForm.handleSubmit((v) => saveCategoryMutation.mutate(v))}
            className="space-y-4 py-2"
          >
            <div className="space-y-2">
              <label htmlFor="modal-cat-name" className="text-sm font-medium">
                {t("policyPanel.colCategoryName")}
              </label>
              <Input
                id="modal-cat-name"
                placeholder={t("policyPanel.categoryNamePlaceholder")}
                autoFocus
                {...categoryForm.register("name")}
              />
              {categoryForm.formState.errors.name && (
                <p className="text-xs text-destructive">{categoryForm.formState.errors.name.message}</p>
              )}
            </div>

            <div className="space-y-2">
              <label htmlFor="modal-cat-days" className="text-sm font-medium">
                {t("policyPanel.defaultLoanPeriodDaysLabel")}
              </label>
              <Input
                id="modal-cat-days"
                type="number"
                min={0}
                placeholder={t("policyPanel.loanPeriodPlaceholder")}
                {...categoryForm.register("defaultLoanPeriodDays", { valueAsNumber: true })}
              />
              <p className="text-xs text-muted-foreground">{t("policyPanel.loanPeriodHint")}</p>
            </div>

            <div className="flex items-start space-x-3 rounded-lg border p-3">
              <Controller
                name="requiresApproval"
                control={categoryForm.control}
                render={({ field }) => (
                  <Checkbox
                    id="modal-cat-approval"
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                )}
              />
              <div className="space-y-0.5 leading-none">
                <label
                  htmlFor="modal-cat-approval"
                  className="text-sm font-medium cursor-pointer"
                >
                  {t("policyPanel.requiresApprovalBadge")}
                </label>
                <p className="text-xs text-muted-foreground">{t("policyPanel.requiresApprovalHint")}</p>
              </div>
            </div>

            <DialogFooter className="pt-4">
              <Button
                type="button"
                variant="outline"
                onClick={() => setCategoryModalOpen(false)}
              >
                {t("common.cancel")}
              </Button>
              <Button type="submit" disabled={saveCategoryMutation.isPending}>
                {saveCategoryMutation.isPending
                  ? t("policyPanel.savingPolicy")
                  : editingCategory
                  ? t("policyPanel.saveChanges")
                  : t("policyPanel.createCategory")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
