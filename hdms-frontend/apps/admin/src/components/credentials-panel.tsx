import {
  type Credential,
  type CredentialKind,
  bindCredential,
  getCredentialHistory,
  issueCredential,
  listCredentialsBySubject,
  reissueCredential,
  reprintCredential,
  resolveCredential,
  revokeCredential,
} from "@hdms/api-client";
import { inspectToken } from "@hdms/domain";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  ChevronDown,
  ChevronUp,
  CreditCard,
  Link as LinkIcon,
  Plus,
  Printer,
  RotateCcw,
  ShieldX,
} from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
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
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { credentialStatusTone, labelize, StatusBadge } from "@/components/status-badge";
import { TokenRevealDialog, type TokenRevealSubject } from "@/components/token-reveal-dialog";

// docs/05: nfc/rfid aren't issuable until Phase 6; manual requires an
// explicit token typed by an attendant, so it's not offered here.
const ISSUABLE_KINDS: CredentialKind[] = ["qr", "code128"];

export function ReasonAlertDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  confirmVariant = "default",
  onConfirm,
  pending,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  confirmLabel: string;
  confirmVariant?: "default" | "destructive" | "warning";
  onConfirm: (reason: string) => void;
  pending: boolean;
}) {
  const [reason, setReason] = useState("");

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) {
      setReason("");
    }
    onOpenChange(nextOpen);
  };

  return (
    <AlertDialog open={open} onOpenChange={handleOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            {confirmVariant === "destructive" || confirmVariant === "warning" ? (
              <AlertTriangle className="size-5 text-destructive shrink-0" />
            ) : null}
            {title}
          </AlertDialogTitle>
          <AlertDialogDescription className="text-sm leading-relaxed text-muted-foreground">
            {description}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="py-2">
          <label htmlFor="destructive-reason" className="text-xs font-semibold text-foreground mb-1 block">
            Reason for this action <span className="text-destructive">*</span>
          </label>
          <Textarea
            id="destructive-reason"
            placeholder="e.g. Lost in cafeteria / Badge damaged / Routine reissue"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            className="min-h-[80px]"
            autoFocus
          />
          <p className="mt-1 text-[11px] text-muted-foreground">
            This reason is recorded in the permanent audit trail.
          </p>
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel
            disabled={pending}
            onClick={() => {
              setReason("");
              onOpenChange(false);
            }}
          >
            Cancel
          </AlertDialogCancel>
          <AlertDialogAction
            disabled={!reason.trim() || pending}
            className={
              confirmVariant === "destructive"
                ? "bg-destructive text-destructive-foreground hover:bg-destructive/90"
                : confirmVariant === "warning"
                ? "bg-amber-600 text-white hover:bg-amber-700 dark:bg-amber-700 dark:hover:bg-amber-800"
                : ""
            }
            onClick={(e) => {
              e.preventDefault();
              const trimmed = reason.trim();
              if (!trimmed) return;
              onConfirm(trimmed);
            }}
          >
            {pending ? "Processing…" : confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function CredentialHistory({ credentialId }: { credentialId: string }) {
  const { data, isLoading } = useQuery({
    queryKey: ["credentials", credentialId, "history"],
    queryFn: async () => {
      const { data, error } = await getCredentialHistory({ path: { id: credentialId } });
      if (error) throw error;
      return data.items;
    },
  });

  if (isLoading) return <Skeleton className="h-12 w-full" />;

  if (!data || data.length === 0) {
    return <p className="text-xs text-muted-foreground italic py-1">No recorded audit events.</p>;
  }

  return (
    <div className="mt-2 rounded-md bg-muted/40 p-3">
      <h5 className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground mb-2">
        Audit & Lifecycle History
      </h5>
      <ul className="flex flex-col gap-1.5 border-l-2 border-primary/20 pl-3 text-xs">
        {data.map((e, i) => (
          <li key={i} className="flex flex-col">
            <div className="flex items-center gap-2">
              <span className="font-semibold text-foreground capitalize">{e.kind}</span>
              <span className="text-muted-foreground">·</span>
              <span className="text-muted-foreground">by {e.actor}</span>
              <span className="text-muted-foreground">·</span>
              <span className="text-[11px] text-muted-foreground">
                {new Date(e.at).toLocaleString()}
              </span>
            </div>
            {e.reason && (
              <p className="mt-0.5 text-xs text-foreground/90 font-mono bg-background/50 px-1.5 py-0.5 rounded border border-border/50">
                Reason: "{e.reason}"
              </p>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

function BindBlankCardDialog({
  open,
  onOpenChange,
  userId,
  onSuccess,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  userId: string;
  onSuccess: () => void;
}) {
  const [tokenInput, setTokenInput] = useState("");
  const [isVerifying, setIsVerifying] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const bindMutation = useMutation({
    mutationFn: async (cardId: string) => {
      const { data, error } = await bindCredential({
        path: { id: cardId },
        body: { subjectId: userId },
      });
      if (error) throw error;
      return data;
    },
    onSuccess: () => {
      toast.success("Blank card assigned and activated for this borrower");
      setTokenInput("");
      setErrorMsg(null);
      onOpenChange(false);
      onSuccess();
    },
    onError: (err: any) => {
      setErrorMsg(err?.detail ?? err?.message ?? "Could not bind card to user");
    },
  });

  const handleBind = async () => {
    setErrorMsg(null);
    const trimmed = tokenInput.trim();
    if (!trimmed) {
      setErrorMsg("Please scan or enter a blank card token");
      return;
    }

    const inspection = inspectToken(trimmed);
    if (!inspection.isValid) {
      setErrorMsg(inspection.errorMessage ?? "Invalid token structure");
      return;
    }

    setIsVerifying(true);
    try {
      const { data: resolved, error: resolveErr } = await resolveCredential({
        query: { token: trimmed },
      });
      if (resolveErr || !resolved) {
        setErrorMsg("Card token not found in system");
        setIsVerifying(false);
        return;
      }

      if (resolved.type !== "unbound") {
        setErrorMsg(`This card is already bound (Status: ${resolved.credentialStatus}, Type: ${resolved.type})`);
        setIsVerifying(false);
        return;
      }

      bindMutation.mutate(resolved.credentialId);
    } catch (err: any) {
      setErrorMsg(err?.message ?? "Verification failed");
    } finally {
      setIsVerifying(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <CreditCard className="size-5 text-primary" />
            Bind Blank Card Stock
          </DialogTitle>
          <DialogDescription>
            Scan or type the token printed on an unbound blank card from the drawer stock.
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-3 py-2">
          <div>
            <label htmlFor="blank-token-input" className="text-xs font-semibold text-foreground mb-1 block">
              Blank Card Token
            </label>
            <Input
              id="blank-token-input"
              placeholder="HD-U-..."
              value={tokenInput}
              onChange={(e) => {
                setTokenInput(e.target.value);
                setErrorMsg(null);
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  handleBind();
                }
              }}
              autoFocus
              className="font-mono"
            />
          </div>
          {errorMsg && (
            <div className="rounded-md bg-destructive/10 border border-destructive/20 p-2 text-xs text-destructive flex items-center gap-2">
              <AlertTriangle className="size-4 shrink-0" />
              <span>{errorMsg}</span>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            disabled={!tokenInput.trim() || isVerifying || bindMutation.isPending}
            onClick={handleBind}
          >
            {isVerifying || bindMutation.isPending ? "Activating…" : "Bind Card"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function CredentialsPanel({
  subjectType,
  subjectId,
  subject,
}: {
  subjectType: "user" | "device";
  subjectId: string;
  subject: TokenRevealSubject;
}) {
  const queryClient = useQueryClient();
  const listKey = ["credentials", subjectType, subjectId] as const;

  const { data: credentials, isLoading } = useQuery({
    queryKey: listKey,
    queryFn: async () => {
      const { data, error } = await listCredentialsBySubject({ query: { subjectType, subjectId } });
      if (error) throw error;
      return data.items;
    },
  });

  const [historyOpenId, setHistoryOpenId] = useState<string | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<Credential | null>(null);
  const [reissueTarget, setReissueTarget] = useState<Credential | null>(null);
  const [issueOpen, setIssueOpen] = useState(false);
  const [bindOpen, setBindOpen] = useState(false);
  const [issueKind, setIssueKind] = useState<CredentialKind>("qr");
  const [revealToken, setRevealToken] = useState<string | undefined>();

  const invalidate = () => queryClient.invalidateQueries({ queryKey: listKey });

  const issueMutation = useMutation({
    mutationFn: async () =>
      issueCredential({ body: { subjectType, subjectId, kind: issueKind } }),
    onSuccess: async ({ data, error }) => {
      if (error) throw error;
      await invalidate();
      setIssueOpen(false);
      setRevealToken(data?.token);
      toast.success(subjectType === "user" ? "Card issued" : "Credential issued");
    },
    onError: () => toast.error("Could not issue a credential"),
  });

  const reprintMutation = useMutation({
    mutationFn: async (id: string) => reprintCredential({ path: { id } }),
    onSuccess: async ({ data, error }) => {
      if (error) throw error;
      await invalidate();
      setRevealToken(data?.token);
    },
    onError: () => toast.error("Could not reprint — only device credentials store a recoverable token"),
  });

  const revokeMutation = useMutation({
    mutationFn: async ({ id, reason }: { id: string; reason: string }) =>
      revokeCredential({ path: { id }, body: { reason } }),
    onSuccess: async ({ error }) => {
      if (error) throw error;
      await invalidate();
      setRevokeTarget(null);
      toast.success("Credential revoked");
    },
    onError: () => toast.error("Could not revoke credential"),
  });

  const reissueMutation = useMutation({
    mutationFn: async ({ id, reason }: { id: string; reason: string }) =>
      reissueCredential({ path: { id }, body: { reason } }),
    onSuccess: async ({ data, error }) => {
      if (error) throw error;
      await invalidate();
      setReissueTarget(null);
      setRevealToken(data?.token);
      toast.success(subjectType === "user" ? "Card reissued" : "Credential reissued");
    },
    onError: () => toast.error("Could not reissue credential"),
  });

  // Sort credentials by issue sequence descending (latest first)
  const sortedCredentials = credentials ? [...credentials].sort((a, b) => b.issueSeq - a.issueSeq) : [];
  const activeCredential = sortedCredentials.find((c) => c.status === "active");
  const hasActive = Boolean(activeCredential);
  const allRevoked = Boolean(sortedCredentials.length > 0 && !hasActive);

  // Map to find replacement sequence for revoked cards
  const replacementMap = new Map<string, number>();
  for (const c of sortedCredentials) {
    if (c.replacesId) {
      replacementMap.set(c.replacesId, c.issueSeq);
    }
  }

  return (
    <div className="flex flex-col gap-4">
      {/* Header with Title and Primary CTAs */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <CreditCard className="size-4 text-primary" />
          <h3 className="text-base font-semibold text-foreground">
            {subjectType === "user" ? "Borrower Cards" : "Device Credentials"}
          </h3>
        </div>
        <div className="flex items-center gap-2">
          {subjectType === "user" && !hasActive && (
            <Button size="sm" variant="outline" onClick={() => setBindOpen(true)}>
              <LinkIcon className="size-4" data-icon="inline-start" />
              Bind blank card
            </Button>
          )}
          <Button size="sm" variant={hasActive ? "outline" : "default"} onClick={() => setIssueOpen(true)}>
            <Plus className="size-4" data-icon="inline-start" />
            {subjectType === "user" ? "Issue new card" : "Issue credential"}
          </Button>
        </div>
      </div>

      {isLoading && (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-16 w-full" />
        </div>
      )}

      {/* Empty State: No credentials ever issued */}
      {!isLoading && sortedCredentials.length === 0 && (
        <div className="flex flex-col items-center justify-center gap-3 rounded-lg border border-dashed border-border bg-card/50 p-6 text-center">
          <div className="rounded-full bg-muted p-3">
            <CreditCard className="size-6 text-muted-foreground" />
          </div>
          <div>
            <p className="text-sm font-semibold text-foreground">
              {subjectType === "user" ? "No card issued" : "No credential issued"}
            </p>
            <p className="text-xs text-muted-foreground max-w-xs mt-1">
              {subjectType === "user"
                ? "This borrower cannot borrow devices until a credential is assigned."
                : "This device has no active or past credentials."}
            </p>
          </div>
          <div className="flex items-center gap-2 mt-2">
            {subjectType === "user" && (
              <Button size="sm" variant="outline" onClick={() => setBindOpen(true)}>
                <LinkIcon className="size-4" data-icon="inline-start" />
                Bind blank card
              </Button>
            )}
            <Button size="sm" onClick={() => setIssueOpen(true)}>
              <Plus className="size-4" data-icon="inline-start" />
              {subjectType === "user" ? "Issue card" : "Issue credential"}
            </Button>
          </div>
        </div>
      )}

      {/* Empty State: All credentials revoked */}
      {!isLoading && allRevoked && (
        <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-xs text-destructive">
          <div className="flex items-start gap-3">
            <AlertTriangle className="size-5 shrink-0 text-destructive mt-0.5" />
            <div className="flex-1">
              <p className="font-semibold text-sm">All credentials have been revoked</p>
              <p className="text-muted-foreground mt-1">
                This {subjectType === "user" ? "borrower" : "device"} has no working credentials. Issue a replacement card or bind a blank card to restore access.
              </p>
              <div className="flex items-center gap-2 mt-3">
                {subjectType === "user" && (
                  <Button size="sm" variant="outline" onClick={() => setBindOpen(true)}>
                    <LinkIcon className="size-4" data-icon="inline-start" />
                    Bind blank card
                  </Button>
                )}
                <Button size="sm" onClick={() => setIssueOpen(true)}>
                  <Plus className="size-4" data-icon="inline-start" />
                  {subjectType === "user" ? "Issue new card" : "Issue credential"}
                </Button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Credential Cards List */}
      {!isLoading && sortedCredentials.length > 0 && (
        <ul className="flex flex-col gap-3">
          {sortedCredentials.map((c) => {
            const isActive = c.status === "active";
            const replacedBySeq = replacementMap.get(c.id);

            return (
              <li
                key={c.id}
                className={`rounded-lg border p-4 transition-colors ${
                  isActive
                    ? "border-primary/30 bg-card shadow-xs"
                    : "border-border/60 bg-muted/20 opacity-90"
                }`}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <StatusBadge
                        label={labelize(c.status)}
                        tone={credentialStatusTone[c.status] ?? "muted"}
                      />
                      <Badge variant="outline" className="text-xs uppercase font-mono">
                        {c.kind}
                      </Badge>
                      <span className="text-xs font-semibold text-foreground">
                        Issue #{c.issueSeq}
                      </span>
                      {replacedBySeq && (
                        <Badge variant="secondary" className="text-[11px] text-muted-foreground">
                          Replaced by issue #{replacedBySeq}
                        </Badge>
                      )}
                    </div>

                    <div className="mt-2 flex items-center gap-2">
                      <span className="text-xs text-muted-foreground">Token:</span>
                      <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs font-semibold text-foreground">
                        …{c.tokenPreview ?? "————"}
                      </code>
                    </div>

                    <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                      <span>Issued: {new Date(c.issuedAt).toLocaleDateString()}</span>
                      <span>By: {c.issuedBy}</span>
                      {c.printedCount > 0 && (
                        <span>
                          Printed: {c.printedCount}×{" "}
                          {c.lastPrintedAt && `(Last: ${new Date(c.lastPrintedAt).toLocaleDateString()})`}
                        </span>
                      )}
                    </div>

                    {c.status === "revoked" && (
                      <div className="mt-2.5 rounded-md border border-destructive/20 bg-destructive/5 p-2.5 text-xs text-muted-foreground">
                        <div className="flex items-center gap-1.5 font-semibold text-destructive">
                          <ShieldX className="size-3.5" />
                          <span>Revoked on {c.revokedAt ? new Date(c.revokedAt).toLocaleDateString() : "—"}</span>
                          {c.revokedBy && <span>by {c.revokedBy}</span>}
                        </div>
                        {c.revokedReason && (
                          <p className="mt-1 text-foreground font-mono">
                            Reason: "{c.revokedReason}"
                          </p>
                        )}
                      </div>
                    )}
                  </div>

                  {/* Actions Block */}
                  <div className="flex shrink-0 items-center gap-1">
                    {isActive && (
                      <>
                        {subjectType === "device" && (
                          <Button
                            size="sm"
                            variant="outline"
                            title="Print label"
                            onClick={() => reprintMutation.mutate(c.id)}
                            disabled={reprintMutation.isPending}
                          >
                            <Printer className="size-4" data-icon="inline-start" />
                            Print
                          </Button>
                        )}
                        {subjectType === "user" ? (
                          <Button
                            size="sm"
                            variant="outline"
                            className="border-amber-500/30 text-amber-700 hover:bg-amber-50 dark:text-amber-400 dark:hover:bg-amber-950/30"
                            title="Report lost & reissue card"
                            onClick={() => setReissueTarget(c)}
                          >
                            <RotateCcw className="size-4" data-icon="inline-start" />
                            Reissue & print
                          </Button>
                        ) : (
                          <Button
                            size="sm"
                            variant="outline"
                            title="Reissue credential"
                            onClick={() => setReissueTarget(c)}
                          >
                            <RotateCcw className="size-4" data-icon="inline-start" />
                            Reissue
                          </Button>
                        )}
                        <Button
                          size="icon-sm"
                          variant="ghost"
                          className="text-destructive hover:bg-destructive/10"
                          title="Revoke"
                          onClick={() => setRevokeTarget(c)}
                        >
                          <ShieldX className="size-4" />
                        </Button>
                      </>
                    )}
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      title="View history"
                      onClick={() => setHistoryOpenId(historyOpenId === c.id ? null : c.id)}
                    >
                      {historyOpenId === c.id ? (
                        <ChevronUp className="size-4" />
                      ) : (
                        <ChevronDown className="size-4" />
                      )}
                    </Button>
                  </div>
                </div>

                {/* History Drawer */}
                {historyOpenId === c.id && <CredentialHistory credentialId={c.id} />}
              </li>
            );
          })}
        </ul>
      )}

      {/* Modal: Issue Additional Credential */}
      <Dialog open={issueOpen} onOpenChange={setIssueOpen}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>
              {subjectType === "user" ? "Issue a new card" : "Issue a new credential"}
            </DialogTitle>
            <DialogDescription>
              The plaintext token is displayed once immediately after issuance for printing.
            </DialogDescription>
          </DialogHeader>
          <div className="py-2">
            <label htmlFor="issue-kind-select" className="text-xs font-semibold text-foreground mb-1 block">
              Barcode / Credential Symbology
            </label>
            <Select value={issueKind} onValueChange={(v) => setIssueKind(v as CredentialKind)}>
              <SelectTrigger id="issue-kind-select" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ISSUABLE_KINDS.map((k) => (
                  <SelectItem key={k} value={k}>
                    {k === "qr" ? "QR Code (Recommended)" : "Code 128 (1D Barcode)"}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setIssueOpen(false)}>
              Cancel
            </Button>
            <Button disabled={issueMutation.isPending} onClick={() => issueMutation.mutate()}>
              {issueMutation.isPending ? "Issuing…" : "Issue"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Modal: Bind Blank Card to User */}
      {subjectType === "user" && (
        <BindBlankCardDialog
          open={bindOpen}
          onOpenChange={setBindOpen}
          userId={subjectId}
          onSuccess={invalidate}
        />
      )}

      {/* Destructive Flow: Revoke */}
      <ReasonAlertDialog
        open={!!revokeTarget}
        onOpenChange={(o) => !o && setRevokeTarget(null)}
        title="Revoke this credential?"
        description="The card stops working immediately at all kiosks. No replacement credential will be minted. Open loans and borrowing history are preserved."
        confirmLabel="Revoke Credential"
        confirmVariant="destructive"
        pending={revokeMutation.isPending}
        onConfirm={(reason) =>
          revokeTarget && revokeMutation.mutate({ id: revokeTarget.id, reason })
        }
      />

      {/* Destructive Flow: Reissue & Print / Report Lost */}
      <ReasonAlertDialog
        open={!!reissueTarget}
        onOpenChange={(o) => !o && setReissueTarget(null)}
        title={subjectType === "user" ? "Report lost & reissue card?" : "Reissue credential?"}
        description={
          subjectType === "user"
            ? "The current card in the borrower's pocket stops working immediately. Open loans and loan history are completely unaffected. A new card token will be minted for immediate printing and distribution."
            : "The current credential stops working immediately. A new credential will be minted for printing."
        }
        confirmLabel={subjectType === "user" ? "Reissue & Print Card" : "Reissue Credential"}
        confirmVariant="warning"
        pending={reissueMutation.isPending}
        onConfirm={(reason) =>
          reissueTarget && reissueMutation.mutate({ id: reissueTarget.id, reason })
        }
      />

      {/* One-Shot Token Reveal Dialog */}
      <TokenRevealDialog
        open={!!revealToken}
        onOpenChange={(o) => !o && setRevealToken(undefined)}
        token={revealToken}
        subject={subject}
      />
    </div>
  );
}
