import {
  type Department,
  type User,
  type UserStatus,
  archiveUser,
  listDepartments,
  listUsers,
  suspendUser,
} from "@hdms/api-client";
import { createColumnHelper } from "@tanstack/react-table";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute, Link, useNavigate } from "@tanstack/react-router";
import { Plus, Upload } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { z } from "zod";
import { CredentialsPanel } from "@/components/credentials-panel";
import {
  DataTable,
  DataTableColumnHeader,
  useDataTableColumns,
  useTextSortingFn,
} from "@/components/data-table";
import { userStatusTone, labelize, StatusBadge } from "@/components/status-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { UserImportDialog } from "@/components/user-import-dialog";
import { useT } from "@/i18n";
import { RoleGate } from "@/lib/use-role";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { UserForm } from "@/components/user-form";
import { authenticatedRoute } from "./authenticated";

const USER_STATUSES: UserStatus[] = ["active", "suspended", "archived"];

const userSearchSchema = z.object({
  status: z.enum(USER_STATUSES as [UserStatus, ...UserStatus[]]).optional(),
  department: z.string().optional(),
  hasCredential: z
    .union([z.boolean(), z.enum(["true", "false"]).transform((v) => v === "true")])
    .optional(),
  q: z.string().optional(),
});
type UserSearch = z.infer<typeof userSearchSchema>;

const columnHelper = createColumnHelper<User>();

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
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <div className="py-2">
          <Textarea
            placeholder={t("users.reasonPlaceholder")}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            rows={3}
            autoFocus
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleClose} disabled={isPending}>
            {t("users.cancel")}
          </Button>
          <Button
            variant={confirmVariant}
            disabled={!reason.trim() || isPending}
            onClick={() => onConfirm(reason.trim())}
          >
            {isPending ? t("users.processing") : confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function UserDetailSheet({
  user,
  departments,
  onClose,
  onSuspend,
  onArchive,
}: {
  user: User;
  departments: Department[];
  onClose: () => void;
  onSuspend: (user: User) => void;
  onArchive: (user: User) => void;
}) {
  const t = useT();
  const departmentName = departments.find((d) => d.id === user.departmentId)?.name;

  return (
    <Sheet open onOpenChange={(o) => !o && onClose()}>
      <SheetContent className="w-full gap-0 overflow-y-auto sm:max-w-lg">
        <SheetHeader>
          <div className="flex items-center justify-between pr-6">
            <SheetTitle className="font-identifier">{user.employeeNo}</SheetTitle>
            <Button size="sm" variant="ghost" asChild>
              <Link to="/users/$userId" params={{ userId: user.id }} className="text-xs">
                {t("users.fullProfile")}
              </Link>
            </Button>
          </div>
        </SheetHeader>
        <div className="flex flex-col gap-6 px-4 pb-6">
          <div>
            <h3 className="mb-2 text-sm font-semibold">{t("users.statusHeading")}</h3>
            <div className="flex items-center gap-2">
              <StatusBadge label={labelize(user.status)} tone={userStatusTone[user.status] ?? "muted"} />
            </div>
            <div className="mt-3 flex gap-2">
              {user.status === "active" && (
                <RoleGate minRole="technician">
                  <Button size="sm" variant="outline" onClick={() => onSuspend(user)}>
                    {t("users.suspend")}
                  </Button>
                </RoleGate>
              )}
              {user.status !== "archived" && (
                <RoleGate minRole="admin">
                  <Button
                    size="sm"
                    variant="outline"
                    className="text-destructive hover:text-destructive"
                    onClick={() => onArchive(user)}
                  >
                    {t("users.archive")}
                  </Button>
                </RoleGate>
              )}
            </div>
          </div>
          <div>
            <h3 className="mb-2 text-sm font-semibold">{t("users.detailsHeading")}</h3>
            <RoleGate
              minRole="technician"
              fallback={
                <div className="space-y-1 text-sm text-muted-foreground">
                  <p><span className="font-medium text-foreground">{t("users.nameLabel")}</span> {user.fullName}</p>
                  <p><span className="font-medium text-foreground">{t("users.employeeNoLabel")}</span> {user.employeeNo}</p>
                  {departmentName && <p><span className="font-medium text-foreground">{t("users.departmentLabel")}</span> {departmentName}</p>}
                </div>
              }
            >
              <UserForm user={user} departments={departments} onDone={() => {}} />
            </RoleGate>
          </div>
          <RoleGate minRole="technician">
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
          </RoleGate>
        </div>
      </SheetContent>
    </Sheet>
  );
}

export function UsersPage() {
  const t = useT();
  const sortText = useTextSortingFn<User>();
  const search = usersRoute.useSearch();
  const navigate = useNavigate({ from: usersRoute.fullPath });
  const [createOpen, setCreateOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [suspendTarget, setSuspendTarget] = useState<User | null>(null);
  const [archiveTarget, setArchiveTarget] = useState<User | null>(null);

  const queryClient = useQueryClient();

  const { data: departments } = useQuery({
    queryKey: ["departments"],
    queryFn: async () => {
      const { data, error } = await listDepartments();
      if (error) throw error;
      return data.items;
    },
  });
  const departmentName = useMemo(
    () => new Map((departments ?? []).map((d) => [d.id, d.name])),
    [departments],
  );

  const query = useInfiniteQuery({
    queryKey: ["users", search],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await listUsers({
        query: {
          ...search,
          cursor: pageParam,
          limit: 25,
        },
      });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.nextCursor,
  });

  const users = useMemo(
    () => query.data?.pages.flatMap((p) => p.items) ?? [],
    [query.data],
  );
  const selectedUser = users.find((u) => u.id === selectedId);

  function updateSearch(patch: Partial<UserSearch>) {
    void navigate({ search: (prev) => ({ ...prev, ...patch }) });
  }

  const suspendMutation = useMutation({
    mutationFn: async ({ id, reason }: { id: string; reason: string }) => {
      const res = await suspendUser({ path: { id }, body: { reason } });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["users"] });
      toast.success(t("users.suspended"));
      setSuspendTarget(null);
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || t("users.suspendFailed"));
    },
  });

  const archiveMutation = useMutation({
    mutationFn: async ({ id, reason }: { id: string; reason: string }) => {
      const res = await archiveUser({ path: { id }, body: { reason } });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["users"] });
      toast.success(t("users.archived"));
      setArchiveTarget(null);
      if (selectedId === archiveTarget?.id) {
        setSelectedId(null);
      }
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || t("users.archiveFailed"));
    },
  });

  const columns = useDataTableColumns(
    () => [
      columnHelper.accessor("employeeNo", {
        sortingFn: sortText,
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("users.columnEmployeeNo")} />,
        cell: (c) => <span className="font-identifier">{c.getValue()}</span>,
      }),
      columnHelper.accessor("fullName", {
        sortingFn: sortText,
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("columns.name")} />,
      }),
      columnHelper.accessor("departmentId", {
        sortingFn: sortText,
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("users.columnDepartment")} />,
        cell: (c) => (c.getValue() ? (departmentName.get(c.getValue()!) ?? "—") : "—"),
      }),
      columnHelper.accessor("status", {
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("columns.status")} />,
        cell: (c) => (
          <div className="flex items-center gap-2">
            <StatusBadge label={labelize(c.getValue())} tone={userStatusTone[c.getValue()] ?? "muted"} />
            {search.hasCredential === false && (
              <Badge variant="outline" className="border-amber-500/50 bg-amber-500/10 text-amber-600 dark:text-amber-400 font-normal text-xs">
                {t("users.noCardIssued")}
              </Badge>
            )}
          </div>
        ),
      }),
      columnHelper.display({
        id: "actions",
        header: () => <span className="sr-only">{t("columns.actions")}</span>,
        cell: ({ row }) => {
          const u = row.original;
          return (
            <div className="flex justify-end gap-1" onClick={(e) => e.stopPropagation()}>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => setSelectedId(u.id)}
              >
                {t("users.view")}
              </Button>
              {u.status === "active" && (
                <RoleGate minRole="technician">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => setSuspendTarget(u)}
                  >
                    {t("users.suspend")}
                  </Button>
                </RoleGate>
              )}
              {u.status !== "archived" && (
                <RoleGate minRole="admin">
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-destructive hover:text-destructive"
                    onClick={() => setArchiveTarget(u)}
                  >
                    {t("users.archive")}
                  </Button>
                </RoleGate>
              )}
            </div>
          );
        },
      }),
    ],
    [departmentName, search.hasCredential, t, sortText],
  );

  const isFiltered = Boolean(search.q || search.status || search.department || search.hasCredential !== undefined);
  const cardFilterValue = search.hasCredential === false ? "no_card" : search.hasCredential === true ? "has_card" : "all";

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">{t("users.title")}</h1>
        <RoleGate minRole="technician">
          <div className="flex items-center gap-2">
            <Button size="sm" variant="outline" onClick={() => setImportOpen(true)}>
              <Upload className="size-4" data-icon="inline-start" />
              {t("users.importCsv")}
            </Button>
            <Button size="sm" onClick={() => setCreateOpen(true)}>
              <Plus className="size-4" data-icon="inline-start" />
              {t("users.newUser")}
            </Button>
          </div>
        </RoleGate>
      </div>

      <DataTable
        tableId="users"
        columns={columns}
        data={users}
        isLoading={query.isLoading}
        isError={query.isError}
        error={query.error}
        onRetry={() => query.refetch()}
        searchQuery={search.q ?? ""}
        onSearchChange={(q) => updateSearch({ q: q || undefined })}
        searchPlaceholder={t("users.searchPlaceholder")}
        isFiltered={isFiltered}
        onResetFilters={() => updateSearch({ q: undefined, status: undefined, department: undefined, hasCredential: undefined })}
        onRowClick={(row) => setSelectedId(row.id)}
        hasNextPage={query.hasNextPage}
        isFetchingNextPage={query.isFetchingNextPage}
        onFetchNextPage={() => query.fetchNextPage()}
        emptyTitle={t("users.emptyTitle")}
        emptyExplanation={t("users.emptyExplanation")}
        filterControls={
          <>
            <Select
              value={search.status ?? "all"}
              onValueChange={(v) => updateSearch({ status: v === "all" ? undefined : (v as UserStatus) })}
            >
              <SelectTrigger className="h-8 w-36 text-xs" aria-label={t("users.filterByStatusAria")}>
                <SelectValue placeholder={t("users.statusPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t("users.allStatuses")}</SelectItem>
                {USER_STATUSES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {labelize(s)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Select
              value={search.department ?? "all"}
              onValueChange={(v) => updateSearch({ department: v === "all" ? undefined : v })}
            >
              <SelectTrigger className="h-8 w-40 text-xs" aria-label={t("users.filterByDepartmentAria")}>
                <SelectValue placeholder={t("users.departmentPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t("users.allDepartments")}</SelectItem>
                {departments?.map((d) => (
                  <SelectItem key={d.id} value={d.id}>
                    {d.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Select
              value={cardFilterValue}
              onValueChange={(v) =>
                updateSearch({
                  hasCredential: v === "no_card" ? false : v === "has_card" ? true : undefined,
                })
              }
            >
              <SelectTrigger className="h-8 w-48 text-xs" aria-label={t("users.filterByCardStatusAria")}>
                <SelectValue placeholder={t("users.cardStatusPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t("users.allCardStatuses")}</SelectItem>
                <SelectItem value="no_card">{t("users.noCardIssuedFilter")}</SelectItem>
                <SelectItem value="has_card">{t("users.cardIssuedFilter")}</SelectItem>
              </SelectContent>
            </Select>
          </>
        }
      />

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{t("users.registerTitle")}</DialogTitle>
          </DialogHeader>
          <UserForm departments={departments ?? []} onDone={() => setCreateOpen(false)} />
        </DialogContent>
      </Dialog>

      <UserImportDialog open={importOpen} onOpenChange={setImportOpen} />

      {selectedUser && (
        <UserDetailSheet
          user={selectedUser}
          departments={departments ?? []}
          onClose={() => setSelectedId(null)}
          onSuspend={(u) => setSuspendTarget(u)}
          onArchive={(u) => setArchiveTarget(u)}
        />
      )}

      <ReasonActionDialog
        open={Boolean(suspendTarget)}
        onOpenChange={(open) => !open && setSuspendTarget(null)}
        title={t("users.suspendTitle")}
        description={t("users.suspendDescription", {
          fullName: suspendTarget?.fullName ?? "",
          employeeNo: suspendTarget?.employeeNo ?? "",
        })}
        confirmLabel={t("users.suspend")}
        confirmVariant="destructive"
        onConfirm={(reason) => suspendTarget && suspendMutation.mutate({ id: suspendTarget.id, reason })}
        isPending={suspendMutation.isPending}
      />

      <ReasonActionDialog
        open={Boolean(archiveTarget)}
        onOpenChange={(open) => !open && setArchiveTarget(null)}
        title={t("users.archiveTitle")}
        description={t("users.archiveDescription", {
          fullName: archiveTarget?.fullName ?? "",
          employeeNo: archiveTarget?.employeeNo ?? "",
        })}
        confirmLabel={t("users.archive")}
        confirmVariant="destructive"
        onConfirm={(reason) => archiveTarget && archiveMutation.mutate({ id: archiveTarget.id, reason })}
        isPending={archiveMutation.isPending}
      />
    </div>
  );
}

export const usersRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/users",
  validateSearch: userSearchSchema,
  component: UsersPage,
});
