import { describe, expect, it } from "vitest";
import {
  sessionMachine,
  scenarios,
  type InputClass,
  type SessionState,
} from "./session-machine";

describe("sessionMachine", () => {
  it("has version 1.0.0", () => {
    expect(sessionMachine.version).toBe("1.0.0");
  });

  it("defines all four states", () => {
    const states: SessionState[] = [
      "idle",
      "awaiting_user",
      "awaiting_device",
      "ready",
    ];
    expect(Object.keys(sessionMachine.states).sort()).toEqual(states.sort());
  });

  it("covers all 13 input classes in each state", () => {
    const classes: InputClass[] = [
      "device_available",
      "device_on_loan_same_user",
      "device_on_loan_other_user",
      "device_unavailable",
      "device_duplicate",
      "user_active",
      "user_same",
      "user_suspended",
      "user_archived",
      "unbound",
      "unknown",
      "revoked",
      "timeout",
    ];

    for (const [stateName, stateDef] of Object.entries(sessionMachine.states)) {
      const definedClasses = Object.keys(stateDef.on).sort();
      expect(definedClasses, `classes in ${stateName}`).toEqual(classes.sort());

      for (const [className, rule] of Object.entries(stateDef.on)) {
        expect(rule.action, `${stateName}.${className} action`).toBeDefined();
        expect(rule.target, `${stateName}.${className} target`).toBeDefined();
        expect(typeof rule.clearsPending, `${stateName}.${className} clearsPending`).toBe("boolean");
      }
    }
  });

  it("includes correct after timeouts for states", () => {
    expect(sessionMachine.states.idle.after).toBeUndefined();
    expect(sessionMachine.states.awaiting_user.after).toEqual({
      "45000": { target: "idle" },
    });
    expect(sessionMachine.states.awaiting_device.after).toEqual({
      "25000": { target: "idle" },
    });
    expect(sessionMachine.states.ready.after).toEqual({
      "25000": { target: "idle" },
    });
  });
});

describe("scenarios", () => {
  it("exports replay scenarios fixture", () => {
    expect(Array.isArray(scenarios)).toBe(true);
    expect(scenarios.length).toBeGreaterThanOrEqual(59); // 7 worked scenarios + matrix cells
  });

  it("includes worked scenarios 1 through 7", () => {
    const names = scenarios.map((s) => s.name);
    for (let i = 1; i <= 7; i++) {
      const match = names.some((n) => n.startsWith(`${i} — `));
      expect(match, `worked scenario ${i}`).toBe(true);
    }
  });

  it("every scenario has valid inputs and expected outcomes of matching length", () => {
    for (const scenario of scenarios) {
      expect(scenario.name).toBeTruthy();
      expect(scenario.initialState).toBeDefined();
      expect(scenario.inputs.length).toBeGreaterThan(0);
      expect(scenario.inputs.length).toBe(scenario.expected.length);

      for (const input of scenario.inputs) {
        expect(input.class).toBeDefined();
      }

      for (const expected of scenario.expected) {
        expect(expected.action).toBeDefined();
        expect(expected.state).toBeDefined();
      }
    }
  });
});
