export type KnownProblemType =
  | "invalid-token-format"
  | "credential-revoked"
  | "credential-unknown"
  | "credential-unbound"
  | "credential-not-active"
  | "credential-not-found"
  | "credential-not-reprintable"
  | "credential-kind-unavailable"
  | "credential-token-taken"
  | "credential-already-bound"
  | "device-on-loan"
  | "overlapping-custody"
  | "backdated-not-permitted"
  | "device-unavailable"
  | "device-not-found"
  | "asset-tag-taken"
  | "category-not-found"
  | "user-suspended"
  | "user-not-found"
  | "employee-no-taken"
  | "illegal-transition"
  | "session-expired"
  | "session-conflict"
  | "session-not-found"
  | "loan-not-found"
  | "loan-not-open"
  | "idempotency-mismatch"
  | "validation-failed"
  | "unauthorized"
  | "forbidden"
  | "rate-limited"
  | "pairing-code-invalid"
  | "kiosk-not-found"
  | "email-taken"
  | "not-implemented"
  | "not-ready"
  | "events-unavailable";

export interface BaseProblem {
  type: string;
  title: string;
  status: number;
  detail?: string;
  instance?: string;
  requestId?: string;
  supportCode: string;
}

export interface KnownKioskProblem extends BaseProblem {
  kind: KnownProblemType;
  extensions?: Record<string, unknown>;
}

export interface UnknownKioskProblem extends BaseProblem {
  kind: "unknown-problem";
  raw?: unknown;
}

export type KioskProblem = KnownKioskProblem | UnknownKioskProblem;

export interface MappedErrorMessage {
  title: string;
  detail: string;
  tone: "error" | "warning" | "info";
}

export const KNOWN_TYPES: Set<KnownProblemType> = new Set<KnownProblemType>([
  "invalid-token-format",
  "credential-revoked",
  "credential-unknown",
  "credential-unbound",
  "credential-not-active",
  "credential-not-found",
  "credential-not-reprintable",
  "credential-kind-unavailable",
  "credential-token-taken",
  "credential-already-bound",
  "device-on-loan",
  "overlapping-custody",
  "backdated-not-permitted",
  "device-unavailable",
  "device-not-found",
  "asset-tag-taken",
  "category-not-found",
  "user-suspended",
  "user-not-found",
  "employee-no-taken",
  "illegal-transition",
  "session-expired",
  "session-conflict",
  "session-not-found",
  "loan-not-found",
  "loan-not-open",
  "idempotency-mismatch",
  "validation-failed",
  "unauthorized",
  "forbidden",
  "rate-limited",
  "pairing-code-invalid",
  "kiosk-not-found",
  "email-taken",
  "not-implemented",
  "not-ready",
  "events-unavailable",
]);

export const PROBLEM_CATALOGUE: Record<KnownProblemType, MappedErrorMessage> = {
  "invalid-token-format": {
    title: "Unreadable Barcode",
    detail: "The scanned code format is not recognised. Please try scanning again or use manual entry.",
    tone: "warning",
  },
  "credential-revoked": {
    title: "Card Revoked",
    detail: "This ID card has been deactivated. Please speak with an administrator for a replacement.",
    tone: "error",
  },
  "credential-unknown": {
    title: "Unregistered Card",
    detail: "This ID card is not registered in the system. Please register your card with the administrator.",
    tone: "warning",
  },
  "credential-unbound": {
    title: "Unassigned Card",
    detail: "This blank card has not yet been assigned to a staff member.",
    tone: "warning",
  },
  "credential-not-active": {
    title: "Card Inactive",
    detail: "This card is currently inactive. Please contact the equipment coordinator.",
    tone: "warning",
  },
  "credential-not-found": {
    title: "Card Not Found",
    detail: "No card record was found matching this code.",
    tone: "warning",
  },
  "credential-not-reprintable": {
    title: "Cannot Reprint",
    detail: "This credential token cannot be reprinted.",
    tone: "warning",
  },
  "credential-kind-unavailable": {
    title: "Card Type Unavailable",
    detail: "This card type is not supported at this kiosk.",
    tone: "warning",
  },
  "credential-token-taken": {
    title: "Card Already Registered",
    detail: "This card token is already registered in the system.",
    tone: "warning",
  },
  "credential-already-bound": {
    title: "Card Already Assigned",
    detail: "This card has already been assigned to another person.",
    tone: "warning",
  },
  "device-on-loan": {
    title: "Device Already on Loan",
    detail: "This device is currently borrowed by another staff member.",
    tone: "warning",
  },
  "overlapping-custody": {
    title: "Custody Conflict",
    detail: "This transaction conflicts with an existing loan record for this device.",
    tone: "warning",
  },
  "backdated-not-permitted": {
    title: "Invalid Timestamp",
    detail: "Backdated transactions cannot be processed at the kiosk.",
    tone: "warning",
  },
  "device-unavailable": {
    title: "Device Unavailable",
    detail: "This device is marked for maintenance or is unavailable for loan.",
    tone: "warning",
  },
  "device-not-found": {
    title: "Device Not Found",
    detail: "No device record exists matching this asset tag.",
    tone: "warning",
  },
  "asset-tag-taken": {
    title: "Asset Tag Taken",
    detail: "This asset tag is already in use by another device.",
    tone: "warning",
  },
  "category-not-found": {
    title: "Category Not Found",
    detail: "The requested equipment category does not exist.",
    tone: "warning",
  },
  "user-suspended": {
    title: "Account Suspended",
    detail: "Your borrowing privileges are currently suspended. Please contact the equipment desk.",
    tone: "error",
  },
  "user-not-found": {
    title: "Staff Member Not Found",
    detail: "No staff record was found matching this identification.",
    tone: "warning",
  },
  "employee-no-taken": {
    title: "Employee Number Taken",
    detail: "This employee number is already registered to another staff member.",
    tone: "warning",
  },
  "illegal-transition": {
    title: "Action Not Allowed",
    detail: "This status change is not permitted from the current state.",
    tone: "warning",
  },
  "session-expired": {
    title: "Session Expired",
    detail: "Your session timed out due to inactivity. Please scan again to begin a new session.",
    tone: "info",
  },
  "session-conflict": {
    title: "Session Conflict",
    detail: "The session was updated from another operation. Please try scanning again.",
    tone: "warning",
  },
  "session-not-found": {
    title: "Session Not Found",
    detail: "The requested session has ended or is no longer available.",
    tone: "info",
  },
  "loan-not-found": {
    title: "Loan Record Not Found",
    detail: "No active loan was found for this device.",
    tone: "warning",
  },
  "loan-not-open": {
    title: "Loan Already Closed",
    detail: "This device has already been returned.",
    tone: "info",
  },
  "idempotency-mismatch": {
    title: "Transaction Mismatch",
    detail: "A conflicting transaction was previously submitted. Please start a fresh scan.",
    tone: "warning",
  },
  "validation-failed": {
    title: "Invalid Input",
    detail: "The provided information could not be processed. Please check the details and try again.",
    tone: "warning",
  },
  "unauthorized": {
    title: "Kiosk Authorization Required",
    detail: "This kiosk is not paired or its authorization has expired. Please pair the device.",
    tone: "error",
  },
  "forbidden": {
    title: "Access Restricted",
    detail: "This kiosk does not have permission to perform this action.",
    tone: "error",
  },
  "rate-limited": {
    title: "Please Wait",
    detail: "Too many scans were received in a short period. Please wait a few seconds and try again.",
    tone: "warning",
  },
  "pairing-code-invalid": {
    title: "Invalid Pairing Code",
    detail: "The entered pairing code is invalid or has expired. Please request a new code from admin.",
    tone: "error",
  },
  "kiosk-not-found": {
    title: "Kiosk Not Found",
    detail: "This kiosk terminal is not recognised by the system.",
    tone: "error",
  },
  "email-taken": {
    title: "Email Already In Use",
    detail: "This email address is already associated with another account.",
    tone: "warning",
  },
  "not-implemented": {
    title: "Feature Unavailable",
    detail: "This feature is not available on this kiosk version.",
    tone: "warning",
  },
  "not-ready": {
    title: "System Not Ready",
    detail: "The hospital system is initialising. Please try again in a moment.",
    tone: "warning",
  },
  "events-unavailable": {
    title: "Live Updates Offline",
    detail: "Live background events are temporarily unavailable.",
    tone: "info",
  },
};

export function isKnownProblemType(type: string): type is KnownProblemType {
  return KNOWN_TYPES.has(type as KnownProblemType);
}

export function errorMessage(
  problem: KioskProblem,
  t?: (key: any, params?: any) => string
): MappedErrorMessage {
  if (problem.kind !== "unknown-problem" && PROBLEM_CATALOGUE[problem.kind]) {
    const entry = PROBLEM_CATALOGUE[problem.kind];
    const defaultDetail = t ? t(`problem.${problem.kind}.detail`) : entry.detail;
    const title = t ? t(`problem.${problem.kind}.title`) : entry.title;
    const detail =
      problem.detail && problem.detail.trim().length > 0 && !problem.detail.includes("http")
        ? problem.detail
        : defaultDetail;
    return {
      title,
      detail,
      tone: entry.tone,
    };
  }

  // Log unrecognised problem types for operational telemetry / 3.10 hardware checklist
  console.warn(
    `[HDMS Problem] Unrecognised problem type: "${problem.type}", code: "${problem.supportCode}"`,
    problem
  );

  return {
    title: t ? t("problem.fallbackTitle") : "Unable to Complete Request",
    detail: t
      ? t("problem.fallbackDetail", { supportCode: problem.supportCode })
      : `Please contact technical support and provide reference code ${problem.supportCode}.`,
    tone: "error",
  };
}

export function generateSupportCode(requestId?: string): string {
  if (requestId && requestId.trim().length > 0) {
    return requestId.trim();
  }
  // Generate a random 8-character hex code for support reference
  const arr = new Uint8Array(4);
  if (typeof crypto !== "undefined" && crypto.getRandomValues) {
    crypto.getRandomValues(arr);
  } else {
    for (let i = 0; i < 4; i++) {
      arr[i] = Math.floor(Math.random() * 256);
    }
  }
  return Array.from(arr)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("")
    .toUpperCase();
}

function extractTypeSuffix(typeUri?: string): string {
  if (!typeUri) return "";
  const parts = typeUri.split("/");
  return parts[parts.length - 1] ?? "";
}

export function fromProblemJson(body: Record<string, unknown>): KioskProblem {
  const typeUri = typeof body.type === "string" ? body.type : "about:blank";
  const suffix = extractTypeSuffix(typeUri);
  const title = typeof body.title === "string" ? body.title : "An error occurred";
  const status = typeof body.status === "number" ? body.status : 500;
  const detail = typeof body.detail === "string" ? body.detail : undefined;
  const instance = typeof body.instance === "string" ? body.instance : undefined;
  const requestId = typeof body.requestId === "string" ? body.requestId : undefined;
  const supportCode = generateSupportCode(requestId);
  const extensions =
    typeof body.extensions === "object" && body.extensions !== null
      ? (body.extensions as Record<string, unknown>)
      : undefined;

  if (KNOWN_TYPES.has(suffix as KnownProblemType)) {
    return {
      kind: suffix as KnownProblemType,
      type: typeUri,
      title,
      status,
      detail,
      instance,
      requestId,
      supportCode,
      extensions,
    };
  }

  return {
    kind: "unknown-problem",
    type: typeUri,
    title,
    status,
    detail,
    instance,
    requestId,
    supportCode,
    raw: body,
  };
}

export async function parseProblem(responseOrError: unknown): Promise<KioskProblem> {
  try {
    if (responseOrError instanceof Response) {
      const contentType = responseOrError.headers.get("content-type") ?? "";
      if (
        contentType.includes("application/problem+json") ||
        contentType.includes("application/json")
      ) {
        const json = await responseOrError.json();
        if (typeof json === "object" && json !== null) {
          return fromProblemJson(json as Record<string, unknown>);
        }
      }
      return {
        kind: "unknown-problem",
        type: "about:blank",
        title: responseOrError.statusText || "HTTP Error",
        status: responseOrError.status,
        supportCode: generateSupportCode(),
      };
    }

    if (typeof responseOrError === "object" && responseOrError !== null) {
      return fromProblemJson(responseOrError as Record<string, unknown>);
    }

    return {
      kind: "unknown-problem",
      type: "about:blank",
      title: "Unknown Error",
      status: 500,
      detail: typeof responseOrError === "string" ? responseOrError : undefined,
      supportCode: generateSupportCode(),
    };
  } catch {
    return {
      kind: "unknown-problem",
      type: "about:blank",
      title: "Internal Error",
      status: 500,
      supportCode: generateSupportCode(),
    };
  }
}

