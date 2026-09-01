/**
 * Paper Backfill (Phase 4.6)
 *
 * Keyboard-first data entry for transcribing paper register slips.
 * NFR-9b: under 4 minutes for 12 rows (< 20 s / row).
 *
 * Design rules (from docs/phases/phase-4/4.6-paper-backfill.md):
 *  - Tab: Device → Person → Action → Out → In → Note
 *  - Enter: commits row, refocuses cleared Device field
 *  - Escape: resets in-progress row, staged rows untouched
 *  - Server resolves borrow-vs-return; client never reimplements the rule
 *  - Staged rows persisted to localStorage keyed by paperRef
 *  - Debounced preview surfaces conflicts while the paper page is in hand
 */
import {
  type BackfillAction,
  type BackfillConflict,
  type BackfillResolution,
  type BackfillRow,
  type BackfillRowResult,
  type BackfillRowStatus,
  type BackfillUserRef,
  listDepartments,
  listUsers,
  previewBackfillBatch,
  recordBackfillBatch,
} from "@hdms/api-client";
import { parseForgivingTime } from "@hdms/domain";
import { useQuery } from "@tanstack/react-query";
import { createRoute, useNavigate } from "@tanstack/react-router";
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Clock,
  Edit2,
  FileSpreadsheet,
  Loader2,
  Plus,
  Save,
  Trash2,
  UserPlus,
  X,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { toast } from "sonner";
import { InlineUserDialog } from "@/components/backfill/inline-user-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useT } from "@/i18n";
import { authenticatedRoute } from "./authenticated";

// ─────────────────────────────────────────────────────────────
// Types
// ─────────────────────────────────────────────────────────────

interface StagedPerson {
  /** discriminated union matching BackfillUserRef */
  ref: BackfillUserRef;
  /** human-readable display label for the staged row table */
  label: string;
  /** true when person was created inline during this session */
  isNew: boolean;
}

interface StagedRow {
  clientRowId: string;
  deviceRef: string;
  /** null until device resolves */
  deviceLabel: string | null;
  person: StagedPerson | null;
  /** true = admin manually set; false = server-auto-detected */
  actionOverridden: boolean;
  action: BackfillAction | null;
  outTime: string | null; // ISO with TZ offset
  inTime: string | null; // ISO with TZ offset, null = open loan
  note: string;
}

// ─────────────────────────────────────────────────────────────
// Blank in-progress entry row
// ─────────────────────────────────────────────────────────────

function blankEntry(): StagedRow {
  return {
    clientRowId: crypto.randomUUID(),
    deviceRef: "",
    deviceLabel: null,
    person: null,
    actionOverridden: false,
    action: null,
    outTime: null,
    inTime: null,
    note: "",
  };
}

// ─────────────────────────────────────────────────────────────
// Local storage helpers — crash-resilient staging (4.6c)
// ─────────────────────────────────────────────────────────────

const LS_REF_KEY = "hdms_backfill_context_ref";
const LS_DATE_KEY = "hdms_backfill_context_date";

function lsRowsKey(paperRef: string) {
  return `hdms_backfill_staged_${paperRef}`;
}

function readStagedRows(paperRef: string): StagedRow[] {
  try {
    const raw = localStorage.getItem(lsRowsKey(paperRef));
    if (raw) return JSON.parse(raw) as StagedRow[];
  } catch {
    /* ignore */
  }
  return [];
}

function writeStagedRows(paperRef: string, rows: StagedRow[]) {
  try {
    localStorage.setItem(lsRowsKey(paperRef), JSON.stringify(rows));
  } catch {
    /* ignore */
  }
}

function clearStagedRows(paperRef: string) {
  try {
    localStorage.removeItem(lsRowsKey(paperRef));
  } catch {
    /* ignore */
  }
}

// ─────────────────────────────────────────────────────────────
// Debounce hook
// ─────────────────────────────────────────────────────────────

function useDebounce<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(id);
  }, [value, delay]);
  return debounced;
}

// ─────────────────────────────────────────────────────────────
// Preview results index (clientRowId → BackfillRowResult)
// ─────────────────────────────────────────────────────────────

type PreviewMap = Map<string, BackfillRowResult>;

// ─────────────────────────────────────────────────────────────
// Time input with inline parse preview
// ─────────────────────────────────────────────────────────────

function TimeInput({
  id,
  label,
  value,
  onChange,
  baseDate,
  tabIndex,
  onEnter,
  onEscape,
  inputRef,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string, iso: string | null) => void;
  baseDate: Date;
  tabIndex?: number;
  onEnter?: () => void;
  onEscape?: () => void;
  inputRef?: React.Ref<HTMLInputElement>;
}) {
  const t = useT();
  const parsed = value ? parseForgivingTime(value, baseDate) : null;
  const preview = parsed?.ok ? parsed.formatted : null;
  const error = parsed && !parsed.ok ? parsed.error : null;

  return (
    <div className="relative">
      <Field>
        <FieldLabel htmlFor={id}>{label}</FieldLabel>
        <div className="relative">
          <Input
            id={id}
            ref={inputRef}
            value={value}
            onChange={(e) => {
              const raw = e.target.value;
              const p = raw ? parseForgivingTime(raw, baseDate) : null;
              onChange(raw, p?.ok ? p.iso : null);
            }}
            placeholder={t("backfill.timePlaceholder")}
            className={error ? "border-red-500" : undefined}
            tabIndex={tabIndex}
            onKeyDown={(e) => {
              if (e.key === "Enter") { e.preventDefault(); onEnter?.(); }
              if (e.key === "Escape") { e.preventDefault(); onEscape?.(); }
            }}
          />
          {preview && (
            <span className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 flex items-center gap-1 text-xs text-muted-foreground">
              <Clock className="h-3 w-3" />
              {preview}
            </span>
          )}
        </div>
        {error && <p className="text-xs text-red-500 mt-0.5">{error}</p>}
      </Field>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Person typeahead with inline create
// ─────────────────────────────────────────────────────────────

function PersonInput({
  value,
  person,
  onPersonSelect,
  onCreateNew,
  tabIndex,
  onEnter,
  onEscape,
  inputRef,
}: {
  value: string;
  person: StagedPerson | null;
  onPersonSelect: (p: StagedPerson | null, raw: string) => void;
  onCreateNew: (name: string) => void;
  tabIndex?: number;
  onEnter?: () => void;
  onEscape?: () => void;
  inputRef?: React.Ref<HTMLInputElement>;
}) {
  const t = useT();
  const [showDropdown, setShowDropdown] = useState(false);
  const wrapperRef = useRef<HTMLDivElement>(null);

  const { data: deptList } = useQuery({
    queryKey: ["departments"],
    queryFn: async () => {
      const { data, error } = await listDepartments();
      if (error) throw error;
      return data.items;
    },
    staleTime: 5 * 60_000,
  });

  const deptMap = useMemo(
    () => new Map(deptList?.map((d) => [d.id, d.name])),
    [deptList],
  );

  const { data: results, isFetching } = useQuery({
    queryKey: ["users", "search", value],
    queryFn: async () => {
      if (!value.trim() || value.length < 2 || person) return [];
      const { data, error } = await listUsers({
        query: { q: value.trim(), limit: 8 },
      });
      if (error) throw error;
      return data.items;
    },
    enabled: value.trim().length >= 2 && !person,
    staleTime: 15_000,
  });

  // Close on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) {
        setShowDropdown(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, []);

  const hasResults = results && results.length > 0;

  return (
    <div ref={wrapperRef} className="relative">
      <Field>
        <FieldLabel htmlFor="pv-person">{t("backfill.personLabel")}</FieldLabel>
        <div className="flex gap-1">
          <div className="relative flex-1">
            <Input
              id="pv-person"
              ref={inputRef}
              value={person ? person.label : value}
              onChange={(e) => {
                if (person) onPersonSelect(null, e.target.value);
                else onPersonSelect(null, e.target.value);
                setShowDropdown(true);
              }}
              onFocus={() => {
                if (!person) setShowDropdown(true);
              }}
              placeholder={t("backfill.personPlaceholder")}
              tabIndex={tabIndex}
              onKeyDown={(e) => {
                if (e.key === "Escape") {
                  e.preventDefault();
                  if (person) { onPersonSelect(null, ""); setShowDropdown(false); }
                  else onEscape?.();
                }
                if (e.key === "Enter") { e.preventDefault(); onEnter?.(); }
              }}
            />
            {isFetching && (
              <Loader2 className="absolute right-2 top-1/2 -translate-y-1/2 h-3 w-3 animate-spin text-muted-foreground" />
            )}
            {person?.isNew && (
              <Badge variant="secondary" className="absolute right-2 top-1/2 -translate-y-1/2 text-[10px] py-0">
                {t("backfill.newBadge")}
              </Badge>
            )}
          </div>
          <Button
            type="button"
            variant="outline"
            size="icon"
            tabIndex={-1}
            aria-label={t("backfill.createNewPersonAria")}
            onClick={() => onCreateNew(value)}
            className="shrink-0"
          >
            <UserPlus className="h-4 w-4" />
          </Button>
        </div>
      </Field>

      {showDropdown && !person && (hasResults || value.length >= 2) && (
        <div className="absolute z-50 mt-1 w-full rounded-md border bg-popover shadow-md">
          {hasResults ? (
            <ul role="listbox" className="max-h-48 overflow-y-auto py-1">
              {results!.map((u) => {
                const deptName = u.departmentId ? deptMap.get(u.departmentId) : undefined;
                return (
                  <li key={u.id}>
                    <button
                      type="button"
                      className="flex w-full flex-col px-3 py-2 text-left text-sm hover:bg-accent focus:bg-accent focus:outline-none"
                      onClick={() => {
                        onPersonSelect(
                          {
                            ref: { userId: u.id },
                            label: `${u.fullName}${u.employeeNo ? ` (${u.employeeNo})` : ""}${deptName ? ` · ${deptName}` : ""}`,
                            isNew: false,
                          },
                          u.fullName,
                        );
                        setShowDropdown(false);
                      }}
                    >
                      <span className="font-medium">{u.fullName}</span>
                      <span className="text-xs text-muted-foreground">
                        {[u.employeeNo, deptName].filter(Boolean).join(" · ")}
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          ) : (
            <div className="px-3 py-2 text-sm text-muted-foreground">
              {t("backfill.noMatches")}{" "}
              <button
                type="button"
                className="text-primary underline hover:no-underline"
                onClick={() => { setShowDropdown(false); onCreateNew(value); }}
              >
                {t("backfill.createNewPerson")}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Status / conflict badge helpers
// ─────────────────────────────────────────────────────────────

function statusLabel(t: ReturnType<typeof useT>, s: BackfillRowStatus): string {
  switch (s) {
    case "ok": return t("backfill.statusOk");
    case "conflict": return t("backfill.statusConflict");
    case "unresolved": return t("backfill.statusUnresolved");
    case "discarded": return t("backfill.statusDiscarded");
  }
}

function statusVariant(s: BackfillRowStatus): "default" | "secondary" | "destructive" | "outline" {
  switch (s) {
    case "ok": return "default";
    case "conflict": return "destructive";
    case "unresolved": return "destructive";
    case "discarded": return "secondary";
  }
}

// ─────────────────────────────────────────────────────────────
// Conflict panel (4.6d)
// ─────────────────────────────────────────────────────────────

function ConflictPanel({
  rowIndex,
  row,
  conflict,
  onResolve,
}: {
  rowIndex: number;
  row: StagedRow;
  conflict: BackfillConflict;
  onResolve: (resolution: BackfillResolution) => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(true);
  const el = conflict.existingLoan;

  return (
    <div className="col-span-full rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm">
      <button
        type="button"
        className="flex w-full items-center justify-between font-medium text-destructive"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
      >
        <span>{t("backfill.conflictHeading", { row: rowIndex + 1 })}</span>
        {open ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
      </button>

      {open && (
        <div className="mt-3 space-y-3">
          {/* Side-by-side comparison */}
          <div className="grid grid-cols-2 gap-4 text-xs">
            <div className="rounded border bg-background p-3">
              <p className="mb-1 font-semibold uppercase tracking-wide text-muted-foreground">
                {t("backfill.existingRecord")}
              </p>
              <p className="font-medium">{el.userDisplay}</p>
              {el.department && <p className="text-muted-foreground">{el.department}</p>}
              <p className="mt-1">
                {t("backfill.outAt", {
                  time: new Date(el.borrowedAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
                })}
                {el.returnedAt
                  ? t("backfill.inAt", {
                      time: new Date(el.returnedAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
                    })
                  : t("backfill.stillOnLoan")}
              </p>
              <p className="mt-1 text-muted-foreground">{t("backfill.originLabel", { origin: el.origin })}</p>
            </div>

            <div className="rounded border bg-background p-3">
              <p className="mb-1 font-semibold uppercase tracking-wide text-muted-foreground">
                {t("backfill.paperEntry")}
              </p>
              <p className="font-medium">{row.person?.label ?? "—"}</p>
              <p className="mt-1">
                {row.deviceRef}
                {row.deviceLabel ? ` — ${row.deviceLabel}` : ""}
              </p>
              <p className="mt-1">
                {row.outTime
                  ? t("backfill.outAt", {
                      time: new Date(row.outTime).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
                    })
                  : t("backfill.outUnknown")}
                {row.inTime
                  ? t("backfill.inAt", {
                      time: new Date(row.inTime).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
                    })
                  : ""}
              </p>
              <p className="mt-1 text-muted-foreground">{t("backfill.actionValueLabel", { action: row.action ?? "auto" })}</p>
            </div>
          </div>

          {/* Resolution buttons */}
          <div className="flex flex-wrap gap-2">
            {conflict.resolutions.includes("truncate-existing") && (
              <Button size="sm" variant="outline" onClick={() => onResolve("truncate-existing")}>
                {t("backfill.resolveTruncate")}
              </Button>
            )}
            {conflict.resolutions.includes("change-device") && (
              <Button size="sm" variant="outline" onClick={() => onResolve("change-device")}>
                {t("backfill.resolveChangeDevice")}
              </Button>
            )}
            {conflict.resolutions.includes("discard-row") && (
              <Button size="sm" variant="outline" onClick={() => onResolve("discard-row")}>
                {t("backfill.resolveDiscard")}
              </Button>
            )}
            {conflict.resolutions.includes("record-as-disputed") && (
              <Button
                size="sm"
                variant="outline"
                className="border-amber-500 text-amber-700 hover:bg-amber-50"
                onClick={() => onResolve("record-as-disputed")}
              >
                {t("backfill.resolveDisputed")}
              </Button>
            )}
          </div>

          <p className="text-xs text-muted-foreground">
            <strong>{t("backfill.disputedNoteLead")}</strong>
            {t("backfill.disputedNoteRest")}
          </p>
        </div>
      )}
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Post-commit follow-through dialog (4.6e)
// ─────────────────────────────────────────────────────────────

interface CommitSummary {
  paperRef: string;
  totalRows: number;
  newUsers: Array<{ userId: string; name: string }>;
}

function FollowThroughDialog({
  summary,
  onRecordAnother,
  onDone,
}: {
  summary: CommitSummary;
  onRecordAnother: () => void;
  onDone: () => void;
}) {
  const t = useT();
  const navigate = useNavigate();

  return (
    <Dialog open>
      <DialogContent className="sm:max-w-lg" data-testid="commit-success-dialog">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-green-700">
            <CheckCircle2 className="h-5 w-5" />
            {summary.totalRows === 1
              ? t("backfill.committedOne", { count: summary.totalRows })
              : t("backfill.committedOther", { count: summary.totalRows })}
          </DialogTitle>
          <DialogDescription>
            {t("backfill.fromPageReference")} <strong>{summary.paperRef}</strong>
          </DialogDescription>
        </DialogHeader>

        {summary.newUsers.length > 0 && (
          <div className="rounded-md border border-amber-300 bg-amber-50 p-4">
            <p className="mb-2 font-medium text-amber-800">
              {summary.newUsers.length === 1
                ? t("backfill.newPeopleOne", { count: summary.newUsers.length })
                : t("backfill.newPeopleOther", { count: summary.newUsers.length })}
            </p>
            <ul className="mb-3 space-y-1 text-sm text-amber-700">
              {summary.newUsers.map((u) => (
                <li key={u.userId}>• {u.name}</li>
              ))}
            </ul>
            <Button
              className="w-full"
              onClick={() => {
                const ids = summary.newUsers.map((u) => u.userId).join(",");
                navigate({ to: "/credentials", search: { issueFor: ids } as never });
              }}
            >
              <UserPlus className="mr-2 h-4 w-4" />
              {summary.newUsers.length === 1
                ? t("backfill.issueCardsOne", { count: summary.newUsers.length })
                : t("backfill.issueCardsOther", { count: summary.newUsers.length })}
            </Button>
          </div>
        )}

        <DialogFooter className="gap-2 sm:flex-row">
          <Button variant="outline" onClick={onRecordAnother}>
            {t("backfill.recordAnother")}
          </Button>
          <Button onClick={onDone}>{t("backfill.done")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ─────────────────────────────────────────────────────────────
// Main BackfillPage component
// ─────────────────────────────────────────────────────────────

function BackfillPage() {
  const t = useT();
  // ── Sticky context (4.6c) ──
  const [paperRef, setPaperRef] = useState<string>(() => {
    return localStorage.getItem(LS_REF_KEY) ?? "";
  });
  const [pageDate, setPageDate] = useState<string>(() => {
    return (
      localStorage.getItem(LS_DATE_KEY) ??
      new Date(Date.now() - 86_400_000).toISOString().slice(0, 10) // yesterday
    );
  });

  useEffect(() => { localStorage.setItem(LS_REF_KEY, paperRef); }, [paperRef]);
  useEffect(() => { localStorage.setItem(LS_DATE_KEY, pageDate); }, [pageDate]);

  const baseDate = useMemo(() => new Date(`${pageDate}T00:00:00`), [pageDate]);

  // ── Staged rows (4.6c) ──
  const [staged, setStaged] = useState<StagedRow[]>(() =>
    paperRef ? readStagedRows(paperRef) : [],
  );

  // Sync staged rows with localStorage and paperRef changes
  const activePaperRef = useRef(paperRef);
  useEffect(() => {
    if (paperRef !== activePaperRef.current) {
      activePaperRef.current = paperRef;
      setStaged(paperRef ? readStagedRows(paperRef) : []);
    } else if (paperRef) {
      writeStagedRows(paperRef, staged);
    }
  }, [staged, paperRef]);

  // ── Per-staged-row resolutions chosen by admin ──
  const [resolutions, setResolutions] = useState<Map<string, BackfillResolution>>(
    new Map(),
  );

  // ── In-progress entry row ──
  const [entry, setEntry] = useState<StagedRow>(blankEntry);
  const [outTimeRaw, setOutTimeRaw] = useState("");
  const [inTimeRaw, setInTimeRaw] = useState("");
  const [personSearch, setPersonSearch] = useState("");

  // Focus refs for keyboard navigation
  const deviceRef = useRef<HTMLInputElement>(null);
  const outTimeRef = useRef<HTMLInputElement>(null);
  const inTimeRef = useRef<HTMLInputElement>(null);

  // ── Inline create person dialog ──
  const [createPersonOpen, setCreatePersonOpen] = useState(false);
  const [createPersonInitialName, setCreatePersonInitialName] = useState("");

  // ── Preview (4.6d) ──
  const debouncedStaged = useDebounce(staged, 600);

  // Build the preview batch from staged rows
  const previewBatch = useMemo(() => {
    if (!paperRef) return null;
    const rows: BackfillRow[] = staged.map((r) => ({
      clientRowId: r.clientRowId,
      deviceRef: r.deviceRef,
      userRef: r.person?.ref ?? {},
      borrowedAt: r.outTime ?? new Date().toISOString(),
      returnedAt: r.inTime ?? undefined,
      action: r.action ?? undefined,
      resolution: resolutions.get(r.clientRowId) ?? undefined,
      note: r.note || undefined,
    }));
    if (rows.length === 0) return null;
    return { paperRef, rows };
  }, [debouncedStaged, paperRef, resolutions]); // eslint-disable-line react-hooks/exhaustive-deps

  const [previewMap, setPreviewMap] = useState<PreviewMap>(new Map());
  const [isPreviewing, setIsPreviewing] = useState(false);
  const previewAbortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    if (!previewBatch) { setPreviewMap(new Map()); return; }
    previewAbortRef.current?.abort();
    const ctrl = new AbortController();
    previewAbortRef.current = ctrl;
    setIsPreviewing(true);

    previewBackfillBatch({ body: previewBatch })
      .then(({ data, error }) => {
        if (ctrl.signal.aborted) return;
        if (!error && data) {
          const m = new Map<string, BackfillRowResult>();
          for (const row of data.rows) m.set(row.clientRowId, row);
          setPreviewMap(m);
        }
      })
      .finally(() => {
        if (!ctrl.signal.aborted) setIsPreviewing(false);
      });

    return () => ctrl.abort();
  }, [previewBatch]);

  // ── Conflict count ──
  const unresolvedCount = useMemo(() => {
    return staged.filter((r) => {
      const pv = previewMap.get(r.clientRowId);
      if (!pv) return false;
      if (pv.status === "conflict" && !resolutions.has(r.clientRowId)) return true;
      // change-device is client-side: row still carrying old assetTag is unresolved
      if (resolutions.get(r.clientRowId) === "change-device") return true;
      return false;
    }).length;
  }, [staged, previewMap, resolutions]);

  // ── Commit batch (4.6e) ──
  const [commitSummary, setCommitSummary] = useState<CommitSummary | null>(null);
  const [isCommitting, setIsCommitting] = useState(false);
  const [discardConfirmOpen, setDiscardConfirmOpen] = useState(false);

  const handleCommit = async () => {
    if (!paperRef || staged.length === 0 || unresolvedCount > 0) return;
    setIsCommitting(true);
    try {
      const rows: BackfillRow[] = staged.map((r) => ({
        clientRowId: r.clientRowId,
        deviceRef: r.deviceRef,
        userRef: r.person?.ref ?? {},
        borrowedAt: r.outTime ?? new Date().toISOString(),
        returnedAt: r.inTime ?? undefined,
        action: r.action ?? undefined,
        resolution: resolutions.get(r.clientRowId) ?? undefined,
        note: r.note || undefined,
      }));
      const { data, error } = await recordBackfillBatch({ body: { paperRef, rows } });
      if (error) {
        toast.error(
          t("backfill.commitFailed", {
            message:
              (error as { message?: string }).message ?? t("backfill.commitUnknownError"),
          }),
        );
        return;
      }
      if (!data.committed) {
        toast.error(t("backfill.notCommitted"));
        return;
      }

      // Build follow-through summary (4.6e)
      const newUsers = data.rows
        .filter((r) => r.createsUser && r.userId)
        .map((r) => ({
          userId: r.userId!,
          name: staged.find((s) => s.clientRowId === r.clientRowId)?.person?.label ?? r.userId!,
        }));

      clearStagedRows(paperRef);
      setStaged([]);
      setResolutions(new Map());
      setPreviewMap(new Map());
      setCommitSummary({ paperRef, totalRows: staged.length, newUsers });
    } finally {
      setIsCommitting(false);
    }
  };

  // ── Commit row to staged list ──
  const commitRow = useCallback(() => {
    const e = entry;
    if (!e.deviceRef.trim()) return; // need at least a device

    const newRow: StagedRow = {
      ...e,
      clientRowId: e.clientRowId,
      outTime: e.outTime,
      inTime: e.inTime,
    };

    setStaged((prev) => [...prev, newRow]);
    setEntry(blankEntry());
    setOutTimeRaw("");
    setInTimeRaw("");
    setPersonSearch("");

    // Refocus device field (the most critical UX rule)
    requestAnimationFrame(() => deviceRef.current?.focus());
  }, [entry]);

  // ── Remove / edit staged row ──
  const removeRow = (id: string) => {
    setStaged((prev) => prev.filter((r) => r.clientRowId !== id));
    setResolutions((prev) => { const m = new Map(prev); m.delete(id); return m; });
  };

  const editRow = (row: StagedRow) => {
    setEntry({ ...row, clientRowId: crypto.randomUUID() }); // keep old id in staged until user re-commits
    setOutTimeRaw(row.outTime ? formatTimeFromISO(row.outTime) : "");
    setInTimeRaw(row.inTime ? formatTimeFromISO(row.inTime) : "");
    setPersonSearch(row.person?.label ?? "");
    removeRow(row.clientRowId);
    requestAnimationFrame(() => deviceRef.current?.focus());
  };

  // ── Reset in-progress row (Escape) ──
  const resetEntry = useCallback(() => {
    setEntry(blankEntry());
    setOutTimeRaw("");
    setInTimeRaw("");
    setPersonSearch("");
    requestAnimationFrame(() => deviceRef.current?.focus());
  }, []);

  // ── Global Ctrl+Enter to commit ──
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.ctrlKey && e.key === "Enter") commitRow();
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [commitRow]);

  function formatTimeFromISO(iso: string): string {
    const d = new Date(iso);
    return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  }

  const canSave =
    paperRef.trim().length > 0 &&
    staged.length > 0 &&
    unresolvedCount === 0 &&
    !isCommitting;

  return (
    <div className="flex flex-col gap-6">
      {/* ── Header ─────────────────────────────────── */}
      <div>
        <h1 className="text-xl font-semibold flex items-center gap-2">
          <FileSpreadsheet className="h-5 w-5" />
          {t("backfill.title")}
        </h1>
        <p className="text-sm text-muted-foreground mt-1">
          {t("backfill.keyboardHintPrefix")}
          <kbd className="rounded border px-1 py-0.5 text-xs font-mono">{t("backfill.keyboardHintEnter")}</kbd>
          {t("backfill.keyboardHintMiddle")}
          <kbd className="rounded border px-1 py-0.5 text-xs font-mono">{t("backfill.keyboardHintEscape")}</kbd>
          {t("backfill.keyboardHintSuffix")}
        </p>
      </div>

      {/* ── Sticky context (4.6c) ──────────────────── */}
      <div className="flex flex-wrap gap-4 rounded-md border bg-muted/30 p-4">
        <Field className="flex-1 min-w-48">
          <FieldLabel htmlFor="ctx-ref">
            {t("backfill.pageReferenceLabel")}
          </FieldLabel>
          <Input
            id="ctx-ref"
            value={paperRef}
            onChange={(e) => setPaperRef(e.target.value)}
            placeholder={t("backfill.pageReferencePlaceholder")}
          />
        </Field>
        <Field className="flex-1 min-w-40">
          <FieldLabel htmlFor="ctx-date">
            {t("backfill.pageDateLabel")}
          </FieldLabel>
          <Input
            id="ctx-date"
            type="date"
            value={pageDate}
            onChange={(e) => setPageDate(e.target.value)}
          />
        </Field>
      </div>

      {!paperRef.trim() && (
        <div className="rounded-md border border-amber-300 bg-amber-50 p-3 text-sm text-amber-700">
          {t("backfill.setPageReferenceHint")}
        </div>
      )}

      {/* ── Entry bar (4.6a, 4.6b) ────────────────── */}
      {paperRef.trim() && (
        <div className="rounded-md border p-4 space-y-3">
          <h2 className="text-sm font-medium">{t("backfill.newEntryHeading")}</h2>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {/* Device */}
            <Field>
              <FieldLabel htmlFor="entry-device">
                {t("backfill.deviceLabel")}
              </FieldLabel>
              <Input
                id="entry-device"
                ref={deviceRef}
                value={entry.deviceRef}
                onChange={(e) =>
                  setEntry((prev) => ({
                    ...prev,
                    deviceRef: e.target.value,
                    deviceLabel: null,
                  }))
                }
                placeholder={t("backfill.devicePlaceholder")}
                autoFocus
                onKeyDown={(e) => {
                  if (e.key === "Enter") { e.preventDefault(); commitRow(); }
                  if (e.key === "Escape") { e.preventDefault(); resetEntry(); }
                }}
              />
            </Field>

            {/* Person */}
            <PersonInput
              value={personSearch}
              person={entry.person}
              onPersonSelect={(p, raw) => {
                setPersonSearch(raw);
                setEntry((prev) => ({ ...prev, person: p }));
              }}
              onCreateNew={(name) => {
                setCreatePersonInitialName(name);
                setCreatePersonOpen(true);
              }}
              onEnter={commitRow}
              onEscape={resetEntry}
            />

            {/* Action */}
            <Field>
              <FieldLabel htmlFor="entry-action">
                {t("backfill.actionLabel")}
                {entry.actionOverridden && (
                  <Badge variant="secondary" className="ml-2 text-[10px]">
                    {t("backfill.overrideBadge")}
                  </Badge>
                )}
              </FieldLabel>
              <Select
                value={entry.action ?? "auto"}
                onValueChange={(v) =>
                  setEntry((prev) => ({
                    ...prev,
                    action: v === "auto" ? null : (v as BackfillAction),
                    actionOverridden: v !== "auto",
                  }))
                }
              >
                <SelectTrigger id="entry-action">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="auto">{t("backfill.actionAuto")}</SelectItem>
                  <SelectItem value="borrow">{t("backfill.actionBorrow")}</SelectItem>
                  <SelectItem value="return">{t("backfill.actionReturn")}</SelectItem>
                </SelectContent>
              </Select>
            </Field>

            {/* Out time */}
            <TimeInput
              id="entry-out"
              label={t("backfill.outTimeLabel")}
              value={outTimeRaw}
              onChange={(raw, iso) => {
                setOutTimeRaw(raw);
                setEntry((prev) => ({ ...prev, outTime: iso }));
              }}
              baseDate={baseDate}
              inputRef={outTimeRef}
              onEnter={commitRow}
              onEscape={resetEntry}
            />

            {/* In time */}
            <TimeInput
              id="entry-in"
              label={t("backfill.inTimeLabel")}
              value={inTimeRaw}
              onChange={(raw, iso) => {
                setInTimeRaw(raw);
                setEntry((prev) => ({ ...prev, inTime: iso }));
              }}
              baseDate={baseDate}
              inputRef={inTimeRef}
              onEnter={commitRow}
              onEscape={resetEntry}
            />

            {/* Note */}
            <Field>
              <FieldLabel htmlFor="entry-note">{t("backfill.noteLabel")}</FieldLabel>
              <Input
                id="entry-note"
                value={entry.note}
                onChange={(e) =>
                  setEntry((prev) => ({ ...prev, note: e.target.value }))
                }
                placeholder={t("backfill.notePlaceholder")}
                onKeyDown={(e) => {
                  if (e.key === "Enter") { e.preventDefault(); commitRow(); }
                  if (e.key === "Escape") { e.preventDefault(); resetEntry(); }
                }}
              />
            </Field>
          </div>

          <div className="flex gap-2">
            <Button
              onClick={commitRow}
              disabled={!entry.deviceRef.trim()}
            >
              <Plus className="mr-1 h-4 w-4" />
              {t("backfill.stageRow")}
            </Button>
            <Button variant="outline" onClick={resetEntry}>
              <X className="mr-1 h-4 w-4" />
              {t("backfill.clearEntry")}
            </Button>
          </div>
        </div>
      )}

      {/* ── Staged rows table (4.6c, 4.6d) ──────────── */}
      {staged.length > 0 && (
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-medium">
              {t("backfill.stagedRowsHeading", { count: staged.length })}
              {isPreviewing && (
                <Loader2 className="ml-2 inline h-3 w-3 animate-spin text-muted-foreground" />
              )}
            </h2>
            <Button
              variant="ghost"
              size="sm"
              className="text-destructive hover:text-destructive"
              onClick={() => setDiscardConfirmOpen(true)}
            >
              <Trash2 className="mr-1 h-3 w-3" />
              {t("backfill.discardAll", { count: staged.length })}
            </Button>
          </div>

          <div className="overflow-x-auto rounded-md border">
            <table className="w-full text-sm">
              <thead className="bg-muted/40 text-xs uppercase tracking-wide text-muted-foreground">
                <tr>
                  <th className="px-3 py-2 text-left w-8">#</th>
                  <th className="px-3 py-2 text-left">{t("backfill.colDevice")}</th>
                  <th className="px-3 py-2 text-left">{t("backfill.colPerson")}</th>
                  <th className="px-3 py-2 text-left">{t("backfill.colAction")}</th>
                  <th className="px-3 py-2 text-left">{t("backfill.colTimes")}</th>
                  <th className="px-3 py-2 text-left">{t("backfill.colStatus")}</th>
                  <th className="px-3 py-2 text-right">{t("backfill.colActions")}</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {staged.map((row, idx) => {
                  const pv = previewMap.get(row.clientRowId);
                  const status = pv?.status;
                  const conflict = pv?.conflict;
                  const resolution = resolutions.get(row.clientRowId);
                  const serverAction = pv?.action;

                  return (
                    <>
                      <tr key={row.clientRowId} className="hover:bg-muted/20">
                        <td className="px-3 py-2 text-muted-foreground">{idx + 1}</td>
                        <td className="px-3 py-2 font-mono text-xs">
                          {row.deviceRef}
                          {pv?.device && (
                            <span className="ml-1 text-muted-foreground">{pv.device.name}</span>
                          )}
                        </td>
                        <td className="px-3 py-2">
                          {row.person ? (
                            <span>
                              {row.person.label}
                              {row.person.isNew && (
                                <Badge variant="secondary" className="ml-1 text-[10px]">{t("backfill.newBadge")}</Badge>
                              )}
                            </span>
                          ) : (
                            <span className="text-muted-foreground">—</span>
                          )}
                        </td>
                        <td className="px-3 py-2">
                          {serverAction ?? row.action ?? (
                            <span className="text-muted-foreground">{t("backfill.autoFallback")}</span>
                          )}
                          {row.actionOverridden && (
                            <Badge variant="outline" className="ml-1 text-[10px]">{t("backfill.overrideBadge")}</Badge>
                          )}
                        </td>
                        <td className="px-3 py-2 text-xs text-muted-foreground">
                          {row.outTime
                            ? new Date(row.outTime).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
                            : "—"}
                          {row.inTime
                            ? ` → ${new Date(row.inTime).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
                            : ""}
                        </td>
                        <td className="px-3 py-2">
                          {status ? (
                            <Badge variant={statusVariant(status)}>
                              {resolution ? `${statusLabel(t, status)} (${resolution})` : statusLabel(t, status)}
                            </Badge>
                          ) : (
                            <span className="text-muted-foreground text-xs">—</span>
                          )}
                        </td>
                        <td className="px-3 py-2 text-right">
                          <div className="flex justify-end gap-1">
                            <Button
                              size="icon"
                              variant="ghost"
                              className="h-7 w-7"
                              onClick={() => editRow(row)}
                              aria-label={t("backfill.editRowAria")}
                            >
                              <Edit2 className="h-3 w-3" />
                            </Button>
                            <Button
                              size="icon"
                              variant="ghost"
                              className="h-7 w-7 text-destructive hover:text-destructive"
                              onClick={() => removeRow(row.clientRowId)}
                              aria-label={t("backfill.removeRowAria")}
                            >
                              <Trash2 className="h-3 w-3" />
                            </Button>
                          </div>
                        </td>
                      </tr>

                      {/* Conflict panel inline below the conflicting row */}
                      {conflict && !resolution && (
                        <tr key={`${row.clientRowId}-conflict`}>
                          <td colSpan={7} className="px-3 py-2">
                            <ConflictPanel
                              rowIndex={idx}
                              row={row}
                              conflict={conflict}
                              onResolve={(res) => {
                                if (res === "discard-row") {
                                  removeRow(row.clientRowId);
                                } else {
                                  setResolutions((prev) => {
                                    const m = new Map(prev);
                                    m.set(row.clientRowId, res);
                                    return m;
                                  });
                                }
                              }}
                            />
                          </td>
                        </tr>
                      )}
                    </>
                  );
                })}
              </tbody>
            </table>
          </div>

          {/* Save bar */}
          <div className="flex items-center justify-between rounded-md border bg-muted/20 px-4 py-3">
            <div>
              {unresolvedCount > 0 ? (
                <p className="text-sm text-destructive font-medium flex items-center gap-1">
                  <AlertTriangle className="h-4 w-4" />
                  {unresolvedCount === 1
                    ? t("backfill.unresolvedOne", { count: unresolvedCount })
                    : t("backfill.unresolvedOther", { count: unresolvedCount })}
                </p>
              ) : (
                <p className="text-sm text-muted-foreground">
                  {staged.length === 1
                    ? t("backfill.readyOne", { count: staged.length })
                    : t("backfill.readyOther", { count: staged.length })}
                </p>
              )}
            </div>
            <Button onClick={handleCommit} disabled={!canSave}>
              {isCommitting ? (
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              ) : (
                <Save className="mr-2 h-4 w-4" />
              )}
              {t("backfill.saveBatch")}
            </Button>
          </div>
        </div>
      )}

      {/* ── Inline create-person dialog (4.6b) ──────── */}
      <InlineUserDialog
        open={createPersonOpen}
        initialName={createPersonInitialName}
        onClose={() => setCreatePersonOpen(false)}
        onConfirm={(created) => {
          setEntry((prev) => ({
            ...prev,
            person: {
              ref: { newUser: created.newUser },
              label: created.displayName,
              isNew: true,
            },
          }));
          setPersonSearch(created.displayName);
        }}
      />

      {/* ── Discard confirmation ─────────────────────── */}
      {discardConfirmOpen && (
        <Dialog open onOpenChange={setDiscardConfirmOpen}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t("backfill.discardConfirmTitle")}</DialogTitle>
              <DialogDescription>
                {staged.length === 1
                  ? t("backfill.discardConfirmOne", { count: staged.length })
                  : t("backfill.discardConfirmOther", { count: staged.length })}
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline" onClick={() => setDiscardConfirmOpen(false)}>
                {t("backfill.cancel")}
              </Button>
              <Button
                variant="destructive"
                onClick={() => {
                  clearStagedRows(paperRef);
                  setStaged([]);
                  setResolutions(new Map());
                  setPreviewMap(new Map());
                  setDiscardConfirmOpen(false);
                }}
              >
                {staged.length === 1
                  ? t("backfill.discardConfirmButtonOne", { count: staged.length })
                  : t("backfill.discardConfirmButtonOther", { count: staged.length })}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}

      {/* ── Follow-through dialog (4.6e) ─────────────── */}
      {commitSummary && (
        <FollowThroughDialog
          summary={commitSummary}
          onRecordAnother={() => {
            setCommitSummary(null);
            setPaperRef("");
            requestAnimationFrame(() => deviceRef.current?.focus());
          }}
          onDone={() => {
            setCommitSummary(null);
          }}
        />
      )}
    </div>
  );
}

export const backfillRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/backfill",
  component: BackfillPage,
});
