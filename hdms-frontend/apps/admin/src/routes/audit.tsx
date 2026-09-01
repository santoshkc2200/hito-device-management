import {
  type AuditEvent,
  listAuditEvents,
} from "@hdms/api-client";
import { useQuery } from "@tanstack/react-query";
import { createRoute, useNavigate } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import {
  ArrowLeft,
  ArrowRight,
  Code2,
  Download,
  RefreshCw,
  Search,
  ShieldAlert,
  X,
} from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { z } from "zod";

import { DataTable, DataTableColumnHeader, useDataTableColumns } from "@/components/data-table";
import { EmptyState, ErrorState } from "@/components/states";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { useT } from "@/i18n";
import { authenticatedRoute } from "./authenticated";

const auditSearchSchema = z.object({
  actor: z.string().optional(),
  action: z.string().optional(),
  subject: z.string().optional(),
  from: z.string().optional(),
  to: z.string().optional(),
  cursor: z.string().optional(),
});
export type AuditSearch = z.infer<typeof auditSearchSchema>;

function formatDate(iso?: string | null): string {
  if (!iso) return "—";
  try {
    const d = new Date(iso);
    return d.toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  } catch {
    return iso;
  }
}

function downloadFile(url: string, filename: string, successMessage: string) {
  const link = document.createElement("a");
  link.href = url;
  link.setAttribute("download", filename);
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  toast.success(successMessage);
}

function getActionTone(action: string): "default" | "secondary" | "destructive" | "outline" {
  if (action.includes("deleted") || action.includes("archived") || action.includes("revoked") || action.includes("write_off")) {
    return "destructive";
  }
  if (action.includes("created") || action.includes("registered") || action.includes("borrowed") || action.includes("returned")) {
    return "secondary";
  }
  return "outline";
}

const columnHelper = createColumnHelper<AuditEvent>();

function AuditPage() {
  const t = useT();
  const navigate = useNavigate({ from: auditRoute.fullPath });
  const search = auditRoute.useSearch();

  const [actorInput, setActorInput] = useState(search.actor || "");
  const [actionInput, setActionInput] = useState(search.action || "");
  const [subjectInput, setSubjectInput] = useState(search.subject || "");
  const [fromInput, setFromInput] = useState(search.from ? search.from.slice(0, 10) : "");
  const [toInput, setToInput] = useState(search.to ? search.to.slice(0, 10) : "");

  // Modal inspector state
  const [selectedEvent, setSelectedEvent] = useState<AuditEvent | null>(null);

  const {
    data,
    isLoading,
    isError,
    refetch,
    isFetching,
  } = useQuery({

    queryKey: ["audit", search],
    queryFn: async () => {
      const { data, error } = await listAuditEvents({
        query: {
          actor: search.actor || undefined,
          action: search.action || undefined,
          subject: search.subject || undefined,
          from: search.from || undefined,
          to: search.to || undefined,
          cursor: search.cursor || undefined,
          limit: 50,
        },
      });
      if (error) throw error;
      return data;
    },
  });

  const handleApplyFilters = () => {
    navigate({
      search: (prev) => ({
        ...prev,
        actor: actorInput.trim() || undefined,
        action: actionInput.trim() || undefined,
        subject: subjectInput.trim() || undefined,
        from: fromInput ? new Date(`${fromInput}T00:00:00.000Z`).toISOString() : undefined,
        to: toInput ? new Date(`${toInput}T23:59:59.999Z`).toISOString() : undefined,
        cursor: undefined, // Reset cursor on filter change
      }),
    });
  };

  const handleClearFilters = () => {
    setActorInput("");
    setActionInput("");
    setSubjectInput("");
    setFromInput("");
    setToInput("");
    navigate({
      search: () => ({}),
    });
  };

  const handleExportCsv = () => {
    const params = new URLSearchParams();
    if (search.actor) params.set("actor", search.actor);
    if (search.action) params.set("action", search.action);
    if (search.subject) params.set("subject", search.subject);
    if (search.from) params.set("from", search.from);
    if (search.to) params.set("to", search.to);

    const queryStr = params.toString() ? `?${params.toString()}` : "";
    const filename = `audit-log-${new Date().toISOString().slice(0, 10)}.csv`;
    downloadFile(`/v1/audit.csv${queryStr}`, filename, t("audit.exporting", { filename }));
  };

  const columns = useDataTableColumns<AuditEvent>(
    () => [
      columnHelper.accessor("at", {
        id: "at",
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t("audit.columnTimestamp")} />
        ),
        cell: (info) => (
          <span className="font-mono text-xs text-muted-foreground whitespace-nowrap">
            {formatDate(info.getValue())}
          </span>
        ),
      }),
      columnHelper.accessor("actor", {
        id: "actor",
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t("audit.actorLabel")} />
        ),
        cell: (info) => (
          <div className="flex flex-col">
            <span className="font-medium text-xs">{info.getValue()}</span>
            {info.row.original.actorIp && (
              <span className="text-[10px] text-muted-foreground">
                IP: {info.row.original.actorIp}
              </span>
            )}
          </div>
        ),
      }),
      columnHelper.accessor("action", {
        id: "action",
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t("audit.actionLabel")} />
        ),
        cell: (info) => (
          <Badge variant={getActionTone(info.getValue())} className="font-mono text-xs">
            {info.getValue()}
          </Badge>
        ),
      }),
      columnHelper.accessor("subject", {
        id: "subject",
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t("audit.subjectLabel")} />
        ),
        cell: (info) => (
          <span className="font-mono text-xs text-muted-foreground">
            {info.getValue() || "—"}
          </span>
        ),
      }),
      columnHelper.display({
        id: "actions",
        header: () => t("audit.columnDetails"),
        cell: ({ row }) => (
          <Button
            variant="ghost"
            size="sm"
            className="h-7 text-xs gap-1"
            onClick={() => setSelectedEvent(row.original)}
            data-testid={`view-event-${row.original.id}`}
          >
            <Code2 className="h-3.5 w-3.5" />
            <span>{t("audit.payload")}</span>
          </Button>
        ),
      }),
    ],
    [t]
  );


  return (
    <div className="flex flex-col gap-6" data-testid="audit-page">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("audit.title")}</h1>
          <p className="text-sm text-muted-foreground">
            {t("audit.subtitle")}
          </p>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            className="gap-2"
            onClick={handleExportCsv}
            data-testid="export-audit-csv"
          >
            <Download className="h-4 w-4" />
            <span>{t("audit.exportCsv")}</span>
          </Button>
          <Button
            variant="outline"
            size="icon"
            onClick={() => refetch()}
            disabled={isFetching}
            title={t("audit.refreshLog")}
          >
            <RefreshCw className={`h-4 w-4 ${isFetching ? "animate-spin" : ""}`} />
          </Button>
        </div>
      </div>

      {/* Filter Toolbar */}
      <Card className="bg-card/50">
        <CardContent className="p-4 flex flex-wrap items-end gap-3">
          <div className="space-y-1">
            <Label htmlFor="filter-actor" className="text-xs">{t("audit.actorLabel")}</Label>
            <Input
              id="filter-actor"
              placeholder={t("audit.actorPlaceholder")}
              value={actorInput}
              onChange={(e) => setActorInput(e.target.value)}
              className="h-8 w-44 text-xs"
              data-testid="filter-actor-input"
            />
          </div>

          <div className="space-y-1">
            <Label htmlFor="filter-action" className="text-xs">{t("audit.actionLabel")}</Label>
            <Input
              id="filter-action"
              placeholder={t("audit.actionPlaceholder")}
              value={actionInput}
              onChange={(e) => setActionInput(e.target.value)}
              className="h-8 w-44 text-xs"
              data-testid="filter-action-input"
            />
          </div>

          <div className="space-y-1">
            <Label htmlFor="filter-subject" className="text-xs">{t("audit.subjectLabel")}</Label>
            <Input
              id="filter-subject"
              placeholder={t("audit.subjectPlaceholder")}
              value={subjectInput}
              onChange={(e) => setSubjectInput(e.target.value)}
              className="h-8 w-44 text-xs"
              data-testid="filter-subject-input"
            />
          </div>

          <div className="space-y-1">
            <Label htmlFor="filter-from" className="text-xs">{t("audit.fromLabel")}</Label>
            <Input
              id="filter-from"
              type="date"
              value={fromInput}
              onChange={(e) => setFromInput(e.target.value)}
              className="h-8 w-36 text-xs"
            />
          </div>

          <div className="space-y-1">
            <Label htmlFor="filter-to" className="text-xs">{t("audit.toLabel")}</Label>
            <Input
              id="filter-to"
              type="date"
              value={toInput}
              onChange={(e) => setToInput(e.target.value)}
              className="h-8 w-36 text-xs"
            />
          </div>

          <div className="flex items-center gap-2">
            <Button size="sm" onClick={handleApplyFilters} className="h-8 text-xs gap-1.5" data-testid="apply-filters-btn">
              <Search className="h-3.5 w-3.5" />
              <span>{t("audit.filter")}</span>
            </Button>
            <Button size="sm" variant="ghost" onClick={handleClearFilters} className="h-8 text-xs gap-1">
              <X className="h-3.5 w-3.5" />
              <span>{t("audit.reset")}</span>
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* Main Table */}
      {isLoading ? (
        <div className="space-y-3">
          <Skeleton className="h-10 w-full" />
          {Array.from({ length: 8 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
          ))}
        </div>
      ) : isError || !data ? (
        <ErrorState
          title={t("audit.loadFailedTitle")}
          detail={t("audit.loadFailedDetail")}
          onRetry={() => refetch()}
        />
      ) : data.items.length === 0 ? (
        <EmptyState
          icon={ShieldAlert}
          title={t("audit.emptyTitle")}
          explanation={t("audit.emptyExplanation")}
        />
      ) : (
        <div className="space-y-4">
          <DataTable
            columns={columns}
            data={data.items}
          />


          {/* Keyset Cursor Pagination Controls */}
          <div className="flex items-center justify-between text-xs text-muted-foreground pt-2">
            <div>
              Showing {data.items.length} records
            </div>
            <div className="flex items-center gap-2">
              {search.cursor && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() =>
                    navigate({
                      search: (prev) => ({
                        ...prev,
                        cursor: undefined,
                      }),
                    })
                  }
                  className="h-8 text-xs gap-1"
                >
                  <ArrowLeft className="h-3.5 w-3.5" />
                  <span>{t("audit.firstPage")}</span>
                </Button>
              )}
              {data.nextCursor && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() =>
                    navigate({
                      search: (prev) => ({
                        ...prev,
                        cursor: data.nextCursor || undefined,
                      }),
                    })
                  }
                  className="h-8 text-xs gap-1"
                  data-testid="next-page-btn"
                >
                  <span>{t("audit.nextPage")}</span>
                  <ArrowRight className="h-3.5 w-3.5" />
                </Button>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Event Details / JSON Payload Inspector Dialog */}
      <Dialog open={!!selectedEvent} onOpenChange={(open) => !open && setSelectedEvent(null)}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-base">
              <ShieldAlert className="h-5 w-5 text-primary" />
              <span>{t("audit.detailsTitle")}</span>
            </DialogTitle>
            <DialogDescription className="font-mono text-xs">
              {t("audit.eventIdLabel")} {selectedEvent?.id}
            </DialogDescription>
          </DialogHeader>

          {selectedEvent && (
            <div className="space-y-4 py-2 text-xs">
              <div className="grid grid-cols-2 gap-3 p-3 bg-muted/40 rounded-lg">
                <div>
                  <span className="text-muted-foreground block text-[10px]">{t("audit.actionLabel")}</span>
                  <span className="font-semibold">{selectedEvent.action}</span>
                </div>
                <div>
                  <span className="text-muted-foreground block text-[10px]">{t("audit.timestampLabel")}</span>
                  <span>{formatDate(selectedEvent.at)}</span>
                </div>
                <div>
                  <span className="text-muted-foreground block text-[10px]">{t("audit.actorLabel")}</span>
                  <span className="font-mono">{selectedEvent.actor}</span>
                </div>
                <div>
                  <span className="text-muted-foreground block text-[10px]">{t("audit.subjectLabel")}</span>
                  <span className="font-mono">{selectedEvent.subject || "—"}</span>
                </div>
                {selectedEvent.actorIp && (
                  <div>
                    <span className="text-muted-foreground block text-[10px]">{t("audit.actorIpLabel")}</span>
                    <span className="font-mono">{selectedEvent.actorIp}</span>
                  </div>
                )}
                {selectedEvent.requestId && (
                  <div>
                    <span className="text-muted-foreground block text-[10px]">{t("audit.requestIdLabel")}</span>
                    <span className="font-mono">{selectedEvent.requestId}</span>
                  </div>
                )}
              </div>

              <div className="space-y-1.5">
                <Label className="text-xs font-semibold">{t("audit.payloadJsonLabel")}</Label>
                <pre className="p-4 bg-secondary/80 text-foreground font-mono text-xs rounded-lg overflow-x-auto max-h-72 whitespace-pre-wrap">
                  {JSON.stringify(selectedEvent.payload, null, 2)}
                </pre>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}

export const auditRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/audit",
  validateSearch: auditSearchSchema,
  component: AuditPage,
});
