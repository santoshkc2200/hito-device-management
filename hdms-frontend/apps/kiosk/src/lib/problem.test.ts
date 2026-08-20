import { describe, expect, it } from "vitest";
import { fromProblemJson, type KnownProblemType, parseProblem } from "./problem";

describe("problem parsing", () => {
  const knownTypes: KnownProblemType[] = [
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
  ];

  it.each(knownTypes)("maps known type %s to its discriminated variant", (kind) => {
    const raw = {
      type: `https://hdms.hito.local/errors/${kind}`,
      title: `Error: ${kind}`,
      status: 400,
      detail: "Something happened",
      instance: "/v1/sessions/123/scan",
      requestId: "req-abc-999",
      extensions: { foo: "bar" },
    };

    const parsed = fromProblemJson(raw);
    expect(parsed.kind).toBe(kind);
    expect(parsed.type).toBe(raw.type);
    expect(parsed.title).toBe(raw.title);
    expect(parsed.status).toBe(400);
    expect(parsed.supportCode).toBe("req-abc-999");
    if (parsed.kind !== "unknown-problem") {
      expect(parsed.extensions).toEqual({ foo: "bar" });
    }
  });

  it("maps unknown type to unknown-problem with support code and never throws", () => {
    const raw = {
      type: "https://hdms.hito.local/errors/unrecognized-custom-error",
      title: "Unknown strange error",
      status: 500,
    };

    const parsed = fromProblemJson(raw);
    expect(parsed.kind).toBe("unknown-problem");
    expect(parsed.type).toBe(raw.type);
    expect(parsed.supportCode).toBeTruthy();
    expect(typeof parsed.supportCode).toBe("string");
    expect(parsed.supportCode.length).toBeGreaterThan(0);
  });

  it("generates support code when requestId is absent", () => {
    const raw = {
      type: "https://hdms.hito.local/errors/session-expired",
      title: "Expired",
      status: 410,
    };

    const parsed = fromProblemJson(raw);
    expect(parsed.kind).toBe("session-expired");
    expect(parsed.requestId).toBeUndefined();
    expect(parsed.supportCode).toBeTruthy();
    expect(parsed.supportCode.length).toBeGreaterThanOrEqual(8);
  });

  it("parseProblem parses a Response object", async () => {
    const body = {
      type: "https://hdms.hito.local/errors/device-on-loan",
      title: "Device On Loan",
      status: 409,
      detail: "Device is already out",
      requestId: "01JCXYZ",
      extensions: { holderDepartment: "Surgery" },
    };

    const response = new Response(JSON.stringify(body), {
      status: 409,
      headers: { "Content-Type": "application/problem+json" },
    });

    const parsed = await parseProblem(response);
    expect(parsed.kind).toBe("device-on-loan");
    expect(parsed.status).toBe(409);
    expect(parsed.supportCode).toBe("01JCXYZ");
  });

  it("parseProblem handles non-JSON or network error responses without throwing", async () => {
    const response = new Response("Bad Gateway", {
      status: 502,
      statusText: "Bad Gateway",
      headers: { "Content-Type": "text/plain" },
    });

    const parsed = await parseProblem(response);
    expect(parsed.kind).toBe("unknown-problem");
    expect(parsed.status).toBe(502);
    expect(parsed.title).toBe("Bad Gateway");
    expect(parsed.supportCode).toBeTruthy();
  });

  it("parseProblem handles arbitrary error objects or strings gracefully", async () => {
    const parsedStr = await parseProblem("Network request failed");
    expect(parsedStr.kind).toBe("unknown-problem");
    expect(parsedStr.detail).toBe("Network request failed");

    const parsedNull = await parseProblem(null);
    expect(parsedNull.kind).toBe("unknown-problem");
    expect(parsedNull.status).toBe(500);
  });
});
