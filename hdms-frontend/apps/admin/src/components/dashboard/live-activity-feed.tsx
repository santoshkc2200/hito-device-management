import {
  Activity,
  AlertTriangle,
  ArrowDownLeft,
  ArrowUpRight,
  Clock,
  CreditCard,
  RefreshCw,
  UserPlus,
  Wifi,
  WifiOff,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@hdms/ui";

export interface FeedEvent {
  id: string | number;
  topic: string;
  payload: Record<string, any>;
  receivedAt: Date;
}

export type ConnectionState = "connected" | "reconnecting" | "stale";

interface LiveActivityFeedProps {
  maxEvents?: number;
  // Optional override for custom event sources in tests
  streamUrl?: string;
  initialEvents?: FeedEvent[];
}

function formatTime(d: Date): string {
  return d.toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function renderEventDescription(event: FeedEvent): {
  title: string;
  description: string;
  icon: typeof Activity;
  tone: string;
} {
  const { topic, payload } = event;

  switch (topic) {
    case "loan.opened": {
      const user = payload.userName || payload.userId || "User";
      const device = payload.deviceName || payload.assetTag || payload.deviceId || "Device";
      const kiosk = payload.kioskId ? ` at kiosk ${payload.kioskId}` : "";
      return {
        title: "Device Checked Out",
        description: `${user} borrowed ${device}${kiosk}`,
        icon: ArrowUpRight,
        tone: "text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-950/40",
      };
    }
    case "loan.closed": {
      const device = payload.deviceName || payload.assetTag || payload.deviceId || "Device";
      const user = payload.userName || payload.userId;
      const returnedBy = user ? ` by ${user}` : "";
      return {
        title: "Device Returned",
        description: `${device} returned${returnedBy}`,
        icon: ArrowDownLeft,
        tone: "text-emerald-600 dark:text-emerald-400 bg-emerald-50 dark:bg-emerald-950/40",
      };
    }
    case "loan.overdue": {
      const device = payload.deviceName || payload.assetTag || payload.deviceId || "Device";
      const user = payload.userName || payload.userId;
      return {
        title: "Loan Overdue",
        description: `${device} (held by ${user || "borrower"}) is now past due`,
        icon: Clock,
        tone: "text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-950/40",
      };
    }
    case "device.status_changed": {
      const tag = payload.assetTag || payload.deviceId || "Device";
      const status = payload.status || payload.toStatus || "updated";
      const reason = payload.reason ? ` (${payload.reason})` : "";
      return {
        title: "Device Status Changed",
        description: `${tag} set to ${status}${reason}`,
        icon: Activity,
        tone: "text-purple-600 dark:text-purple-400 bg-purple-50 dark:bg-purple-950/40",
      };
    }
    case "credential.revoked": {
      const subject = payload.subjectName || payload.subjectId || "Credential";
      const reason = payload.reason ? ` — ${payload.reason}` : "";
      return {
        title: "Card Revoked",
        description: `${subject} card was revoked${reason}`,
        icon: CreditCard,
        tone: "text-rose-600 dark:text-rose-400 bg-rose-50 dark:bg-rose-950/40",
      };
    }
    case "user.registered": {
      const name = payload.fullName || payload.name || "New borrower";
      const emp = payload.employeeNo ? ` (${payload.employeeNo})` : "";
      return {
        title: "User Registered",
        description: `${name}${emp} was added to system`,
        icon: UserPlus,
        tone: "text-indigo-600 dark:text-indigo-400 bg-indigo-50 dark:bg-indigo-950/40",
      };
    }
    default:
      return {
        title: topic || "System Event",
        description: JSON.stringify(payload),
        icon: Activity,
        tone: "text-muted-foreground bg-muted",
      };
  }
}

export function LiveActivityFeed({
  maxEvents = 50,
  streamUrl = "/v1/events/stream",
  initialEvents = [],
}: LiveActivityFeedProps) {
  const [events, setEvents] = useState<FeedEvent[]>(initialEvents);
  const [connectionState, setConnectionState] = useState<ConnectionState>("reconnecting");
  const [retryCount, setRetryCount] = useState(0);

  const seenEventIdsRef = useRef<Set<string>>(new Set(initialEvents.map((e) => String(e.id))));
  const lastEventIdRef = useRef<string | null>(null);
  const abortControllerRef = useRef<AbortController | null>(null);
  const retryTimeoutRef = useRef<number | null>(null);
  const isMountedRef = useRef(true);

  const addEvent = useCallback((event: FeedEvent) => {
    const key = String(event.id);
    if (seenEventIdsRef.current.has(key)) {
      return; // deduplicate
    }
    seenEventIdsRef.current.add(key);
    lastEventIdRef.current = key;

    setEvents((prev) => [event, ...prev].slice(0, maxEvents));
  }, [maxEvents]);

  const connect = useCallback(() => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }
    if (retryTimeoutRef.current) {
      window.clearTimeout(retryTimeoutRef.current);
      retryTimeoutRef.current = null;
    }

    const abortController = new AbortController();
    abortControllerRef.current = abortController;

    const headers: Record<string, string> = {
      Accept: "text/event-stream",
    };
    if (lastEventIdRef.current) {
      headers["Last-Event-ID"] = lastEventIdRef.current;
    }

    fetch(streamUrl, {
      headers,
      signal: abortController.signal,
      credentials: "same-origin",
    })
      .then(async (response) => {
        if (!response.ok) {
          throw new Error(`SSE HTTP error: ${response.status}`);
        }

        if (!response.body) {
          throw new Error("No response body in SSE stream");
        }

        setConnectionState("connected");
        setRetryCount(0);

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";

        let currentId = "";
        let currentEvent = "message";
        let currentData = "";

        while (true) {
          const { done, value } = await reader.read();
          if (done) break;

          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split(/\r\n|\r|\n/);
          buffer = lines.pop() || "";

          for (const line of lines) {
            const trimmed = line.trim();
            if (!trimmed) {
              // End of SSE event block
              if (currentData) {
                try {
                  const payload = JSON.parse(currentData);
                  addEvent({
                    id: currentId || `gen-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
                    topic: currentEvent,
                    payload,
                    receivedAt: new Date(),
                  });
                } catch {
                  // non-json or ping
                }
              }
              currentId = "";
              currentEvent = "message";
              currentData = "";
              continue;
            }

            if (trimmed.startsWith(":")) {
              // Comment / Ping
              continue;
            } else if (trimmed.startsWith("id:")) {
              currentId = trimmed.slice(3).trim();
            } else if (trimmed.startsWith("event:")) {
              currentEvent = trimmed.slice(6).trim();
            } else if (trimmed.startsWith("data:")) {
              const dataPart = trimmed.slice(5).trim();
              currentData = currentData ? `${currentData}\n${dataPart}` : dataPart;
            }
          }
        }
      })
      .catch((err) => {
        if (err.name === "AbortError" || !isMountedRef.current) {
          return;
        }

        setRetryCount((prev) => {
          const nextRetry = prev + 1;
          if (nextRetry >= 5) {
            setConnectionState("stale");
          } else {
            setConnectionState("reconnecting");
          }

          // Exponential backoff with jitter: min(1000 * 2^attempt, 30000) + random(0, 1000)
          const delay = Math.min(1000 * Math.pow(1.5, Math.min(nextRetry, 6)), 15000) + Math.random() * 1000;
          retryTimeoutRef.current = window.setTimeout(() => {
            if (isMountedRef.current) {
              connect();
            }
          }, delay);

          return nextRetry;
        });
      });
  }, [streamUrl, addEvent]);

  useEffect(() => {
    isMountedRef.current = true;
    connect();

    return () => {
      isMountedRef.current = false;
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }
      if (retryTimeoutRef.current) {
        window.clearTimeout(retryTimeoutRef.current);
      }
    };
  }, [connect]);

  const handleManualReconnect = () => {
    setConnectionState("reconnecting");
    setRetryCount(0);
    connect();
  };

  return (
    <Card className="h-full flex flex-col">
      <CardHeader className="pb-3 flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-base font-semibold flex items-center gap-2">
            <Activity className="size-4 text-primary" />
            Live Activity Feed
          </CardTitle>
          <p className="text-xs text-muted-foreground mt-0.5">
            Real-time equipment transactions and audit events
          </p>
        </div>

        {/* Connection status badge */}
        <div className="flex items-center gap-2">
          {connectionState === "connected" && (
            <div
              data-testid="connection-status-connected"
              className="inline-flex items-center gap-1.5 rounded-full bg-emerald-50 dark:bg-emerald-950/40 px-2.5 py-1 text-xs font-medium text-emerald-700 dark:text-emerald-400 border border-emerald-200 dark:border-emerald-900"
            >
              <span className="relative flex h-2 w-2">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
              </span>
              <Wifi className="size-3" />
              Live
            </div>
          )}

          {connectionState === "reconnecting" && (
            <div
              data-testid="connection-status-reconnecting"
              className="inline-flex items-center gap-1.5 rounded-full bg-amber-50 dark:bg-amber-950/40 px-2.5 py-1 text-xs font-medium text-amber-700 dark:text-amber-400 border border-amber-200 dark:border-amber-900"
            >
              <RefreshCw className="size-3 animate-spin" />
              Reconnecting {retryCount > 0 ? `(${retryCount})` : ""}
            </div>
          )}

          {connectionState === "stale" && (
            <div
              data-testid="connection-status-stale"
              className="inline-flex items-center gap-2"
            >
              <span className="inline-flex items-center gap-1.5 rounded-full bg-rose-50 dark:bg-rose-950/40 px-2.5 py-1 text-xs font-medium text-rose-700 dark:text-rose-400 border border-rose-200 dark:border-rose-900">
                <WifiOff className="size-3" />
                Offline
              </span>
              <Button
                variant="outline"
                size="sm"
                className="h-7 px-2 text-xs"
                onClick={handleManualReconnect}
                data-testid="reconnect-button"
              >
                <RefreshCw className="size-3 mr-1" />
                Reconnect
              </Button>
            </div>
          )}
        </div>
      </CardHeader>

      <CardContent className="flex-1 p-0">
        {events.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-12 px-4 text-center">
            <div className="flex size-10 items-center justify-center rounded-full bg-muted text-muted-foreground mb-2">
              <Activity className="size-5" />
            </div>
            <h3 className="text-sm font-medium text-foreground">
              Awaiting transactions
            </h3>
            <p className="mt-1 text-xs text-muted-foreground max-w-xs">
              Live scans, checkouts, returns, and registrations from kiosks and admin stations will appear here in real time.
            </p>
          </div>
        ) : (
          <div className="divide-y divide-border/60 max-h-[420px] overflow-y-auto">
            {events.map((ev) => {
              const info = renderEventDescription(ev);
              const Icon = info.icon;
              return (
                <div
                  key={String(ev.id)}
                  data-testid={`feed-event-${ev.id}`}
                  className="flex items-start gap-3 p-3.5 hover:bg-muted/30 transition-colors"
                >
                  <div className={cn("flex size-8 shrink-0 items-center justify-center rounded-lg mt-0.5", info.tone)}>
                    <Icon className="size-4" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center justify-between gap-2">
                      <p className="text-xs font-semibold text-foreground truncate">
                        {info.title}
                      </p>
                      <span className="text-[11px] font-mono text-muted-foreground whitespace-nowrap">
                        {formatTime(ev.receivedAt)}
                      </span>
                    </div>
                    <p className="text-xs text-muted-foreground mt-0.5 break-words">
                      {info.description}
                    </p>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
