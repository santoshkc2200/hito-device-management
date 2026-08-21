import {
  type Department,
  type User,
  type UserStatus,
  listDepartments,
  listUsers,
  suspendUser,
} from "@hdms/api-client";
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute, useNavigate } from "@tanstack/react-router";
import { Plus, Search } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { z } from "zod";
import { CredentialsPanel } from "@/components/credentials-panel";
import { userStatusTone, labelize, StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { RoleGate } from "@/lib/use-role";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { UserForm } from "@/components/user-form";
import { authenticatedRoute } from "./authenticated";

const USER_STATUSES: UserStatus[] = ["active", "suspended", "archived"];

const userSearchSchema = z.object({
  status: z.enum(USER_STATUSES as [UserStatus, ...UserStatus[]]).optional(),
  department: z.string().optional(),
  q: z.string().optional(),
});
type UserSearch = z.infer<typeof userSearchSchema>;

const columnHelper = createColumnHelper<User>();

function SuspendForm({ user, onDone }: { user: User; onDone: () => void }) {
  const queryClient = useQueryClient();
  const [reason, setReason] = useState("");
  const mutation = useMutation({
    mutationFn: async () => suspendUser({ path: { id: user.id }, body: { reason } }),
    onSuccess: async ({ error }) => {
      if (error) throw error;
      await queryClient.invalidateQueries({ queryKey: ["users"] });
      toast.success("User suspended");
      onDone();
    },
    onError: () => toast.error("Could not suspend"),
  });

  return (
    <div className="flex flex-col gap-2">
      <Textarea placeholder="Reason" value={reason} onChange={(e) => setReason(e.target.value)} />
      <Button
        size="sm"
        variant="destructive"
        className="self-start"
        disabled={!reason.trim() || mutation.isPending}
        onClick={() => mutation.mutate()}
      >
        Suspend
      </Button>
    </div>
  );
}

function UserDetailSheet({
  user,
  departments,
  onClose,
}: {
  user: User;
  departments: Department[];
  onClose: () => void;
}) {
  const departmentName = departments.find((d) => d.id === user.departmentId)?.name;

  return (
    <Sheet open onOpenChange={(o) => !o && onClose()}>
      <SheetContent className="w-full gap-0 overflow-y-auto sm:max-w-lg">
        <SheetHeader>
          <SheetTitle className="font-identifier">{user.employeeNo}</SheetTitle>
        </SheetHeader>
        <div className="flex flex-col gap-6 px-4 pb-6">
          <div>
            <h3 className="mb-2 text-sm font-semibold">Status</h3>
            <StatusBadge label={labelize(user.status)} tone={userStatusTone[user.status] ?? "muted"} />
            {user.status === "active" && (
              <RoleGate minRole="technician">
                <div className="mt-3">
                  <SuspendForm user={user} onDone={() => {}} />
                </div>
              </RoleGate>
            )}
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

function UsersPage() {
  const search = usersRoute.useSearch();
  const navigate = useNavigate({ from: usersRoute.fullPath });
  const [qInput, setQInput] = useState(search.q ?? "");
  const [createOpen, setCreateOpen] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);

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
      const { data, error } = await listUsers({ query: { ...search, cursor: pageParam, limit: 25 } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.nextCursor,
  });

  const users = useMemo(() => query.data?.pages.flatMap((p) => p.items) ?? [], [query.data]);
  const selectedUser = users.find((u) => u.id === selectedId);

  function updateSearch(patch: Partial<UserSearch>) {
    void navigate({ search: (prev) => ({ ...prev, ...patch }) });
  }

  const columns = useMemo(
    () => [
      columnHelper.accessor("employeeNo", {
        header: "Employee no.",
        cell: (c) => <span className="font-identifier">{c.getValue()}</span>,
      }),
      columnHelper.accessor("fullName", { header: "Name" }),
      columnHelper.accessor("departmentId", {
        header: "Department",
        cell: (c) => (c.getValue() ? (departmentName.get(c.getValue()!) ?? "—") : "—"),
      }),
      columnHelper.accessor("status", {
        header: "Status",
        cell: (c) => <StatusBadge label={labelize(c.getValue())} tone={userStatusTone[c.getValue()] ?? "muted"} />,
      }),
    ],
    [departmentName],
  );

  const table = useReactTable({
    data: users,
    columns,
    getCoreRowModel: getCoreRowModel(),
  });

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

      <div className="flex items-center gap-2">
        <div className="relative w-64">
          <Search className="absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            placeholder="Search name, employee no.…"
            className="pl-8"
            value={qInput}
            onChange={(e) => setQInput(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && updateSearch({ q: qInput || undefined })}
            onBlur={() => updateSearch({ q: qInput || undefined })}
          />
        </div>
        <Select
          value={search.status ?? "all"}
          onValueChange={(v) => updateSearch({ status: v === "all" ? undefined : (v as UserStatus) })}
        >
          <SelectTrigger className="w-40">
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
          <SelectTrigger className="w-44">
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
      </div>

      <div className="rounded-md border border-border">
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((hg) => (
              <TableRow key={hg.id}>
                {hg.headers.map((h) => (
                  <TableHead key={h.id}>{flexRender(h.column.columnDef.header, h.getContext())}</TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {query.isLoading && (
              <TableRow>
                <TableCell colSpan={4}>
                  <Skeleton className="h-6 w-full" />
                </TableCell>
              </TableRow>
            )}
            {table.getRowModel().rows.map((row) => (
              <TableRow key={row.id} className="cursor-pointer" onClick={() => setSelectedId(row.original.id)}>
                {row.getVisibleCells().map((cell) => (
                  <TableCell key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>
                ))}
              </TableRow>
            ))}
            {!query.isLoading && users.length === 0 && (
              <TableRow>
                <TableCell colSpan={4} className="text-center text-muted-foreground">
                  No users match these filters.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      {query.hasNextPage && (
        <Button
          variant="outline"
          size="sm"
          className="self-center"
          disabled={query.isFetchingNextPage}
          onClick={() => query.fetchNextPage()}
        >
          Load more
        </Button>
      )}

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
        />
      )}
    </div>
  );
}

export const usersRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/users",
  validateSearch: userSearchSchema,
  component: UsersPage,
});
