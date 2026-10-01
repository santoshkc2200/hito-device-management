import { type Device, listCredentialsBySubject, reprintCredential } from "@hdms/api-client";
import { useMutation } from "@tanstack/react-query";
import { Printer } from "lucide-react";
import { useEffect } from "react";
import { DeviceStickerLabel, LabelSheet } from "@/components/label-templates";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { SymbologyToggle } from "@/components/symbology-toggle";
import { Skeleton } from "@/components/ui/skeleton";
import { useCodeSymbology } from "@/lib/code-symbology";
import { useLabelSheetSettings } from "@/lib/label-settings";
import { useT } from "@/i18n";

// The real bulk-labelling workflow (docs/05-credentials-and-labeling.md):
// CSV import mints one device credential each; this reprints each device's
// existing token (device credentials store it reversibly — see
// credentialsapi.Service.Reprint) rather than minting anything new, so the
// sheet reflects the labels already bound to these devices.
async function loadDeviceLabel(device: Device) {
  const { data, error } = await listCredentialsBySubject({
    query: { subjectType: "device", subjectId: device.id },
  });
  if (error) throw error;
  const active = data.items.find((c) => c.status === "active");
  if (!active) return { device, token: null as string | null };
  const reprint = await reprintCredential({ path: { id: active.id } });
  if (reprint.error) throw reprint.error;
  return { device, token: reprint.data?.token ?? null };
}

export function DeviceLabelSheetDialog({
  devices,
  open,
  onOpenChange,
}: {
  devices: Device[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const t = useT();
  const [settings] = useLabelSheetSettings();
  const [symbology] = useCodeSymbology();

  // A manual one-shot mutation, not a query: reprinting increments
  // printed_count server-side, so this must run exactly once per dialog
  // open, never silently again on refetch/refocus.
  const mutation = useMutation({
    mutationFn: () => Promise.all(devices.map(loadDeviceLabel)),
  });

  useEffect(() => {
    if (open && devices.length > 0) mutation.mutate();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const results = mutation.data ?? [];
  const withToken = results.filter((d) => d.token);
  const withoutToken = results.filter((d) => !d.token);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>{t("deviceLabelSheetDialog.title")}</DialogTitle>
          <DialogDescription>
            {mutation.isPending
              ? t("deviceLabelSheetDialog.reprinting")
              : [
                  withToken.length === 1
                    ? t("deviceLabelSheetDialog.readyOne", { count: withToken.length })
                    : t("deviceLabelSheetDialog.readyOther", { count: withToken.length }),
                  withoutToken.length > 0
                    ? withoutToken.length === 1
                      ? t("deviceLabelSheetDialog.skippedOne", { count: withoutToken.length })
                      : t("deviceLabelSheetDialog.skippedOther", { count: withoutToken.length })
                    : null,
                ]
                  .filter(Boolean)
                  .join(" ")}
          </DialogDescription>
        </DialogHeader>
        <SymbologyToggle />
        {mutation.isPending ? (
          <Skeleton className="h-64 w-full" />
        ) : (
          <div className="max-h-[70vh] overflow-auto rounded-md border border-border bg-secondary p-4">
            <LabelSheet settings={settings}>
              {withToken.map(({ device, token }) => (
                <DeviceStickerLabel
                  key={device.id}
                  assetTag={device.assetTag}
                  name={device.name}
                  model={device.model}
                  token={token!}
                  symbology={symbology}
                  widthMm={settings.labelWidthMm}
                  heightMm={settings.labelHeightMm}
                />
              ))}
            </LabelSheet>
          </div>
        )}
        <div className="flex justify-end">
          <Button disabled={mutation.isPending || withToken.length === 0} onClick={() => window.print()}>
            <Printer className="size-4" data-icon="inline-start" />
            {t("deviceLabelSheetDialog.printSheet")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
