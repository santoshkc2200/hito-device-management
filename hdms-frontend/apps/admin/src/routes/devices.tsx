import {
  type Device,
  type DeviceStatus,
  listCategories,
  listDevices,
  setDeviceStatus,
} from "@hdms/api-client";
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute, useNavigate } from "@tanstack/react-router";
import { Plus, Search, Tag } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { z } from "zod";
import { CategoryManagerDialog } from "@/components/category-manager-dialog";
import { CredentialsPanel } from "@/components/credentials-panel";
import { DeviceForm } from "@/components/device-form";
import { DeviceLabelSheetDialog } from "@/components/device-label-sheet-dialog";
import { deviceStatusTone, labelize, StatusBadge } from "@/components/status-badge";
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
import { authenticatedRoute } from "./authenticated";

const DEVICE_STATUSES: DeviceStatus[] = ["available", "on_loan", "maintenance", "retired", "lost"];

// Mirrors internal/modules/catalog/internal/domain/device.go's transition
// matrix — enforced server-side; this only keeps illegal targets from ever
// being offered in the UI.
const DEVICE_TRANSITIONS: Record<DeviceStatus, DeviceStatus[]> = {
  available: ["on_loan", "maintenance", "lost", "retired"],
  on_loan: ["available", "lost"],
  maintenance: ["available", "retired"],
  lost: ["available", "retired"],
  retired: [],
};

const deviceSearchSchema = z.object({
  status: z.enum(DEVICE_STATUSES as [DeviceStatus, ...DeviceStatus[]]).optional(),
  category: z.string().optional(),
  q: z.string().optional(),
});
type DeviceSearch = z.infer<typeof deviceSearchSchema>;

const columnHelper = createColumnHelper<Device>();

function StatusChangeForm({ device, onDone }: { device: Device; onDone: () => void }) {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<DeviceStatus | "">("");
  const [reason, setReason] = useState("");
  const options = DEVICE_TRANSITIONS[device.status];

  const mutation = useMutation({
    mutationFn: async () =>
      setDeviceStatus({ path: { id: device.id }, body: { status: status as DeviceStatus, reason } }),
    onSuccess: async ({ error }) => {
      if (error) throw error;
      await queryClient.invalidateQueries({ queryKey: ["devices"] });
      toast.success("Status updated");
      onDone();
    },
    onError: () => toast.error("Could not change status"),
  });

  if (options.length === 0) {
    return <p className="text-sm text-muted-foreground">Retired devices have no further transitions.</p>;
  }

  return (
    <div className="flex flex-col gap-2">
      <Select value={status} onValueChange={(v) => setStatus(v as DeviceStatus)}>
        <SelectTrigger className="w-full">
          <SelectValue placeholder="Change status to…" />
        </SelectTrigger>
        <SelectContent>
          {options.map((s) => (
            <SelectItem key={s} value={s}>
              {labelize(s)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Textarea
        placeholder="Reason"
        value={reason}
        onChange={(e) => setReason(e.target.value)}
      />
      <Button
        size="sm"
        className="self-start"
        disabled={!status || !reason.trim() || mutation.isPending}
        onClick={() => mutation.mutate()}
      >
        Apply
      </Button>
    </div>
  );
}

function DeviceDetailSheet({ device, onClose }: { device: Device; onClose: () => void }) {
  const { data: categories } = useQuery({
    queryKey: ["categories"],
    queryFn: async () => {
      const { data, error } = await listCategories();
      if (error) throw error;
      return data.items;
    },
  });

  return (
    <Sheet open onOpenChange={(o) => !o && onClose()}>
      <SheetContent className="w-full gap-0 overflow-y-auto sm:max-w-lg">
        <SheetHeader>
          <SheetTitle className="font-identifier">{device.assetTag}</SheetTitle>
        </SheetHeader>
        <div className="flex flex-col gap-6 px-4 pb-6">
          <div>
            <h3 className="mb-2 text-sm font-semibold">Status</h3>
            <StatusBadge label={labelize(device.status)} tone={deviceStatusTone[device.status] ?? "muted"} />
            <RoleGate minRole="technician">
              <div className="mt-3">
                <StatusChangeForm device={device} onDone={() => {}} />
              </div>
            </RoleGate>
          </div>
          <div>
            <h3 className="mb-2 text-sm font-semibold">Details</h3>
            <RoleGate
              minRole="technician"
              fallback={
                <div className="space-y-1 text-sm text-muted-foreground">
                  <p><span className="font-medium text-foreground">Name:</span> {device.name}</p>
                  {device.model && <p><span className="font-medium text-foreground">Model:</span> {device.model}</p>}
                  <p><span className="font-medium text-foreground">Condition:</span> {labelize(device.condition)}</p>
                </div>
              }
            >
              <DeviceForm device={device} categories={categories ?? []} onDone={() => {}} />
            </RoleGate>
          </div>
          <RoleGate minRole="technician">
            <CredentialsPanel
              subjectType="device"
              subjectId={device.id}
              subject={{ type: "device", assetTag: device.assetTag, name: device.name, model: device.model }}
            />
          </RoleGate>
        </div>
      </SheetContent>
    </Sheet>
  );
}

function DevicesPage() {
  const search = devicesRoute.useSearch();
  const navigate = useNavigate({ from: devicesRoute.fullPath });
  const [qInput, setQInput] = useState(search.q ?? "");
  const [createOpen, setCreateOpen] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [labelSheetOpen, setLabelSheetOpen] = useState(false);

  const { data: categories } = useQuery({
    queryKey: ["categories"],
    queryFn: async () => {
      const { data, error } = await listCategories();
      if (error) throw error;
      return data.items;
    },
  });
  const categoryName = useMemo(
    () => new Map((categories ?? []).map((c) => [c.id, c.name])),
    [categories],
  );

  const query = useInfiniteQuery({
    queryKey: ["devices", search],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await listDevices({
        query: { ...search, cursor: pageParam, limit: 25 },
      });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.nextCursor,
  });

  const devices = useMemo(
    () => query.data?.pages.flatMap((p) => p.items) ?? [],
    [query.data],
  );
  // Derived from live data (not a click-time snapshot) so the detail sheet
  // reflects a status/edit mutation immediately, without needing to be
  // closed and reopened.
  const selectedDevice = devices.find((d) => d.id === selectedId);

  function updateSearch(patch: Partial<DeviceSearch>) {
    void navigate({ search: (prev) => ({ ...prev, ...patch }) });
  }

  // Unmemoized columns/data given fresh references to useReactTable on every
  // render is a documented TanStack Table v8 trap: data-change auto-reset
  // behavior calls setState, which triggers a re-render, which (without
  // memoization) hands the table a "new" data array again — an infinite loop
  // that burns CPU silently with no console error.
  const columns = useMemo(
    () => [
      columnHelper.accessor("assetTag", {
        header: "Asset tag",
        cell: (c) => <span className="font-identifier">{c.getValue()}</span>,
      }),
      columnHelper.accessor("name", { header: "Name" }),
      columnHelper.accessor("categoryId", {
        header: "Category",
        cell: (c) => categoryName.get(c.getValue()) ?? "—",
      }),
      columnHelper.accessor("status", {
        header: "Status",
        cell: (c) => <StatusBadge label={labelize(c.getValue())} tone={deviceStatusTone[c.getValue()] ?? "muted"} />,
      }),
      columnHelper.accessor("condition", { header: "Condition", cell: (c) => labelize(c.getValue()) }),
    ],
    [categoryName],
  );

  const table = useReactTable({
    data: devices,
    columns,
    getCoreRowModel: getCoreRowModel(),
  });

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">Devices</h1>
        <RoleGate minRole="technician">
          <div className="flex gap-2">
            <RoleGate minRole="admin">
              <CategoryManagerDialog />
            </RoleGate>
            <Button
              size="sm"
              variant="outline"
              disabled={devices.length === 0}
              onClick={() => setLabelSheetOpen(true)}
            >
              <Tag className="size-4" data-icon="inline-start" />
              Print labels
            </Button>
            <Button size="sm" onClick={() => setCreateOpen(true)}>
              <Plus className="size-4" data-icon="inline-start" />
              New device
            </Button>
          </div>
        </RoleGate>
      </div>

      <div className="flex items-center gap-2">
        <div className="relative w-64">
          <Search className="absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            placeholder="Search asset tag, name, model…"
            className="pl-8"
            value={qInput}
            onChange={(e) => setQInput(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && updateSearch({ q: qInput || undefined })}
            onBlur={() => updateSearch({ q: qInput || undefined })}
          />
        </div>
        <Select
          value={search.status ?? "all"}
          onValueChange={(v) => updateSearch({ status: v === "all" ? undefined : (v as DeviceStatus) })}
        >
          <SelectTrigger className="w-40">
            <SelectValue placeholder="Status" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All statuses</SelectItem>
            {DEVICE_STATUSES.map((s) => (
              <SelectItem key={s} value={s}>
                {labelize(s)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={search.category ?? "all"}
          onValueChange={(v) => updateSearch({ category: v === "all" ? undefined : v })}
        >
          <SelectTrigger className="w-44">
            <SelectValue placeholder="Category" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All categories</SelectItem>
            {categories?.map((c) => (
              <SelectItem key={c.id} value={c.id}>
                {c.name}
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
                <TableCell colSpan={5}>
                  <Skeleton className="h-6 w-full" />
                </TableCell>
              </TableRow>
            )}
            {table.getRowModel().rows.map((row) => (
              <TableRow
                key={row.id}
                className="cursor-pointer"
                onClick={() => setSelectedId(row.original.id)}
              >
                {row.getVisibleCells().map((cell) => (
                  <TableCell key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>
                ))}
              </TableRow>
            ))}
            {!query.isLoading && devices.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground">
                  No devices match these filters.
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
            <DialogTitle>Register a device</DialogTitle>
          </DialogHeader>
          <DeviceForm categories={categories ?? []} onDone={() => setCreateOpen(false)} />
        </DialogContent>
      </Dialog>

      {selectedDevice && (
        <DeviceDetailSheet device={selectedDevice} onClose={() => setSelectedId(null)} />
      )}

      <DeviceLabelSheetDialog devices={devices} open={labelSheetOpen} onOpenChange={setLabelSheetOpen} />
    </div>
  );
}

export const devicesRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/devices",
  validateSearch: deviceSearchSchema,
  component: DevicesPage,
});
