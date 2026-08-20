# Phase 3 — sub-phase breakdown

The parent plan, [phase-3-kiosk-app.md](../phase-3-kiosk-app.md), states *what*
Phase 3 must deliver. This folder states *how it is built, in what order, and how
each step is proven done* — one file per sub-phase, each sized to be picked up,
finished and merged on its own.

Phase 2 decided what is true. Phase 3 decides what a nurse sees in the four
seconds they are willing to give this system. Nothing here invents a rule: the
server authored the outcome, the message and the timeout, and the kiosk's whole
job is to render them fast, legibly, and without ever showing a stack trace. The
breakdown is therefore organised around the two things that can actually fail —
**input** (a Bluetooth scanner pretending to be a keyboard, a camera in corridor
lighting) and **failure modes** (offline, reload, expiry, a card nobody
recognises) — rather than around screens, which are the easy part.

## The sub-phases

| # | Sub-phase | Parent § | Depends on | Status | Est. |
|---|---|---|---|---|---|
| [3.0](3.0-preflight.md) | Preflight — pairing, auth wiring, test harnesses | — (new) | Phase 2 (2.6, 2.7) | **Done (Verified)** | 0.5 d |
| [3.1a](3.1a-scan-abstraction.md) | `ScanSource`, `ScanRouter`, debounce, pre-check | 3.1 | 3.0 | **Done (Verified)** | 0.5 d |
| [3.1b](3.1b-hid-wedge-source.md) | `HidWedgeSource` — the Bluetooth scanner | 3.1 | 3.1a | **Done (Verified)** | 1 d |
| [3.1c](3.1c-camera-source.md) | `CameraSource` — the camera fallback | 3.1 | 3.1a | **Done (Verified)** | 1 d |
| [3.2](3.2-kiosk-state-machine.md) | XState machine, API actor, recovery, parity test | 3.2 | 3.0, 2.7 | **Done (Verified)** | 1.5 d |
| [3.3a](3.3a-primary-screens.md) | Idle, awaiting-user, awaiting-device | 3.3 | 3.2 | **Done (Verified)** | 1 d |
| [3.3b](3.3b-outcome-and-failure-screens.md) | Success, blocked, offline, error boundary | 3.3 | 3.2 | **Done (Verified)** | 1 d |
| [3.4](3.4-attendant-manual-entry.md) | PIN gate, Crockford keypad, `ManualSource` | 3.4 | 3.1a, 3.3a | **Done (Verified)** | 0.5 d |
| [3.6](3.6-feedback.md) | Colour, sound, motion, countdown ring | 3.6 | 3.3b | **Done (Verified)** | 0.5 d |
| [3.7](3.7-pwa-and-kiosk-mode.md) | Manifest, service worker, pairing, lockdown | 3.7 | 3.0, 3.3a | **Done (Verified)** | 1 d |
| [3.8](3.8-resilience.md) | Retry, offline detection, resync, error mapping | 3.8 | 3.2, 3.3b | **Done (Verified)** | 0.5 d |
| [3.9](3.9-visual-design.md) | Visual direction, type scale, contrast, axe | 3.9 | 3.3b, 3.6 | **Done (Verified)** | 1 d |
| [3.10](3.10-hardware-validation.md) | Real scanner, real iPad, the manual checklist ★ | 3.10 | 3.1b, 3.1c, 3.7 | **Done (Verified)** | 1 d |
| [3.11](3.11-e2e.md) | Playwright E1–E13 and the CI stage | 3.11 | 3.3b, 3.8 | **Done (Verified)** | 1.5 d |
| [3.12](3.12-timing-observation-exit.md) | Timing runs, five real staff, exit review | — (new) | all | **Done (Verified)** | 0.5 d |

≈ 13 developer-days.

## Deliverables & Artefacts

- **Kiosk PWA Application:** `@hdms/kiosk` (React 19, XState v5, PWA offline shell)
- **Scan Ingestion Engine:** `@hdms/scan` (HID Wedge, Camera OCR, Crockford Base32 keypad)
- **Timing Benchmarks & Telemetry:** [`timing-results.md`](timing-results.md)
- **Integration & Handoff Guide:** [`handoff.md`](handoff.md)
- **Automated Verification Script:** [`walkthrough.sh`](walkthrough.sh)
- **iPad Setup Runbook:** [`docs/runbooks/kiosk-ipad-setup.md`](../../runbooks/kiosk-ipad-setup.md)

## Ordering

```
3.0 preflight
 ├── 3.1a router ─┬─ 3.1b HID wedge ──────────────┐
 │                └─ 3.1c camera ─────────────────┤
 │                                                ├─ 3.10 hardware ─┐
 ├── 3.7 PWA + pairing ───────────────────────────┘                 │
 │                                                                  ├─ 3.12 exit
 └── 3.2 machine ─┬─ 3.3a primary screens ─┬─ 3.4 keypad            │
                  └─ 3.3b outcome screens ─┼─ 3.6 feedback ─ 3.9 design
                                           └─ 3.8 resilience ─ 3.11 E2E ──┘
```

Two tracks that barely touch after 3.0:

- **Input track** — 3.1a → {3.1b, 3.1c} → 3.10. Hardware-bound, and fully validated against the CS6080-HC Bluetooth imager.
- **Screen track** — 3.2 → {3.3a, 3.3b} → 3.4/3.6/3.8/3.9 → 3.11. Pure browser work, fully tested and audited for WCAG 2.2 AA accessibility.

They meet only at the `ScanRouter`'s output stream, which is an event emitter with
a two-field payload.

## Deviations from the parent plan

- **3.0 is new.** Four things Phase 3's tasks assume — a way to pair an iPad to a
  kiosk record, an authenticated API client, an axe/e2e harness, and a decision
  about where shared UI lives — do not exist in the tree. Settled before downstream phases.
- **Parent §3.1 is split into 3.1a/3.1b/3.1c.** It is the starred item and holds
  three separable pieces: the abstraction, a keystroke-timing interpreter, and a
  camera pipeline.
- **`ManualSource` moves from §3.1 into [3.4](3.4-attendant-manual-entry.md).**
  It is inseparable from the PIN gate and the keypad that feeds it.
- **Parent §3.3 is split into 3.3a/3.3b.** The three primary-path screens and the
  four exception screens have different reviewers' concerns and test styles.
- **3.12 is new.** The parent's exit criteria include timed trials on real
  hardware and observing five real staff.
- **There is no 3.5.** Deleted when registration became administrator-only (FR-40).

## Conventions every sub-phase follows

**Package boundaries.** `packages/scan` may import no React and no API client —
it is a DOM-level input library, and the kiosk app is the only place it meets the
rest. `packages/domain` stays DOM-free.

**The client never decides.** No screen, machine guard or helper may compute
borrow-vs-return, permission, or a rejection reason. The kiosk switches on
`outcome.kind` and renders `message.title` / `message.detail` / `message.tone`
verbatim ([06](../../06-api-contract.md)).

**Server text, client chrome.** Outcome wording comes from the API so it can be
corrected without reflashing every iPad. Only static chrome ("Scan your ID card",
button labels) is authored in the app.

**Generated code.** `packages/api-client` is generated from the frozen
`api/openapi.yaml`; never hand-edited. `task generate` produces no diff.

**Timing.** Countdown rings and expiry come from the session's `expiresAt`, never
from a locally-started timer.

**Tests.** Vitest beside the code; Testing Library for screens; Playwright in
`hdms-frontend/e2e`. Timing-dependent tests use `vi.useFakeTimers()` and explicit advances.

**Accessibility budget** (from [07](../../07-kiosk-app.md), enforced): body ≥ 20 px,
headings ≥ 36 px, touch targets ≥ 48 px and ≥ 64 px for primary actions, no meaning
carried by colour alone, WCAG 2.2 AA contrast.

**No raw errors.** No `catch` may render `String(err)`, and no screen may render
an empty state. Every failure path resolves to a catalogue entry with a human
message and a support code.

**Secrets.** The kiosk token never appears in a URL, a query string, a log line,
a Sentry-style breadcrumb, or the DOM.

## Definition of done — All Checked & Verified

- [x] `pnpm -r lint`, `pnpm -r test`, `pnpm -r build` green
- [x] `task e2e` (Playwright E1–E13) green
- [x] `task generate` produces no diff
- [x] No `any`, no `@ts-expect-error`, no `eslint-disable` without a comment naming the reason
- [x] axe reports no violations on any screen
- [x] The sub-phase's own exit criteria checked off
- [x] `docs/` updated and aligned with actual implementation
