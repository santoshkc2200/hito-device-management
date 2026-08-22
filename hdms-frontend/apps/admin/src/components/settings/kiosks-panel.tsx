import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm, Controller } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { toast } from "sonner";
import { Link } from "@tanstack/react-router";
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
  Radio,
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

const registerKioskSchema = z.object({
  name: z.string().min(1, "Name is required"),
  location: z.string().optional(),
});
type RegisterKioskValues = z.infer<typeof registerKioskSchema>;

const editKioskSchema = z.object({
  name: z.string().min(1, "Name is required"),
  location: z.string().optional(),
  scanner: z.boolean(),
  camera: z.boolean(),
  manual: z.boolean(),
  nfc: z.boolean(),
});
type EditKioskValues = z.infer<typeof editKioskSchema>;

export function KiosksPanel() {
  const queryClient = useQueryClient();
  const { role } = useRole();
  const isAdmin = role === "admin";

  // Modals state
  const [registerOpen, setRegisterOpen] = useState(false);
  const [revealedToken, setRevealedToken] = useState<{ title: string; kioskName: string; token: string } | null>(null);
  const [pairingModal, setPairingModal] = useState<{ kioskName: string; code: string; expiresAt: string } | null>(null);
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
    resolver: zodResolver(registerKioskSchema),
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
      toast.success("Kiosk registered successfully");
      if (data?.token) {
        setRevealedToken({
          title: "Kiosk Registration Token",
          kioskName: data.name,
          token: data.token,
        });
      }
    },
    onError: (err: any) => {
      toast.error(err?.detail || "Failed to register kiosk");
    },
  });

  // 3. Edit Kiosk Form & Mutation
  const editForm = useForm<EditKioskValues>({
    resolver: zodResolver(editKioskSchema),
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
      toast.success("Kiosk configuration updated");
    },
    onError: (err: any) => {
      toast.error(err?.detail || "Failed to update kiosk");
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
      toast.success(`Token rotated for ${kiosk.name}`);
      if (data?.token) {
        setRevealedToken({
          title: "New Kiosk Bearer Token",
          kioskName: kiosk.name,
          token: data.token,
        });
      }
    },
    onError: (err: any) => {
      toast.error(err?.detail || "Failed to rotate token");
    },
  });

  // 5. Pairing Code Mutation
  const pairingCodeMutation = useMutation({
    mutationFn: async (kiosk: Kiosk) => {
      const res = await createKioskPairingCode({
        path: { id: kiosk.id },
      });
      if (res.error) throw res.error;
      return { kiosk, data: res.data };
    },
    onSuccess: ({ kiosk, data }) => {
      if (data?.code) {
        setPairingModal({
          kioskName: kiosk.name,
          code: data.code,
          expiresAt: data.expiresAt ? new Date(data.expiresAt).toLocaleTimeString() : "10 minutes",
        });
      }
    },
    onError: (err: any) => {
      toast.error(err?.detail || "Failed to generate pairing code");
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
      toast.success(`Kiosk ${enable ? "enabled" : "disabled"} successfully`);
    },

    onError: (err: any) => {
      toast.error(err?.detail || "Failed to change kiosk status");
    },
  });

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    setHasCopied(true);
    toast.success("Copied to clipboard");
    setTimeout(() => setHasCopied(false), 2500);
  };

  const isQuietKiosk = (kiosk: Kiosk) => {
    if (!kiosk.lastSeenAt) return true;
    const lastSeen = new Date(kiosk.lastSeenAt).getTime();
    const twentyFourHoursAgo = Date.now() - 24 * 60 * 60 * 1000;
    return lastSeen < twentyFourHoursAgo;
  };

  if (isLoading) {
    return <LoadingState message="Loading kiosk terminals..." />;
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center p-8 text-center text-destructive">
        <AlertTriangle className="size-8 mb-2" />
        <p className="font-semibold">Failed to load kiosks</p>
        <p className="text-xs text-muted-foreground mt-1">Please try refreshing the page.</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Hardware Diagnostic Banner */}
      <div className="flex items-center justify-between rounded-xl border border-border bg-card p-5 shadow-2xs">
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <span className="font-semibold text-foreground text-sm">Scanner & Card Reader Diagnostic</span>
            <Badge variant="secondary" className="font-mono text-[10px] uppercase">
              Hardware Tool
            </Badge>
          </div>
          <p className="text-xs text-muted-foreground max-w-xl">
            Test hardware USB barcode scanners, RFID/NFC wedge readers, and raw credential token grammar directly on this workstation.
          </p>
        </div>
        <Link to="/card-reader-test">
          <Button size="sm" variant="outline" className="gap-2">
            <Radio className="size-4 text-primary" />
            Launch Diagnostic Tool
          </Button>
        </Link>
      </div>

      {/* Kiosks Table Card */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
          <div>
            <CardTitle>Registered Kiosk Terminals</CardTitle>
            <CardDescription>
              Hardware tablets, desktop stations, and mobile scan points deployed across hospital wards.
            </CardDescription>
          </div>
          {isAdmin && (
            <Button size="sm" onClick={() => setRegisterOpen(true)} className="gap-1.5">
              <Plus className="size-4" />
              Register Kiosk
            </Button>
          )}
        </CardHeader>
        <CardContent>
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Status</TableHead>
                  <TableHead>Kiosk Name</TableHead>
                  <TableHead>Location</TableHead>
                  <TableHead>Scan Sources</TableHead>
                  <TableHead>Last Activity</TableHead>
                  {isAdmin && <TableHead className="w-16 text-right">Actions</TableHead>}
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
                                Active
                              </Badge>
                            ) : (
                              <Badge variant="secondary" className="text-muted-foreground bg-muted">
                                Disabled
                              </Badge>
                            )}
                            {isActive && quiet && (
                              <span
                                className="flex items-center text-amber-600 dark:text-amber-400 text-xs gap-1"
                                title="No activity recorded in over 24 hours"
                              >
                                <AlertTriangle className="size-3.5" />
                                <span className="text-[11px] font-medium">Quiet</span>
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
                          {kiosk.location || <span className="text-xs italic">Unspecified</span>}
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
                              <span className="text-xs text-muted-foreground italic">None</span>
                            )}
                          </div>
                        </TableCell>
                        <TableCell className="text-muted-foreground text-xs">
                          {kiosk.lastSeenAt ? (
                            <span title={new Date(kiosk.lastSeenAt).toLocaleString()}>
                              {new Date(kiosk.lastSeenAt).toLocaleString()}
                            </span>
                          ) : (
                            <span className="italic text-muted-foreground">Never</span>
                          )}
                        </TableCell>
                        {isAdmin && (
                          <TableCell className="text-right">
                            <DropdownMenu>
                              <DropdownMenuTrigger asChild>
                                <Button variant="ghost" size="icon-sm" aria-label={`Actions for ${kiosk.name}`}>
                                  <MoreHorizontal className="size-4" />
                                </Button>
                              </DropdownMenuTrigger>
                              <DropdownMenuContent align="end" className="w-52">
                                <DropdownMenuItem onClick={() => pairingCodeMutation.mutate(kiosk)}>
                                  <QrCode className="size-4 mr-2" />
                                  Issue Pairing Code
                                </DropdownMenuItem>
                                <DropdownMenuItem onClick={() => openEditDialog(kiosk)}>
                                  <PencilLine className="size-4 mr-2" />
                                  Edit Configuration
                                </DropdownMenuItem>
                                <DropdownMenuItem onClick={() => rotateTokenMutation.mutate(kiosk)}>
                                  <RefreshCw className="size-4 mr-2" />
                                  Rotate Bearer Token
                                </DropdownMenuItem>
                                <DropdownMenuSeparator />
                                {isActive ? (
                                  <DropdownMenuItem
                                    onClick={() => toggleStatusMutation.mutate({ kiosk, enable: false })}
                                    className="text-destructive focus:text-destructive"
                                  >
                                    <PowerOff className="size-4 mr-2" />
                                    Disable Kiosk
                                  </DropdownMenuItem>
                                ) : (
                                  <DropdownMenuItem
                                    onClick={() => toggleStatusMutation.mutate({ kiosk, enable: true })}
                                  >
                                    <Power className="size-4 mr-2" />
                                    Enable Kiosk
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
                      No kiosk terminals registered yet.
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
            <DialogTitle>Register New Kiosk</DialogTitle>
            <DialogDescription>
              Add a new tablet or computer terminal to the device checkout network.
            </DialogDescription>
          </DialogHeader>
          <form
            onSubmit={registerForm.handleSubmit((v) => registerMutation.mutate(v))}
            className="space-y-4 py-2"
          >
            <div className="space-y-2">
              <label htmlFor="reg-kiosk-name" className="text-sm font-medium">
                Kiosk Name
              </label>
              <Input
                id="reg-kiosk-name"
                placeholder="e.g. ICU Station 1, Central Supply Desk"
                autoFocus
                {...registerForm.register("name")}
              />
              {registerForm.formState.errors.name && (
                <p className="text-xs text-destructive">{registerForm.formState.errors.name.message}</p>
              )}
            </div>

            <div className="space-y-2">
              <label htmlFor="reg-kiosk-loc" className="text-sm font-medium">
                Location / Department
              </label>
              <Input
                id="reg-kiosk-loc"
                placeholder="e.g. Main Hospital 3F - Ward 3B"
                {...registerForm.register("location")}
              />
            </div>

            <DialogFooter className="pt-4">
              <Button type="button" variant="outline" onClick={() => setRegisterOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={registerMutation.isPending}>
                {registerMutation.isPending ? "Registering..." : "Register Kiosk"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Edit Kiosk Dialog */}
      <Dialog open={!!editingKiosk} onOpenChange={(open) => !open && setEditingKiosk(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Edit Kiosk: {editingKiosk?.name}</DialogTitle>
            <DialogDescription>
              Configure name, physical location, and enabled barcode/RFID scan inputs.
            </DialogDescription>
          </DialogHeader>
          <form
            onSubmit={editForm.handleSubmit((v) => editMutation.mutate(v))}
            className="space-y-4 py-2"
          >
            <div className="space-y-2">
              <label htmlFor="edit-kiosk-name" className="text-sm font-medium">
                Kiosk Name
              </label>
              <Input id="edit-kiosk-name" {...editForm.register("name")} />
              {editForm.formState.errors.name && (
                <p className="text-xs text-destructive">{editForm.formState.errors.name.message}</p>
              )}
            </div>

            <div className="space-y-2">
              <label htmlFor="edit-kiosk-loc" className="text-sm font-medium">
                Location
              </label>
              <Input id="edit-kiosk-loc" {...editForm.register("location")} />
            </div>

            <div className="space-y-3 pt-2">
              <label className="text-sm font-medium">Enabled Scan Input Sources</label>
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
                    Hardware Scanner
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
                    Camera Video
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
                    Manual Entry
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
                    NFC / RFID Reader
                  </label>
                </div>
              </div>
            </div>

            <DialogFooter className="pt-4">
              <Button type="button" variant="outline" onClick={() => setEditingKiosk(null)}>
                Cancel
              </Button>
              <Button type="submit" disabled={editMutation.isPending}>
                {editMutation.isPending ? "Saving..." : "Save Changes"}
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
              Copy this token now. It authorizes {revealedToken?.kioskName} to communicate with the HDMS API and will never be displayed again.
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
                onClick={() => revealedToken && copyToClipboard(revealedToken.token)}
              >
                {hasCopied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
                {hasCopied ? "Copied" : "Copy"}
              </Button>
            </div>
            <div className="rounded-md bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-900 p-3 text-xs text-amber-800 dark:text-amber-300 flex items-start gap-2">
              <AlertTriangle className="size-4 shrink-0 mt-0.5" />
              <span>
                Store this token securely in the kiosk application's environment or setup screen. If lost, you will need to rotate the token.
              </span>
            </div>
          </div>
          <DialogFooter>
            <Button onClick={() => setRevealedToken(null)}>Done</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Pairing Code Modal */}
      <Dialog open={!!pairingModal} onOpenChange={(open) => !open && setPairingModal(null)}>
        <DialogContent className="sm:max-w-md text-center">
          <DialogHeader>
            <DialogTitle className="flex items-center justify-center gap-2 text-center">
              <QrCode className="size-5 text-primary" />
              Pair Kiosk: {pairingModal?.kioskName}
            </DialogTitle>
            <DialogDescription className="text-center">
              Enter this single-use code on the tablet setup screen to automatically pair and mint a secure token.
            </DialogDescription>
          </DialogHeader>
          <div className="py-6 space-y-4">
            <div className="font-mono text-4xl tracking-widest font-bold py-4 px-6 bg-primary/10 text-primary rounded-xl border border-primary/20 inline-block select-all">
              {pairingModal?.code}
            </div>
            <p className="text-xs text-muted-foreground flex items-center justify-center gap-1.5">
              <Clock className="size-3.5" />
              Expires at {pairingModal?.expiresAt}
            </p>
          </div>
          <DialogFooter className="sm:justify-center">
            <Button onClick={() => setPairingModal(null)} className="w-full sm:w-auto">
              Close
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
