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
} from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { z } from "zod";
import { BulkCategoryDialog } from "@/components/bulk-category-dialog";
import { CategoryManagerDialog } from "@/components/category-manager-dialog";
import {
  DataTable,
  DataTableColumnHeader,
  useDataTableColumns,
  useTextSortingFn,
} from "@/components/data-table";
import { DeviceForm } from "@/components/device-form";
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
import { useT } from "@/i18n";
import { csvBlob } from "@/lib/csv";
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
  const t = useT();
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
      toast.success(t("devices.statusDialog.updated"));
      handleClose();
    },
    onError: (err: any) => {
      toast.error(err?.detail || err?.title || t("devices.statusDialog.updateFailed"));
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
          <DialogTitle>{t("devices.statusDialog.title", { assetTag: device.assetTag })}</DialogTitle>
          <DialogDescription>
            {t("devices.statusDialog.descriptionPrefix")}{" "}
            <strong>{labelize(device.status)}</strong>
            {t("devices.statusDialog.descriptionSuffix")}
          </DialogDescription>
        </DialogHeader>

        {options.length === 0 ? (
          <p className="text-sm text-muted-foreground py-2">
            {t("devices.statusDialog.noTransitions")}
          </p>
        ) : (
          <div className="flex flex-col gap-3 py-2">
            <div>
              <label className="text-xs font-semibold text-muted-foreground uppercase">
                {t("devices.statusDialog.targetLabel")}
              </label>
              <div className="mt-1">
                <Select
                  value={targetStatus}
                  onValueChange={(v) => setTargetStatus(v as DeviceStatus)}
                >
                  <SelectTrigger className="w-full" aria-label={t("devices.statusDialog.selectNewStatusAria")}>
                    <SelectValue placeholder={t("devices.statusDialog.selectNewStatusPlaceholder")} />
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
                {t("devices.statusDialog.reasonLabel")}
              </label>
              <div className="mt-1">
                <Textarea
                  placeholder={t("devices.statusDialog.reasonPlaceholder")}
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
            {t("devices.statusDialog.cancel")}
          </Button>
          <Button
            disabled={!targetStatus || !reason.trim() || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending ? t("devices.statusDialog.applying") : t("devices.statusDialog.apply")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * Device table columns, memoized on the translator and the category lookup.
 *
 * The definitions live in a hook rather than inline so the memoization is
 * testable: an unmemoized column array is a new reference on every render,
 * which sends TanStack Table v8 into a silent 100% CPU loop with nothing in
 * the console. Every dependency here is either memoized (`categoryName`), a
 * `useCallback`-stable translator (`t`), or a `useState` setter, so the array
 * is rebuilt on a language switch and at no other time.
 */
export function useDeviceColumns({
  categoryName,
  onEditDevice,
  onChangeStatus,
  setLabelDevices,
  setLabelSheetOpen,
}: {
  categoryName: Map<string, string>;
  onEditDevice: (device: Device) => void;
  onChangeStatus: (device: Device) => void;
  setLabelDevices: (devices: Device[]) => void;
  setLabelSheetOpen: (open: boolean) => void;
}) {
  const t = useT();
  const sortText = useTextSortingFn<Device>();

  return useDataTableColumns<Device>(
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
            aria-label={t("columns.selectAll")}
            className="translate-y-0.5"
          />
        ),
        cell: ({ row }) => (
          <Checkbox
            checked={row.getIsSelected()}
            onCheckedChange={(val) => row.toggleSelected(!!val)}
            aria-label={t("devices.selectDeviceAria", { assetTag: row.original.assetTag })}
            className="translate-y-0.5"
          />
        ),
        enableSorting: false,
        enableHiding: false,
      }),
      columnHelper.accessor("assetTag", {
        sortingFn: sortText,
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("columns.assetTag")} />,
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
        sortingFn: sortText,
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("columns.name")} />,
        cell: (c) => <span className="font-medium text-foreground">{c.getValue()}</span>,
      }),
      columnHelper.accessor("categoryId", {
        sortingFn: sortText,
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("columns.category")} />,
        cell: (c) => categoryName.get(c.getValue()) ?? "—",
      }),
      columnHelper.accessor("status", {
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("columns.status")} />,
        cell: (c) => (
          <StatusBadge
            label={labelize(c.getValue())}
            tone={deviceStatusTone[c.getValue()] ?? "muted"}
          />
        ),
      }),
      columnHelper.accessor("condition", {
        sortingFn: sortText,
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("columns.condition")} />,
        cell: (c) => labelize(c.getValue()),
      }),
      columnHelper.accessor("model", {
        sortingFn: sortText,
        header: ({ column }) => <DataTableColumnHeader column={column} title={t("columns.model")} />,
        cell: (c) => c.getValue() || "—",
      }),
      columnHelper.display({
        id: "actions",
        header: () => <span className="sr-only">{t("columns.actions")}</span>,
        cell: ({ row }) => {
          const device = row.original;
          return (
            <div className="flex justify-end" onClick={(e) => e.stopPropagation()}>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="icon" className="size-8">
                    <MoreHorizontal className="size-4" />
                    <span className="sr-only">{t("columns.openMenu")}</span>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem asChild>
                    <Link to="/devices/$deviceId" params={{ deviceId: device.id }}>
                      <ExternalLink className="mr-2 size-4" />
                      {t("devices.viewDetails")}
                    </Link>
                  </DropdownMenuItem>
                  <RoleGate minRole="technician">
                    <DropdownMenuItem onClick={() => onEditDevice(device)}>
                      <Edit3 className="mr-2 size-4" />
                      {t("devices.editDevice")}
                    </DropdownMenuItem>
                    <DropdownMenuItem onClick={() => onChangeStatus(device)}>
                      <RotateCw className="mr-2 size-4" />
                      {t("devices.changeStatus")}
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      onClick={() => {
                        setLabelDevices([device]);
                        setLabelSheetOpen(true);
                      }}
                    >
                      <Printer className="mr-2 size-4" />
                      {t("devices.printLabel")}
                    </DropdownMenuItem>
                  </RoleGate>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          );
        },
      }),
    ],
    [categoryName, t, sortText, onEditDevice, onChangeStatus, setLabelDevices, setLabelSheetOpen],
  );
}

export function DevicesPage() {
  const t = useT();
  const search = devicesRoute.useSearch();
  const navigate = useNavigate({ from: devicesRoute.fullPath });

  const [createOpen, setCreateOpen] = useState(false);
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
      toast.error(t("devices.nothingToExport"));
      return;
    }
    const headers = [
      t("devices.csvHeaders.assetTag"),
      t("devices.csvHeaders.name"),
      t("devices.csvHeaders.category"),
      t("devices.csvHeaders.status"),
      t("devices.csvHeaders.condition"),
      t("devices.csvHeaders.manufacturer"),
      t("devices.csvHeaders.model"),
      t("devices.csvHeaders.serialNumber"),
      t("devices.csvHeaders.homeLocation"),
      t("devices.csvHeaders.notes"),
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
    const blob = csvBlob(csvLines);
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `devices-export-${new Date().toISOString().slice(0, 10)}.csv`;
    link.click();
    URL.revokeObjectURL(url);
    toast.success(t("devices.exported", { count: rows.length }));
  };

  const columns = useDeviceColumns({
    categoryName,
    onEditDevice: setEditingDevice,
    onChangeStatus: setStatusTargetDevice,
    setLabelDevices,
    setLabelSheetOpen,
  });

  const isFiltered = Boolean(search.q || search.status || search.category);

  return (
    <div className="flex flex-col gap-4">
      {/* Header Bar */}
      <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
        <div>
          <h1 className="text-xl font-bold tracking-tight">{t("devices.title")}</h1>
          <p className="text-sm text-muted-foreground">
            {t("devices.subtitle")}
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
              disabled={devices.length === 0}
              onClick={() => {
                setLabelDevices(devices);
                setLabelSheetOpen(true);
              }}
            >
              <Tag className="size-4" data-icon="inline-start" />
              {t("devices.printLabels")}
            </Button>
            <Button size="sm" onClick={() => setCreateOpen(true)}>
              <Plus className="size-4" data-icon="inline-start" />
              {t("devices.newDevice")}
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
        searchPlaceholder={t("devices.searchPlaceholder")}
        isFiltered={isFiltered}
        onResetFilters={() => updateSearch({ q: undefined, status: undefined, category: undefined })}
        onRowClick={(row) => void navigate({ to: "/devices/$deviceId", params: { deviceId: row.id } })}
        hasNextPage={query.hasNextPage}
        isFetchingNextPage={query.isFetchingNextPage}
        onFetchNextPage={() => query.fetchNextPage()}
        rowSelection={rowSelection}
        onRowSelectionChange={setRowSelection}
        enableRowSelection={true}
        itemLabel={t("devices.itemLabel")}
        emptyTitle={t("devices.emptyTitle")}
        emptyExplanation={t("devices.emptyExplanation")}
        filterControls={
          <>
            <Select
              value={search.status ?? "all"}
              onValueChange={(v) =>
                updateSearch({ status: v === "all" ? undefined : (v as DeviceStatus) })
              }
            >
              <SelectTrigger className="h-8 w-36 text-xs" aria-label={t("devices.filterByStatusAria")}>
                <SelectValue placeholder={t("devices.statusPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t("devices.allStatuses")}</SelectItem>
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
              <SelectTrigger className="h-8 w-40 text-xs" aria-label={t("devices.filterByCategoryAria")}>
                <SelectValue placeholder={t("devices.categoryPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t("devices.allCategories")}</SelectItem>
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
              {t("devices.printLabelsWithCount", { count: selectedDevices.length })}
            </Button>
            <RoleGate minRole="technician">
              <Button
                size="sm"
                variant="outline"
                className="h-8 text-xs"
                onClick={() => setBulkCategoryOpen(true)}
              >
                <Folders className="mr-1.5 size-3.5" />
                {t("devices.changeCategory")}
              </Button>
            </RoleGate>
            <Button
              size="sm"
              variant="outline"
              className="h-8 text-xs"
              onClick={() => handleExportCSV(selectedDevices)}
            >
              <Download className="mr-1.5 size-3.5" />
              {t("devices.exportCsv")}
            </Button>
          </div>
        )}
      />

      {/* Register New Device Dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{t("devices.registerTitle")}</DialogTitle>
            <DialogDescription>
              {t("devices.registerDescription")}
            </DialogDescription>
          </DialogHeader>
          <DeviceForm categories={categories ?? []} onDone={() => setCreateOpen(false)} />
        </DialogContent>
      </Dialog>

      {/* Edit Device Dialog */}
      <Dialog open={Boolean(editingDevice)} onOpenChange={(o) => !o && setEditingDevice(null)}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{t("devices.editTitle", { assetTag: editingDevice?.assetTag ?? "" })}</DialogTitle>
            <DialogDescription>
              {t("devices.editDescription")}
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

    </div>
  );
}

export const devicesRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/devices",
  validateSearch: deviceSearchSchema,
  component: DevicesPage,
});
