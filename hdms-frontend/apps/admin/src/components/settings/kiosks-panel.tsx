import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm, Controller } from "react-hook-form";
import { z } from "zod";
import { useLocalizedResolver } from "@/lib/localized-resolver";
import { toast } from "sonner";
import {
  listKiosks,
  createKiosk,
  updateKiosk,
  enableKiosk,
  disableKiosk,
  rotateKioskToken,
  createKioskPairingCode,
  type Kiosk,
} from "@hdms/api-client";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
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
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { LoadingState } from "@/components/states";
import { useRole } from "@/lib/use-role";
import {
  MoreHorizontal,
  Plus,
  RefreshCw,
  Key,
  QrCode,
  PencilLine,
  PowerOff,
  Power,
  Copy,
  Check,
  AlertTriangle,
  Clock,
  Tablet,
} from "lucide-react";
import { useT } from "@/i18n";

const registerKioskSchema = z.object({
  name: z.string().min(1, "validation.nameRequired"),
  location: z.string().optional(),
});
type RegisterKioskValues = z.infer<typeof registerKioskSchema>;

const editKioskSchema = z.object({
  name: z.string().min(1, "validation.nameRequired"),
  location: z.string().optional(),
  scanner: z.boolean(),
  camera: z.boolean(),
  manual: z.boolean(),
  nfc: z.boolean(),
});
type EditKioskValues = z.infer<typeof editKioskSchema>;

type PairingModal = {
  kioskId: string;
  kioskName: string;
  location?: string;
  code: string | null;
  expiresAt: string | null;
  fallbackToken?: string;
};

type PairingKiosk = Kiosk & { fallbackToken?: string };

function PairingCountdown({ expiresAt, onExpired }: { expiresAt: string | null; onExpired: () => void }) {
  const t = useT();
  const [remainingSeconds, setRemainingSeconds] = useState(() => remaining(expiresAt));

  useEffect(() => {
    setRemainingSeconds(remaining(expiresAt));
    if (!expiresAt) return;
    const timer = window.setInterval(() => setRemainingSeconds(remaining(expiresAt)), 1_000);
    return () => window.clearInterval(timer);
  }, [expiresAt]);

  useEffect(() => {
    if (expiresAt && remaining(expiresAt) === 0) onExpired();
  }, [expiresAt, onExpired]);

  if (!expiresAt || remainingSeconds === 0) {
    return <p role="status" className="text-sm font-medium text-destructive">{t("kiosksPanel.codeExpired")}</p>;
  }

  const minutes = Math.floor(remainingSeconds / 60);
  const seconds = remainingSeconds % 60;
  return (
    <p className="text-xs text-muted-foreground">
      {t("kiosksPanel.remainingCountdown", {
        time: `${minutes}:${seconds.toString().padStart(2, "0")}`,
      })}
    </p>
  );
}

function remaining(expiresAt: string | null): number {
  if (!expiresAt) return 0;
  return Math.max(0, Math.ceil((new Date(expiresAt).getTime() - Date.now()) / 1_000));
}

export function KiosksPanel() {
  const t = useT();
  const queryClient = useQueryClient();
  const { role } = useRole();
  const isAdmin = role === "admin";

  // Modals state
  const [registerOpen, setRegisterOpen] = useState(false);
  const [revealedToken, setRevealedToken] = useState<{ title: string; kioskName: string; token: string } | null>(null);
  const [pairingModal, setPairingModal] = useState<PairingModal | null>(null);
  const [editingKiosk, setEditingKiosk] = useState<Kiosk | null>(null);
  const [hasCopied, setHasCopied] = useState(false);

  // 1. Fetch Kiosks
  const {
    data: kiosksData,
    isLoading,
    error,
  } = useQuery({
    queryKey: ["kiosks"],
    queryFn: async () => {
      const res = await listKiosks();
      if (res.error) throw res.error;
      return res.data?.items ?? [];
    },
  });

  // 2. Register Kiosk Form & Mutation
  const registerForm = useForm<RegisterKioskValues>({
    resolver: useLocalizedResolver(registerKioskSchema),
    defaultValues: { name: "", location: "" },
  });

  const registerMutation = useMutation({
    mutationFn: async (values: RegisterKioskValues) => {
      const res = await createKiosk({
        body: {
          name: values.name,
          location: values.location || undefined,
        },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ["kiosks"] });
      setRegisterOpen(false);
      registerForm.reset();
      toast.success(t("kiosksPanel.kioskRegistered"));
      if (data) {
        pairingCodeMutation.mutate({
          id: data.id,
          name: data.name,
          location: data.location,
          enabledSources: data.enabledSources,
          status: data.status,
          lastSeenAt: data.lastSeenAt,
          createdAt: data.createdAt,
          defaultLocale: data.defaultLocale,
          fallbackToken: data.token,
        });
      }
    },
    onError: (err: any) => {
      toast.error(err?.detail || t("kiosksPanel.registerFailed"));
    },
  });

  // 3. Edit Kiosk Form & Mutation
  const editForm = useForm<EditKioskValues>({
    resolver: useLocalizedResolver(editKioskSchema),
    defaultValues: {
      name: "",
      location: "",
      scanner: true,
      camera: false,
      manual: true,
      nfc: false,
    },
  });

  const openEditDialog = (kiosk: Kiosk) => {
    setEditingKiosk(kiosk);
    const sources = new Set(kiosk.enabledSources || []);
    editForm.reset({
      name: kiosk.name,
      location: kiosk.location ?? "",
      scanner: sources.has("scanner"),
      camera: sources.has("camera"),
      manual: sources.has("manual"),
      nfc: sources.has("nfc"),
    });
  };

  const editMutation = useMutation({
    mutationFn: async (values: EditKioskValues) => {
      if (!editingKiosk) return;
      const enabledSources: string[] = [];
      if (values.scanner) enabledSources.push("scanner");
      if (values.camera) enabledSources.push("camera");
      if (values.manual) enabledSources.push("manual");
      if (values.nfc) enabledSources.push("nfc");

      const res = await updateKiosk({
        path: { id: editingKiosk.id },
        body: {
          name: values.name,
          location: values.location || undefined,
          enabledSources,
        },
      });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["kiosks"] });
      setEditingKiosk(null);
      toast.success(t("kiosksPanel.kioskUpdated"));
    },
    onError: (err: any) => {
      toast.error(err?.detail || t("kiosksPanel.updateFailed"));
    },
  });

  // 4. Rotate Token Mutation
  const rotateTokenMutation = useMutation({
    mutationFn: async (kiosk: Kiosk) => {
      const res = await rotateKioskToken({
        path: { id: kiosk.id },
      });
      if (res.error) throw res.error;
      return { kiosk, data: res.data };
    },
    onSuccess: ({ kiosk, data }) => {
      queryClient.invalidateQueries({ queryKey: ["kiosks"] });
      toast.success(t("kiosksPanel.tokenRotatedToast", { name: kiosk.name }));
      if (data?.token) {
        setRevealedToken({
          title: t("kiosksPanel.newBearerTokenTitle"),
          kioskName: kiosk.name,
          token: data.token,
        });
      }
    },
    onError: (err: any) => {
      toast.error(err?.detail || t("kiosksPanel.rotateTokenFailed"));
    },
  });

  // 5. Pairing Code Mutation
  const pairingCodeMutation = useMutation({
    mutationFn: async (kiosk: PairingKiosk) => {
      const res = await createKioskPairingCode({
        path: { id: kiosk.id },
      });
      if (res.error) throw res.error;
      return { kiosk, data: res.data };
    },
    onSuccess: ({ kiosk, data }) => {
      if (data?.code) {
        setPairingModal({
          kioskId: kiosk.id,
          kioskName: kiosk.name,
          code: data.code,
          expiresAt: data.expiresAt ?? null,
          location: kiosk.location,
          fallbackToken: kiosk.fallbackToken,
        });
      }
    },
    onError: (err: any) => {
      toast.error(err?.detail || t("kiosksPanel.pairingCodeFailed"));
    },
  });

  // 6. Enable / Disable Mutations
  const toggleStatusMutation = useMutation({
    mutationFn: async ({ kiosk, enable }: { kiosk: Kiosk; enable: boolean }) => {
      const res = enable
        ? await enableKiosk({ path: { id: kiosk.id } })
        : await disableKiosk({ path: { id: kiosk.id } });
      if (res.error) throw res.error;
      return res.data;
    },
    onSuccess: (_, { enable }) => {
      queryClient.invalidateQueries({ queryKey: ["kiosks"] });
      toast.success(
        enable ? t("kiosksPanel.kioskEnabledToast") : t("kiosksPanel.kioskDisabledToast"),
      );
    },

    onError: (err: any) => {
      toast.error(err?.detail || t("kiosksPanel.statusChangeFailed"));
    },
  });

  const copyToClipboard = async (text: string) => {
    await navigator.clipboard.writeText(text);
    setHasCopied(true);
    setTimeout(() => setHasCopied(false), 2500);
  };

  const issuePairingCode = (kiosk: PairingKiosk) => {
    setPairingModal({ kioskId: kiosk.id, kioskName: kiosk.name, location: kiosk.location, code: null, expiresAt: null });
    pairingCodeMutation.mutate(kiosk);
  };

  const isQuietKiosk = (kiosk: Kiosk) => {
    if (!kiosk.lastSeenAt) return true;
    const lastSeen = new Date(kiosk.lastSeenAt).getTime();
    const twentyFourHoursAgo = Date.now() - 24 * 60 * 60 * 1000;
    return lastSeen < twentyFourHoursAgo;
  };

  if (isLoading) {
    return <LoadingState message={t("kiosksPanel.loadingKiosks")} />;
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center p-8 text-center text-destructive">
        <AlertTriangle className="size-8 mb-2" />
        <p className="font-semibold">{t("kiosksPanel.loadFailedTitle")}</p>
        <p className="text-xs text-muted-foreground mt-1">{t("kiosksPanel.tryRefreshing")}</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Kiosks Table Card */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
          <div>
            <CardTitle>{t("kiosksPanel.registeredKioskTerminals")}</CardTitle>
            <CardDescription>{t("kiosksPanel.registeredKioskTerminalsDescription")}</CardDescription>
          </div>
          {isAdmin && (
            <Button size="sm" onClick={() => setRegisterOpen(true)} className="gap-1.5">
              <Plus className="size-4" />
              {t("kiosksPanel.registerKiosk")}
            </Button>
          )}
        </CardHeader>
        <CardContent>
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("columns.status")}</TableHead>
                  <TableHead>{t("kiosksPanel.colKioskName")}</TableHead>
                  <TableHead>{t("kiosksPanel.colLocation")}</TableHead>
                  <TableHead>{t("kiosksPanel.colScanSources")}</TableHead>
                  <TableHead>{t("kiosksPanel.colLastActivity")}</TableHead>
                  {isAdmin && <TableHead className="w-16 text-right">{t("columns.actions")}</TableHead>}
                </TableRow>
              </TableHeader>
              <TableBody>
                {kiosksData && kiosksData.length > 0 ? (
                  kiosksData.map((kiosk) => {
                    const quiet = isQuietKiosk(kiosk);
                    const isActive = kiosk.status === "active";

                    return (
                      <TableRow key={kiosk.id}>
                        <TableCell>
                          <div className="flex items-center gap-2">
                            {isActive ? (
                              <Badge variant="outline" className="text-emerald-700 border-emerald-300 bg-emerald-50 dark:bg-emerald-950/30">
                                {t("adminAccountsPanel.statusActive")}
                              </Badge>
                            ) : (
                              <Badge variant="secondary" className="text-muted-foreground bg-muted">
                                {t("adminAccountsPanel.statusDisabled")}
                              </Badge>
                            )}
                            {isActive && quiet && (
                              <span
                                className="flex items-center text-amber-600 dark:text-amber-400 text-xs gap-1"
                                title={t("kiosksPanel.quietTooltip")}
                              >
                                <AlertTriangle className="size-3.5" />
                                <span className="text-[11px] font-medium">{t("kiosksPanel.quiet")}</span>
                              </span>
                            )}
                          </div>
                        </TableCell>
                        <TableCell className="font-medium text-foreground">
                          <div className="flex items-center gap-2">
                            <Tablet className="size-4 text-muted-foreground" />
                            {kiosk.name}
                          </div>
                        </TableCell>
                        <TableCell className="text-muted-foreground">
                          {kiosk.location || <span className="text-xs italic">{t("kiosksPanel.unspecified")}</span>}
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-wrap gap-1">
                            {kiosk.enabledSources && kiosk.enabledSources.length > 0 ? (
                              kiosk.enabledSources.map((src) => (
                                <Badge key={src} variant="outline" className="text-[10px] font-mono px-1.5 py-0">
                                  {src}
                                </Badge>
                              ))
                            ) : (
                              <span className="text-xs text-muted-foreground italic">{t("kiosksPanel.none")}</span>
                            )}
                          </div>
                        </TableCell>
                        <TableCell className="text-muted-foreground text-xs">
                          {kiosk.lastSeenAt ? (
                            <span title={new Date(kiosk.lastSeenAt).toLocaleString()}>
                              {new Date(kiosk.lastSeenAt).toLocaleString()}
                            </span>
                          ) : (
                            <span className="italic text-muted-foreground">{t("kiosksPanel.never")}</span>
                          )}
                        </TableCell>
                        {isAdmin && (
                          <TableCell className="text-right">
                            <DropdownMenu>
                              <DropdownMenuTrigger asChild>
                                <Button variant="ghost" size="icon-sm" aria-label={t("kiosksPanel.actionsForAria", { name: kiosk.name })}>
                                  <MoreHorizontal className="size-4" />
                                </Button>
                              </DropdownMenuTrigger>
                              <DropdownMenuContent align="end" className="w-52">
                                <DropdownMenuItem disabled={!isActive || pairingCodeMutation.isPending} onClick={() => issuePairingCode(kiosk)}>
                                  <QrCode className="size-4 mr-2" />
                                  {t("kiosksPanel.issuePairingCode")}
                                </DropdownMenuItem>
                                <DropdownMenuItem onClick={() => openEditDialog(kiosk)}>
                                  <PencilLine className="size-4 mr-2" />
                                  {t("kiosksPanel.editConfiguration")}
                                </DropdownMenuItem>
                                <DropdownMenuItem onClick={() => rotateTokenMutation.mutate(kiosk)}>
                                  <RefreshCw className="size-4 mr-2" />
                                  {t("kiosksPanel.rotateBearerToken")}
                                </DropdownMenuItem>
                                <DropdownMenuSeparator />
                                {isActive ? (
                                  <DropdownMenuItem
                                    onClick={() => toggleStatusMutation.mutate({ kiosk, enable: false })}
                                    className="text-destructive focus:text-destructive"
                                  >
                                    <PowerOff className="size-4 mr-2" />
                                    {t("kiosksPanel.disableKiosk")}
                                  </DropdownMenuItem>
                                ) : (
                                  <DropdownMenuItem
                                    onClick={() => toggleStatusMutation.mutate({ kiosk, enable: true })}
                                  >
                                    <Power className="size-4 mr-2" />
                                    {t("kiosksPanel.enableKiosk")}
                                  </DropdownMenuItem>
                                )}
                              </DropdownMenuContent>
                            </DropdownMenu>
                          </TableCell>
                        )}
                      </TableRow>
                    );
                  })
                ) : (
                  <TableRow>
                    <TableCell colSpan={isAdmin ? 6 : 5} className="text-center py-6 text-muted-foreground">
                      {t("kiosksPanel.noKiosksYet")}
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>

      {/* Register Kiosk Dialog */}
      <Dialog open={registerOpen} onOpenChange={setRegisterOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("kiosksPanel.registerNewKiosk")}</DialogTitle>
            <DialogDescription>{t("kiosksPanel.registerDialogDescription")}</DialogDescription>
          </DialogHeader>
          <form
            onSubmit={registerForm.handleSubmit((v) => registerMutation.mutate(v))}
            className="space-y-4 py-2"
          >
            <div className="space-y-2">
              <label htmlFor="reg-kiosk-name" className="text-sm font-medium">
                {t("kiosksPanel.kioskNameLabel")}
              </label>
              <Input
                id="reg-kiosk-name"
                placeholder={t("kiosksPanel.kioskNamePlaceholder")}
                autoFocus
                {...registerForm.register("name")}
              />
              {registerForm.formState.errors.name && (
                <p className="text-xs text-destructive">{registerForm.formState.errors.name.message}</p>
              )}
            </div>

            <div className="space-y-2">
              <label htmlFor="reg-kiosk-loc" className="text-sm font-medium">
                {t("kiosksPanel.locationDepartmentLabel")}
              </label>
              <Input
                id="reg-kiosk-loc"
                placeholder={t("kiosksPanel.locationPlaceholder")}
                {...registerForm.register("location")}
              />
            </div>

            <DialogFooter className="pt-4">
              <Button type="button" variant="outline" onClick={() => setRegisterOpen(false)}>
                {t("common.cancel")}
              </Button>
              <Button type="submit" disabled={registerMutation.isPending}>
                {registerMutation.isPending ? t("kiosksPanel.registering") : t("kiosksPanel.registerKiosk")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Edit Kiosk Dialog */}
      <Dialog open={!!editingKiosk} onOpenChange={(open) => !open && setEditingKiosk(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("kiosksPanel.editKioskTitle", { name: editingKiosk?.name ?? "" })}</DialogTitle>
            <DialogDescription>{t("kiosksPanel.editDialogDescription")}</DialogDescription>
          </DialogHeader>
          <form
            onSubmit={editForm.handleSubmit((v) => editMutation.mutate(v))}
            className="space-y-4 py-2"
          >
            <div className="space-y-2">
              <label htmlFor="edit-kiosk-name" className="text-sm font-medium">
                {t("kiosksPanel.kioskNameLabel")}
              </label>
              <Input id="edit-kiosk-name" {...editForm.register("name")} />
              {editForm.formState.errors.name && (
                <p className="text-xs text-destructive">{editForm.formState.errors.name.message}</p>
              )}
            </div>

            <div className="space-y-2">
              <label htmlFor="edit-kiosk-loc" className="text-sm font-medium">
                {t("kiosksPanel.colLocation")}
              </label>
              <Input id="edit-kiosk-loc" {...editForm.register("location")} />
            </div>

            <div className="space-y-3 pt-2">
              <label className="text-sm font-medium">{t("kiosksPanel.enabledScanSources")}</label>
              <div className="grid grid-cols-2 gap-3 border rounded-lg p-3">
                <div className="flex items-center space-x-2">
                  <Controller
                    name="scanner"
                    control={editForm.control}
                    render={({ field }) => (
                      <Checkbox
                        id="src-scanner"
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    )}
                  />
                  <label htmlFor="src-scanner" className="text-xs font-medium cursor-pointer">
                    {t("kiosksPanel.sourceHardwareScanner")}
                  </label>
                </div>

                <div className="flex items-center space-x-2">
                  <Controller
                    name="camera"
                    control={editForm.control}
                    render={({ field }) => (
                      <Checkbox
                        id="src-camera"
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    )}
                  />
                  <label htmlFor="src-camera" className="text-xs font-medium cursor-pointer">
                    {t("kiosksPanel.sourceCameraVideo")}
                  </label>
                </div>

                <div className="flex items-center space-x-2">
                  <Controller
                    name="manual"
                    control={editForm.control}
                    render={({ field }) => (
                      <Checkbox
                        id="src-manual"
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    )}
                  />
                  <label htmlFor="src-manual" className="text-xs font-medium cursor-pointer">
                    {t("kiosksPanel.sourceManualEntry")}
                  </label>
                </div>

                <div className="flex items-center space-x-2">
                  <Controller
                    name="nfc"
                    control={editForm.control}
                    render={({ field }) => (
                      <Checkbox
                        id="src-nfc"
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    )}
                  />
                  <label htmlFor="src-nfc" className="text-xs font-medium cursor-pointer">
                    {t("kiosksPanel.sourceNfcReader")}
                  </label>
                </div>
              </div>
            </div>

            <DialogFooter className="pt-4">
              <Button type="button" variant="outline" onClick={() => setEditingKiosk(null)}>
                {t("common.cancel")}
              </Button>
              <Button type="submit" disabled={editMutation.isPending}>
                {editMutation.isPending ? t("kiosksPanel.savingChanges") : t("kiosksPanel.saveChanges")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* One-Shot Token Reveal Dialog */}
      <Dialog open={!!revealedToken} onOpenChange={(open) => !open && setRevealedToken(null)}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Key className="size-5 text-amber-500" />
              {revealedToken?.title}
            </DialogTitle>
            <DialogDescription>
              {t("kiosksPanel.tokenRevealDescription", { name: revealedToken?.kioskName ?? "" })}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="relative">
              <div className="rounded-lg bg-muted p-4 font-mono text-sm break-all select-all border text-foreground">
                {revealedToken?.token}
              </div>
              <Button
                size="sm"
                variant="secondary"
                className="absolute top-2 right-2 gap-1.5"
                onClick={() => revealedToken && void copyToClipboard(revealedToken.token)}
              >
                {hasCopied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
                {hasCopied ? t("forcedTotpDialog.copied") : t("adminAccountsPanel.copy")}
              </Button>
            </div>
            <div className="rounded-md bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-900 p-3 text-xs text-amber-800 dark:text-amber-300 flex items-start gap-2">
              <AlertTriangle className="size-4 shrink-0 mt-0.5" />
              <span>{t("kiosksPanel.tokenStoreHint")}</span>
            </div>
          </div>
          <DialogFooter>
            <Button onClick={() => setRevealedToken(null)}>{t("adminAccountsPanel.done")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Pairing Code Modal */}
      <Dialog open={!!pairingModal} onOpenChange={(open) => !open && setPairingModal(null)}>
        <DialogContent className="sm:max-w-md text-center">
          <DialogHeader>
            <DialogTitle className="flex items-center justify-center gap-2 text-center">
              <QrCode className="size-5 text-primary" />
              {t("kiosksPanel.pairKioskTitle", { name: pairingModal?.kioskName ?? "" })}
            </DialogTitle>
            <DialogDescription className="text-center">{t("kiosksPanel.pairingDialogDescription")}</DialogDescription>
          </DialogHeader>
          <div className="py-6 space-y-4">
            {pairingModal?.code ? (
              <div className="space-y-3">
                <div data-testid="pairing-code" className="font-mono text-4xl tracking-[0.35em] font-bold py-4 px-6 bg-primary/10 text-primary rounded-xl border border-primary/20 inline-block select-all">
                  {pairingModal.code.slice(0, 3)} {pairingModal.code.slice(3)}
                </div>
                <div>
                  <Button variant="outline" size="sm" onClick={() => void copyToClipboard(pairingModal.code!)}>
                    {hasCopied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}{" "}
                    {hasCopied ? t("forcedTotpDialog.copied") : t("kiosksPanel.copyCode")}
                  </Button>
                  <span className="sr-only" aria-live="polite">{hasCopied ? t("kiosksPanel.pairingCodeCopied") : ""}</span>
                </div>
              </div>
            ) : pairingCodeMutation.isPending ? (
              <p className="text-sm text-muted-foreground">{t("kiosksPanel.issuingPairingCode")}</p>
            ) : null}
            <p className="text-xs text-muted-foreground flex items-center justify-center gap-1.5">
              <Clock className="size-3.5" />
              {pairingModal?.expiresAt
                ? t("kiosksPanel.expiresAt", { date: new Date(pairingModal.expiresAt).toLocaleString() })
                : t("kiosksPanel.noActiveCode")}
            </p>
            {pairingModal && <PairingCountdown expiresAt={pairingModal.expiresAt} onExpired={() => setPairingModal((current) => current ? { ...current, code: null, expiresAt: null } : null)} />}
            {pairingModal?.location && (
              <p className="text-xs text-muted-foreground">
                {t("kiosksPanel.locationLine", { location: pairingModal.location })}
              </p>
            )}
          </div>
          <DialogFooter className="sm:justify-center">
            {!pairingCodeMutation.isPending && (
              <Button variant="outline" onClick={() => pairingModal && issuePairingCode({ id: pairingModal.kioskId, name: pairingModal.kioskName, location: pairingModal.location, enabledSources: [], status: "active", createdAt: "", defaultLocale: "en" })}>
                {t("kiosksPanel.issueNewCode")}
              </Button>
            )}
            <Button onClick={() => setPairingModal(null)} className="w-full sm:w-auto">
              {t("common.close")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
