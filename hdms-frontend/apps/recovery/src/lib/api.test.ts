import { describe, expect, it, vi } from "vitest";
import { RecoveryError, asRecoveryError, recoveryApi } from "./api";
import { fakeWorker } from "@/test/worker";

describe("recoveryApi", () => {
  it("calls the worker under /recovery/api with the session cookie and JSON", async () => {
    const worker = fakeWorker({ "POST /unlock": { body: { keysMatch: true } } });
    await recoveryApi.unlock("local", "ABCD");
    expect(worker.called("POST /unlock")[0].body).toEqual({ source: "local", key: "ABCD" });
    const init = vi.mocked(globalThis.fetch).mock.calls[0][1];
    expect(vi.mocked(globalThis.fetch).mock.calls[0][0]).toBe("/recovery/api/unlock");
    expect(init?.credentials).toBe("same-origin");
  });

  it("turns {error} answers into a RecoveryError with the code and Retry-After", async () => {
    fakeWorker({ "POST /unlock": { status: 429, body: { error: "too_many_attempts" }, headers: { "Retry-After": "120" } } });
    const err = await recoveryApi.unlock("local", "ABCD").catch((e) => e);
    expect(err).toBeInstanceOf(RecoveryError);
    expect(err).toMatchObject({ status: 429, code: "too_many_attempts", retryAfterSeconds: 120 });
  });

  it("knows a lost session when it sees one", async () => {
    fakeWorker({ "GET /snapshots": { status: 401, body: { error: "session_required" } } });
    const err = await recoveryApi.snapshots().then(() => {
      throw new Error("expected the call to fail");
    }, asRecoveryError);
    expect(err.sessionLost).toBe(true);
  });

  it("reports an unreachable worker as status 0", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new TypeError("Failed to fetch"));
    const err = await recoveryApi.status().catch(asRecoveryError);
    expect(err).toMatchObject({ status: 0, code: "unreachable" });
  });

  it("unwraps list and view payloads", async () => {
    fakeWorker({
      "GET /sources": { body: { sources: [{ id: "local" }] } },
      "GET /restore": { body: { restore: null } },
    });
    expect(await recoveryApi.sources()).toEqual([{ id: "local" }]);
    expect(await recoveryApi.restore()).toBeNull();
  });
});
