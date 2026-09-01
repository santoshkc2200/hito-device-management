import {
  archiveUser,
  getUser,
  listCredentialsBySubject,
  listDepartments,
  listUserLoans,
  suspendUser,
} from "@hdms/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute, Link, useNavigate } from "@tanstack/react-router";
import { ArrowLeft, Edit3, History, Laptop, ShieldAlert, User as UserIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { CredentialsPanel } from "@/components/credentials-panel";
import { ErrorState, LoadingState } from "@/components/states";
import { StatusBadge, labelize, userStatusTone } from "@/components/status-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { UserForm } from "@/components/user-form";
import { RoleGate } from "@/lib/use-role";
import { useT } from "@/i18n";
import { authenticatedRoute } from "./authenticated";

function ReasonActionDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  confirmVariant = "destructive",
  onConfirm,
  isPending,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  confirmLabel: string;
  confirmVariant?: "default" | "destructive";
  onConfirm: (reason: string) => void;
  isPending: boolean;
}) {
  const t = useT();
  const [reason, setReason] = useState("");

  const handleClose = () => {
    setReason("");
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={(o) => (!o ? handleClose() : onOpenChange(true))}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <p className="text-sm text-muted-foreground">{description}</p>
        <div className="py-2">
          <textarea
            className="w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
            placeholder={t("userDetail.reasonPlaceholder")}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            rows={3}
            autoFocus
          />
        </div>
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={handleClose} disabled={isPending}>
            {t("userDetail.cancel")}
          </Button>
          <Button
            variant={confirmVariant}
            disabled={!reason.trim() || isPending}
            onClick={() => onConfirm(reason.trim())}
          >
            {isPending ? t("userDetail.processing") : confirmLabel}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

export function formatProvenance(
  t: ReturnType<typeof useT>,
  registeredBy?: string,
): { label: string; kind: "admin" | "import" | "other" } {
  if (!registeredBy) {
    return { label: t("userDetail.provenanceUnknown"), kind: "other" };
  }
  if (registeredBy.startsWith("admin:")) {
    const adminId = registeredBy.slice(6);
    return { label: t("userDetail.provenanceAdmin", { adminId }), kind: "admin" };
  }
  if (registeredBy.startsWith("import:")) {
    const batchId = registeredBy.slice(7);
    return { label: t("userDetail.provenanceImportBatch", { batchId }), kind: "import" };
  }
  if (registeredBy === "import") {
    return { label: t("userDetail.provenanceCsvImport"), kind: "import" };
  }
  return { label: registeredBy, kind: "other" };
}

function loanOriginTone(origin: string): "default" | "secondary" | "outline" {
  switch (origin) {
    case "kiosk":
      return "default";
    case "paper":
      return "secondary";
    default:
      return "outline";
  }
}

export function UserDetailPage() {
  const t = useT();
  const { userId } = userDetailRoute.useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [editOpen, setEditOpen] = useState(false);
  const [suspendOpen, setSuspendOpen] = useState(false);
  const [archiveOpen, setArchiveOpen] = useState(false);

  const {
    data: user,
    isLoading: isUserLoading,
    isError: isUserError,
    error: userError,
    refetch: refetchUser,
  } = useQuery({
    queryKey: ["users", userId],
    queryFn: async () => {
      const { data, error } = await getUser({ path: { id: userId } });
      if (error) throw error;
      return data;
    },
  });

  const { data: departments } = useQuery({
    queryKey: ["departments"],
    queryFn: async () => {
      const { data, error } = await listDepartments();
      if (error) throw error;
      return data.items;
    },
  });

  const departmentName = useMemo(() => {
    if (!user?.departmentId || !departments) return undefined;
    return departments.find((d) => d.id === user.departmentId)?.name;
  }, [user, departments]);

  const {
    data: loansData,
    isLoading: isLoansLoading,
  } = useQuery({
    queryKey: ["users", userId, "loans"],
    queryFn: async () => {
      const { data, error } = await listUserLoans({ path: { id: userId } });
      if (error) throw error;
      return data;
    },
    enabled: Boolean(userId),
  });

  const loans = useMemo(() => loansData?.items ?? [], [loansData]);
  const activeLoans = useMemo(() => loans.filter((l) => l.status === "open"), [loans]);

  // Query user credentials to know if "no card issued" badge should show in header
  const { data: credentialsData } = useQuery({
    queryKey: ["credentials", "user", userId],
    queryFn: async () => {
      const { data, error } = await listCredentialsBySubject({
        query: { subjectType: "user", subjectId: userId },
      });
      if (error) throw error;
      return data.items;
    },
    enabled: Boolean(userId),
  });

  const hasActiveCredential = useMemo(
    () => credentialsData?.some((c) => c.status === "active") ?? true,
    [credentialsData],
  );

  const suspendMutation = useMutation({
    mutationFn: async (reason: string) => {
      const res = await suspendUser({ path: { id: userId }, body: { reason } });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["users"] });
      toast.success(t("users.suspended"));
      setSuspendOpen(false);
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || t("users.suspendFailed"));
    },
  });

  const archiveMutation = useMutation({
    mutationFn: async (reason: string) => {
      const res = await archiveUser({ path: { id: userId }, body: { reason } });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["users"] });
      toast.success(t("users.archived"));
      setArchiveOpen(false);
      void navigate({ to: "/users" });
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || t("users.archiveFailed"));
    },
  });

  if (isUserLoading) {
    return <LoadingState message={t("userDetail.loadingUser")} />;
  }

  if (isUserError || !user) {
    return (
      <ErrorState
        error={userError}
        title={t("userDetail.notFoundTitle")}
        detail={t("userDetail.notFoundDetail")}
        onRetry={() => refetchUser()}
      />
    );
  }

  const provenance = formatProvenance(t, user.registeredBy);

  return (
    <div className="flex flex-col gap-6">
      {/* Navigation Breadcrumb */}
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Link to="/users" className="flex items-center gap-1.5 hover:text-foreground">
          <ArrowLeft className="size-4" />
          <span>{t("userDetail.breadcrumbUsers")}</span>
        </Link>
        <span>/</span>
        <span className="font-identifier text-foreground">{user.employeeNo}</span>
      </div>

      {/* Header Banner */}
      <div className="flex flex-col justify-between gap-4 rounded-lg border border-border bg-card p-6 sm:flex-row sm:items-center">
        <div className="flex items-start gap-4">
          <div className="rounded-full bg-primary/10 p-3 text-primary">
            <UserIcon className="size-6" />
          </div>
          <div>
            <div className="flex flex-wrap items-center gap-3">
              <h1 className="text-2xl font-bold tracking-tight text-foreground">{user.fullName}</h1>
              <span className="font-identifier text-lg text-muted-foreground">{user.employeeNo}</span>
              <StatusBadge label={labelize(user.status)} tone={userStatusTone[user.status] ?? "muted"} />
              {!hasActiveCredential && (
                <Badge
                  variant="outline"
                  className="border-amber-500/50 bg-amber-500/10 text-amber-600 dark:text-amber-400 font-normal text-xs"
                >
                  {t("userDetail.noCardIssued")}
                </Badge>
              )}
            </div>
            <p className="mt-1 text-sm text-muted-foreground">
              {departmentName ? t("userDetail.departmentPrefix", { department: departmentName }) : ""}
              {t("userDetail.registeredOn", {
                date: new Date(user.registeredAt).toLocaleDateString(),
              })}
            </p>
          </div>
        </div>

        {/* Action Buttons */}
        <div className="flex flex-wrap items-center gap-2">
          <RoleGate minRole="technician">
            <Button size="sm" variant="outline" onClick={() => setEditOpen(true)}>
              <Edit3 className="size-4" data-icon="inline-start" />
              {t("userDetail.edit")}
            </Button>
          </RoleGate>
          {user.status === "active" && (
            <RoleGate minRole="technician">
              <Button size="sm" variant="outline" onClick={() => setSuspendOpen(true)}>
                <ShieldAlert className="size-4" data-icon="inline-start" />
                {t("userDetail.suspend")}
              </Button>
            </RoleGate>
          )}
          {user.status !== "archived" && (
            <RoleGate minRole="admin">
              <Button
                size="sm"
                variant="outline"
                className="text-destructive hover:text-destructive"
                onClick={() => setArchiveOpen(true)}
              >
                {t("userDetail.archive")}
              </Button>
            </RoleGate>
          )}
        </div>
      </div>

      {/* Main Grid: Details + Held Devices / History on left, Credentials on right */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        {/* Left Column (2 spans): Profile, Held Devices, History */}
        <div className="flex flex-col gap-6 lg:col-span-2">
          {/* Profile Card */}
          <div className="rounded-lg border border-border bg-card p-6">
            <h2 className="text-base font-semibold text-foreground">{t("userDetail.profileHeading")}</h2>
            <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("userDetail.fullNameLabel")}</span>
                <p className="text-sm font-medium text-foreground mt-0.5">{user.fullName}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("userDetail.employeeNumberLabel")}</span>
                <p className="font-identifier text-sm text-foreground mt-0.5">{user.employeeNo}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("userDetail.departmentLabel")}</span>
                <p className="text-sm text-foreground mt-0.5">{departmentName || "—"}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("userDetail.emailLabel")}</span>
                <p className="text-sm text-foreground mt-0.5">{user.email || "—"}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("userDetail.phoneLabel")}</span>
                <p className="text-sm text-foreground mt-0.5">{user.phone || "—"}</p>
              </div>
              <div>
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("userDetail.statusLabel")}</span>
                <div className="mt-0.5">
                  <StatusBadge label={labelize(user.status)} tone={userStatusTone[user.status] ?? "muted"} />
                </div>
              </div>
            </div>
            {user.notes && (
              <div className="mt-4 border-t border-border pt-3">
                <span className="text-xs font-medium text-muted-foreground uppercase">{t("userDetail.notesLabel")}</span>
                <p className="text-sm text-foreground mt-0.5 whitespace-pre-wrap">{user.notes}</p>
              </div>
            )}

            {/* Provenance Section */}
            <div className="mt-6 border-t border-border pt-4">
              <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("userDetail.provenanceHeading")}</h3>
              <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 text-xs">
                <div>
                  <span className="text-muted-foreground">{t("userDetail.registeredAtLabel")}</span>
                  <p className="font-medium text-foreground mt-0.5">{new Date(user.registeredAt).toLocaleString()}</p>
                </div>
                <div>
                  <span className="text-muted-foreground">{t("userDetail.registeredByLabel")}</span>
                  <div className="flex items-center gap-1.5 mt-0.5">
                    <Badge variant={provenance.kind === "import" ? "secondary" : "outline"} className="text-xs">
                      {provenance.label}
                    </Badge>
                  </div>
                </div>
                <div>
                  <span className="text-muted-foreground">{t("userDetail.lastUpdatedLabel")}</span>
                  <p className="font-medium text-foreground mt-0.5">{new Date(user.updatedAt).toLocaleString()}</p>
                </div>
              </div>
            </div>
          </div>

          {/* Currently Held Devices */}
          <div className="rounded-lg border border-border bg-card p-6">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Laptop className="size-4 text-primary" />
                <h2 className="text-base font-semibold text-foreground">{t("userDetail.heldDevicesHeading")}</h2>
              </div>
              <span className="text-xs text-muted-foreground">{t("userDetail.activeCount", { count: activeLoans.length })}</span>
            </div>

            {isLoansLoading && <div className="mt-4"><LoadingState message={t("userDetail.loadingHeldDevices")} /></div>}

            {!isLoansLoading && activeLoans.length === 0 && (
              <p className="mt-4 text-sm text-muted-foreground">{t("userDetail.noHeldDevices")}</p>
            )}

            {!isLoansLoading && activeLoans.length > 0 && (
              <ul className="mt-4 divide-y divide-border">
                {activeLoans.map((loan) => {
                  const isOverdue = Boolean(loan.dueAt && new Date(loan.dueAt) < new Date());
                  return (
                    <li key={loan.id} className="flex items-center justify-between py-3">
                      <div>
                        <div className="flex items-center gap-2">
                          <Link
                            to="/devices/$deviceId"
                            params={{ deviceId: loan.deviceId }}
                            className="font-identifier text-sm font-semibold hover:underline"
                          >
                            {t("userDetail.devicePrefix", { deviceId: loan.deviceId })}
                          </Link>
                          {isOverdue && (
                            <Badge variant="destructive" className="text-xs">
                              {t("userDetail.overdue")}
                            </Badge>
                          )}
                        </div>
                        <div className="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                          <span>{t("userDetail.borrowedLabel", { date: new Date(loan.borrowedAt).toLocaleDateString() })}</span>
                          {loan.dueAt && <span>{t("userDetail.dueLabel", { date: new Date(loan.dueAt).toLocaleDateString() })}</span>}
                          {loan.paperRef && <span>{t("userDetail.paperRefLabel", { ref: loan.paperRef })}</span>}
                        </div>
                      </div>
                      <Link
                        to="/loans/$loanId"
                        params={{ loanId: loan.id }}
                        className="text-xs font-medium text-primary hover:underline"
                      >
                        {t("userDetail.viewLoan")}
                      </Link>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>

          {/* Loan History */}
          <div className="rounded-lg border border-border bg-card p-6">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <History className="size-4 text-primary" />
                <h2 className="text-base font-semibold text-foreground">{t("userDetail.historyHeading")}</h2>
              </div>
              <span className="text-xs text-muted-foreground">{t("userDetail.totalCount", { count: loans.length })}</span>
            </div>

            {isLoansLoading && <div className="mt-4"><LoadingState message={t("userDetail.loadingHistory")} /></div>}

            {!isLoansLoading && loans.length === 0 && (
              <p className="mt-4 text-sm text-muted-foreground">{t("userDetail.noHistory")}</p>
            )}

            {!isLoansLoading && loans.length > 0 && (
              <div className="mt-4 overflow-x-auto">
                <table className="w-full text-left text-sm">
                  <thead className="border-b border-border text-xs text-muted-foreground uppercase">
                    <tr>
                      <th className="pb-2 font-medium">{t("userDetail.colDevice")}</th>
                      <th className="pb-2 font-medium">{t("userDetail.colStatus")}</th>
                      <th className="pb-2 font-medium">{t("userDetail.colOrigin")}</th>
                      <th className="pb-2 font-medium">{t("userDetail.colBorrowed")}</th>
                      <th className="pb-2 font-medium">{t("userDetail.colReturnedDue")}</th>
                      <th className="pb-2 font-medium text-right">{t("userDetail.colAction")}</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border">
                    {loans.map((loan) => (
                      <tr key={loan.id} className="hover:bg-muted/50">
                        <td className="py-2.5 font-identifier text-xs">
                          <Link
                            to="/devices/$deviceId"
                            params={{ deviceId: loan.deviceId }}
                            className="hover:underline font-semibold"
                          >
                            {loan.deviceId}
                          </Link>
                        </td>
                        <td className="py-2.5">
                          <StatusBadge
                            label={labelize(loan.status)}
                            tone={
                              loan.status === "open"
                                ? "primary"
                                : loan.status === "returned"
                                ? "success"
                                : "destructive"
                            }
                          />
                        </td>
                        <td className="py-2.5">
                          <Badge variant={loanOriginTone(loan.origin)} className="text-xs font-normal">
                            {loan.origin}
                          </Badge>
                        </td>
                        <td className="py-2.5 text-xs text-muted-foreground">
                          {new Date(loan.borrowedAt).toLocaleDateString()}
                        </td>
                        <td className="py-2.5 text-xs text-muted-foreground">
                          {loan.returnedAt
                            ? new Date(loan.returnedAt).toLocaleDateString()
                            : loan.dueAt
                            ? new Date(loan.dueAt).toLocaleDateString()
                            : "—"}
                        </td>
                        <td className="py-2.5 text-right">
                          <Link
                            to="/loans/$loanId"
                            params={{ loanId: loan.id }}
                            className="text-xs font-medium text-primary hover:underline"
                          >
                            {t("userDetail.view")}
                          </Link>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>

        {/* Right Column: Credentials Panel */}
        <div className="flex flex-col gap-6">
          <div className="rounded-lg border border-border bg-card p-6">
            <CredentialsPanel
              subjectType="user"
              subjectId={user.id}
              subject={{
                type: "user",
                fullName: user.fullName,
                employeeNo: user.employeeNo,
                department: departmentName,
              }}
            />
          </div>
        </div>
      </div>

      {/* Edit User Dialog */}
      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{t("userDetail.editProfileTitle")}</DialogTitle>
          </DialogHeader>
          <UserForm
            user={user}
            departments={departments ?? []}
            onDone={() => setEditOpen(false)}
          />
        </DialogContent>
      </Dialog>

      {/* Suspend Reason Dialog */}
      <ReasonActionDialog
        open={suspendOpen}
        onOpenChange={setSuspendOpen}
        title={t("userDetail.suspendTitle")}
        description={t("userDetail.suspendDescription", {
          fullName: user.fullName,
          employeeNo: user.employeeNo,
        })}
        confirmLabel={t("userDetail.suspend")}
        confirmVariant="destructive"
        onConfirm={(reason) => suspendMutation.mutate(reason)}
        isPending={suspendMutation.isPending}
      />

      {/* Archive Reason Dialog */}
      <ReasonActionDialog
        open={archiveOpen}
        onOpenChange={setArchiveOpen}
        title={t("userDetail.archiveTitle")}
        description={t("userDetail.archiveDescription", {
          fullName: user.fullName,
          employeeNo: user.employeeNo,
        })}
        confirmLabel={t("userDetail.archive")}
        confirmVariant="destructive"
        onConfirm={(reason) => archiveMutation.mutate(reason)}
        isPending={archiveMutation.isPending}
      />
    </div>
  );
}

export const userDetailRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/users/$userId",
  component: UserDetailPage,
});

