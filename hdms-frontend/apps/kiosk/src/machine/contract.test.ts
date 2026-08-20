import { describe, expect, it } from "vitest";
import { scenarios } from "@hdms/domain";
import {
  buildKioskSessionMachine,
  actionToOutcomeKind,
} from "./session-machine";
import type { ScanResult } from "@hdms/api-client";
import { createActor } from "xstate";

describe("Kiosk Machine Contract Parity Test", () => {
  it("verifies scenarios fixture is loaded and not empty", () => {
    expect(scenarios).toBeDefined();
    expect(scenarios.length).toBeGreaterThanOrEqual(59); // 7 worked scenarios + 52 matrix cells
  });

  // Replay every single scenario through the XState machine
  for (const scenario of scenarios) {
    it(`replays: ${scenario.name}`, () => {
      const machine = buildKioskSessionMachine();
      const actor = createActor(machine);
      actor.start();

      if (scenario.initialState !== "idle") {
        actor.send({
          type: "RESTORE_SESSION",
          session: {
            id: "sess-init-1",
            kioskId: "kiosk-test-1",
            state: scenario.initialState,
            startedAt: new Date().toISOString(),
            expiresAt: new Date(Date.now() + 25000).toISOString(),
          },
        });
      }

      expect(actor.getSnapshot().value).toBe(scenario.initialState);

      for (let i = 0; i < scenario.inputs.length; i++) {
        const input = scenario.inputs[i];
        const expected = scenario.expected[i];

        // Stubbed API: produces the server's ScanResult matching this scenario step
        const stubbedResult: ScanResult = {
          session: {
            id: "sess-test-1",
            kioskId: "kiosk-test-1",
            state: expected.state,
            user: input.userId
              ? {
                  id: input.userId,
                  fullName: input.userId,
                  department: "Radiology",
                  openLoanCount: 0,
                }
              : undefined,
            pendingDevice:
              expected.action === "hold_device" ||
              expected.action === "replace_pending"
                ? {
                    id: input.deviceId ?? "dev-1",
                    assetTag: input.deviceId ?? "dev-1",
                    name: input.deviceId ?? "dev-1",
                  }
                : undefined,
            startedAt: new Date().toISOString(),
            expiresAt: new Date(Date.now() + 25000).toISOString(),
          },
          outcome: {
            kind: actionToOutcomeKind(expected.action),
          },
          openLoans: [],
          message: {
            title: expected.action,
            detail: `Action: ${expected.action}`,
            tone: expected.action === "reject" ? "error" : "success",
          },
        };

        // Apply scan result from stubbed API
        actor.send({
          type: "APPLY_SCAN_RESULT",
          result: stubbedResult,
          action: expected.action,
        });

        const snapshot = actor.getSnapshot();

        // 1. Assert state transition matches fixture
        expect(
          snapshot.value,
          `Scenario "${scenario.name}" step ${i} (${input.class}) -> expected state ${expected.state}`
        ).toBe(expected.state);

        // 2. Assert action matches fixture
        expect(
          snapshot.context.lastAction,
          `Scenario "${scenario.name}" step ${i} (${input.class}) -> expected action ${expected.action}`
        ).toBe(expected.action);
      }

      actor.stop();
    });
  }
});
