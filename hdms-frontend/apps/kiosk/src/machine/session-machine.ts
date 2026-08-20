import {
  sessionMachine as rawSessionMachine,
  type SessionMachineDefinition,
  type SessionState,
  type InputClass,
  type Action,
} from "@hdms/domain";
import {
  createSession,
  submitScan,
  getSession,
  closeSession,
  cancelSession,
  returnSessionLoan,
  type ScanResult,
  type Session,
  type SessionUser,
  type SessionDevice,
  type SessionOpenLoan,
  type Outcome,
  type OutcomeKind,
  type SessionMessage,
  type ScanSource,
} from "@hdms/api-client";
import {
  getKioskConfig,
  getSessionId,
  setSessionId,
  clearSessionId,
} from "../lib/kiosk-config";
import {
  abortActiveSessionRequests,
  getSessionAbortSignal,
} from "../lib/api";
import { parseProblem, type KioskProblem } from "../lib/problem";
import {
  setup,
  assign,
  fromPromise,
  type ActorRefFrom,
} from "xstate";

export const SCREEN_MAP: Record<SessionState, string> = {
  idle: "ScreenIdle",
  awaiting_user: "ScreenAwaitingUser",
  awaiting_device: "ScreenAwaitingDevice",
  ready: "ScreenReady",
};

export function assertMachineScreensMapped(
  definition: SessionMachineDefinition = rawSessionMachine
): void {
  const states = Object.keys(definition.states) as SessionState[];
  for (const state of states) {
    if (!SCREEN_MAP[state]) {
      throw new Error(
        `Unmapped machine state: "${state}" has no corresponding screen defined in SCREEN_MAP`
      );
    }
  }
}

// Startup assertion ensuring every state in the JSON definition maps to a screen
assertMachineScreensMapped();

export function logMachineStartup(): void {
  const buildId =
    (typeof import.meta !== "undefined" && import.meta.env?.VITE_APP_BUILD_ID) ||
    (typeof import.meta !== "undefined" && import.meta.env?.MODE) ||
    "development";
  console.info(
    `[HDMS Kiosk] Machine definition version: ${rawSessionMachine.version}, Build: ${buildId}`
  );
}

// Log machine version on startup
logMachineStartup();

export interface SessionContext {
  sessionId: string | null;
  kioskId: string;
  user: SessionUser | null;
  pendingDevice: SessionDevice | null;
  openLoans: SessionOpenLoan[];
  lastOutcome: Outcome | null;
  lastMessage: SessionMessage | null;
  expiresAt: string | null;
  supportCode: string | null;
  lastProblem: KioskProblem | null;
  lastAction: Action | null;
  isSubmitting: boolean;
}

export function createInitialContext(kioskId?: string): SessionContext {
  const cfg = getKioskConfig();
  return {
    sessionId: getSessionId(),
    kioskId: kioskId ?? cfg?.kioskId ?? "unpaired-kiosk",
    user: null,
    pendingDevice: null,
    openLoans: [],
    lastOutcome: null,
    lastMessage: null,
    expiresAt: null,
    supportCode: null,
    lastProblem: null,
    lastAction: null,
    isSubmitting: false,
  };
}

export type SessionMachineEvent =
  | { type: "SCAN"; token: string; source: ScanSource }
  | { type: "RETURN_LOAN"; loanId: string }
  | { type: "CLOSE" }
  | { type: "CANCEL" }
  | { type: "RECONCILE_EXPIRY" }
  | { type: "TIMEOUT" }
  | { type: "WATCHDOG_TIMEOUT" }
  | { type: "RESET" }
  | { type: "RESTORE_SESSION"; session: Session; openLoans?: SessionOpenLoan[] }
  | { type: "APPLY_SCAN_RESULT"; result: ScanResult; action?: Action }
  | { type: "SET_PROBLEM"; problem: KioskProblem }
  | ({ type: InputClass } & Record<string, unknown>);

export interface ScanActorInput {
  sessionId: string | null;
  kioskId: string;
  token: string;
  source: ScanSource;
}

export interface ReturnLoanActorInput {
  sessionId: string;
  loanId: string;
}

export interface CloseActorInput {
  sessionId: string;
}

export interface CancelActorInput {
  sessionId: string;
}

export interface ResumeActorInput {
  sessionId: string;
}

export function outcomeKindToAction(kind?: OutcomeKind): Action {
  switch (kind) {
    case "device_pending":
      return "hold_device";
    case "user_identified":
      return "set_user";
    case "user_switched":
      return "switch_user";
    case "borrowed":
      return "borrow";
    case "returned":
      return "return";
    case "rejected":
      return "reject";
    case "duplicate":
      return "duplicate";
    default:
      return "none";
  }
}

export function actionToOutcomeKind(action?: Action): OutcomeKind {
  switch (action) {
    case "hold_device":
    case "replace_pending":
      return "device_pending";
    case "set_user":
      return "user_identified";
    case "switch_user":
      return "user_switched";
    case "borrow":
    case "resolve_pending":
      return "borrowed";
    case "return":
      return "returned";
    case "reject":
    case "expire":
      return "rejected";
    case "duplicate":
      return "duplicate";
    default:
      return "user_identified";
  }
}

export function applyScanResultContext(
  context: SessionContext,
  result: ScanResult,
  actionOverride?: Action
): SessionContext {
  const isIdle =
    result.session.state === "idle" ||
    (result.session.state as string) === "closed" ||
    (result.session.state as string) === "completed";
  if (isIdle) {
    clearSessionId();
  } else {
    setSessionId(result.session.id);
  }

  const derivedAction = actionOverride ?? outcomeKindToAction(result.outcome?.kind);

  return {
    ...context,
    sessionId: isIdle ? null : result.session.id,
    user: isIdle ? null : (result.session.user ?? null),
    pendingDevice: isIdle ? null : (result.session.pendingDevice ?? null),
    openLoans: isIdle ? [] : (result.openLoans ?? []),
    lastOutcome: result.outcome ?? null,
    lastMessage: result.message ?? null,
    expiresAt: isIdle ? null : (result.session.expiresAt ?? null),
    lastAction: derivedAction,
    lastProblem: null,
    isSubmitting: false,
  };
}

export async function executeScan(input: ScanActorInput): Promise<ScanResult> {
  const signal = getSessionAbortSignal();
  if (signal.aborted) {
    throw new DOMException("Aborted", "AbortError");
  }

  let sid = input.sessionId;
  if (!sid) {
    const createRes = await createSession({ signal });
    if (createRes.error || !createRes.data) {
      const problem = await parseProblem(createRes.error);
      throw problem;
    }
    sid = createRes.data.id;
    setSessionId(sid);
  }

  const res = await submitScan({
    path: { id: sid },
    body: {
      token: input.token,
      source: input.source,
      scannedAt: new Date().toISOString(),
    },
    signal,
  });

  if (res.error || !res.data) {
    const problem = await parseProblem(res.error);
    throw problem;
  }

  if (
    res.data.session.state === "idle" ||
    (res.data.session.state as string) === "closed" ||
    (res.data.session.state as string) === "completed"
  ) {
    clearSessionId();
  } else {
    setSessionId(res.data.session.id);
  }
  return res.data;
}

export async function executeReturnLoan(
  input: ReturnLoanActorInput
): Promise<ScanResult> {
  const signal = getSessionAbortSignal();
  const res = await returnSessionLoan({
    path: { id: input.sessionId },
    body: { loanId: input.loanId },
    signal,
  });

  if (res.error || !res.data) {
    const problem = await parseProblem(res.error);
    throw problem;
  }

  setSessionId(res.data.session.id);
  return res.data;
}

export async function executeClose(input: CloseActorInput): Promise<void> {
  abortActiveSessionRequests();
  try {
    await closeSession({ path: { id: input.sessionId } });
  } finally {
    clearSessionId();
  }
}

export async function executeCancel(input: CancelActorInput): Promise<void> {
  abortActiveSessionRequests();
  try {
    await cancelSession({ path: { id: input.sessionId } });
  } finally {
    clearSessionId();
  }
}

export async function executeResume(
  input: ResumeActorInput
): Promise<Session | null> {
  try {
    const res = await getSession({ path: { id: input.sessionId } });
    if (res.data) {
      if (
        res.data.state === "completed" ||
        res.data.state === "expired" ||
        res.data.state === "cancelled"
      ) {
        clearSessionId();
        return null;
      }
      return res.data;
    }
    clearSessionId();
    return null;
  } catch {
    clearSessionId();
    return null;
  }
}

export interface MachineDependencies {
  scanExecutor?: (input: ScanActorInput) => Promise<ScanResult>;
  returnLoanExecutor?: (input: ReturnLoanActorInput) => Promise<ScanResult>;
  closeExecutor?: (input: CloseActorInput) => Promise<void>;
  cancelExecutor?: (input: CancelActorInput) => Promise<void>;
  resumeExecutor?: (input: ResumeActorInput) => Promise<Session | null>;
}

export function buildKioskSessionMachine(
  definition: SessionMachineDefinition = rawSessionMachine,
  deps: MachineDependencies = {}
) {
  const scanActor = fromPromise(async ({ input }: { input: ScanActorInput }) => {
    const executor = deps.scanExecutor ?? executeScan;
    return executor(input);
  });

  const returnLoanActor = fromPromise(
    async ({ input }: { input: ReturnLoanActorInput }) => {
      const executor = deps.returnLoanExecutor ?? executeReturnLoan;
      return executor(input);
    }
  );

  const closeActor = fromPromise(
    async ({ input }: { input: CloseActorInput }) => {
      const executor = deps.closeExecutor ?? executeClose;
      return executor(input);
    }
  );

  const cancelActor = fromPromise(
    async ({ input }: { input: CancelActorInput }) => {
      const executor = deps.cancelExecutor ?? executeCancel;
      return executor(input);
    }
  );

  const statesConfig: Record<string, any> = {};

  const clearSessionContext = (context: SessionContext): SessionContext => {
    abortActiveSessionRequests();
    clearSessionId();
    return {
      ...context,
      sessionId: null,
      user: null,
      pendingDevice: null,
      openLoans: [],
      lastOutcome: null,
      lastMessage: null,
      expiresAt: null,
      lastProblem: null,
      isSubmitting: false,
    };
  };

  for (const [stateName, stateDef] of Object.entries(definition.states)) {
    const onTransitions: Record<string, any> = {};

    // 1. Map input classes from sessionMachine.json
    for (const [inputClass, rule] of Object.entries(stateDef.on)) {
      onTransitions[inputClass] = {
        target: rule.target,
        actions: assign(({ context, event }: { context: SessionContext; event: any }) => {
          const nextCtx: SessionContext = {
            ...context,
            lastAction: rule.action,
          };
          if (rule.clearsPending) {
            nextCtx.pendingDevice = null;
          }
          if (event && "result" in event && event.result) {
            const res = event.result as ScanResult;
            nextCtx.sessionId = res.session.id;
            nextCtx.user = res.session.user ?? null;
            nextCtx.pendingDevice = res.session.pendingDevice ?? null;
            nextCtx.openLoans = res.openLoans ?? [];
            nextCtx.lastOutcome = res.outcome ?? null;
            nextCtx.lastMessage = res.message ?? null;
            nextCtx.expiresAt = res.session.expiresAt ?? null;
          }
          if (rule.target === "idle" && rule.action === "expire") {
            clearSessionId();
            nextCtx.sessionId = null;
            nextCtx.user = null;
            nextCtx.pendingDevice = null;
            nextCtx.openLoans = [];
            nextCtx.expiresAt = null;
          }
          return nextCtx;
        }),
      };
    }

    // 2. High-level kiosk operations with wholesale context replacement
    onTransitions["APPLY_SCAN_RESULT"] = [
      {
        guard: ({ event }: { event: any }) =>
          event.result?.outcome?.kind === "duplicate",
        actions: assign(({ context, event }: { context: SessionContext; event: any }) => {
          const res = event.result as ScanResult;
          return {
            ...context,
            lastOutcome: res.outcome,
            lastMessage: res.message,
            lastAction: "duplicate" as Action,
            isSubmitting: false,
          };
        }),
      },
      {
        target: "ready",
        guard: ({ event }: { event: any }) =>
          event.result?.session?.state === "ready",
        actions: assign(({ context, event }: { context: SessionContext; event: any }) =>
          applyScanResultContext(context, event.result, event.action)
        ),
      },
      {
        target: "awaiting_user",
        guard: ({ event }: { event: any }) =>
          event.result?.session?.state === "awaiting_user",
        actions: assign(({ context, event }: { context: SessionContext; event: any }) =>
          applyScanResultContext(context, event.result, event.action)
        ),
      },
      {
        target: "awaiting_device",
        guard: ({ event }: { event: any }) =>
          event.result?.session?.state === "awaiting_device",
        actions: assign(({ context, event }: { context: SessionContext; event: any }) =>
          applyScanResultContext(context, event.result, event.action)
        ),
      },
      {
        target: "idle",
        actions: assign(({ context, event }: { context: SessionContext; event: any }) =>
          applyScanResultContext(context, event.result, event.action)
        ),
      },
    ];

    onTransitions["SET_PROBLEM"] = {
      actions: assign(({ context, event }: { context: SessionContext; event: any }) => {
        const problem = event.problem as KioskProblem;
        if (problem.kind === "session-expired") {
          clearSessionId();
        }
        return {
          ...context,
          lastProblem: problem,
          supportCode: problem.supportCode,
          isSubmitting: false,
        };
      }),
    };

    onTransitions["RESTORE_SESSION"] = [
      {
        target: "ready",
        guard: ({ event }: { event: any }) => event.session?.state === "ready",
        actions: assign(({ context, event }: { context: SessionContext; event: any }) => {
          const session = event.session as Session;
          setSessionId(session.id);
          return {
            ...context,
            sessionId: session.id,
            user: session.user ?? null,
            pendingDevice: session.pendingDevice ?? null,
            openLoans: event.openLoans ?? context.openLoans,
            expiresAt: session.expiresAt ?? null,
            lastProblem: null,
          };
        }),
      },
      {
        target: "awaiting_user",
        guard: ({ event }: { event: any }) =>
          event.session?.state === "awaiting_user",
        actions: assign(({ context, event }: { context: SessionContext; event: any }) => {
          const session = event.session as Session;
          setSessionId(session.id);
          return {
            ...context,
            sessionId: session.id,
            user: session.user ?? null,
            pendingDevice: session.pendingDevice ?? null,
            openLoans: event.openLoans ?? context.openLoans,
            expiresAt: session.expiresAt ?? null,
            lastProblem: null,
          };
        }),
      },
      {
        target: "awaiting_device",
        guard: ({ event }: { event: any }) =>
          event.session?.state === "awaiting_device",
        actions: assign(({ context, event }: { context: SessionContext; event: any }) => {
          const session = event.session as Session;
          setSessionId(session.id);
          return {
            ...context,
            sessionId: session.id,
            user: session.user ?? null,
            pendingDevice: session.pendingDevice ?? null,
            openLoans: event.openLoans ?? context.openLoans,
            expiresAt: session.expiresAt ?? null,
            lastProblem: null,
          };
        }),
      },
      {
        target: "idle",
        actions: assign(({ context }: { context: SessionContext }) =>
          clearSessionContext(context)
        ),
      },
    ];

    onTransitions["RECONCILE_EXPIRY"] = [
      {
        guard: ({ context }: { context: SessionContext }) => {
          if (!context.expiresAt) return false;
          return new Date(context.expiresAt).getTime() <= Date.now();
        },
        target: "idle",
        actions: assign(({ context }: { context: SessionContext }) =>
          clearSessionContext(context)
        ),
      },
    ];

    onTransitions["TIMEOUT"] = {
      target: "idle",
      actions: assign(({ context }: { context: SessionContext }) =>
        clearSessionContext(context)
      ),
    };

    onTransitions["WATCHDOG_TIMEOUT"] = {
      target: "idle",
      actions: assign(({ context }: { context: SessionContext }) =>
        clearSessionContext(context)
      ),
    };

    onTransitions["CLOSE"] = {
      target: "idle",
      actions: assign(({ context }: { context: SessionContext }) =>
        clearSessionContext(context)
      ),
    };

    onTransitions["CANCEL"] = {
      target: "idle",
      actions: assign(({ context }: { context: SessionContext }) =>
        clearSessionContext(context)
      ),
    };

    onTransitions["RESET"] = {
      target: "idle",
      actions: assign(({ context }: { context: SessionContext }) =>
        clearSessionContext(context)
      ),
    };

    // Timeout definitions from JSON `after`
    const afterTransitions: Record<string, any> = {};
    if (stateDef.after) {
      for (const [msStr, timeoutDef] of Object.entries(stateDef.after)) {
        afterTransitions[msStr] = {
          target: timeoutDef.target,
          actions: assign(({ context }: { context: SessionContext }) =>
            clearSessionContext(context)
          ),
        };
      }
    }

    // 3-minute unconditional watchdog from any non-idle state (180,000 ms)
    if (stateName !== "idle") {
      afterTransitions["180000"] = {
        target: "idle",
        actions: assign(({ context }: { context: SessionContext }) =>
          clearSessionContext(context)
        ),
      };
    }

    statesConfig[stateName] = {
      on: onTransitions,
      after:
        Object.keys(afterTransitions).length > 0
          ? afterTransitions
          : undefined,
    };
  }

  return setup({
    types: {
      context: {} as SessionContext,
      events: {} as SessionMachineEvent,
    },
    actors: {
      scanActor,
      returnLoanActor,
      closeActor,
      cancelActor,
    },
  }).createMachine({
    id: "kioskSession",
    initial: "idle",
    context: createInitialContext(),
    states: statesConfig,
  });
}

export const sessionMachine = buildKioskSessionMachine();

export type SessionMachineActor = ActorRefFrom<typeof sessionMachine>;

/**
 * Attaches visibility change listener to reconcile expiry on app foregrounding,
 * and reconcile full session from server if app was hidden for > 30 seconds.
 */
export function setupVisibilityReconciliation(
  actor: { send: (event: SessionMachineEvent) => void }
): () => void {
  if (typeof document === "undefined") {
    return () => {};
  }
  let hiddenAt: number | null = null;

  const handler = () => {
    if (document.visibilityState === "hidden") {
      hiddenAt = Date.now();
    } else if (document.visibilityState === "visible") {
      const now = Date.now();
      const hiddenDuration = hiddenAt !== null ? now - hiddenAt : 0;
      hiddenAt = null;

      const sid = getSessionId();
      if (hiddenDuration > 30000 && sid) {
        void executeResume({ sessionId: sid }).then((session) => {
          if (session) {
            actor.send({ type: "RESTORE_SESSION", session });
          } else {
            actor.send({ type: "RESET" });
          }
        });
      } else {
        actor.send({ type: "RECONCILE_EXPIRY" });
      }
    }
  };
  document.addEventListener("visibilitychange", handler);
  return () => {
    document.removeEventListener("visibilitychange", handler);
  };
}
