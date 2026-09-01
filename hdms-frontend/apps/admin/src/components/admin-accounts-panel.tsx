import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { z } from "zod";
import {
  createAdmin,
  forceAdminTotpReenrolment,
  listAdmins,
  resetAdminPassword,
  unlockAdmin,
  updateAdmin,
  type Admin,
  type AdminEnrolment,
  type AdminRole,
  type AdminStatus,
  type TotpEnrolment,
} from "@hdms/api-client";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { LoadingState, EmptyState } from "@/components/states";
import { RecoveryCodesDialog } from "@/components/recovery-codes-dialog";
import { Barcode } from "@/components/barcode";
import { useRole } from "@/lib/use-role";
import { regenerateRecoveryCodesAdmin } from "@/lib/auth";
import { useLocalizedResolver } from "@/lib/localized-resolver";
import { queryClient } from "@/lib/query-client";
import { toast } from "sonner";
import {
  Check,
  Copy,
  KeyRound,
  Lock,
  MoreHorizontal,
  QrCode,
  ShieldAlert,
  ShieldCheck,
  Unlock,
  UserCheck,
  UserPlus,
  Users,
} from "lucide-react";

export const adminsQueryKey = ["admins"] as const;

// -----------------------------------------------------------------------------
// Validation Schemas
// -----------------------------------------------------------------------------

const createAdminSchema = z.object({
  fullName: z.string().min(1, "validation.fullNameRequired"),
  email: z.string().min(1, "validation.emailRequired").email("validation.emailInvalid"),
  role: z.enum(["admin", "technician", "viewer"] as const),
  password: z.string().min(12, "validation.initialPasswordMin"),
});

type CreateAdminFormValues = z.infer<typeof createAdminSchema>;

const editAdminSchema = z.object({
  fullName: z.string().min(1, "validation.fullNameRequired"),
  role: z.enum(["admin", "technician", "viewer"] as const),
  status: z.enum(["active", "disabled"] as const),
});

type EditAdminFormValues = z.infer<typeof editAdminSchema>;

const resetPasswordSchema = z.object({
  password: z.string().min(12, "validation.newPasswordMin"),
  reason: z.string().min(3, "validation.reasonRequired"),
});

type ResetPasswordFormValues = z.infer<typeof resetPasswordSchema>;

const actionWithReasonSchema = z.object({
  reason: z.string().min(3, "validation.reasonRequired"),
});

type ActionWithReasonFormValues = z.infer<typeof actionWithReasonSchema>;

// -----------------------------------------------------------------------------
// Admin Accounts Panel Component
// -----------------------------------------------------------------------------

export function AdminAccountsPanel() {
  const { isAdmin, admin: currentAdmin } = useRole();

  const { data, isLoading, error } = useQuery({
    queryKey: adminsQueryKey,
    queryFn: async () => {
      const res = await listAdmins();
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
  });

  // Dialog states
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [editAdminTarget, setEditAdminTarget] = useState<Admin | null>(null);
  const [resetPasswordTarget, setResetPasswordTarget] = useState<Admin | null>(null);
  const [forceTotpTarget, setForceTotpTarget] = useState<Admin | null>(null);
  const [unlockTarget, setUnlockTarget] = useState<Admin | null>(null);

  // Post-action enrolment modal
  const [enrolmentResult, setEnrolmentResult] = useState<AdminEnrolment | null>(null);
  const [forcedTotpResult, setForcedTotpResult] = useState<{ admin: Admin; enrolment: TotpEnrolment } | null>(null);

  // Self-service recovery codes modal
  const [selfRecoveryCodes, setSelfRecoveryCodes] = useState<string[] | null>(null);

  const regenRecoveryMutation = useMutation({
    mutationFn: regenerateRecoveryCodesAdmin,
    onSuccess: (res) => {
      setSelfRecoveryCodes(res?.codes ?? []);
    },
    onError: () => {
      toast.error("Failed to regenerate recovery codes.");
    },
  });

  if (isLoading) {
    return <LoadingState message="Loading administrator accounts…" />;
  }

  if (error) {
    return (
      <EmptyState
        icon={ShieldAlert}
        title="Failed to load admin accounts"
        explanation={
          error && typeof error === "object" && "detail" in error
            ? String((error as { detail: string }).detail)
            : "An unexpected error occurred."
        }
      />
    );
  }

  const admins = data ?? [];

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-lg font-semibold tracking-tight text-foreground">
            Administrator accounts
          </h2>
          <p className="text-sm text-muted-foreground">
            Manage console operators, assign role-based access, and oversee credentials.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => regenRecoveryMutation.mutate()}
            disabled={regenRecoveryMutation.isPending}
            className="gap-1.5"
          >
            <KeyRound className="size-4" />
            My recovery codes
          </Button>
          {isAdmin && (
            <Button
              type="button"
              size="sm"
              onClick={() => setCreateDialogOpen(true)}
              className="gap-1.5"
            >
              <UserPlus className="size-4" />
              Add administrator
            </Button>
          )}
        </div>
      </div>

      {admins.length === 0 ? (
        <EmptyState
          icon={Users}
          title="No administrator accounts found"
          explanation="No administrators exist yet in the directory."
        />
      ) : (
        <div className="rounded-lg border border-border bg-card">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Administrator</TableHead>
                <TableHead>Role</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Last login</TableHead>
                {isAdmin && <TableHead className="w-16 text-right">Actions</TableHead>}
              </TableRow>
            </TableHeader>
            <TableBody>
              {admins.map((account) => {
                const isLocked =
                  account.status === "locked" ||
                  (account.lockedUntil && new Date(account.lockedUntil) > new Date());
                const isCurrentAccount = account.id === currentAdmin?.id;

                return (
                  <TableRow key={account.id}>
                    <TableCell>
                      <div className="font-medium text-foreground">
                        {account.fullName}
                        {isCurrentAccount && (
                          <span className="ml-2 rounded-full bg-primary/10 px-2 py-0.5 text-[10px] font-semibold text-primary">
                            You
                          </span>
                        )}
                      </div>
                      <div className="font-mono text-xs text-muted-foreground">
                        {account.email}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={
                          account.role === "admin" || account.role === "superadmin"
                            ? "default"
                            : account.role === "technician" || account.role === "operator"
                            ? "secondary"
                            : "outline"
                        }
                        className="capitalize"
                      >
                        {account.role}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      {isLocked ? (
                        <Badge variant="destructive" className="gap-1">
                          <Lock className="size-3" />
                          Locked
                        </Badge>
                      ) : account.status === "disabled" ? (
                        <Badge variant="secondary" className="gap-1 text-muted-foreground">
                          Disabled
                        </Badge>
                      ) : (
                        <Badge variant="outline" className="gap-1 border-green-500/30 text-green-700 dark:text-green-400 bg-green-500/10">
                          <Check className="size-3" />
                          Active
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {account.lastLoginAt
                        ? new Date(account.lastLoginAt).toLocaleString()
                        : "Never"}
                    </TableCell>
                    {isAdmin && (
                      <TableCell className="text-right">
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button
                              variant="ghost"
                              size="sm"
                              className="size-8 p-0"
                              aria-label={`Actions for ${account.fullName}`}
                            >
                              <MoreHorizontal className="size-4" />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuItem onClick={() => setEditAdminTarget(account)}>
                              <UserCheck className="size-4 mr-2" />
                              Edit role & status
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => setResetPasswordTarget(account)}>
                              <KeyRound className="size-4 mr-2" />
                              Reset password
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => setForceTotpTarget(account)}>
                              <QrCode className="size-4 mr-2" />
                              Force TOTP re-enrolment
                            </DropdownMenuItem>
                            {isLocked && (
                              <>
                                <DropdownMenuSeparator />
                                <DropdownMenuItem
                                  onClick={() => setUnlockTarget(account)}
                                  className="text-amber-600 dark:text-amber-400"
                                >
                                  <Unlock className="size-4 mr-2" />
                                  Unlock account
                                </DropdownMenuItem>
                              </>
                            )}
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </TableCell>
                    )}
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      )}

      {/* Dialogs */}
      {isAdmin && (
        <>
          <CreateAdminDialog
            open={createDialogOpen}
            onOpenChange={setCreateDialogOpen}
            onSuccess={(enrolment) => {
              setCreateDialogOpen(false);
              setEnrolmentResult(enrolment);
              queryClient.invalidateQueries({ queryKey: adminsQueryKey });
            }}
          />

          {editAdminTarget && (
            <EditAdminDialog
              open={Boolean(editAdminTarget)}
              admin={editAdminTarget}
              onOpenChange={(open) => !open && setEditAdminTarget(null)}
              onSuccess={() => {
                setEditAdminTarget(null);
                queryClient.invalidateQueries({ queryKey: adminsQueryKey });
              }}
            />
          )}

          {resetPasswordTarget && (
            <ResetPasswordDialog
              open={Boolean(resetPasswordTarget)}
              admin={resetPasswordTarget}
              onOpenChange={(open) => !open && setResetPasswordTarget(null)}
              onSuccess={() => {
                setResetPasswordTarget(null);
                queryClient.invalidateQueries({ queryKey: adminsQueryKey });
              }}
            />
          )}

          {forceTotpTarget && (
            <ForceTotpDialog
              open={Boolean(forceTotpTarget)}
              admin={forceTotpTarget}
              onOpenChange={(open) => !open && setForceTotpTarget(null)}
              onSuccess={(enrolment) => {
                const target = forceTotpTarget;
                setForceTotpTarget(null);
                setForcedTotpResult({ admin: target, enrolment });
                queryClient.invalidateQueries({ queryKey: adminsQueryKey });
              }}
            />
          )}

          {unlockTarget && (
            <UnlockAdminDialog
              open={Boolean(unlockTarget)}
              admin={unlockTarget}
              onOpenChange={(open) => !open && setUnlockTarget(null)}
              onSuccess={() => {
                setUnlockTarget(null);
                queryClient.invalidateQueries({ queryKey: adminsQueryKey });
              }}
            />
          )}
        </>
      )}

      {/* Post-creation Enrolment Modal with Recovery Codes */}
      {enrolmentResult && (
        <EnrolmentResultDialog
          open={Boolean(enrolmentResult)}
          enrolment={enrolmentResult}
          onDismiss={() => setEnrolmentResult(null)}
        />
      )}

      {/* Forced TOTP Result Modal */}
      {forcedTotpResult && (
        <TotpSetupResultDialog
          open={Boolean(forcedTotpResult)}
          adminName={forcedTotpResult.admin.fullName}
          enrolment={forcedTotpResult.enrolment}
          onDismiss={() => setForcedTotpResult(null)}
        />
      )}

      {/* Self-service recovery codes dialog */}
      {selfRecoveryCodes && (
        <RecoveryCodesDialog
          open={Boolean(selfRecoveryCodes)}
          codes={selfRecoveryCodes}
          title="Your New Recovery Codes"
          description="Your previous recovery codes have been invalidated. Save these newly generated codes."
          onDismiss={() => setSelfRecoveryCodes(null)}
        />
      )}
    </div>
  );
}

// -----------------------------------------------------------------------------
// Create Admin Dialog
// -----------------------------------------------------------------------------

function CreateAdminDialog({
  open,
  onOpenChange,
  onSuccess,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSuccess: (enrolment: AdminEnrolment) => void;
}) {
  const form = useForm<CreateAdminFormValues>({
    resolver: useLocalizedResolver(createAdminSchema),
    defaultValues: {
      fullName: "",
      email: "",
      role: "technician",
      password: "",
    },
  });

  const mutation = useMutation({
    mutationFn: async (values: CreateAdminFormValues) => {
      const res = await createAdmin({ body: values });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (data) => {
      if (data) {
        form.reset();
        toast.success("Administrator account created.");
        onSuccess(data);
      }
    },
    onError: (error: unknown) => {
      let message = "Failed to create administrator.";
      if (error && typeof error === "object") {
        if ("detail" in error && typeof (error as { detail: string }).detail === "string") {
          message = (error as { detail: string }).detail;
        } else if ("title" in error && typeof (error as { title: string }).title === "string") {
          message = (error as { title: string }).title;
        }
      }
      form.setError("root", { message });
    },
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Add administrator</DialogTitle>
          <DialogDescription>
            Create a new console account. You will receive one-time setup credentials and recovery codes upon creation.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit((values) => mutation.mutate(values))}>
          <FieldGroup className="mt-3">
            <Field data-invalid={!!form.formState.errors.fullName}>
              <FieldLabel htmlFor="create-admin-name">Full name</FieldLabel>
              <Input
                id="create-admin-name"
                autoFocus
                aria-invalid={!!form.formState.errors.fullName}
                {...form.register("fullName")}
              />
              {form.formState.errors.fullName && (
                <FieldError>{form.formState.errors.fullName.message}</FieldError>
              )}
            </Field>

            <Field data-invalid={!!form.formState.errors.email}>
              <FieldLabel htmlFor="create-admin-email">Email</FieldLabel>
              <Input
                id="create-admin-email"
                type="email"
                aria-invalid={!!form.formState.errors.email}
                {...form.register("email")}
              />
              {form.formState.errors.email && (
                <FieldError>{form.formState.errors.email.message}</FieldError>
              )}
            </Field>

            <Field data-invalid={!!form.formState.errors.role}>
              <FieldLabel htmlFor="create-admin-role">Role</FieldLabel>
              <Select
                value={form.watch("role")}
                onValueChange={(val) =>
                  form.setValue("role", val as "admin" | "technician" | "viewer")
                }
              >
                <SelectTrigger id="create-admin-role">
                  <SelectValue placeholder="Select role" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="admin">Admin (Full Access & User Management)</SelectItem>
                  <SelectItem value="technician">Technician (Inventory & Label Operations)</SelectItem>
                  <SelectItem value="viewer">Viewer (Read-only)</SelectItem>
                </SelectContent>
              </Select>
              {form.formState.errors.role && (
                <FieldError>{form.formState.errors.role.message}</FieldError>
              )}
            </Field>

            <Field data-invalid={!!form.formState.errors.password}>
              <FieldLabel htmlFor="create-admin-password">Initial password</FieldLabel>
              <Input
                id="create-admin-password"
                type="password"
                placeholder="At least 12 characters"
                aria-invalid={!!form.formState.errors.password}
                {...form.register("password")}
              />
              {form.formState.errors.password && (
                <FieldError>{form.formState.errors.password.message}</FieldError>
              )}
            </Field>

            {form.formState.errors.root && (
              <p className="text-sm text-destructive" role="alert">
                {form.formState.errors.root.message}
              </p>
            )}

            <DialogFooter className="mt-4">
              <Button
                type="button"
                variant="outline"
                onClick={() => onOpenChange(false)}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={mutation.isPending}>
                {mutation.isPending ? "Creating…" : "Create account"}
              </Button>
            </DialogFooter>
          </FieldGroup>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// -----------------------------------------------------------------------------
// Edit Admin Dialog
// -----------------------------------------------------------------------------

function EditAdminDialog({
  open,
  admin,
  onOpenChange,
  onSuccess,
}: {
  open: boolean;
  admin: Admin;
  onOpenChange: (open: boolean) => void;
  onSuccess: () => void;
}) {
  const form = useForm<EditAdminFormValues>({
    resolver: useLocalizedResolver(editAdminSchema),
    defaultValues: {
      fullName: admin.fullName,
      role: (admin.role === "superadmin" ? "admin" : admin.role === "operator" ? "technician" : admin.role) as "admin" | "technician" | "viewer",
      status: (admin.status === "disabled" ? "disabled" : "active") as "active" | "disabled",
    },
  });

  const mutation = useMutation({
    mutationFn: async (values: EditAdminFormValues) => {
      const res = await updateAdmin({
        path: { id: admin.id },
        body: {
          fullName: values.fullName,
          role: values.role as AdminRole,
          status: values.status as AdminStatus,
        },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: () => {
      toast.success("Administrator account updated.");
      onSuccess();
    },
    onError: (error: unknown) => {
      let message = "Failed to update administrator.";
      if (error && typeof error === "object") {
        if ("detail" in error && typeof (error as { detail: string }).detail === "string") {
          message = (error as { detail: string }).detail;
        } else if ("title" in error && typeof (error as { title: string }).title === "string") {
          message = (error as { title: string }).title;
        }
      }
      form.setError("root", { message });
    },
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Edit administrator</DialogTitle>
          <DialogDescription>
            Update profile details, role, and account status for {admin.email}.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit((values) => mutation.mutate(values))}>
          <FieldGroup className="mt-3">
            <Field data-invalid={!!form.formState.errors.fullName}>
              <FieldLabel htmlFor="edit-admin-name">Full name</FieldLabel>
              <Input
                id="edit-admin-name"
                aria-invalid={!!form.formState.errors.fullName}
                {...form.register("fullName")}
              />
              {form.formState.errors.fullName && (
                <FieldError>{form.formState.errors.fullName.message}</FieldError>
              )}
            </Field>

            <Field data-invalid={!!form.formState.errors.role}>
              <FieldLabel htmlFor="edit-admin-role">Role</FieldLabel>
              <Select
                value={form.watch("role")}
                onValueChange={(val) =>
                  form.setValue("role", val as "admin" | "technician" | "viewer")
                }
              >
                <SelectTrigger id="edit-admin-role">
                  <SelectValue placeholder="Select role" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="admin">Admin</SelectItem>
                  <SelectItem value="technician">Technician</SelectItem>
                  <SelectItem value="viewer">Viewer</SelectItem>
                </SelectContent>
              </Select>
              {form.formState.errors.role && (
                <FieldError>{form.formState.errors.role.message}</FieldError>
              )}
            </Field>

            <Field data-invalid={!!form.formState.errors.status}>
              <FieldLabel htmlFor="edit-admin-status">Account status</FieldLabel>
              <Select
                value={form.watch("status")}
                onValueChange={(val) =>
                  form.setValue("status", val as "active" | "disabled")
                }
              >
                <SelectTrigger id="edit-admin-status">
                  <SelectValue placeholder="Select status" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="active">Active</SelectItem>
                  <SelectItem value="disabled">Disabled</SelectItem>
                </SelectContent>
              </Select>
              {form.formState.errors.status && (
                <FieldError>{form.formState.errors.status.message}</FieldError>
              )}
            </Field>

            {form.formState.errors.root && (
              <p className="text-sm text-destructive" role="alert">
                {form.formState.errors.root.message}
              </p>
            )}

            <DialogFooter className="mt-4">
              <Button
                type="button"
                variant="outline"
                onClick={() => onOpenChange(false)}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={mutation.isPending}>
                {mutation.isPending ? "Saving…" : "Save changes"}
              </Button>
            </DialogFooter>
          </FieldGroup>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// -----------------------------------------------------------------------------
// Reset Password Dialog
// -----------------------------------------------------------------------------

function ResetPasswordDialog({
  open,
  admin,
  onOpenChange,
  onSuccess,
}: {
  open: boolean;
  admin: Admin;
  onOpenChange: (open: boolean) => void;
  onSuccess: () => void;
}) {
  const form = useForm<ResetPasswordFormValues>({
    resolver: useLocalizedResolver(resetPasswordSchema),
    defaultValues: { password: "", reason: "" },
  });

  const mutation = useMutation({
    mutationFn: async (values: ResetPasswordFormValues) => {
      const res = await resetAdminPassword({
        path: { id: admin.id },
        body: {
          password: values.password,
          reason: values.reason,
        },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: () => {
      toast.success(`Password reset for ${admin.fullName}.`);
      onSuccess();
    },
    onError: (error: unknown) => {
      let message = "Failed to reset password.";
      if (error && typeof error === "object") {
        if ("detail" in error && typeof (error as { detail: string }).detail === "string") {
          message = (error as { detail: string }).detail;
        } else if ("title" in error && typeof (error as { title: string }).title === "string") {
          message = (error as { title: string }).title;
        }
      }
      form.setError("root", { message });
    },
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Reset password</DialogTitle>
          <DialogDescription>
            Set a new temporary password for {admin.fullName} ({admin.email}).
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit((values) => mutation.mutate(values))}>
          <FieldGroup className="mt-3">
            <Field data-invalid={!!form.formState.errors.password}>
              <FieldLabel htmlFor="reset-admin-password">New password</FieldLabel>
              <Input
                id="reset-admin-password"
                type="password"
                autoFocus
                placeholder="At least 12 characters"
                aria-invalid={!!form.formState.errors.password}
                {...form.register("password")}
              />
              {form.formState.errors.password && (
                <FieldError>{form.formState.errors.password.message}</FieldError>
              )}
            </Field>

            <Field data-invalid={!!form.formState.errors.reason}>
              <FieldLabel htmlFor="reset-admin-reason">Audit reason</FieldLabel>
              <Input
                id="reset-admin-reason"
                placeholder="e.g. Account lockout recovery, forgotten password"
                aria-invalid={!!form.formState.errors.reason}
                {...form.register("reason")}
              />
              {form.formState.errors.reason && (
                <FieldError>{form.formState.errors.reason.message}</FieldError>
              )}
            </Field>

            {form.formState.errors.root && (
              <p className="text-sm text-destructive" role="alert">
                {form.formState.errors.root.message}
              </p>
            )}

            <DialogFooter className="mt-4">
              <Button
                type="button"
                variant="outline"
                onClick={() => onOpenChange(false)}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={mutation.isPending}>
                {mutation.isPending ? "Resetting…" : "Reset password"}
              </Button>
            </DialogFooter>
          </FieldGroup>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// -----------------------------------------------------------------------------
// Force TOTP Re-enrolment Dialog
// -----------------------------------------------------------------------------

function ForceTotpDialog({
  open,
  admin,
  onOpenChange,
  onSuccess,
}: {
  open: boolean;
  admin: Admin;
  onOpenChange: (open: boolean) => void;
  onSuccess: (enrolment: TotpEnrolment) => void;
}) {
  const form = useForm<ActionWithReasonFormValues>({
    resolver: useLocalizedResolver(actionWithReasonSchema),
    defaultValues: { reason: "" },
  });

  const mutation = useMutation({
    mutationFn: async (values: ActionWithReasonFormValues) => {
      const res = await forceAdminTotpReenrolment({
        path: { id: admin.id },
        body: { reason: values.reason },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (data) => {
      if (data) {
        toast.success(`TOTP re-enrolment triggered for ${admin.fullName}.`);
        onSuccess(data);
      }
    },
    onError: (error: unknown) => {
      let message = "Failed to trigger TOTP re-enrolment.";
      if (error && typeof error === "object") {
        if ("detail" in error && typeof (error as { detail: string }).detail === "string") {
          message = (error as { detail: string }).detail;
        } else if ("title" in error && typeof (error as { title: string }).title === "string") {
          message = (error as { title: string }).title;
        }
      }
      form.setError("root", { message });
    },
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Force TOTP re-enrolment</DialogTitle>
          <DialogDescription>
            Invalidates {admin.fullName}&apos;s existing authenticator secret. A new secret and QR code will be generated immediately.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit((values) => mutation.mutate(values))}>
          <FieldGroup className="mt-3">
            <Field data-invalid={!!form.formState.errors.reason}>
              <FieldLabel htmlFor="force-totp-reason">Audit reason</FieldLabel>
              <Input
                id="force-totp-reason"
                autoFocus
                placeholder="e.g. Lost device, security review"
                aria-invalid={!!form.formState.errors.reason}
                {...form.register("reason")}
              />
              {form.formState.errors.reason && (
                <FieldError>{form.formState.errors.reason.message}</FieldError>
              )}
            </Field>

            {form.formState.errors.root && (
              <p className="text-sm text-destructive" role="alert">
                {form.formState.errors.root.message}
              </p>
            )}

            <DialogFooter className="mt-4">
              <Button
                type="button"
                variant="outline"
                onClick={() => onOpenChange(false)}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={mutation.isPending}>
                {mutation.isPending ? "Generating…" : "Force re-enrolment"}
              </Button>
            </DialogFooter>
          </FieldGroup>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// -----------------------------------------------------------------------------
// Unlock Admin Dialog
// -----------------------------------------------------------------------------

function UnlockAdminDialog({
  open,
  admin,
  onOpenChange,
  onSuccess,
}: {
  open: boolean;
  admin: Admin;
  onOpenChange: (open: boolean) => void;
  onSuccess: () => void;
}) {
  const form = useForm<ActionWithReasonFormValues>({
    resolver: useLocalizedResolver(actionWithReasonSchema),
    defaultValues: { reason: "" },
  });

  const mutation = useMutation({
    mutationFn: async (values: ActionWithReasonFormValues) => {
      const res = await unlockAdmin({
        path: { id: admin.id },
        body: { reason: values.reason },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: () => {
      toast.success(`Account unlocked for ${admin.fullName}.`);
      onSuccess();
    },
    onError: (error: unknown) => {
      let message = "Failed to unlock account.";
      if (error && typeof error === "object") {
        if ("detail" in error && typeof (error as { detail: string }).detail === "string") {
          message = (error as { detail: string }).detail;
        } else if ("title" in error && typeof (error as { title: string }).title === "string") {
          message = (error as { title: string }).title;
        }
      }
      form.setError("root", { message });
    },
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Unlock administrator account</DialogTitle>
          <DialogDescription>
            Clear the temporary lockout for {admin.fullName} ({admin.email}) and allow immediate login attempts.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={form.handleSubmit((values) => mutation.mutate(values))}>
          <FieldGroup className="mt-3">
            <Field data-invalid={!!form.formState.errors.reason}>
              <FieldLabel htmlFor="unlock-admin-reason">Audit reason</FieldLabel>
              <Input
                id="unlock-admin-reason"
                autoFocus
                placeholder="e.g. Identity verified by supervisor"
                aria-invalid={!!form.formState.errors.reason}
                {...form.register("reason")}
              />
              {form.formState.errors.reason && (
                <FieldError>{form.formState.errors.reason.message}</FieldError>
              )}
            </Field>

            {form.formState.errors.root && (
              <p className="text-sm text-destructive" role="alert">
                {form.formState.errors.root.message}
              </p>
            )}

            <DialogFooter className="mt-4">
              <Button
                type="button"
                variant="outline"
                onClick={() => onOpenChange(false)}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={mutation.isPending}>
                {mutation.isPending ? "Unlocking…" : "Unlock account"}
              </Button>
            </DialogFooter>
          </FieldGroup>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// -----------------------------------------------------------------------------
// Enrolment Result Dialog (TOTP QR + Recovery Codes)
// -----------------------------------------------------------------------------

function EnrolmentResultDialog({
  open,
  enrolment,
  onDismiss,
}: {
  open: boolean;
  enrolment: AdminEnrolment;
  onDismiss: () => void;
}) {
  const [confirmed, setConfirmed] = useState(false);
  const [copiedSecret, setCopiedSecret] = useState(false);
  const [copiedCodes, setCopiedCodes] = useState(false);

  const handleCopySecret = async () => {
    try {
      await navigator.clipboard.writeText(enrolment.enrolment.totpSecret);
      setCopiedSecret(true);
      toast.success("TOTP secret copied");
      setTimeout(() => setCopiedSecret(false), 3000);
    } catch {
      toast.error("Failed to copy secret");
    }
  };

  const handleCopyCodes = async () => {
    try {
      const text = [
        `Hito Hospital — Recovery Codes for ${enrolment.admin.fullName}`,
        `Email: ${enrolment.admin.email}`,
        `Generated: ${new Date().toISOString()}`,
        "",
        ...enrolment.recoveryCodes.codes.map((c, i) => `${i + 1}. ${c}`),
      ].join("\n");
      await navigator.clipboard.writeText(text);
      setCopiedCodes(true);
      toast.success("Recovery codes copied");
      setTimeout(() => setCopiedCodes(false), 3000);
    } catch {
      toast.error("Failed to copy recovery codes");
    }
  };

  const handleClose = () => {
    if (!confirmed) {
      toast.error("Please confirm credentials have been saved before closing.");
      return;
    }
    setConfirmed(false);
    onDismiss();
  };

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => {
      if (!nextOpen && confirmed) {
        handleClose();
      }
    }}>
      <DialogContent
        className="sm:max-w-xl max-h-[90vh] overflow-y-auto"
        onEscapeKeyDown={(e) => !confirmed && e.preventDefault()}
        onPointerDownOutside={(e) => !confirmed && e.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle>Administrator credentials created</DialogTitle>
          <DialogDescription>
            Hand off the 2FA setup and recovery codes to {enrolment.admin.fullName} ({enrolment.admin.email}).
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="rounded-lg border border-border bg-card p-4 space-y-3">
            <h3 className="text-sm font-semibold text-foreground flex items-center gap-2">
              <QrCode className="size-4 text-primary" />
              1. Two-Factor Authenticator Setup
            </h3>
            <div className="flex flex-col sm:flex-row items-center gap-4">
              <div className="size-36 rounded-md border border-border bg-white p-2 flex items-center justify-center">
                <Barcode
                  value={enrolment.enrolment.otpauthUrl}
                  symbology="qrcode"
                  scale={3}
                  className="max-h-full max-w-full"
                />
              </div>
              <div className="flex-1 space-y-2 text-xs">
                <p className="text-muted-foreground">
                  Scan this QR code in Google Authenticator or 1Password.
                </p>
                <div className="rounded bg-muted p-2 font-mono text-[11px] break-all">
                  {enrolment.enrolment.totpSecret}
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={handleCopySecret}
                  className="gap-1.5 h-7 text-xs"
                >
                  {copiedSecret ? <Check className="size-3 text-green-600" /> : <Copy className="size-3" />}
                  Copy secret
                </Button>
              </div>
            </div>
          </div>

          <div className="rounded-lg border border-border bg-card p-4 space-y-3">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-semibold text-foreground flex items-center gap-2">
                <ShieldCheck className="size-4 text-primary" />
                2. Single-Use Recovery Codes
              </h3>
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={handleCopyCodes}
                  className="gap-1.5 h-7 text-xs"
                >
                  {copiedCodes ? <Check className="size-3 text-green-600" /> : <Copy className="size-3" />}
                  Copy codes
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => window.print()}
                  className="gap-1.5 h-7 text-xs"
                >
                  Print
                </Button>
              </div>
            </div>
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 font-mono text-xs">
              {enrolment.recoveryCodes.codes.map((code, i) => (
                <div key={i} className="rounded border border-border bg-muted/50 p-1.5 text-center font-medium">
                  {code}
                </div>
              ))}
            </div>
          </div>

          <div className="flex items-start gap-2.5 rounded-md border border-border bg-card p-3">
            <input
              id="confirm-enrolment-saved"
              type="checkbox"
              checked={confirmed}
              onChange={(e) => setConfirmed(e.target.checked)}
              className="mt-0.5 size-4 rounded border-border text-primary cursor-pointer focus:ring-ring"
            />
            <label
              htmlFor="confirm-enrolment-saved"
              className="text-xs font-medium text-foreground cursor-pointer leading-snug"
            >
              I have provided or saved these credentials for the administrator. I understand this is the only time they will be visible.
            </label>
          </div>
        </div>

        <DialogFooter className="mt-3">
          <Button
            type="button"
            disabled={!confirmed}
            onClick={handleClose}
            className="w-full sm:w-auto"
          >
            Done & close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// -----------------------------------------------------------------------------
// TOTP Setup Result Dialog
// -----------------------------------------------------------------------------

function TotpSetupResultDialog({
  open,
  adminName,
  enrolment,
  onDismiss,
}: {
  open: boolean;
  adminName: string;
  enrolment: TotpEnrolment;
  onDismiss: () => void;
}) {
  const [copiedSecret, setCopiedSecret] = useState(false);

  const handleCopySecret = async () => {
    try {
      await navigator.clipboard.writeText(enrolment.totpSecret);
      setCopiedSecret(true);
      toast.success("Secret copied");
      setTimeout(() => setCopiedSecret(false), 3000);
    } catch {
      toast.error("Failed to copy secret");
    }
  };

  return (
    <Dialog open={open} onOpenChange={(open) => !open && onDismiss()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New TOTP secret for {adminName}</DialogTitle>
          <DialogDescription>
            Provide this QR code or secret key to {adminName} to complete their authenticator setup.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="flex flex-col items-center justify-center rounded-lg border border-border bg-white p-4">
            <div className="size-40 flex items-center justify-center">
              <Barcode
                value={enrolment.otpauthUrl}
                symbology="qrcode"
                scale={3}
                className="max-h-full max-w-full"
              />
            </div>
          </div>
          <div className="rounded-md border border-border bg-muted/40 p-3">
            <div className="flex items-center justify-between text-xs text-muted-foreground mb-1">
              <span>Secret key</span>
              <button
                type="button"
                onClick={handleCopySecret}
                className="flex items-center gap-1 font-medium text-foreground hover:underline cursor-pointer"
              >
                {copiedSecret ? <Check className="size-3 text-green-600" /> : <Copy className="size-3" />}
                Copy
              </button>
            </div>
            <p className="font-mono text-xs font-semibold tracking-wider text-foreground select-all break-all">
              {enrolment.totpSecret}
            </p>
          </div>
        </div>
        <DialogFooter className="mt-2">
          <Button type="button" onClick={onDismiss} className="w-full">
            Done
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
