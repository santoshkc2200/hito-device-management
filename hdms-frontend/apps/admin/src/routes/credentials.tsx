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
      setSuccessMsg(`Card successfully bound to ${selectedUserName}!`);
      setTokenInput("");
      setEmployeeSearch("");
      setSelectedUserId(null);
      setSelectedUserName(null);
      setErrorMsg(null);
      toast.success("Card bound and activated");
      onBound();
    },
    onError: (err: any) => {
      setErrorMsg(err?.detail ?? err?.message ?? "Could not bind card to user");
    },
  });

  const handleBind = async () => {
    setErrorMsg(null);
    setSuccessMsg(null);

    if (!selectedUserId) {
      setErrorMsg("Please select a borrower to assign this card to");
      return;
    }

    const trimmedToken = tokenInput.trim();
    if (!trimmedToken) {
      setErrorMsg("Please scan or type a blank card token");
      return;
    }

    const inspection = inspectToken(trimmedToken);
    if (!inspection.isValid) {
      setErrorMsg(inspection.errorMessage ?? "Invalid token structure");
      return;
    }

    setIsVerifying(true);
    try {
      const { data: resolved, error: resolveErr } = await resolveCredential({
        query: { token: trimmedToken },
      });
      if (resolveErr || !resolved) {
        setErrorMsg("Card token not found in database");
        setIsVerifying(false);
        return;
      }

      if (resolved.type !== "unbound") {
        setErrorMsg(`This card is already bound or active (Type: ${resolved.type}, Status: ${resolved.credentialStatus})`);
        setIsVerifying(false);
        return;
      }

      bindMutation.mutate({ cardId: resolved.credentialId, userId: selectedUserId });
    } catch (err: any) {
      setErrorMsg(err?.message ?? "Failed to verify token");
    } finally {
      setIsVerifying(false);
    }
  };

  return (
    <div className="flex flex-col gap-4 rounded-xl border border-border bg-card p-6 shadow-xs">
      <div className="flex items-center gap-2">
        <LinkIcon className="size-5 text-primary" />
        <div>
          <h2 className="text-base font-semibold text-foreground">Bind Blank Card to Borrower</h2>
          <p className="text-xs text-muted-foreground">
            Take a pre-printed blank card from the drawer and assign it to an existing registered borrower.
          </p>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mt-2">
        {/* Step 1: Pick User */}
        <div className="flex flex-col gap-2">
          <label htmlFor="user-search-input" className="text-xs font-semibold text-foreground">
            1. Select Borrower
          </label>
          <Input
            id="user-search-input"
            placeholder="Search by name or employee number…"
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
              <span className="font-medium">Selected: {selectedUserName}</span>
              <button
                type="button"
                className="underline hover:opacity-80"
                onClick={() => {
                  setSelectedUserId(null);
                  setSelectedUserName(null);
                  setEmployeeSearch("");
                }}
              >
                Change
              </button>
            </div>
          )}
        </div>

        {/* Step 2: Scan / Type Blank Card */}
        <div className="flex flex-col gap-2">
          <label htmlFor="card-token-input" className="text-xs font-semibold text-foreground">
            2. Scan or Enter Blank Card Token
          </label>
          <Input
            id="card-token-input"
            placeholder="HD-U-..."
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
            USB wedge scanners will automatically fill this input when focused.
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
          {isVerifying || bindMutation.isPending ? "Activating Card…" : "Bind and Activate Card"}
        </Button>
      </div>
    </div>
  );
}

function CredentialsPage() {
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
      toast.success(`${data!.items.length} blank cards minted`);
    },
    onError: () => toast.error("Could not mint the batch"),
  });

  if (batch) {
    return (
      <div className="flex flex-col gap-4 max-w-5xl">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold text-foreground">Print New Card Batch</h1>
            <p className="text-sm text-muted-foreground">
              {batch.length} pre-minted cards — print, laminate, and store in the desk drawer for on-the-spot registration.
            </p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => setBatch(null)}>
              Done
            </Button>
            <Button onClick={() => window.print()}>
              <Printer className="size-4" data-icon="inline-start" />
              Print sheet
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
            Blank Card Stock & Credentials
          </h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Manage physical blank card drawer inventory, print badge stock, and bind cards to staff borrowers.
          </p>
        </div>
        <Link to="/card-reader-test">
          <Button variant="outline" size="sm">
            <Radio className="size-4" data-icon="inline-start" />
            Scanner Diagnostic Tool
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
              Unbound Drawer Stock
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
            <span className="text-xs text-muted-foreground">cards ready</span>
          </div>
          {isLowStock && (
            <div className="mt-2 flex items-center gap-1.5 text-xs font-semibold text-amber-700 dark:text-amber-400">
              <AlertTriangle className="size-3.5" />
              <span>Stock running low (under 10). Mint more.</span>
            </div>
          )}
        </div>

        <div className="rounded-xl border border-border bg-card p-5 shadow-xs">
          <div className="flex items-center justify-between">
            <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              Label Sheet Capacity
            </span>
            <Printer className="size-4 text-muted-foreground" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-3xl font-bold text-foreground">10</span>
            <span className="text-xs text-muted-foreground">cards per A4 sheet (2×5)</span>
          </div>
          <p className="mt-1 text-[11px] text-muted-foreground">
            Standard 85.6 × 54 mm ISO card layout.
          </p>
        </div>

        <div className="rounded-xl border border-border bg-card p-5 shadow-xs">
          <div className="flex items-center justify-between">
            <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              Symbology Standard
            </span>
            <CreditCard className="size-4 text-muted-foreground" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-lg font-bold text-foreground">Crockford Base32</span>
          </div>
          <p className="mt-1 text-[11px] text-muted-foreground">
            Mod-37 checksum with ambiguous character protection.
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
            <h2 className="text-base font-semibold text-foreground">Mint Batch of Blank Cards</h2>
            <p className="text-xs text-muted-foreground">
              Generate pre-allocated unbound credential tokens and print them on perforated badge stock.
            </p>
          </div>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <Field>
            <FieldLabel htmlFor="batch-count">Number of Cards (1–200)</FieldLabel>
            <Input
              id="batch-count"
              type="number"
              min={1}
              max={200}
              value={count}
              onChange={(e) => setCount(Math.max(1, Math.min(200, Number(e.target.value) || 1)))}
            />
            <p className="mt-1 text-[11px] text-muted-foreground">
              Tip: Minting multiples of 10 matches full A4 sheet grids.
            </p>
          </Field>

          <Field>
            <FieldLabel htmlFor="batch-kind">Barcode Format</FieldLabel>
            <Select value={kind} onValueChange={(v) => setKind(v as CredentialKind)}>
              <SelectTrigger id="batch-kind" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="qr">QR Code (2D 15mm – Recommended)</SelectItem>
                <SelectItem value="code128">Code 128 (1D Barcode)</SelectItem>
              </SelectContent>
            </Select>
            <p className="mt-1 text-[11px] text-muted-foreground">
              Every minted card includes a legible plaintext token code for manual typing.
            </p>
          </Field>
        </div>

        <div className="flex justify-end mt-6">
          <Button
            disabled={mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            <Printer className="size-4" data-icon="inline-start" />
            {mutation.isPending ? "Generating tokens…" : `Mint & Print ${count} Blank Cards`}
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
