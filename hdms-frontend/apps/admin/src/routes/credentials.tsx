import {
  type CredentialKind,
  bindCredential,
  getUnboundCredentialCount,
  issueBlankBatch,
  listUsers,
  resolveCredential,
} from "@hdms/api-client";
import { inspectToken } from "@hdms/domain";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRoute, Link } from "@tanstack/react-router";
import {
  AlertTriangle,
  CheckCircle2,
  CreditCard,
  Layers,
  Link as LinkIcon,
  Printer,
  Radio,
  Sparkles,
} from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { BlankCardLabel, LabelSheet } from "@/components/label-templates";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { useT } from "@/i18n";
import { authenticatedRoute } from "./authenticated";

const CARD_SHEET_SETTINGS = {
  pageWidthMm: 210,
  pageHeightMm: 297,
  columns: 2,
  rows: 5,
  labelWidthMm: 85.6,
  labelHeightMm: 54,
  marginTopMm: 12,
  marginLeftMm: 15,
  gapXMm: 8,
  gapYMm: 6,
};

function QuickBindCardSection({ onBound }: { onBound: () => void }) {
  const t = useT();
  const [tokenInput, setTokenInput] = useState("");
  const [employeeSearch, setEmployeeSearch] = useState("");
  const [selectedUserId, setSelectedUserId] = useState<string | null>(null);
  const [selectedUserName, setSelectedUserName] = useState<string | null>(null);
  const [isVerifying, setIsVerifying] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);

  // Search users for binding
  const { data: usersData, isLoading: isSearchingUsers } = useQuery({
    queryKey: ["users", "quick-search", employeeSearch],
    queryFn: async () => {
      if (!employeeSearch.trim() || employeeSearch.length < 2) return [];
      const { data, error } = await listUsers({
        query: { q: employeeSearch.trim(), limit: 5 },
      });
      if (error) throw error;
      return data.items;
    },
    enabled: employeeSearch.trim().length >= 2,
  });

  const bindMutation = useMutation({
    mutationFn: async ({ cardId, userId }: { cardId: string; userId: string }) => {
      const { data, error } = await bindCredential({
        path: { id: cardId },
        body: { subjectId: userId },
      });
      if (error) throw error;
      return data;
    },
    onSuccess: () => {
      setSuccessMsg(t("credentials.boundSuccess", { user: selectedUserName ?? "" }));
      setTokenInput("");
      setEmployeeSearch("");
      setSelectedUserId(null);
      setSelectedUserName(null);
      setErrorMsg(null);
      toast.success(t("credentials.boundToast"));
      onBound();
    },
    onError: (err: any) => {
      setErrorMsg(err?.detail ?? err?.message ?? t("credentials.bindFailed"));
    },
  });

  const handleBind = async () => {
    setErrorMsg(null);
    setSuccessMsg(null);

    if (!selectedUserId) {
      setErrorMsg(t("credentials.selectBorrowerFirst"));
      return;
    }

    const trimmedToken = tokenInput.trim();
    if (!trimmedToken) {
      setErrorMsg(t("credentials.scanTokenFirst"));
      return;
    }

    const inspection = inspectToken(trimmedToken);
    if (!inspection.isValid) {
      setErrorMsg(inspection.errorMessage ?? t("credentials.invalidTokenStructure"));
      return;
    }

    setIsVerifying(true);
    try {
      const { data: resolved, error: resolveErr } = await resolveCredential({
        query: { token: trimmedToken },
      });
      if (resolveErr || !resolved) {
        setErrorMsg(t("credentials.tokenNotFound"));
        setIsVerifying(false);
        return;
      }

      if (resolved.type !== "unbound") {
        setErrorMsg(
          t("credentials.tokenAlreadyBound", {
            type: resolved.type,
            status: resolved.credentialStatus,
          }),
        );
        setIsVerifying(false);
        return;
      }

      bindMutation.mutate({ cardId: resolved.credentialId, userId: selectedUserId });
    } catch (err: any) {
      setErrorMsg(err?.message ?? t("credentials.verifyFailed"));
    } finally {
      setIsVerifying(false);
    }
  };

  return (
    <div className="flex flex-col gap-4 rounded-xl border border-border bg-card p-6 shadow-xs">
      <div className="flex items-center gap-2">
        <LinkIcon className="size-5 text-primary" />
        <div>
          <h2 className="text-base font-semibold text-foreground">{t("credentials.quickBindTitle")}</h2>
          <p className="text-xs text-muted-foreground">
            {t("credentials.quickBindSubtitle")}
          </p>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mt-2">
        {/* Step 1: Pick User */}
        <div className="flex flex-col gap-2">
          <label htmlFor="user-search-input" className="text-xs font-semibold text-foreground">
            {t("credentials.step1")}
          </label>
          <Input
            id="user-search-input"
            placeholder={t("credentials.userSearchPlaceholder")}
            value={employeeSearch}
            onChange={(e) => {
              setEmployeeSearch(e.target.value);
              setSelectedUserId(null);
              setSelectedUserName(null);
              setErrorMsg(null);
              setSuccessMsg(null);
            }}
          />

          {isSearchingUsers && <Skeleton className="h-10 w-full mt-1" />}

          {usersData && usersData.length > 0 && !selectedUserId && (
            <ul className="divide-y divide-border rounded-md border border-border bg-background shadow-xs overflow-hidden max-h-48 overflow-y-auto">
              {usersData.map((u) => (
                <li key={u.id}>
                  <button
                    type="button"
                    className="w-full text-left px-3 py-2 text-xs hover:bg-accent/50 transition-colors flex items-center justify-between"
                    onClick={() => {
                      setSelectedUserId(u.id);
                      setSelectedUserName(`${u.fullName} (${u.employeeNo})`);
                      setEmployeeSearch(`${u.fullName} (${u.employeeNo})`);
                    }}
                  >
                    <div>
                      <span className="font-semibold text-foreground">{u.fullName}</span>
                      <span className="ml-2 font-mono text-muted-foreground">{u.employeeNo}</span>
                    </div>
                    <Badge variant="outline" className="text-[10px]">
                      {u.status}
                    </Badge>
                  </button>
                </li>
              ))}
            </ul>
          )}

          {selectedUserId && (
            <div className="flex items-center justify-between rounded-md bg-primary/10 border border-primary/20 px-3 py-2 text-xs text-primary">
              <span className="font-medium">{t("credentials.selectedPrefix")} {selectedUserName}</span>
              <button
                type="button"
                className="underline hover:opacity-80"
                onClick={() => {
                  setSelectedUserId(null);
                  setSelectedUserName(null);
                  setEmployeeSearch("");
                }}
              >
                {t("credentials.change")}
              </button>
            </div>
          )}
        </div>

        {/* Step 2: Scan / Type Blank Card */}
        <div className="flex flex-col gap-2">
          <label htmlFor="card-token-input" className="text-xs font-semibold text-foreground">
            {t("credentials.step2")}
          </label>
          <Input
            id="card-token-input"
            placeholder={t("credentials.tokenPlaceholder")}
            value={tokenInput}
            onChange={(e) => {
              setTokenInput(e.target.value);
              setErrorMsg(null);
              setSuccessMsg(null);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                handleBind();
              }
            }}
            className="font-mono"
          />
          <p className="text-[11px] text-muted-foreground">
            {t("credentials.wedgeHint")}
          </p>
        </div>
      </div>

      {errorMsg && (
        <div className="rounded-md bg-destructive/10 border border-destructive/20 p-2.5 text-xs text-destructive flex items-center gap-2">
          <AlertTriangle className="size-4 shrink-0" />
          <span>{errorMsg}</span>
        </div>
      )}

      {successMsg && (
        <div className="rounded-md bg-success/10 border border-success/20 p-2.5 text-xs text-success flex items-center gap-2">
          <CheckCircle2 className="size-4 shrink-0" />
          <span>{successMsg}</span>
        </div>
      )}

      <div className="flex justify-end mt-1">
        <Button
          disabled={!selectedUserId || !tokenInput.trim() || isVerifying || bindMutation.isPending}
          onClick={handleBind}
        >
          <LinkIcon className="size-4" data-icon="inline-start" />
          {isVerifying || bindMutation.isPending
            ? t("credentials.activatingCard")
            : t("credentials.bindAndActivate")}
        </Button>
      </div>
    </div>
  );
}

function CredentialsPage() {
  const t = useT();
  const queryClient = useQueryClient();
  const [count, setCount] = useState(10);
  const [kind, setKind] = useState<CredentialKind>("qr");
  const [batch, setBatch] = useState<string[] | null>(null);

  const { data: unboundCount, isLoading: isCountLoading } = useQuery({
    queryKey: ["credentials", "unbound-count"],
    queryFn: async () => {
      const { data, error } = await getUnboundCredentialCount();
      if (error) throw error;
      return data.count;
    },
  });

  const mutation = useMutation({
    mutationFn: async () => issueBlankBatch({ body: { count, kind } }),
    onSuccess: async ({ data, error }) => {
      if (error) throw error;
      await queryClient.invalidateQueries({ queryKey: ["credentials", "unbound-count"] });
      setBatch(data!.items.map((c) => c.token));
      toast.success(t("credentials.minted", { count: data!.items.length }));
    },
    onError: () => toast.error(t("credentials.mintFailed")),
  });

  if (batch) {
    return (
      <div className="flex flex-col gap-4 max-w-5xl">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold text-foreground">{t("credentials.batchTitle")}</h1>
            <p className="text-sm text-muted-foreground">
              {t("credentials.batchSubtitle", { count: batch.length })}
            </p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => setBatch(null)}>
              {t("credentials.done")}
            </Button>
            <Button onClick={() => window.print()}>
              <Printer className="size-4" data-icon="inline-start" />
              {t("credentials.printSheet")}
            </Button>
          </div>
        </div>
        <div className="overflow-auto rounded-xl border border-border bg-secondary/50 p-6">
          <LabelSheet settings={CARD_SHEET_SETTINGS}>
            {batch.map((token) => (
              <BlankCardLabel key={token} token={token} />
            ))}
          </LabelSheet>
        </div>
      </div>
    );
  }

  const isLowStock = typeof unboundCount === "number" && unboundCount < 10;

  return (
    <div className="flex flex-col gap-6 max-w-5xl">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">
            {t("credentials.title")}
          </h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            {t("credentials.subtitle")}
          </p>
        </div>
        <Link to="/card-reader-test">
          <Button variant="outline" size="sm">
            <Radio className="size-4" data-icon="inline-start" />
            {t("credentials.scannerDiagnostic")}
          </Button>
        </Link>
      </div>

      {/* Stock Status Stat Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div className={`rounded-xl border p-5 shadow-xs transition-colors ${
          isLowStock
            ? "border-amber-500/40 bg-amber-500/10 dark:bg-amber-950/20"
            : "border-border bg-card"
        }`}>
          <div className="flex items-center justify-between">
            <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              {t("credentials.unboundStockLabel")}
            </span>
            <Layers className="size-4 text-primary" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            {isCountLoading ? (
              <Skeleton className="h-8 w-16" />
            ) : (
              <span className="text-3xl font-bold text-foreground">
                {unboundCount ?? 0}
              </span>
            )}
            <span className="text-xs text-muted-foreground">{t("credentials.cardsReady")}</span>
          </div>
          {isLowStock && (
            <div className="mt-2 flex items-center gap-1.5 text-xs font-semibold text-amber-700 dark:text-amber-400">
              <AlertTriangle className="size-3.5" />
              <span>{t("credentials.lowStockWarning")}</span>
            </div>
          )}
        </div>

        <div className="rounded-xl border border-border bg-card p-5 shadow-xs">
          <div className="flex items-center justify-between">
            <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              {t("credentials.sheetCapacityLabel")}
            </span>
            <Printer className="size-4 text-muted-foreground" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-3xl font-bold text-foreground">10</span>
            <span className="text-xs text-muted-foreground">{t("credentials.cardsPerSheet")}</span>
          </div>
          <p className="mt-1 text-[11px] text-muted-foreground">
            {t("credentials.cardLayoutNote")}
          </p>
        </div>

        <div className="rounded-xl border border-border bg-card p-5 shadow-xs">
          <div className="flex items-center justify-between">
            <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              {t("credentials.symbologyLabel")}
            </span>
            <CreditCard className="size-4 text-muted-foreground" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-lg font-bold text-foreground">{t("credentials.symbologyValue")}</span>
          </div>
          <p className="mt-1 text-[11px] text-muted-foreground">
            {t("credentials.symbologyNote")}
          </p>
        </div>
      </div>

      {/* Quick Bind Card Section */}
      <QuickBindCardSection
        onBound={() => {
          queryClient.invalidateQueries({ queryKey: ["credentials", "unbound-count"] });
        }}
      />

      {/* Mint New Blank Batch Card */}
      <div className="rounded-xl border border-border bg-card p-6 shadow-xs">
        <div className="flex items-center gap-2 mb-4">
          <Sparkles className="size-5 text-primary" />
          <div>
            <h2 className="text-base font-semibold text-foreground">{t("credentials.mintTitle")}</h2>
            <p className="text-xs text-muted-foreground">
              {t("credentials.mintSubtitle")}
            </p>
          </div>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <Field>
            <FieldLabel htmlFor="batch-count">{t("credentials.countLabel")}</FieldLabel>
            <Input
              id="batch-count"
              type="number"
              min={1}
              max={200}
              value={count}
              onChange={(e) => setCount(Math.max(1, Math.min(200, Number(e.target.value) || 1)))}
            />
            <p className="mt-1 text-[11px] text-muted-foreground">
              {t("credentials.countHint")}
            </p>
          </Field>

          <Field>
            <FieldLabel htmlFor="batch-kind">{t("credentials.formatLabel")}</FieldLabel>
            <Select value={kind} onValueChange={(v) => setKind(v as CredentialKind)}>
              <SelectTrigger id="batch-kind" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="qr">{t("credentials.formatQr")}</SelectItem>
                <SelectItem value="code128">{t("credentials.formatCode128")}</SelectItem>
              </SelectContent>
            </Select>
            <p className="mt-1 text-[11px] text-muted-foreground">
              {t("credentials.formatHint")}
            </p>
          </Field>
        </div>

        <div className="flex justify-end mt-6">
          <Button
            disabled={mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            <Printer className="size-4" data-icon="inline-start" />
            {mutation.isPending
              ? t("credentials.generatingTokens")
              : t("credentials.mintAndPrint", { count })}
          </Button>
        </div>
      </div>
    </div>
  );
}

export const credentialsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/credentials",
  component: CredentialsPage,
});
