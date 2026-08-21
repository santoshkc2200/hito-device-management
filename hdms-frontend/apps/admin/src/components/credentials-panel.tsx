import {
  type Credential,
  type CredentialKind,
  getCredentialHistory,
  issueCredential,
  listCredentialsBySubject,
  reissueCredential,
  reprintCredential,
  revokeCredential,
} from "@hdms/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, History, Plus, Printer, RotateCcw, ShieldX } from "lucide-react";
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
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { credentialStatusTone, labelize, StatusBadge } from "@/components/status-badge";
import { TokenRevealDialog, type TokenRevealSubject } from "@/components/token-reveal-dialog";

// docs/05: nfc/rfid aren't issuable until Phase 6; manual requires an
// explicit token typed by an attendant, so it's not offered here.
const ISSUABLE_KINDS: CredentialKind[] = ["qr", "code128"];

function ReasonAlertDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  onConfirm,
  pending,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  confirmLabel: string;
  onConfirm: (reason: string) => void;
  pending: boolean;
}) {
  const [reason, setReason] = useState("");
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <Textarea
          placeholder="Reason (required)"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          autoFocus
        />
        <AlertDialogFooter>
          <AlertDialogCancel onClick={() => setReason("")}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            disabled={!reason.trim() || pending}
            onClick={(e) => {
              e.preventDefault();
              onConfirm(reason.trim());
            }}
          >
            {confirmLabel}
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

  return (
    <ul className="flex flex-col gap-1 border-l border-border pl-3 text-xs text-muted-foreground">
      {data?.map((e, i) => (
        <li key={i}>
          <span className="font-medium text-foreground">{e.kind}</span> · {e.actor} ·{" "}
          {new Date(e.at).toLocaleString()}
          {e.reason && <> — "{e.reason}"</>}
        </li>
      ))}
    </ul>
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
      toast.success("Credential issued");
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
    onError: () => toast.error("Could not revoke"),
  });

  const reissueMutation = useMutation({
    mutationFn: async ({ id, reason }: { id: string; reason: string }) =>
      reissueCredential({ path: { id }, body: { reason } }),
    onSuccess: async ({ data, error }) => {
      if (error) throw error;
      await invalidate();
      setReissueTarget(null);
      setRevealToken(data?.token);
      toast.success("Credential reissued");
    },
    onError: () => toast.error("Could not reissue"),
  });

  const hasActive = credentials?.some((c) => c.status === "active");
  const allRevoked = Boolean(credentials && credentials.length > 0 && !hasActive);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold">Credentials</h3>
        {!hasActive && credentials && credentials.length > 0 && (
          <Button size="sm" variant="outline" onClick={() => setIssueOpen(true)}>
            <Plus className="size-4" data-icon="inline-start" />
            {subjectType === "user" ? "Issue card" : "Issue credential"}
          </Button>
        )}
      </div>

      {isLoading && <Skeleton className="h-16 w-full" />}

      {credentials?.length === 0 && (
        <div className="flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-border p-6 text-center">
          <p className="text-sm font-medium text-foreground">
            {subjectType === "user" ? "No card issued" : "No credential issued"}
          </p>
          <p className="text-xs text-muted-foreground">
            {subjectType === "user"
              ? "This borrower cannot borrow devices until a credential is assigned."
              : "This device has no active or past credentials."}
          </p>
          <Button size="sm" variant="outline" className="mt-1" onClick={() => setIssueOpen(true)}>
            <Plus className="size-4" data-icon="inline-start" />
            {subjectType === "user" ? "Issue card" : "Issue credential"}
          </Button>
        </div>
      )}

      {allRevoked && (
        <div className="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-xs text-destructive">
          <p className="font-semibold">All credentials have been revoked</p>
          <p className="text-muted-foreground mt-0.5">
            This {subjectType === "user" ? "borrower" : "device"} has no working credentials. Issue a new credential to restore access.
          </p>
        </div>
      )}

      <ul className="flex flex-col gap-3">
        {credentials?.map((c) => (
          <li key={c.id} className="rounded-md border border-border p-3">
            <div className="flex items-start justify-between gap-2">
              <div>
                <div className="flex items-center gap-2">
                  <StatusBadge label={labelize(c.status)} tone={credentialStatusTone[c.status] ?? "muted"} />
                  <span className="text-xs text-muted-foreground uppercase">{c.kind}</span>
                  <span className="text-xs text-muted-foreground">issue #{c.issueSeq}</span>
                </div>
                <p className="mt-1 font-identifier text-xs text-muted-foreground">
                  …{c.tokenPreview ?? "————"}
                </p>
                <p className="text-xs text-muted-foreground">
                  issued {new Date(c.issuedAt).toLocaleDateString()} by {c.issuedBy}
                  {c.printedCount > 0 && <> · printed {c.printedCount}×</>}
                </p>
                {c.status === "revoked" && c.revokedReason && (
                  <p className="text-xs text-muted-foreground">reason: "{c.revokedReason}"</p>
                )}
              </div>
              {c.status === "active" && (
                <div className="flex shrink-0 gap-1">
                  {subjectType === "device" && (
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      title="Reprint"
                      onClick={() => reprintMutation.mutate(c.id)}
                    >
                      <Printer className="size-4" />
                    </Button>
                  )}
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    title="Report lost & reissue"
                    onClick={() => setReissueTarget(c)}
                  >
                    <RotateCcw className="size-4" />
                  </Button>
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    title="Revoke"
                    onClick={() => setRevokeTarget(c)}
                  >
                    <ShieldX className="size-4" />
                  </Button>
                </div>
              )}
              <Button
                size="icon-sm"
                variant="ghost"
                title="History"
                onClick={() => setHistoryOpenId(historyOpenId === c.id ? null : c.id)}
              >
                {historyOpenId === c.id ? <ChevronDown className="size-4" /> : <History className="size-4" />}
              </Button>
            </div>
            {historyOpenId === c.id && (
              <div className="mt-2">
                <CredentialHistory credentialId={c.id} />
              </div>
            )}
          </li>
        ))}
      </ul>

      <Dialog open={issueOpen} onOpenChange={setIssueOpen}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>Issue a credential</DialogTitle>
            <DialogDescription>The plaintext token is shown once, immediately after.</DialogDescription>
          </DialogHeader>
          <Select value={issueKind} onValueChange={(v) => setIssueKind(v as CredentialKind)}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {ISSUABLE_KINDS.map((k) => (
                <SelectItem key={k} value={k}>
                  {k.toUpperCase()}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <DialogFooter>
            <Button variant="outline" onClick={() => setIssueOpen(false)}>
              Cancel
            </Button>
            <Button disabled={issueMutation.isPending} onClick={() => issueMutation.mutate()}>
              Issue
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ReasonAlertDialog
        open={!!revokeTarget}
        onOpenChange={(o) => !o && setRevokeTarget(null)}
        title="Revoke this credential?"
        description="The card stops working immediately. No replacement is minted."
        confirmLabel="Revoke"
        pending={revokeMutation.isPending}
        onConfirm={(reason) => revokeTarget && revokeMutation.mutate({ id: revokeTarget.id, reason })}
      />
      <ReasonAlertDialog
        open={!!reissueTarget}
        onOpenChange={(o) => !o && setReissueTarget(null)}
        title="Report lost & reissue?"
        description="The old card stops working immediately. Open loans and history are unaffected. A new card will be minted for printing."
        confirmLabel="Reissue"
        pending={reissueMutation.isPending}
        onConfirm={(reason) => reissueTarget && reissueMutation.mutate({ id: reissueTarget.id, reason })}
      />
      <TokenRevealDialog
        open={!!revealToken}
        onOpenChange={(o) => !o && setRevealToken(undefined)}
        token={revealToken}
        subject={subject}
      />
    </div>
  );
}
