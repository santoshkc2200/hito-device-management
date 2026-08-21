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
import { Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { z } from "zod";
import { CredentialsPanel } from "@/components/credentials-panel";
import { DataTable, DataTableColumnHeader, useDataTableColumns } from "@/components/data-table";
import { userStatusTone, labelize, StatusBadge } from "@/components/status-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
            placeholder="Reason (required)"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            rows={3}
            autoFocus
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleClose} disabled={isPending}>
            Cancel
          </Button>
          <Button
            variant={confirmVariant}
            disabled={!reason.trim() || isPending}
            onClick={() => onConfirm(reason.trim())}
          >
            {isPending ? "Processing…" : confirmLabel}
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
  const departmentName = departments.find((d) => d.id === user.departmentId)?.name;

  return (
    <Sheet open onOpenChange={(o) => !o && onClose()}>
      <SheetContent className="w-full gap-0 overflow-y-auto sm:max-w-lg">
        <SheetHeader>
          <div className="flex items-center justify-between pr-6">
            <SheetTitle className="font-identifier">{user.employeeNo}</SheetTitle>
            <Button size="sm" variant="ghost" asChild>
              <Link to="/users/$userId" params={{ userId: user.id }} className="text-xs">
                Full profile →
              </Link>
            </Button>
          </div>
        </SheetHeader>
        <div className="flex flex-col gap-6 px-4 pb-6">
          <div>
            <h3 className="mb-2 text-sm font-semibold">Status</h3>
            <div className="flex items-center gap-2">
              <StatusBadge label={labelize(user.status)} tone={userStatusTone[user.status] ?? "muted"} />
            </div>
            <div className="mt-3 flex gap-2">
              {user.status === "active" && (
                <RoleGate minRole="technician">
                  <Button size="sm" variant="outline" onClick={() => onSuspend(user)}>
                    Suspend
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
                    Archive
                  </Button>
                </RoleGate>
              )}
            </div>
          </div>
          <div>
            <h3 className="mb-2 text-sm font-semibold">Details</h3>
            <RoleGate
              minRole="technician"
              fallback={
                <div className="space-y-1 text-sm text-muted-foreground">
                  <p><span className="font-medium text-foreground">Name:</span> {user.fullName}</p>
                  <p><span className="font-medium text-foreground">Employee No:</span> {user.employeeNo}</p>
                  {departmentName && <p><span className="font-medium text-foreground">Department:</span> {departmentName}</p>}
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
  const search = usersRoute.useSearch();
  const navigate = useNavigate({ from: usersRoute.fullPath });
  const [createOpen, setCreateOpen] = useState(false);
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
      toast.success("Borrower suspended");
      setSuspendTarget(null);
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || "Could not suspend borrower");
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
      toast.success("Borrower archived");
      setArchiveTarget(null);
      if (selectedId === archiveTarget?.id) {
        setSelectedId(null);
      }
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || "Could not archive borrower");
    },
  });

  const columns = useDataTableColumns(
    () => [
      columnHelper.accessor("employeeNo", {
        header: ({ column }) => <DataTableColumnHeader column={column} title="Employee no." />,
        cell: (c) => <span className="font-identifier">{c.getValue()}</span>,
      }),
      columnHelper.accessor("fullName", {
        header: ({ column }) => <DataTableColumnHeader column={column} title="Name" />,
      }),
      columnHelper.accessor("departmentId", {
        header: ({ column }) => <DataTableColumnHeader column={column} title="Department" />,
        cell: (c) => (c.getValue() ? (departmentName.get(c.getValue()!) ?? "—") : "—"),
      }),
      columnHelper.accessor("status", {
        header: ({ column }) => <DataTableColumnHeader column={column} title="Status" />,
        cell: (c) => (
          <div className="flex items-center gap-2">
            <StatusBadge label={labelize(c.getValue())} tone={userStatusTone[c.getValue()] ?? "muted"} />
            {search.hasCredential === false && (
              <Badge variant="outline" className="border-amber-500/50 bg-amber-500/10 text-amber-600 dark:text-amber-400 font-normal text-xs">
                no card issued
              </Badge>
            )}
          </div>
        ),
      }),
      columnHelper.display({
        id: "actions",
        header: () => <span className="sr-only">Actions</span>,
        cell: ({ row }) => {
          const u = row.original;
          return (
            <div className="flex justify-end gap-1" onClick={(e) => e.stopPropagation()}>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => setSelectedId(u.id)}
              >
                View
              </Button>
              {u.status === "active" && (
                <RoleGate minRole="technician">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => setSuspendTarget(u)}
                  >
                    Suspend
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
                    Archive
                  </Button>
                </RoleGate>
              )}
            </div>
          );
        },
      }),
    ],
    [departmentName, search.hasCredential],
  );

  const isFiltered = Boolean(search.q || search.status || search.department || search.hasCredential !== undefined);
  const cardFilterValue = search.hasCredential === false ? "no_card" : search.hasCredential === true ? "has_card" : "all";

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">Users</h1>
        <RoleGate minRole="technician">
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            <Plus className="size-4" data-icon="inline-start" />
            New user
          </Button>
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
        searchPlaceholder="Search name, employee no.…"
        isFiltered={isFiltered}
        onResetFilters={() => updateSearch({ q: undefined, status: undefined, department: undefined, hasCredential: undefined })}
        onRowClick={(row) => setSelectedId(row.id)}
        hasNextPage={query.hasNextPage}
        isFetchingNextPage={query.isFetchingNextPage}
        onFetchNextPage={() => query.fetchNextPage()}
        emptyTitle="No users found"
        emptyExplanation="No borrowers match the selected filters."
        filterControls={
          <>
            <Select
              value={search.status ?? "all"}
              onValueChange={(v) => updateSearch({ status: v === "all" ? undefined : (v as UserStatus) })}
            >
              <SelectTrigger className="h-8 w-36 text-xs" aria-label="Filter by status">
                <SelectValue placeholder="Status" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All statuses</SelectItem>
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
              <SelectTrigger className="h-8 w-40 text-xs" aria-label="Filter by department">
                <SelectValue placeholder="Department" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All departments</SelectItem>
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
              <SelectTrigger className="h-8 w-48 text-xs" aria-label="Filter by card status">
                <SelectValue placeholder="Card status" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All card statuses</SelectItem>
                <SelectItem value="no_card">No card issued (work queue)</SelectItem>
                <SelectItem value="has_card">Card issued</SelectItem>
              </SelectContent>
            </Select>
          </>
        }
      />

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Register a user</DialogTitle>
          </DialogHeader>
          <UserForm departments={departments ?? []} onDone={() => setCreateOpen(false)} />
        </DialogContent>
      </Dialog>

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
        title="Suspend borrower"
        description={`Suspend ${suspendTarget?.fullName} (${suspendTarget?.employeeNo}). They will not be able to borrow devices until reactivated.`}
        confirmLabel="Suspend"
        confirmVariant="destructive"
        onConfirm={(reason) => suspendTarget && suspendMutation.mutate({ id: suspendTarget.id, reason })}
        isPending={suspendMutation.isPending}
      />

      <ReasonActionDialog
        open={Boolean(archiveTarget)}
        onOpenChange={(open) => !open && setArchiveTarget(null)}
        title="Archive borrower"
        description={`Archive ${archiveTarget?.fullName} (${archiveTarget?.employeeNo}). Archiving is permanent and will be refused if they hold any open loans.`}
        confirmLabel="Archive"
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
