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


const policyFormSchema = z.object({
  blockOnOverdue: z.boolean(),
  sessionIdleTimeoutSeconds: z.number().int().min(5, "validation.sessionTimeoutMin").max(600, "validation.sessionTimeoutMax"),
  kioskSoundEnabled: z.boolean(),
  lowStockThreshold: z.number().int().min(0, "validation.thresholdMin").max(10000, "validation.thresholdMax"),
  paperBacklogHours: z.number().int().min(1, "validation.backlogThresholdMin").max(720, "validation.backlogThresholdMax"),
});

type PolicyFormValues = z.infer<typeof policyFormSchema>;

const categoryFormSchema = z.object({
  name: z.string().min(1, "validation.nameRequired"),
  defaultLoanPeriodDays: z.number().int().min(0).optional(),
  requiresApproval: z.boolean(),
});

type CategoryFormValues = z.infer<typeof categoryFormSchema>;

export function PolicyPanel() {
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
      toast.success("Policy settings updated successfully");
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || "Failed to update policy settings");
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
      toast.success(editingCategory ? "Category updated" : "Category created");
      setCategoryModalOpen(false);
    },
    onError: (err: any) => {
      toast.error(err?.detail || "Failed to save category");
    },
  });

  if (isSettingsLoading || isCategoriesLoading) {
    return <LoadingState message="Loading system settings and categories..." />;
  }

  if (settingsError) {
    return (
      <div className="flex flex-col items-center justify-center p-8 text-center text-destructive">
        <AlertTriangle className="size-8 mb-2" />
        <p className="font-semibold">Failed to load system settings</p>
        <p className="text-xs text-muted-foreground mt-1">Please try refreshing the page.</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Category Management Card */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
          <div>
            <CardTitle>Device Categories</CardTitle>
            <CardDescription>
              Categories define default loan duration and borrow approval policies for devices.
            </CardDescription>
          </div>
          {isAdmin && (
            <Button size="sm" onClick={openNewCategoryModal} className="gap-1.5">
              <Plus className="size-4" />
              New Category
            </Button>
          )}
        </CardHeader>
        <CardContent>
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Category Name</TableHead>
                  <TableHead>Default Loan Period</TableHead>
                  <TableHead>Approval Policy</TableHead>
                  {isAdmin && <TableHead className="w-16 text-right">Action</TableHead>}
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
                            {Math.round(cat.defaultLoanPeriodSeconds / 86400)} days
                          </span>
                        ) : (
                          <span className="text-xs text-muted-foreground italic">Indefinite (No limit)</span>
                        )}
                      </TableCell>
                      <TableCell>
                        {cat.requiresApproval ? (
                          <Badge variant="outline" className="text-amber-600 border-amber-300 bg-amber-50 dark:bg-amber-950/30">
                            Requires Approval
                          </Badge>
                        ) : (
                          <Badge variant="secondary" className="text-muted-foreground">
                            Standard
                          </Badge>
                        )}
                      </TableCell>
                      {isAdmin && (
                        <TableCell className="text-right">
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            onClick={() => openEditCategoryModal(cat)}
                            title={`Edit ${cat.name}`}
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
                      No categories configured. Click "New Category" to add one.
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
          <CardTitle>System & Checkout Policy</CardTitle>
          <CardDescription>
            Configure operational guardrails, kiosk session timeouts, and dashboard alert thresholds.
          </CardDescription>
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
                    Strict Overdue Enforcement
                  </label>
                  <p className="text-xs text-muted-foreground">
                    Block new borrows at kiosk terminals if the borrower currently holds any overdue equipment.
                  </p>
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
                    Kiosk Sound Feedback
                  </label>
                  <p className="text-xs text-muted-foreground">
                    Play auditory chimes and warning sounds on scan success and error events at kiosk tablets.
                  </p>
                </div>
              </div>
            </div>

            <div className="grid gap-6 md:grid-cols-3">
              {/* Session Timeout */}
              <div className="space-y-2">
                <label htmlFor="sessionIdleTimeoutSeconds" className="text-sm font-medium">
                  Kiosk Session Timeout (Seconds)
                </label>
                <Input
                  id="sessionIdleTimeoutSeconds"
                  type="number"
                  min={5}
                  max={600}
                  disabled={!isAdmin}
                  {...register("sessionIdleTimeoutSeconds", { valueAsNumber: true })}
                />
                <p className="text-xs text-muted-foreground">
                  Inactivity seconds before an active kiosk scan session resets to idle.
                </p>
                {errors.sessionIdleTimeoutSeconds && (
                  <p className="text-xs text-destructive">{errors.sessionIdleTimeoutSeconds.message}</p>
                )}
              </div>

              {/* Blank Stock Threshold */}
              <div className="space-y-2">
                <label htmlFor="lowStockThreshold" className="text-sm font-medium">
                  Low Stock Threshold (Cards)
                </label>
                <Input
                  id="lowStockThreshold"
                  type="number"
                  min={0}
                  max={10000}
                  disabled={!isAdmin}
                  {...register("lowStockThreshold", { valueAsNumber: true })}
                />
                <p className="text-xs text-muted-foreground">
                  Dashboard alerts when unassigned blank credential inventory drops below this number.
                </p>
                {errors.lowStockThreshold && (
                  <p className="text-xs text-destructive">{errors.lowStockThreshold.message}</p>
                )}
              </div>

              {/* Paper Backlog Hours */}
              <div className="space-y-2">
                <label htmlFor="paperBacklogHours" className="text-sm font-medium">
                  Paper Ledger Warning (Hours)
                </label>
                <Input
                  id="paperBacklogHours"
                  type="number"
                  min={1}
                  max={720}
                  disabled={!isAdmin}
                  {...register("paperBacklogHours", { valueAsNumber: true })}
                />
                <p className="text-xs text-muted-foreground">
                  Dashboard alerts if no paper ledger entries have been digitized in this timeframe.
                </p>
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
                      Last modified: {new Date(settingsData.updatedAt).toLocaleString()}
                      {settingsData.updatedBy && ` by ${settingsData.updatedBy}`}
                    </span>
                  )}
                </div>
                <Button
                  type="submit"
                  disabled={updatePolicyMutation.isPending || !isDirty}
                  className="gap-2"
                >
                  <Save className="size-4" />
                  {updatePolicyMutation.isPending ? "Saving..." : "Save Policy Settings"}
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
            <DialogTitle>{editingCategory ? "Edit Category" : "New Category"}</DialogTitle>
            <DialogDescription>
              Specify the default borrowing duration and approval policy for this equipment category.
            </DialogDescription>
          </DialogHeader>
          <form
            onSubmit={categoryForm.handleSubmit((v) => saveCategoryMutation.mutate(v))}
            className="space-y-4 py-2"
          >
            <div className="space-y-2">
              <label htmlFor="modal-cat-name" className="text-sm font-medium">
                Category Name
              </label>
              <Input
                id="modal-cat-name"
                placeholder="e.g. Infusion Pump, Ultrasound Probe"
                autoFocus
                {...categoryForm.register("name")}
              />
              {categoryForm.formState.errors.name && (
                <p className="text-xs text-destructive">{categoryForm.formState.errors.name.message}</p>
              )}
            </div>

            <div className="space-y-2">
              <label htmlFor="modal-cat-days" className="text-sm font-medium">
                Default Loan Period (Days)
              </label>
              <Input
                id="modal-cat-days"
                type="number"
                min={0}
                placeholder="Leave blank or 0 for indefinite loan period"
                {...categoryForm.register("defaultLoanPeriodDays", { valueAsNumber: true })}
              />
              <p className="text-xs text-muted-foreground">
                Set to 0 or leave empty for items that don't have a mandatory due date.
              </p>
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
                  Requires Approval
                </label>
                <p className="text-xs text-muted-foreground">
                  High-value or specialty equipment requiring clinical supervisor sign-off before loan release.
                </p>
              </div>
            </div>

            <DialogFooter className="pt-4">
              <Button
                type="button"
                variant="outline"
                onClick={() => setCategoryModalOpen(false)}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={saveCategoryMutation.isPending}>
                {saveCategoryMutation.isPending
                  ? "Saving..."
                  : editingCategory
                  ? "Save Changes"
                  : "Create Category"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
