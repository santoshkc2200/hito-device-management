import {
  type Device,
  type DeviceStatus,
  listCategories,
  listDevices,
  setDeviceStatus,
} from "@hdms/api-client";
import { type RowSelectionState, createColumnHelper } from "@tanstack/react-table";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute, Link, useNavigate } from "@tanstack/react-router";
import {
  Download,
  Edit3,
  ExternalLink,
  Folders,
  MoreHorizontal,
  Plus,
  Printer,
  RotateCw,
  Tag,
  Upload,
} from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { z } from "zod";
import { BulkCategoryDialog } from "@/components/bulk-category-dialog";
import { CategoryManagerDialog } from "@/components/category-manager-dialog";
import { DataTable, DataTableColumnHeader, useDataTableColumns } from "@/components/data-table";
import { DeviceForm } from "@/components/device-form";
import { DeviceImportDialog } from "@/components/device-import-dialog";
import { DeviceLabelSheetDialog } from "@/components/device-label-sheet-dialog";
import { deviceStatusTone, labelize, StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
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
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { RoleGate } from "@/lib/use-role";
import { authenticatedRoute } from "./authenticated";

const DEVICE_STATUSES: DeviceStatus[] = ["available", "on_loan", "maintenance", "retired", "lost"];

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

function StatusChangeDialog({
  device,
  open,
  onOpenChange,
}: {
  device: Device | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const [targetStatus, setTargetStatus] = useState<DeviceStatus | "">("");
  const [reason, setReason] = useState("");

  const options = device ? DEVICE_TRANSITIONS[device.status] : [];

  const mutation = useMutation({
    mutationFn: async () => {
      if (!device || !targetStatus) return;
      const res = await setDeviceStatus({
        path: { id: device.id },
        body: { status: targetStatus as DeviceStatus, reason: reason.trim() },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["devices"] });
      toast.success("Device status updated");
      handleClose();
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || "Could not change status");
    },
  });

  const handleClose = () => {
    setTargetStatus("");
    setReason("");
    onOpenChange(false);
  };

  if (!device) return null;

  return (
    <Dialog open={open} onOpenChange={(o) => (!o ? handleClose() : onOpenChange(true))}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Change status: {device.assetTag}</DialogTitle>
          <DialogDescription>
            Current status is <strong>{labelize(device.status)}</strong>. A mandatory reason is required
            for custody audit logging.
          </DialogDescription>
        </DialogHeader>

        {options.length === 0 ? (
          <p className="text-sm text-muted-foreground py-2">
            Retired devices have no further transitions.
          </p>
        ) : (
          <div className="flex flex-col gap-3 py-2">
            <div>
              <label className="text-xs font-semibold text-muted-foreground uppercase">
                Target status
              </label>
              <div className="mt-1">
                <Select
                  value={targetStatus}
                  onValueChange={(v) => setTargetStatus(v as DeviceStatus)}
                >
                  <SelectTrigger className="w-full" aria-label="Select new status">
                    <SelectValue placeholder="Select new status…" />
                  </SelectTrigger>
                  <SelectContent>
                    {options.map((s) => (
                      <SelectItem key={s} value={s}>
                        {labelize(s)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div>
              <label className="text-xs font-semibold text-muted-foreground uppercase">
                Reason (required)
              </label>
              <div className="mt-1">
                <Textarea
                  placeholder="State the operational reason for this status change…"
                  value={reason}
                  onChange={(e) => setReason(e.target.value)}
                  rows={3}
                />
              </div>
            </div>
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={handleClose} disabled={mutation.isPending}>
            Cancel
          </Button>
          <Button
            disabled={!targetStatus || !reason.trim() || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending ? "Applying…" : "Apply status change"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function DevicesPage() {
  const search = devicesRoute.useSearch();
  const navigate = useNavigate({ from: devicesRoute.fullPath });

  const [createOpen, setCreateOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [labelSheetOpen, setLabelSheetOpen] = useState(false);
  const [labelDevices, setLabelDevices] = useState<Device[]>([]);
  const [bulkCategoryOpen, setBulkCategoryOpen] = useState(false);

  const [editingDevice, setEditingDevice] = useState<Device | null>(null);
  const [statusTargetDevice, setStatusTargetDevice] = useState<Device | null>(null);
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({});

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

  function updateSearch(patch: Partial<DeviceSearch>) {
    void navigate({ search: (prev) => ({ ...prev, ...patch }) });
  }

  const selectedDevices = useMemo(() => {
    const ids = Object.keys(rowSelection).filter((k) => rowSelection[k]);
    return devices.filter((d) => ids.includes(d.id));
  }, [rowSelection, devices]);

  const handleExportCSV = (devicesToExport: Device[]) => {
    const rows = devicesToExport.length > 0 ? devicesToExport : devices;
    if (rows.length === 0) {
      toast.error("No devices to export");
      return;
    }
    const headers = [
      "asset_tag",
      "name",
      "category",
      "status",
      "condition",
      "manufacturer",
      "model",
      "serial_no",
      "home_location",
      "notes",
    ];
    const csvLines = [headers.join(",")];
    for (const d of rows) {
      const cat = categoryName.get(d.categoryId) || "";
      const line = [
        d.assetTag,
        `"${(d.name || "").replace(/"/g, '""')}"`,
        `"${cat.replace(/"/g, '""')}"`,
        d.status,
        d.condition,
        `"${(d.manufacturer || "").replace(/"/g, '""')}"`,
        `"${(d.model || "").replace(/"/g, '""')}"`,
        `"${(d.serialNo || "").replace(/"/g, '""')}"`,
        `"${(d.homeLocation || "").replace(/"/g, '""')}"`,
        `"${(d.notes || "").replace(/"/g, '""')}"`,
      ];
      csvLines.push(line.join(","));
    }
    const blob = new Blob([csvLines.join("\n")], { type: "text/csv;charset=utf-8;" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `devices-export-${new Date().toISOString().slice(0, 10)}.csv`;
    link.click();
    URL.revokeObjectURL(url);
    toast.success(`Exported ${rows.length} devices to CSV`);
  };

  const columns = useDataTableColumns<Device>(
    () => [
      columnHelper.display({
        id: "select",
        header: ({ table }) => (
          <Checkbox
            checked={
              table.getIsAllPageRowsSelected() ||
              (table.getIsSomePageRowsSelected() && "indeterminate")
            }
            onCheckedChange={(val) => table.toggleAllPageRowsSelected(!!val)}
            aria-label="Select all"
            className="translate-y-0.5"
          />
        ),
        cell: ({ row }) => (
          <Checkbox
            checked={row.getIsSelected()}
            onCheckedChange={(val) => row.toggleSelected(!!val)}
            aria-label={`Select device ${row.original.assetTag}`}
            className="translate-y-0.5"
          />
        ),
        enableSorting: false,
        enableHiding: false,
      }),
      columnHelper.accessor("assetTag", {
        header: ({ column }) => <DataTableColumnHeader column={column} title="Asset tag" />,
        cell: (c) => (
          <Link
            to="/devices/$deviceId"
            params={{ deviceId: c.row.original.id }}
            className="font-identifier font-semibold text-primary hover:underline"
            onClick={(e) => e.stopPropagation()}
          >
            {c.getValue()}
          </Link>
        ),
      }),
      columnHelper.accessor("name", {
        header: ({ column }) => <DataTableColumnHeader column={column} title="Name" />,
        cell: (c) => <span className="font-medium text-foreground">{c.getValue()}</span>,
      }),
      columnHelper.accessor("categoryId", {
        header: ({ column }) => <DataTableColumnHeader column={column} title="Category" />,
        cell: (c) => categoryName.get(c.getValue()) ?? "—",
      }),
      columnHelper.accessor("status", {
        header: ({ column }) => <DataTableColumnHeader column={column} title="Status" />,
        cell: (c) => (
          <StatusBadge
            label={labelize(c.getValue())}
            tone={deviceStatusTone[c.getValue()] ?? "muted"}
          />
        ),
      }),
      columnHelper.accessor("condition", {
        header: ({ column }) => <DataTableColumnHeader column={column} title="Condition" />,
        cell: (c) => labelize(c.getValue()),
      }),
      columnHelper.accessor("model", {
        header: ({ column }) => <DataTableColumnHeader column={column} title="Model" />,
        cell: (c) => c.getValue() || "—",
      }),
      columnHelper.display({
        id: "actions",
        header: () => <span className="sr-only">Actions</span>,
        cell: ({ row }) => {
          const device = row.original;
          return (
            <div className="flex justify-end" onClick={(e) => e.stopPropagation()}>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon" className="size-8">
                    <MoreHorizontal className="size-4" />
                    <span className="sr-only">Open menu</span>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem asChild>
                    <Link to="/devices/$deviceId" params={{ deviceId: device.id }}>
                      <ExternalLink className="mr-2 size-4" />
                      View details
                    </Link>
                  </DropdownMenuItem>
                  <RoleGate minRole="technician">
                    <DropdownMenuItem onClick={() => setEditingDevice(device)}>
                      <Edit3 className="mr-2 size-4" />
                      Edit device
                    </DropdownMenuItem>
                    <DropdownMenuItem onClick={() => setStatusTargetDevice(device)}>
                      <RotateCw className="mr-2 size-4" />
                      Change status
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      onClick={() => {
                        setLabelDevices([device]);
                        setLabelSheetOpen(true);
                      }}
                    >
                      <Printer className="mr-2 size-4" />
                      Print label
                    </DropdownMenuItem>
                  </RoleGate>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          );
        },
      }),
    ],
    [categoryName],
  );

  const isFiltered = Boolean(search.q || search.status || search.category);

  return (
    <div className="flex flex-col gap-4">
      {/* Header Bar */}
      <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
        <div>
          <h1 className="text-xl font-bold tracking-tight">Devices</h1>
          <p className="text-sm text-muted-foreground">
            Equipment catalogue, live custody tracking, status transitions, and barcode labeling.
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <RoleGate minRole="admin">
            <CategoryManagerDialog />
          </RoleGate>
          <RoleGate minRole="technician">
            <Button
              size="sm"
              variant="outline"
              onClick={() => setImportOpen(true)}
            >
              <Upload className="size-4" data-icon="inline-start" />
              Import CSV
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={devices.length === 0}
              onClick={() => {
                setLabelDevices(devices);
                setLabelSheetOpen(true);
              }}
            >
              <Tag className="size-4" data-icon="inline-start" />
              Print labels
            </Button>
            <Button size="sm" onClick={() => setCreateOpen(true)}>
              <Plus className="size-4" data-icon="inline-start" />
              New device
            </Button>
          </RoleGate>
        </div>
      </div>

      {/* Main Data Table */}
      <DataTable
        tableId="devices"
        columns={columns}
        data={devices}
        getRowId={(row) => row.id}
        isLoading={query.isLoading}
        isError={query.isError}
        error={query.error}
        onRetry={() => query.refetch()}
        searchQuery={search.q ?? ""}
        onSearchChange={(q) => updateSearch({ q: q || undefined })}
        searchPlaceholder="Search asset tag, name, model, serial…"
        isFiltered={isFiltered}
        onResetFilters={() => updateSearch({ q: undefined, status: undefined, category: undefined })}
        onRowClick={(row) => void navigate({ to: "/devices/$deviceId", params: { deviceId: row.id } })}
        hasNextPage={query.hasNextPage}
        isFetchingNextPage={query.isFetchingNextPage}
        onFetchNextPage={() => query.fetchNextPage()}
        rowSelection={rowSelection}
        onRowSelectionChange={setRowSelection}
        enableRowSelection={true}
        itemLabel="device"
        emptyTitle="No devices registered"
        emptyExplanation="No devices match the current filter or have been registered yet."
        filterControls={
          <>
            <Select
              value={search.status ?? "all"}
              onValueChange={(v) =>
                updateSearch({ status: v === "all" ? undefined : (v as DeviceStatus) })
              }
            >
              <SelectTrigger className="h-8 w-36 text-xs" aria-label="Filter by status">
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
              <SelectTrigger className="h-8 w-40 text-xs" aria-label="Filter by category">
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
          </>
        }
        bulkActions={() => (
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              className="h-8 text-xs"
              onClick={() => {
                setLabelDevices(selectedDevices);
                setLabelSheetOpen(true);
              }}
            >
              <Printer className="mr-1.5 size-3.5" />
              Print labels ({selectedDevices.length})
            </Button>
            <RoleGate minRole="technician">
              <Button
                size="sm"
                variant="outline"
                className="h-8 text-xs"
                onClick={() => setBulkCategoryOpen(true)}
              >
                <Folders className="mr-1.5 size-3.5" />
                Change category
              </Button>
            </RoleGate>
            <Button
              size="sm"
              variant="outline"
              className="h-8 text-xs"
              onClick={() => handleExportCSV(selectedDevices)}
            >
              <Download className="mr-1.5 size-3.5" />
              Export CSV
            </Button>
          </div>
        )}
      />

      {/* Register New Device Dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Register a device</DialogTitle>
            <DialogDescription>
              Add a new equipment asset to the inventory with asset tag and category.
            </DialogDescription>
          </DialogHeader>
          <DeviceForm categories={categories ?? []} onDone={() => setCreateOpen(false)} />
        </DialogContent>
      </Dialog>

      {/* Edit Device Dialog */}
      <Dialog open={Boolean(editingDevice)} onOpenChange={(o) => !o && setEditingDevice(null)}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Edit device: {editingDevice?.assetTag}</DialogTitle>
            <DialogDescription>
              Update equipment attributes, model, serial, and location.
            </DialogDescription>
          </DialogHeader>
          {editingDevice && (
            <DeviceForm
              device={editingDevice}
              categories={categories ?? []}
              onDone={() => setEditingDevice(null)}
            />
          )}
        </DialogContent>
      </Dialog>

      {/* Status Change Dialog with Reason */}
      <StatusChangeDialog
        device={statusTargetDevice}
        open={Boolean(statusTargetDevice)}
        onOpenChange={(o) => !o && setStatusTargetDevice(null)}
      />

      {/* Bulk Category Change Dialog */}
      <BulkCategoryDialog
        open={bulkCategoryOpen}
        onOpenChange={setBulkCategoryOpen}
        selectedDevices={selectedDevices}
        categories={categories ?? []}
        onDone={() => {
          setBulkCategoryOpen(false);
          setRowSelection({});
        }}
      />

      {/* Label Sheet Printing Dialog */}
      <DeviceLabelSheetDialog
        devices={labelDevices.length > 0 ? labelDevices : devices}
        open={labelSheetOpen}
        onOpenChange={setLabelSheetOpen}
      />

      {/* CSV Import Wizard */}
      <DeviceImportDialog
        open={importOpen}
        onOpenChange={setImportOpen}
        onPrintLabels={(imported) => {
          setLabelDevices(imported);
          setLabelSheetOpen(true);
        }}
      />
    </div>
  );
}

export const devicesRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/devices",
  validateSearch: deviceSearchSchema,
  component: DevicesPage,
});
