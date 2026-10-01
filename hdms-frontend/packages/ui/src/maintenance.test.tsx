import "@testing-library/jest-dom/vitest";
import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { client } from "@hdms/api-client";
import {
  MAINTENANCE_RECHECK_MS,
  clearMaintenance,
  installMaintenanceInterceptor,
  isMaintenanceResponse,
  isUnderMaintenance,
  reportMaintenance,
  resetMaintenanceForTesting,
  useMaintenance,
} from "./maintenance";

vi.mock("@hdms/api-client", () => ({
  client: { interceptors: { response: { use: vi.fn() } } },
  getReadyz: vi.fn(),
}));

function problem(status: number, type: string): Response {
  return new Response(
    JSON.stringify({ type: `https://hdms.hito.local/errors/${type}`, title: "x", status }),
    { status, headers: { "Content-Type": "application/problem+json" } },
  );
}

describe("isMaintenanceResponse", () => {
  it("recognises only a 503 whose problem type is maintenance", async () => {
    expect(await isMaintenanceResponse(problem(503, "maintenance"))).toBe(true);
    expect(await isMaintenanceResponse(problem(503, "not-ready"))).toBe(false);
    expect(await isMaintenanceResponse(problem(500, "maintenance"))).toBe(false);
    expect(await isMaintenanceResponse(new Response("<html>bad gateway</html>", { status: 503 }))).toBe(false);
  });

  it("leaves the body readable for the caller", async () => {
    const res = problem(503, "maintenance");
    await isMaintenanceResponse(res);
    expect(((await res.json()) as { type: string }).type).toMatch(/maintenance$/);
  });
});

describe("maintenance state", () => {
  let ready: boolean | Error = false;
  const probe = vi.fn(async () => {
    if (ready instanceof Error) throw ready;
    return ready;
  });

  beforeEach(() => {
    vi.useFakeTimers();
    ready = false;
    probe.mockClear();
    resetMaintenanceForTesting(probe);
  });

  afterEach(() => {
    resetMaintenanceForTesting();
    vi.useRealTimers();
  });

  it("re-checks every 15 seconds and ends only when readyz is ready", async () => {
    reportMaintenance();
    reportMaintenance();
    expect(isUnderMaintenance()).toBe(true);

    await vi.advanceTimersByTimeAsync(MAINTENANCE_RECHECK_MS - 1);
    expect(probe).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(probe).toHaveBeenCalledTimes(1);
    expect(isUnderMaintenance()).toBe(true);

    ready = new Error("network down");
    await vi.advanceTimersByTimeAsync(MAINTENANCE_RECHECK_MS);
    expect(probe).toHaveBeenCalledTimes(2);
    expect(isUnderMaintenance()).toBe(true);

    ready = true;
    await vi.advanceTimersByTimeAsync(MAINTENANCE_RECHECK_MS);
    expect(isUnderMaintenance()).toBe(false);

    // Over means over: no more probing until the next report.
    await vi.advanceTimersByTimeAsync(MAINTENANCE_RECHECK_MS * 3);
    expect(probe).toHaveBeenCalledTimes(3);
  });

  it("useMaintenance re-renders and runs onEnd once when maintenance ends", () => {
    const onEnd = vi.fn();
    function Flag() {
      return <p>{useMaintenance(onEnd) ? "on" : "off"}</p>;
    }
    render(<Flag />);
    expect(screen.getByText("off")).toBeInTheDocument();

    act(() => reportMaintenance());
    expect(screen.getByText("on")).toBeInTheDocument();
    expect(onEnd).not.toHaveBeenCalled();

    act(() => clearMaintenance());
    expect(screen.getByText("off")).toBeInTheDocument();
    expect(onEnd).toHaveBeenCalledTimes(1);
  });

  it("the interceptor reports a maintenance response and passes every response through", async () => {
    installMaintenanceInterceptor();
    installMaintenanceInterceptor();
    const use = vi.mocked(client.interceptors.response.use);
    expect(use).toHaveBeenCalledTimes(1);
    const interceptor = use.mock.calls[0][0] as unknown as (r: Response) => Promise<Response>;

    const ok = new Response("{}", { status: 200 });
    expect(await interceptor(ok)).toBe(ok);
    expect(isUnderMaintenance()).toBe(false);

    const down = problem(503, "maintenance");
    expect(await interceptor(down)).toBe(down);
    expect(isUnderMaintenance()).toBe(true);
  });
});
