import rawMachine from "../session-machine.json" with { type: "json" };
import rawScenarios from "../scenarios.json" with { type: "json" };

export type SessionState = "idle" | "awaiting_user" | "awaiting_device" | "ready";

export type InputClass =
  | "device_available"
  | "device_on_loan_same_user"
  | "device_on_loan_other_user"
  | "device_unavailable"
  | "device_duplicate"
  | "device_reserved_by_self"
  | "device_reserved_by_other"
  | "user_active"
  | "user_same"
  | "user_suspended"
  | "user_archived"
  | "unbound"
  | "unknown"
  | "revoked"
  | "timeout";

export type Action =
  | "none"
  | "hold_device"
  | "replace_pending"
  | "set_user"
  | "switch_user"
  | "borrow"
  | "return"
  | "reject"
  | "duplicate"
  | "expire"
  | "resolve_pending";

export interface TransitionRule {
  readonly action: Action;
  readonly target: SessionState;
  readonly clearsPending: boolean;
}

export interface TimeoutTarget {
  readonly target: SessionState;
}

export interface StateDefinition {
  readonly on: Record<InputClass, TransitionRule>;
  readonly after?: Record<string, TimeoutTarget>;
}

export interface SessionMachineDefinition {
  readonly version: string;
  readonly states: Record<SessionState, StateDefinition>;
}

export interface ScenarioInput {
  readonly class: InputClass;
  readonly kind?: "device" | "user" | "unbound" | "unknown" | "revoked" | "timeout" | string;
  readonly deviceId?: string;
  readonly deviceStatus?: string;
  readonly holderUserId?: string;
  readonly holderBorrowedAt?: string;
  readonly userId?: string;
  readonly userStatus?: string;
  readonly tokenPreview?: string;
  readonly revokedAt?: string;
  readonly sameAsPendingWithin3s?: boolean;
  readonly pendingDeviceId?: string;
  readonly pendingDeviceStatus?: string;
  readonly pendingDeviceHolderId?: string;
}

export interface ScenarioExpected {
  readonly action: Action;
  readonly state: SessionState;
}

export interface Scenario {
  readonly name: string;
  readonly initialState: SessionState;
  readonly inputs: readonly ScenarioInput[];
  readonly expected: readonly ScenarioExpected[];
}

export const sessionMachine: SessionMachineDefinition = rawMachine as unknown as SessionMachineDefinition;

export const scenarios: readonly Scenario[] = rawScenarios as unknown as readonly Scenario[];
