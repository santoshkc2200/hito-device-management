import { resolveCredential } from "@hdms/api-client";
import { formatToken, inspectToken, type TokenInspection } from "@hdms/domain";
import { useQuery } from "@tanstack/react-query";
import { createRoute, Link } from "@tanstack/react-router";
import {
  AlertCircle,
  ArrowLeft,
  CheckCircle2,
  Cpu,
  HelpCircle,
  Laptop,
  Layers,
  Radio,
  RefreshCw,
  Search,
  User,
  Zap,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { authenticatedRoute } from "./authenticated";
import { useT } from "@/i18n";

interface RawScanData {
  raw: string;
  source: "scanner_usb" | "manual_input";
  timestamp: Date;
  charTimesMs?: number[];
}

export function CardReaderTestPage() {
  const t = useT();
  const [manualInput, setManualInput] = useState("");
  const [activeScan, setActiveScan] = useState<RawScanData | null>(null);
  const [isListening, setIsListening] = useState(true);

  // USB HID wedge buffer tracking
  const bufferRef = useRef("");
  const lastKeyTimeRef = useRef(0);
  const keyIntervalsRef = useRef<number[]>([]);

  useEffect(() => {
    if (!isListening) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      // Don't intercept if user is focused on the manual input box
      if (
        document.activeElement?.tagName === "INPUT" ||
        document.activeElement?.tagName === "TEXTAREA"
      ) {
        return;
      }

      const now = Date.now();
      const timeSinceLastKey = lastKeyTimeRef.current > 0 ? now - lastKeyTimeRef.current : 0;
      lastKeyTimeRef.current = now;

      // Enter key marks end of barcode scanner sequence
      if (e.key === "Enter") {
        if (bufferRef.current.length >= 4) {
          const rawScanned = bufferRef.current;
          setActiveScan({
            raw: rawScanned,
            source: "scanner_usb",
            timestamp: new Date(),
            charTimesMs: [...keyIntervalsRef.current],
          });
        }
        bufferRef.current = "";
        keyIntervalsRef.current = [];
        lastKeyTimeRef.current = 0;
        return;
      }

      // Reset buffer if delay between keystrokes exceeds 1000ms
      if (timeSinceLastKey > 1000 && bufferRef.current.length > 0) {
        bufferRef.current = "";
        keyIntervalsRef.current = [];
      }

      if (e.key.length === 1) {
        bufferRef.current += e.key;
        keyIntervalsRef.current.push(timeSinceLastKey);
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [isListening]);

  const handleManualAnalyze = (textToAnalyze?: string) => {
    const target = textToAnalyze ?? manualInput;
    if (!target.trim()) return;
    setActiveScan({
      raw: target.trim(),
      source: "manual_input",
      timestamp: new Date(),
    });
  };

  const rawText = activeScan?.raw ?? "";
  const inspection: TokenInspection | null = rawText ? inspectToken(rawText) : null;

  // Query live database resolution if token has valid format or is present
  const { data: dbResolution, isLoading: isResolving, refetch: retryResolve } = useQuery({
    queryKey: ["credentials", "resolve-test", rawText],
    queryFn: async () => {
      if (!rawText) return null;
      const { data, error } = await resolveCredential({
        query: { token: rawText.trim().toUpperCase() },
      });
      if (error) {
        return { error: true, status: (error as any)?.status ?? 404, detail: (error as any)?.detail };
      }
      return { error: false, data };
    },
    enabled: Boolean(rawText),
  });

  // Calculate hex dump for raw input
  const hexDump = rawText
    ? Array.from(rawText)
        .map((c) => c.charCodeAt(0).toString(16).toUpperCase().padStart(2, "0"))
        .join(" ")
    : "";

  return (
    <div className="flex flex-col gap-6 max-w-5xl">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <div className="flex items-center gap-2 mb-1">
            <Link
              to="/credentials"
              className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
              <ArrowLeft className="size-3" /> {t("cardReaderTest.backToCredentials")}
            </Link>
            <span className="text-muted-foreground">·</span>
            <Badge variant="outline" className="text-[11px] font-mono border-primary/30 text-primary">
              {t("cardReaderTest.diagnosticSurfaceBadge")}
            </Badge>
          </div>
          <h1 className="text-2xl font-semibold tracking-tight text-foreground flex items-center gap-2">
            <Radio className="size-6 text-primary" />
            {t("cardReaderTest.title")}
          </h1>
          <p className="text-sm text-muted-foreground mt-0.5">{t("cardReaderTest.subtitle")}</p>
        </div>

        <div className="flex items-center gap-2">
          <Badge
            variant={isListening ? "secondary" : "outline"}
            className="flex items-center gap-1.5 px-3 py-1 text-xs"
          >
            <span
              className={`size-2 rounded-full ${
                isListening ? "bg-success animate-pulse" : "bg-muted-foreground"
              }`}
            />
            {isListening ? t("cardReaderTest.listenerActive") : t("cardReaderTest.listenerPaused")}
          </Badge>
          <Button
            size="sm"
            variant="outline"
            onClick={() => setIsListening(!isListening)}
          >
            {isListening ? t("cardReaderTest.pauseListener") : t("cardReaderTest.resumeListener")}
          </Button>
        </div>
      </div>

      {/* Quick Test / Manual Input Box */}
      <div className="rounded-xl border border-border bg-card p-5 shadow-xs">
        <h2 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-2">
          {t("cardReaderTest.step1Heading")}
        </h2>
        <div className="flex flex-col sm:flex-row gap-3">
          <Input
            placeholder={t("cardReaderTest.inputPlaceholder")}
            value={manualInput}
            onChange={(e) => setManualInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                handleManualAnalyze();
              }
            }}
            className="font-mono text-sm"
          />
          <Button onClick={() => handleManualAnalyze()}>
            <Search className="size-4" data-icon="inline-start" />
            {t("cardReaderTest.analyzeString")}
          </Button>
        </div>

        {/* Quick Sample Chips */}
        <div className="mt-3 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          <span>{t("cardReaderTest.quickSamples")}</span>
          <button
            type="button"
            className="rounded bg-muted px-2 py-1 font-mono text-[11px] text-foreground hover:bg-muted/80 transition-colors"
            onClick={() => {
              setManualInput("HD-U-B3G6822S6K-H");
              handleManualAnalyze("HD-U-B3G6822S6K-H");
            }}
          >
            {t("cardReaderTest.sampleValidUserQr")}
          </button>
          <button
            type="button"
            className="rounded bg-muted px-2 py-1 font-mono text-[11px] text-foreground hover:bg-muted/80 transition-colors"
            onClick={() => {
              setManualInput("HD-D-BRH8VFTBAA-7");
              handleManualAnalyze("HD-D-BRH8VFTBAA-7");
            }}
          >
            {t("cardReaderTest.sampleValidDeviceLabel")}
          </button>
          <button
            type="button"
            className="rounded bg-muted px-2 py-1 font-mono text-[11px] text-foreground hover:bg-muted/80 transition-colors"
            onClick={() => {
              setManualInput("hd-u-b3g6822s6k-9");
              handleManualAnalyze("hd-u-b3g6822s6k-9");
            }}
          >
            {t("cardReaderTest.sampleCorruptedChecksum")}
          </button>
          <button
            type="button"
            className="rounded bg-muted px-2 py-1 font-mono text-[11px] text-foreground hover:bg-muted/80 transition-colors"
            onClick={() => {
              setManualInput("E1234567890ABCDEF");
              handleManualAnalyze("E1234567890ABCDEF");
            }}
          >
            {t("cardReaderTest.sampleRawMifare")}
          </button>
        </div>
      </div>

      {/* No Input Placeholder */}
      {!activeScan && (
        <div className="flex flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-border bg-card/40 p-12 text-center">
          <div className="rounded-full bg-primary/10 p-4 text-primary">
            <Zap className="size-8 animate-pulse" />
          </div>
          <div>
            <h3 className="text-base font-semibold text-foreground">{t("cardReaderTest.waitingForScan")}</h3>
            <p className="text-xs text-muted-foreground max-w-md mt-1">
              {t("cardReaderTest.waitingForScanHint")}
            </p>
          </div>
        </div>
      )}

      {/* Diagnostic Analysis View */}
      {activeScan && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          {/* Card A: Raw Hardware Capture */}
          <div className="flex flex-col gap-4 rounded-xl border border-border bg-card p-5 shadow-xs">
            <div className="flex items-center justify-between border-b border-border pb-3">
              <div className="flex items-center gap-2">
                <Cpu className="size-4 text-primary" />
                <h3 className="text-sm font-semibold text-foreground">{t("cardReaderTest.rawCaptureTelemetry")}</h3>
              </div>
              <Badge variant="outline" className="text-xs">
                {activeScan.source === "scanner_usb"
                  ? t("cardReaderTest.usbWedgeScanner")
                  : t("cardReaderTest.manualInputBadge")}
              </Badge>
            </div>

            <div className="space-y-3 text-xs">
              <div>
                <span className="text-muted-foreground block mb-1">{t("cardReaderTest.rawReceivedString")}</span>
                <pre className="rounded-md bg-muted p-3 font-mono text-sm text-foreground overflow-x-auto select-all">
                  {activeScan.raw}
                </pre>
              </div>

              <div className="grid grid-cols-2 gap-2">
                <div className="rounded-md border border-border p-2.5">
                  <span className="text-muted-foreground">{t("cardReaderTest.lengthLabel")}</span>
                  <p className="text-sm font-bold text-foreground mt-0.5">
                    {t("cardReaderTest.charactersSuffix", { count: activeScan.raw.length })}
                  </p>
                </div>
                <div className="rounded-md border border-border p-2.5">
                  <span className="text-muted-foreground">{t("cardReaderTest.capturedAtLabel")}</span>
                  <p className="text-xs font-semibold text-foreground mt-0.5">
                    {activeScan.timestamp.toLocaleTimeString()}
                  </p>
                </div>
              </div>

              <div>
                <span className="text-muted-foreground block mb-1">{t("cardReaderTest.hexByteSequence")}</span>
                <code className="block rounded bg-muted/60 p-2 font-mono text-[11px] text-muted-foreground break-all">
                  {hexDump}
                </code>
              </div>
            </div>
          </div>

          {/* Card B: Token Grammar & Structural Analysis */}
          <div className="flex flex-col gap-4 rounded-xl border border-border bg-card p-5 shadow-xs">
            <div className="flex items-center justify-between border-b border-border pb-3">
              <div className="flex items-center gap-2">
                <Radio className="size-4 text-primary" />
                <h3 className="text-sm font-semibold text-foreground">{t("cardReaderTest.grammarChecksumAnalysis")}</h3>
              </div>
              <Badge
                variant={inspection?.isValid ? "default" : "destructive"}
                className={`text-xs ${inspection?.isValid ? "bg-success text-success-foreground" : ""}`}
              >
                {inspection?.isValid ? t("cardReaderTest.validToken") : t("cardReaderTest.invalidToken")}
              </Badge>
            </div>

            {inspection?.isValid && inspection.token ? (
              <div className="space-y-3 text-xs">
                <div className="rounded-md bg-success/10 border border-success/20 p-3 flex items-start gap-2.5 text-success">
                  <CheckCircle2 className="size-4 shrink-0 mt-0.5" />
                  <div>
                    <span className="font-semibold text-xs">{t("cardReaderTest.checksumValidated")}</span>
                    <p className="text-[11px] text-foreground/80 mt-0.5">{t("cardReaderTest.matchesSpec")}</p>
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-2">
                  <div className="rounded-md border border-border p-2.5">
                    <span className="text-muted-foreground">{t("cardReaderTest.namespaceLabel")}</span>
                    <p className="text-sm font-mono font-bold text-foreground mt-0.5">HD</p>
                  </div>
                  <div className="rounded-md border border-border p-2.5">
                    <span className="text-muted-foreground">{t("cardReaderTest.subjectTypeLabel")}</span>
                    <div className="flex items-center gap-1.5 mt-0.5">
                      {inspection.token.hint === "U" ? (
                        <>
                          <User className="size-3.5 text-primary" />
                          <span className="text-xs font-semibold text-foreground">{t("cardReaderTest.userStaffBadge")}</span>
                        </>
                      ) : (
                        <>
                          <Laptop className="size-3.5 text-primary" />
                          <span className="text-xs font-semibold text-foreground">{t("cardReaderTest.deviceEquipmentLabel")}</span>
                        </>
                      )}
                    </div>
                  </div>
                  <div className="rounded-md border border-border p-2.5">
                    <span className="text-muted-foreground">{t("cardReaderTest.payloadLabel")}</span>
                    <p className="text-xs font-mono font-bold text-foreground mt-0.5">
                      {inspection.token.payload}
                    </p>
                  </div>
                  <div className="rounded-md border border-border p-2.5">
                    <span className="text-muted-foreground">{t("cardReaderTest.checkCharacterLabel")}</span>
                    <p className="text-xs font-mono font-bold text-foreground mt-0.5">
                      {inspection.token.check}
                    </p>
                  </div>
                </div>

                <div>
                  <span className="text-muted-foreground block mb-1">{t("cardReaderTest.canonicalFormLabel")}</span>
                  <code className="block rounded bg-primary/10 border border-primary/20 p-2 font-mono text-xs font-bold text-primary select-all">
                    {formatToken(inspection.token)}
                  </code>
                </div>
              </div>
            ) : (
              <div className="space-y-3 text-xs">
                <div className="rounded-md bg-destructive/10 border border-destructive/20 p-3 flex items-start gap-2.5 text-destructive">
                  <AlertCircle className="size-4 shrink-0 mt-0.5" />
                  <div>
                    <span className="font-semibold text-xs capitalize">
                      {t("cardReaderTest.failureReason", {
                        reason: inspection?.reason ?? t("cardReaderTest.unrecognisedStructure"),
                      })}
                    </span>
                    <p className="text-[11px] text-foreground/80 mt-1">
                      {inspection?.errorMessage ?? t("cardReaderTest.defaultTokenFormatError")}
                    </p>
                  </div>
                </div>

                <div className="rounded-md border border-border bg-muted/20 p-3 text-xs text-muted-foreground">
                  <p className="font-semibold text-foreground mb-1">{t("cardReaderTest.expectedFormat")}</p>
                  <p className="font-mono text-[11px]">HD-[U|D]-[10 Base32 Chars]-[Check Digit]</p> {/* i18n-allow-literal: literal token grammar pattern, not translatable prose */}
                  <p className="mt-2 text-[11px]">
                    {t("cardReaderTest.examplesLabel")}{" "}
                    <code className="text-foreground">HD-U-7K3M9QXA2F-4</code> {/* i18n-allow-literal: sample token, not translatable prose */}
                    {t("cardReaderTest.orWord")}{" "}
                    <code className="text-foreground">HD-D-8N4P0RYB3G-K</code> {/* i18n-allow-literal: sample token, not translatable prose */}
                  </p>
                </div>
              </div>
            )}
          </div>

          {/* Card C: Live Database Resolution (Full Width on 2 cols) */}
          <div className="md:col-span-2 rounded-xl border border-border bg-card p-5 shadow-xs">
            <div className="flex items-center justify-between border-b border-border pb-3">
              <div className="flex items-center gap-2">
                <Layers className="size-4 text-primary" />
                <h3 className="text-sm font-semibold text-foreground">
                  {t("cardReaderTest.liveDbResolution")}
                </h3>
              </div>
              <Button size="sm" variant="ghost" onClick={() => retryResolve()}>
                <RefreshCw className="size-3.5" data-icon="inline-start" />
                {t("cardReaderTest.requery")}
              </Button>
            </div>

            <div className="mt-4">
              {isResolving && <Skeleton className="h-16 w-full" />}

              {!isResolving && dbResolution && !dbResolution.error && (
                <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 rounded-lg border border-border bg-muted/20 p-4">
                  <div className="space-y-1 text-xs">
                    <div className="flex items-center gap-2">
                      <Badge
                        variant={
                          dbResolution.data?.credentialStatus === "revoked" ||
                          dbResolution.data?.credentialStatus === "lost"
                            ? "destructive"
                            : "default"
                        }
                        className={
                          dbResolution.data?.credentialStatus === "active"
                            ? "bg-success text-success-foreground"
                            : ""
                        }
                      >
                        {dbResolution.data?.credentialStatus ?? t("cardReaderTest.unknownStatus")}
                      </Badge>
                      <Badge variant="outline" className="uppercase font-mono text-[11px]">
                        {dbResolution.data?.type}
                      </Badge>
                    </div>
                    <p className="text-muted-foreground pt-1">
                      {t("cardReaderTest.credentialIdLabel")}{" "}
                      <span className="font-mono text-foreground">{dbResolution.data?.credentialId}</span>
                    </p>
                    {dbResolution.data?.subjectId && (
                      <p className="text-muted-foreground">
                        {t("cardReaderTest.subjectIdLabel")}{" "}
                        <span className="font-mono text-foreground">{dbResolution.data?.subjectId}</span>
                      </p>
                    )}
                  </div>

                  {dbResolution.data?.type === "user" && dbResolution.data.subjectId && (
                    <Link
                      to="/users/$userId"
                      params={{ userId: dbResolution.data.subjectId }}
                      className="shrink-0"
                    >
                      <Button size="sm">
                        <User className="size-4" data-icon="inline-start" />
                        {t("cardReaderTest.viewBorrowerProfile")}
                      </Button>
                    </Link>
                  )}

                  {dbResolution.data?.type === "device" && dbResolution.data.subjectId && (
                    <Link
                      to="/devices/$deviceId"
                      params={{ deviceId: dbResolution.data.subjectId }}
                      className="shrink-0"
                    >
                      <Button size="sm">
                        <Laptop className="size-4" data-icon="inline-start" />
                        {t("cardReaderTest.viewDeviceRecord")}
                      </Button>
                    </Link>
                  )}

                  {dbResolution.data?.type === "unbound" && (
                    <div className="text-xs text-primary font-medium flex items-center gap-1.5">
                      <CheckCircle2 className="size-4 text-primary" />
                      <span>{t("cardReaderTest.readyToAssign")}</span>
                    </div>
                  )}
                </div>
              )}

              {!isResolving && dbResolution && dbResolution.error && (
                <div className="rounded-lg border border-border bg-muted/10 p-4 text-xs text-muted-foreground flex items-center gap-3">
                  <HelpCircle className="size-5 text-muted-foreground shrink-0" />
                  <div>
                    <p className="font-semibold text-foreground">{t("cardReaderTest.tokenNotFound")}</p>
                    <p className="mt-0.5">{t("cardReaderTest.tokenNotFoundHint")}</p>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export const cardReaderTestRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/card-reader-test",
  component: CardReaderTestPage,
});
