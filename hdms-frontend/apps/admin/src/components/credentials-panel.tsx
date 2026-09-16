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
  revealCredential,
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
  QrCode,
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
import { useT } from "@/i18n";

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
  const t = useT();
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
            {t("credentialsPanel.reasonForActionLabel")} <span className="text-destructive">*</span>
          </label>
          <Textarea
            id="destructive-reason"
            placeholder={t("credentialsPanel.reasonPlaceholder")}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            className="min-h-[80px]"
            autoFocus
          />
          <p className="mt-1 text-[11px] text-muted-foreground">
            {t("credentialsPanel.reasonAuditHint")}
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
            {t("common.cancel")}
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
            {pending ? t("credentialsPanel.processing") : confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function CredentialHistory({ credentialId }: { credentialId: string }) {
  const t = useT();
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
    return <p className="text-xs text-muted-foreground italic py-1">{t("credentialsPanel.noAuditEvents")}</p>;
  }

  return (
    <div className="mt-2 rounded-md bg-muted/40 p-3">
      <h5 className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground mb-2">
        {t("credentialsPanel.auditHistoryHeading")}
      </h5>
      <ul className="flex flex-col gap-1.5 border-l-2 border-primary/20 pl-3 text-xs">
        {data.map((e, i) => (
          <li key={i} className="flex flex-col">
            <div className="flex items-center gap-2">
              <span className="font-semibold text-foreground capitalize">{e.kind}</span>
              <span className="text-muted-foreground">·</span>
              <span className="text-muted-foreground">
                {t("credentialsPanel.byActor", { actor: e.actor })}
              </span>
              <span className="text-muted-foreground">·</span>
              <span className="text-[11px] text-muted-foreground">
                {new Date(e.at).toLocaleString()}
              </span>
            </div>
            {e.reason && (
              <p className="mt-0.5 text-xs text-foreground/90 font-mono bg-background/50 px-1.5 py-0.5 rounded border border-border/50">
                {t("credentialsPanel.reasonQuoted", { reason: e.reason })}
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
  const t = useT();
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
      toast.success(t("credentialsPanel.blankCardBound"));
      setTokenInput("");
      setErrorMsg(null);
      onOpenChange(false);
      onSuccess();
    },
    onError: (err: any) => {
      setErrorMsg(err?.detail ?? err?.message ?? t("credentialsPanel.bindFailed"));
    },
  });

  const handleBind = async () => {
    setErrorMsg(null);
    const trimmed = tokenInput.trim();
    if (!trimmed) {
      setErrorMsg(t("credentialsPanel.scanOrEnterToken"));
      return;
    }

    const inspection = inspectToken(trimmed);
    if (!inspection.isValid) {
      setErrorMsg(inspection.errorMessage ?? t("credentials.invalidTokenStructure"));
      return;
    }

    setIsVerifying(true);
    try {
      const { data: resolved, error: resolveErr } = await resolveCredential({
        query: { token: trimmed },
      });
      if (resolveErr || !resolved) {
        setErrorMsg(t("credentialsPanel.tokenNotFoundInSystem"));
        setIsVerifying(false);
        return;
      }

      if (resolved.type !== "unbound") {
        setErrorMsg(
          t("credentialsPanel.alreadyBound", {
            status: resolved.credentialStatus,
            type: resolved.type,
          }),
        );
        setIsVerifying(false);
        return;
      }

      bindMutation.mutate(resolved.credentialId);
    } catch (err: any) {
      setErrorMsg(err?.message ?? t("credentials.verifyFailed"));
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
            {t("credentialsPanel.bindDialogTitle")}
          </DialogTitle>
          <DialogDescription>{t("credentialsPanel.bindDialogDescription")}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-3 py-2">
          <div>
            <label htmlFor="blank-token-input" className="text-xs font-semibold text-foreground mb-1 block">
              {t("credentialsPanel.blankCardTokenLabel")}
            </label>
            <Input
              id="blank-token-input"
              placeholder={t("credentials.tokenPlaceholder")}
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
            {t("common.cancel")}
          </Button>
          <Button
            disabled={!tokenInput.trim() || isVerifying || bindMutation.isPending}
            onClick={handleBind}
          >
            {isVerifying || bindMutation.isPending
              ? t("credentialsPanel.activating")
              : t("credentialsPanel.bindCard")}
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
  const t = useT();
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
      toast.success(subjectType === "user" ? t("credentialsPanel.cardIssued") : t("credentialsPanel.credentialIssued"));
    },
    onError: () => toast.error(t("credentialsPanel.issueFailed")),
  });

  const reprintMutation = useMutation({
    mutationFn: async (id: string) => reprintCredential({ path: { id } }),
    onSuccess: async ({ data, error }) => {
      if (error) throw error;
      await invalidate();
      setRevealToken(data?.token);
    },
    onError: () => toast.error(t("credentialsPanel.reprintFailed")),
  });

  const revealMutation = useMutation({
    mutationFn: async (id: string) => revealCredential({ path: { id } }),
    onSuccess: async ({ data, error }) => {
      if (error) throw error;
      await invalidate();
      setRevealToken(data?.token);
    },
    onError: () => toast.error(t("credentialsPanel.revealFailed")),
  });

  const revokeMutation = useMutation({
    mutationFn: async ({ id, reason }: { id: string; reason: string }) =>
      revokeCredential({ path: { id }, body: { reason } }),
    onSuccess: async ({ error }) => {
      if (error) throw error;
      await invalidate();
      setRevokeTarget(null);
      toast.success(t("credentialsPanel.credentialRevoked"));
    },
    onError: () => toast.error(t("credentialsPanel.revokeFailed")),
  });

  const reissueMutation = useMutation({
    mutationFn: async ({ id, reason }: { id: string; reason: string }) =>
      reissueCredential({ path: { id }, body: { reason } }),
    onSuccess: async ({ data, error }) => {
      if (error) throw error;
      await invalidate();
      setReissueTarget(null);
      setRevealToken(data?.token);
      toast.success(subjectType === "user" ? t("credentialsPanel.cardReissued") : t("credentialsPanel.credentialReissued"));
    },
    onError: () => toast.error(t("credentialsPanel.reissueFailed")),
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
            {subjectType === "user" ? t("credentialsPanel.borrowerCardsHeading") : t("credentialsPanel.deviceCredentialsHeading")}
          </h3>
        </div>
        <div className="flex items-center gap-2">
          {subjectType === "user" && !hasActive && (
            <Button size="sm" variant="outline" onClick={() => setBindOpen(true)}>
              <LinkIcon className="size-4" data-icon="inline-start" />
              {t("credentialsPanel.bindBlankCard")}
            </Button>
          )}
          <Button size="sm" variant={hasActive ? "outline" : "default"} onClick={() => setIssueOpen(true)}>
            <Plus className="size-4" data-icon="inline-start" />
            {subjectType === "user" ? t("credentialsPanel.issueNewCard") : t("credentialsPanel.issueCredential")}
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
              {subjectType === "user" ? t("credentialsPanel.noCardIssued") : t("credentialsPanel.noCredentialIssued")}
            </p>
            <p className="text-xs text-muted-foreground max-w-xs mt-1">
              {subjectType === "user"
                ? t("credentialsPanel.noCardIssuedHint")
                : t("credentialsPanel.noCredentialIssuedHint")}
            </p>
          </div>
          <div className="flex items-center gap-2 mt-2">
            {subjectType === "user" && (
              <Button size="sm" variant="outline" onClick={() => setBindOpen(true)}>
                <LinkIcon className="size-4" data-icon="inline-start" />
                {t("credentialsPanel.bindBlankCard")}
              </Button>
            )}
            <Button size="sm" onClick={() => setIssueOpen(true)}>
              <Plus className="size-4" data-icon="inline-start" />
              {subjectType === "user" ? t("credentialsPanel.issueCard") : t("credentialsPanel.issueCredential")}
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
              <p className="font-semibold text-sm">{t("credentialsPanel.allRevoked")}</p>
              <p className="text-muted-foreground mt-1">
                {subjectType === "user"
                  ? t("credentialsPanel.allRevokedHintUser")
                  : t("credentialsPanel.allRevokedHintDevice")}
              </p>
              <div className="flex items-center gap-2 mt-3">
                {subjectType === "user" && (
                  <Button size="sm" variant="outline" onClick={() => setBindOpen(true)}>
                    <LinkIcon className="size-4" data-icon="inline-start" />
                    {t("credentialsPanel.bindBlankCard")}
                  </Button>
                )}
                <Button size="sm" onClick={() => setIssueOpen(true)}>
                  <Plus className="size-4" data-icon="inline-start" />
                  {subjectType === "user" ? t("credentialsPanel.issueNewCard") : t("credentialsPanel.issueCredential")}
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
                        {t("credentialsPanel.issueSeq", { seq: c.issueSeq })}
                      </span>
                      {replacedBySeq && (
                        <Badge variant="secondary" className="text-[11px] text-muted-foreground">
                          {t("credentialsPanel.replacedBySeq", { seq: replacedBySeq })}
                        </Badge>
                      )}
                    </div>

                    <div className="mt-2 flex items-center gap-2">
                      <span className="text-xs text-muted-foreground">{t("credentialsPanel.tokenLabel")}</span>
                      <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs font-semibold text-foreground">
                        …{c.tokenPreview ?? "————"}
                      </code>
                    </div>

                    <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                      <span>
                        {t("credentialsPanel.issuedOn", { date: new Date(c.issuedAt).toLocaleDateString() })}
                      </span>
                      <span>{t("credentialsPanel.byLabel", { actor: c.issuedBy })}</span>
                      {c.printedCount > 0 && (
                        <span>
                          {t("credentialsPanel.printedCount", { count: c.printedCount })}{" "}
                          {c.lastPrintedAt &&
                            t("credentialsPanel.lastPrintedOn", {
                              date: new Date(c.lastPrintedAt).toLocaleDateString(),
                            })}
                        </span>
                      )}
                    </div>

                    {c.status === "revoked" && (
                      <div className="mt-2.5 rounded-md border border-destructive/20 bg-destructive/5 p-2.5 text-xs text-muted-foreground">
                        <div className="flex items-center gap-1.5 font-semibold text-destructive">
                          <ShieldX className="size-3.5" />
                          <span>
                            {t("credentialsPanel.revokedOn", {
                              date: c.revokedAt ? new Date(c.revokedAt).toLocaleDateString() : "—",
                            })}
                          </span>
                          {c.revokedBy && <span>{t("credentialsPanel.byLabel", { actor: c.revokedBy })}</span>}
                        </div>
                        {c.revokedReason && (
                          <p className="mt-1 text-foreground font-mono">
                            {t("credentialsPanel.reasonQuoted", { reason: c.revokedReason })}
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
                            title={t("credentialsPanel.printLabelAction")}
                            onClick={() => reprintMutation.mutate(c.id)}
                            disabled={reprintMutation.isPending}
                          >
                            <Printer className="size-4" data-icon="inline-start" />
                            {t("credentialsPanel.reprint")}
                          </Button>
                        )}
                        {subjectType === "user" ? (
                          <>
                            <Button
                              size="sm"
                              variant="outline"
                              title={t("credentialsPanel.viewQrAction")}
                              onClick={() => revealMutation.mutate(c.id)}
                              disabled={revealMutation.isPending}
                            >
                              <QrCode className="size-4" data-icon="inline-start" />
                              {t("credentialsPanel.viewQr")}
                            </Button>
                            <Button
                              size="sm"
                              variant="outline"
                              className="border-amber-500/30 text-amber-700 hover:bg-amber-50 dark:text-amber-400 dark:hover:bg-amber-950/30"
                              title={t("credentialsPanel.reportLostAction")}
                              onClick={() => setReissueTarget(c)}
                            >
                              <RotateCcw className="size-4" data-icon="inline-start" />
                              {t("credentialsPanel.reportLost")}
                            </Button>
                          </>
                        ) : (
                          <Button
                            size="sm"
                            variant="outline"
                            title={t("credentialsPanel.reissueCredentialAction")}
                            onClick={() => setReissueTarget(c)}
                          >
                            <RotateCcw className="size-4" data-icon="inline-start" />
                            {t("credentialsPanel.reissue")}
                          </Button>
                        )}
                        <Button
                          size="icon-sm"
                          variant="ghost"
                          className="text-destructive hover:bg-destructive/10"
                          title={t("credentialsPanel.revokeAction")}
                          onClick={() => setRevokeTarget(c)}
                        >
                          <ShieldX className="size-4" />
                        </Button>
                      </>
                    )}
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      title={t("credentialsPanel.viewHistoryAction")}
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
              {subjectType === "user" ? t("credentialsPanel.issueNewCardTitle") : t("credentialsPanel.issueNewCredentialTitle")}
            </DialogTitle>
            <DialogDescription>{t("credentialsPanel.issueDialogDescription")}</DialogDescription>
          </DialogHeader>
          <div className="py-2">
            <label htmlFor="issue-kind-select" className="text-xs font-semibold text-foreground mb-1 block">
              {t("credentialsPanel.symbologyLabel")}
            </label>
            <Select value={issueKind} onValueChange={(v) => setIssueKind(v as CredentialKind)}>
              <SelectTrigger id="issue-kind-select" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ISSUABLE_KINDS.map((k) => (
                  <SelectItem key={k} value={k}>
                    {k === "qr" ? t("credentialsPanel.qrRecommended") : t("credentials.formatCode128")}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setIssueOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button disabled={issueMutation.isPending} onClick={() => issueMutation.mutate()}>
              {issueMutation.isPending ? t("credentialsPanel.issuing") : t("credentialsPanel.issue")}
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
        title={t("credentialsPanel.revokeConfirmTitle")}
        description={t("credentialsPanel.revokeConfirmDescription")}
        confirmLabel={t("credentialsPanel.revokeConfirmButton")}
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
        title={subjectType === "user" ? t("credentialsPanel.reissueConfirmTitleUser") : t("credentialsPanel.reissueConfirmTitleDevice")}
        description={
          subjectType === "user"
            ? t("credentialsPanel.reissueConfirmDescriptionUser")
            : t("credentialsPanel.reissueConfirmDescriptionDevice")
        }
        confirmLabel={subjectType === "user" ? t("credentialsPanel.reissueConfirmButtonUser") : t("credentialsPanel.reissueConfirmButtonDevice")}
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
