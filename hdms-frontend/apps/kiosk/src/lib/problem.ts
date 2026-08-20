export type KnownProblemType =
  | "session-expired"
  | "session-conflict"
  | "device-on-loan"
  | "overlapping-custody"
  | "backdated-not-permitted"
  | "device-unavailable"
  | "user-suspended"
  | "idempotency-mismatch"
  | "invalid-token-format"
  | "rate-limited"
  | "pairing-code-invalid";

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

const KNOWN_TYPES: Set<KnownProblemType> = new Set([
  "session-expired",
  "session-conflict",
  "device-on-loan",
  "overlapping-custody",
  "backdated-not-permitted",
  "device-unavailable",
  "user-suspended",
  "idempotency-mismatch",
  "invalid-token-format",
  "rate-limited",
  "pairing-code-invalid",
]);

function generateSupportCode(requestId?: string): string {
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
