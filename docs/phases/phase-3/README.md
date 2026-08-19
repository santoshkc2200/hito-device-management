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

| # | Sub-phase | Parent § | Depends on | Est. |
|---|---|---|---|---|
| [3.0](3.0-preflight.md) | Preflight — pairing, auth wiring, test harnesses | — (new) | Phase 2 (2.6, 2.7) | 0.5 d |
| [3.1a](3.1a-scan-abstraction.md) | `ScanSource`, `ScanRouter`, debounce, pre-check | 3.1 | 3.0 | 0.5 d |
| [3.1b](3.1b-hid-wedge-source.md) | `HidWedgeSource` — the Bluetooth scanner | 3.1 | 3.1a | 1 d |
| [3.1c](3.1c-camera-source.md) | `CameraSource` — the camera fallback | 3.1 | 3.1a | 1 d |
| [3.2](3.2-kiosk-state-machine.md) | XState machine, API actor, recovery, parity test | 3.2 | 3.0, 2.7 | 1.5 d |
| [3.3a](3.3a-primary-screens.md) | Idle, awaiting-user, awaiting-device | 3.3 | 3.2 | 1 d |
| [3.3b](3.3b-outcome-and-failure-screens.md) | Success, blocked, offline, error boundary | 3.3 | 3.2 | 1 d |
| [3.4](3.4-attendant-manual-entry.md) | PIN gate, Crockford keypad, `ManualSource` | 3.4 | 3.1a, 3.3a | 0.5 d |
| [3.6](3.6-feedback.md) | Colour, sound, motion, countdown ring | 3.6 | 3.3b | 0.5 d |
| [3.7](3.7-pwa-and-kiosk-mode.md) | Manifest, service worker, pairing, lockdown | 3.7 | 3.0, 3.3a | 1 d |
| [3.8](3.8-resilience.md) | Retry, offline detection, resync, error mapping | 3.8 | 3.2, 3.3b | 0.5 d |
| [3.9](3.9-visual-design.md) | Visual direction, type scale, contrast, axe | 3.9 | 3.3b, 3.6 | 1 d |
| [3.10](3.10-hardware-validation.md) | Real scanner, real iPad, the manual checklist ★ | 3.10 | 3.1b, 3.1c, 3.7 | 1 d |
| [3.11](3.11-e2e.md) | Playwright E1–E13 and the CI stage | 3.11 | 3.3b, 3.8 | 1.5 d |
| [3.12](3.12-timing-observation-exit.md) | Timing runs, five real staff, exit review | — (new) | all | 0.5 d |

≈ 13 developer-days. The parent's "~2 weeks" holds only if the input track
(3.1b/3.1c/3.10) runs alongside the screen track — see the ordering below — and
only if the scanner is **already on the desk**. See the procurement note.

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

- **Input track** — 3.1a → {3.1b, 3.1c} → 3.10. Hardware-bound, unpleasant to
  test, and the only work whose schedule risk is external.
- **Screen track** — 3.2 → {3.3a, 3.3b} → 3.4/3.6/3.8/3.9 → 3.11. Pure browser
  work, testable on a laptop, parallelisable across two people at 3.3.

They meet only at the `ScanRouter`'s output stream, which is an event emitter with
a two-field payload. That is deliberate: a scanner that will not pair must not
block the screens, and a screen still in review must not block the hardware
checklist.

**Hardware status (2026-08-19).** The **iPad is on hand**; the **Bluetooth imager
is not yet available**. That splits 3.10 in two: everything device-shaped —
standalone mode, pairing, camera fallback, Guided Access, storage survival,
layout at 150% type — is verifiable *now*, from the first week, and should be.
Everything scanner-shaped — timing-constant tuning, the 50-label read test, the
sleep/wake case, the keypad-with-scanner-connected check — is blocked until the
imager arrives. Build 3.1b against synthesised timings with its constants marked
untuned, and treat the imager's arrival date as the schedule risk it is: if it
lands after 3.11, Phase 3 exits with [3.10](3.10-hardware-validation.md) and
[3.12](3.12-timing-observation-exit.md) explicitly incomplete rather than
silently assumed.

## Deviations from the parent plan

- **3.0 is new.** Four things Phase 3's tasks assume — a way to pair an iPad to a
  kiosk record, an authenticated API client, an axe/e2e harness, and a decision
  about where shared UI lives — do not exist in the tree. Two of them touch the
  *frozen* Phase 2 contract, so they must be settled before three sub-phases
  start consuming it.
- **Parent §3.1 is split into 3.1a/3.1b/3.1c.** It is the starred item and holds
  three separable pieces: the abstraction, a keystroke-timing interpreter, and a
  camera pipeline. Split, each is reviewable and independently testable; together
  it is one pull request whose diff nobody reads honestly.
- **`ManualSource` moves from §3.1 into [3.4](3.4-attendant-manual-entry.md).**
  It is inseparable from the PIN gate and the keypad that feeds it — building the
  source without its only UI would be building an untestable stub.
- **Parent §3.3 is split into 3.3a/3.3b.** The three primary-path screens and the
  four exception screens have different reviewers' concerns (speed and clarity vs.
  never-show-an-error) and different test styles.
- **3.12 is new.** The parent's exit criteria include timed trials on real
  hardware and observing five real staff. Both are real work with a real audience;
  they get a named slot rather than being assumed to happen on the last afternoon.
- **There is no 3.5.** The parent has none either — it was kiosk self-registration,
  deleted when registration became administrator-only (FR-40). The number is left
  vacant rather than reused, so cross-references from the parent and from
  `docs/10` stay valid.

## Conventions every sub-phase follows

**Package boundaries.** `packages/scan` may import no React and no API client —
it is a DOM-level input library, and the kiosk app is the only place it meets the
rest. `packages/domain` stays DOM-free (it is imported by tests and, later, by
Node tooling). Violations are caught by the fact that neither package lists those
dependencies; keep it that way.

**The client never decides.** No screen, machine guard or helper may compute
borrow-vs-return, permission, or a rejection reason. The kiosk switches on
`outcome.kind` and renders `message.title` / `message.detail` / `message.tone`
verbatim ([06](../../06-api-contract.md)). Any `if` that reimplements a server
rule is a defect, however convenient.

**Server text, client chrome.** Outcome wording comes from the API so it can be
corrected without reflashing every iPad. Only static chrome ("Scan your ID card",
button labels) is authored in the app.

**Generated code.** `packages/api-client` is generated from the frozen
`api/openapi.yaml`; never hand-edited. `task generate` must produce no diff.

**Timing.** Countdown rings and expiry come from the session's `expiresAt`, never
from a locally-started timer — a paused tab, a slow render or a clock skew must
not let the kiosk disagree with the server about whether a session is alive. The
machine's `after` transitions are for *presentation* deadlines only.

**Tests.** Vitest beside the code; Testing Library for screens; Playwright in
`hdms-frontend/e2e`. Anything timing-dependent uses `vi.useFakeTimers()` and
explicit advances — never a real `sleep`.

**Accessibility budget** (from [07](../../07-kiosk-app.md), enforced, not
aspirational): body ≥ 20 px, headings ≥ 36 px, touch targets ≥ 48 px and ≥ 64 px
for primary actions, no meaning carried by colour alone, WCAG 2.2 AA contrast.

**No raw errors.** No `catch` may render `String(err)`, and no screen may render
an empty state. Every failure path resolves to a catalogue entry with a human
message and a support code.

**Secrets.** The kiosk token never appears in a URL, a query string, a log line,
a Sentry-style breadcrumb, or the DOM. Credential tokens are never logged even in
development — the same rule the backend follows.

## Definition of done — applies to every sub-phase

- [ ] `pnpm -r lint`, `pnpm -r test`, `pnpm -r build` green
- [ ] `task e2e` green (from [3.11](3.11-e2e.md) onward; before it, the Phase 0
      smoke test must still pass)
- [ ] `task generate` produces no diff
- [ ] No `any`, no `@ts-expect-error`, no `eslint-disable` without a comment
      naming the reason
- [ ] axe reports no violations on any screen the sub-phase touched
- [ ] The sub-phase's own exit criteria, below its task list, are checked off
- [ ] `docs/` updated if the implementation deviated from the design docs — the
      docs are the contract for Phases 4–6, so drift is a defect

## Gaps in the tree this breakdown found

Recorded here because each one is a task in a sub-phase rather than a surprise:

1. **Nothing pairs an iPad to a kiosk record.** [07](../../07-kiosk-app.md) step 4
   describes a one-time registration code typed into the iPad, and no such
   endpoint existed. **Fixed upstream:** the pairing-code service, migration and
   the two endpoints were folded into [2.0](../phase-2/2.0-preflight.md) and
   [2.6](../phase-2/2.6-api-surface.md), where the spec is still open — an
   amendment to a frozen contract costs more than a line in an open one.
   [3.0](3.0-preflight.md) now only verifies the browser half;
   [3.7](3.7-pwa-and-kiosk-mode.md) builds the pairing screen.
2. **The kiosk app does not authenticate.** `apps/kiosk/src/App.tsx` calls
   `client.setConfig({ baseUrl })` and nothing else: no `Authorization` header, no
   `Idempotency-Key`, no problem+json normalisation. Closed in [3.0](3.0-preflight.md).
3. **`packages/scan` is an empty stub** (`export {}`) with `barcode-detector`
   already installed and unused. Filled in [3.1a](3.1a-scan-abstraction.md)–[3.1c](3.1c-camera-source.md).
4. **CI never runs Playwright or axe.** `.github/workflows/ci.yml` runs
   lint/test/build for the frontend and stops there, while
   [10](../../10-testing-strategy.md) assumes an `e2e` stage against
   docker-compose and axe on every screen. Harness in [3.0](3.0-preflight.md),
   the stage itself in [3.11](3.11-e2e.md).
5. **The kiosk still renders the Phase 0 health-check page**, and
   `e2e/kiosk.spec.ts` asserts on it. Both are replaced in
   [3.3a](3.3a-primary-screens.md) and [3.11](3.11-e2e.md) — not left to rot as a
   route nobody deletes.
6. **`packages/ui` exports only `cn` and a token sheet**, while `apps/kiosk` keeps
   its own `components/ui/button.tsx`. Left deliberately app-local (the kiosk's
   button is 64 px tall and the admin's is not), and written down in
   [3.0](3.0-preflight.md) so Phase 4 does not "fix" it by merging the two.
