# Phase 3 — Kiosk application

**Goal:** a staff member walks up to the iPad, scans twice, and leaves with a
device — in under eight seconds, with no instruction.
**Duration:** ~2 weeks · **Depends on:** Phase 2 (frozen contract) · **Parallel with:** Phase 4

**Detailed breakdown:** [`phase-3/`](phase-3/) splits the tasks below into
fifteen executable sub-phases, each with its file paths, tests and exit criteria,
plus the ordering that lets the hardware-bound input track run in parallel with
the screen track. Start there; this page stays the summary.

> Shortened from the original ~2–3 week estimate. Removing kiosk self-registration
> deleted the enrollment form, the full QWERTY on-screen keyboard, and a state
> from the machine — roughly a week of the most fiddly work in the phase.

## Tasks

### 3.1 `packages/scan` — the source abstraction ★
- [ ] `ScanSource` interface and `Scan` type from
      [07](../07-kiosk-app.md)
- [ ] `ScanRouter`: source registry, 1500 ms debounce, local token format check,
      single output stream
- [ ] `HidWedgeSource` — window-level capture-phase key listener, inter-key timing
      heuristic, Enter suppression, idle-buffer reset, a "scanner ready"
      heartbeat derived from recent keystroke activity
- [ ] `CameraSource` — `getUserMedia`, `barcode-detector` ponyfill, formats
      limited to `qr_code`/`data_matrix`/`code_128`, ~10 fps on downscaled frames,
      torch toggle, viewfinder overlay
- [ ] `ManualSource` — PIN-gated, on-screen keypad
- [ ] Unit tests with synthesised keystroke timings covering scanner-speed,
      human-speed, partial reads and interleaving

### 3.2 Kiosk state machine
- [ ] XState v5 machine generated from `packages/domain/session-machine.json`
- [ ] `after` transitions for the three timeouts
- [ ] API actor invoking the scan endpoint with idempotency keys
- [ ] Session recovery from `sessionStorage` via `GET /v1/sessions/{id}` on load
- [ ] 3-minute watchdog forcing a return to `idle` from any state
- [ ] Parity test against the Go machine using `scenarios.json`

### 3.3 Screens
- [ ] `IdleScreen` — prompt, scanner-ready indicator, camera toggle, kiosk name
- [ ] `AwaitingUserScreen` — pending device card, prompt, countdown ring
- [ ] `AwaitingDeviceScreen` — greeting, open-loan list with tap-to-return,
      prompt, Done, countdown
- [ ] `SuccessScreen` — borrowed/returned variants, due date, continue prompt
- [ ] `BlockedScreen` — reason, guidance, OK, auto-dismiss. Covers device
      conflicts and all three unrecognised-card cases, each with its own wording
      directing the person to the equipment administrator
- [ ] `OfflineScreen` — full-screen, non-technical, with the kiosk name and a
      support code
- [ ] `ErrorBoundary` — cannot render a raw error under any circumstance

### 3.4 Attendant manual-entry keypad
Scoped down from a full keyboard: no borrower ever types anything, so the only
text entry left is the attendant's PIN-gated manual token entry.
- [ ] PIN gate
- [ ] Alphanumeric keypad restricted to the Crockford Base32 alphabet — a smaller,
      more forgiving keypad than QWERTY, and it makes an invalid character
      impossible to enter
- [ ] Large keys (≥ 56 px), press feedback, backspace, clear
- [ ] Local checksum validation before submission
- [ ] Verified on real hardware with the scanner paired (iPadOS suppresses its own
      keyboard while a scanner is connected, which is exactly why this exists)

### 3.6 Feedback
- [ ] Colour, icon and motion per outcome
- [ ] Distinct short sounds for borrow, return, reject; Web Audio, preloaded
- [ ] Per-kiosk mute setting
- [ ] Countdown ring appearing in the final 8 seconds
- [ ] Transitions ≤ 200 ms; `prefers-reduced-motion` respected

### 3.7 PWA and kiosk mode
- [ ] Manifest: name, icons, `display: standalone`, orientation, theme colour
- [ ] Service worker precaching the shell and the zxing WASM bundle
- [ ] Kiosk registration flow: one-time code → kiosk token stored locally
- [ ] Pull-to-refresh and overscroll disabled; text selection and long-press
      callouts disabled
- [ ] Wake lock where supported; documented Auto-Lock = Never otherwise

### 3.8 Resilience
- [ ] TanStack Query retry with backoff
- [ ] Connectivity detection driving the offline screen
- [ ] Automatic recovery and resync on reconnect
- [ ] Every API failure mapped to a human message — no code path can surface a
      stack trace or a bare status code

### 3.9 Visual design
- [ ] Use the `frontend-design` skill for the visual direction; use `shadcn` for
      component scaffolding
- [ ] Type scale: body ≥ 20 px, headings ≥ 36 px
- [ ] WCAG 2.2 AA contrast, verified with axe in CI
- [ ] Restrained hospital-appropriate palette; success/warning/error clearly
      distinguishable including for common colour-vision deficiencies
- [ ] Layout works in both orientations and at 150% Dynamic Type

### 3.10 Hardware validation ★
- [ ] Procure and pair the Bluetooth 2D imager
- [ ] Configure suffix = CR, symbologies, and record the setup barcodes in the
      runbook
- [ ] Work through the full manual checklist in
      [10](../10-testing-strategy.md)
- [ ] Guided Access configured and verified
- [ ] Mount, power and scanner cradle installed at the counter

### 3.11 E2E
- [ ] Playwright scenarios E1–E13 from [10](../10-testing-strategy.md), including
      E7/E8 — unknown and unbound cards refused with the right guidance and no
      user created
- [ ] Scans simulated as keyboard events — the same thing the hardware produces
- [ ] Fake camera stream for E13

## Deliverables

- Kiosk PWA installed and running on the real iPad
- `packages/scan` with three working sources
- Attendant manual-entry keypad
- Parity test green against the backend machine
- E2E suite green
- Completed hardware checklist
- iPad setup runbook

## Exit criteria

- [ ] On real hardware: borrow completed in **< 8 s**, return in **< 6 s**,
      measured over 10 trials each
- [ ] All seven scenarios from [04](../04-scanning-and-checkout-flows.md) work on
      the physical kiosk
- [ ] An unregistered card is refused with clear guidance, and the kiosk provably
      cannot create a user
- [ ] Camera fallback decodes a device label with the scanner powered off
- [ ] Killing the API mid-session shows the offline screen, never a browser error
- [ ] iPad reload mid-session resumes the session correctly
- [ ] Guided Access prevents leaving the app
- [ ] Parity test green
- [ ] axe reports no violations on any screen

## Risks

| Risk | Mitigation |
|---|---|
| iOS keyboard suppression breaks the attendant's manual entry | The in-app keypad (3.4) removes the dependency on the iOS keyboard entirely |
| Borrowers turned away at the kiosk with no idea what to do | Server-authored guidance names the equipment administrator; the counter quick-guide repeats it; the dashboard shows how often it happens |
| Scanner sleep drops the first scan of the morning | Heartbeat indicator plus a "press the trigger to wake" hint; documented in the runbook |
| iPadOS Safari quirks in standalone mode (storage eviction, wake behaviour) | Test on the real device from week one of this phase, never only in desktop Safari |
| Camera decode too slow or unreliable in corridor lighting | Restricted format list, downscaled frames, torch toggle; tested in the actual lighting |
| Borrowers do not understand the screen | Observe five real staff during the phase, before it is called done |
