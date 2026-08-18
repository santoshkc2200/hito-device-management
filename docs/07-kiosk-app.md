# 07 — Kiosk application

The borrower-facing app. An iPad mounted at the equipment counter, a Bluetooth
barcode scanner paired to it, and a person who has thirty seconds and no
patience.

## Design constraints

1. **Zero learning curve.** A borrower must succeed without instruction. There is
   no login, no menu, no mode to choose.
2. **Zero taps in the normal path.** Two scans, auto-confirmed. Taps appear only
   for exceptions and for returning an item without scanning it.
3. **Readable from a metre away.** Counter-mounted iPad, person standing.
4. **The screen must clear itself.** The next person must not see the last
   person's name and loans.
5. **Never show a browser error.** A blank white page or a Safari error page is a
   total failure of the product.
6. **The kiosk only borrows and returns.** It cannot register a user, and its API
   token has no capability to do so (FR-40, FR-45). This removes text entry from
   the borrower's path entirely — the only typing anywhere in the app is the
   attendant's PIN-gated manual token entry.
7. **The kiosk never blocks the counter.** Every dead end tells the borrower they
   can still take the device and that the attendant will write it on the register.
   A screen that says only "no" is a screen that sends a nurse away without the
   equipment they came for.

## Why a PWA and not a native app

See [ADR-0007](adr/0007-pwa-kiosk-not-native.md). Briefly: no Apple Developer
account, no App Store review, no MDM distribution, instant updates to every
kiosk, one language across kiosk and admin, and — critically — the Bluetooth
scanner works as an HID keyboard, so **the main reason people reach for native
(hardware access) does not apply here.** The camera fallback is `getUserMedia`,
supported in iPadOS Safari including standalone home-screen mode.

The one thing native would buy is Core NFC. That matters only for the future
adoption of the hospital's existing RFID/NFC ID cards — and as explained in
[05](05-credentials-and-labeling.md), an HID-keyboard-mode reader delivers the
card UID as keystrokes, so even that integration needs no native code.

## Scan input: the central abstraction

FR-64 requires new input sources without touching checkout logic. One interface:

```ts
// packages/scan/src/types.ts
export interface ScanSource {
  readonly id:        'scanner' | 'camera' | 'manual' | 'nfc' | string;
  readonly label:     string;
  isAvailable():      Promise<boolean>;
  start(emit: (scan: Scan) => void): Promise<void>;
  stop():             Promise<void>;
}

export interface Scan {
  token:     string;
  source:    string;
  scannedAt: string;   // ISO-8601
  raw?:      string;   // pre-normalisation, for diagnostics only
}
```

A `ScanRouter` holds the registered sources, applies the debounce and the local
format check, and emits a single stream to the state machine. **The machine has
no idea which source a scan came from** — it only forwards the label to the
server for the audit trail.

Adding a source later is: implement `ScanSource`, register it, enable it in the
kiosk's `enabled_sources` config. No change to any screen or to the machine. For
the Phase 6 RFID integration, even that is unnecessary — an HID-mode reader
arrives through `HidWedgeSource` already.

### `HidWedgeSource` — the Bluetooth scanner

Bluetooth barcode scanners pair as a **keyboard** and "type" the decoded barcode
followed by Enter. There is no API to open; there is a keystroke stream to
interpret.

The classic detection heuristic, and the details that matter:

```ts
// Human typing: 80–300 ms between keys. Scanner: 2–20 ms.
const MAX_INTERVAL_MS = 35;
const MIN_LENGTH      = 6;

let buffer = '';
let lastKeyAt = 0;

window.addEventListener('keydown', (e) => {
  const now = performance.now();
  if (now - lastKeyAt > MAX_INTERVAL_MS) buffer = '';   // too slow → new sequence
  lastKeyAt = now;

  if (e.key === 'Enter') {
    if (buffer.length >= MIN_LENGTH) { emit(buffer); e.preventDefault(); }
    buffer = '';
    return;
  }
  if (e.key.length === 1) buffer += e.key;
});
```

Real-world details this must handle:

- **Focus.** Listen on `window` in the capture phase, not on an input element. A
  kiosk with a focused input is one stray tap away from silently dropping scans.
- **Suppress the terminating Enter** so it does not submit a form or activate a
  focused button.
- **Configure the scanner's suffix to Enter (CR)** during setup — most default to
  it; some default to Tab or nothing. Document the setup barcode in the runbook.
- **Idle-buffer reset** on a 100 ms timer, so a partial read cannot poison the
  next scan.
- **The iPad on-screen keyboard disappears** while a hardware keyboard is
  connected. This is the most annoying real-world consequence of the HID
  approach. It used to be a serious problem when borrowers registered themselves
  at the kiosk; now that registration is administrator-only, **the kiosk has no
  form for a borrower to fill in at all**, and the only text entry left is the
  attendant's manual token entry — which uses a small in-app keypad rather than
  the iOS keyboard. A whole class of difficulty disappeared with that
  requirement change.
- **Scanner sleep.** Bluetooth scanners sleep and take ~1 s to reconnect on the
  first trigger. The first scan of a quiet morning can be dropped. Mitigation: a
  heartbeat indicator on screen showing "Scanner ready", derived from time since
  last keystroke activity, plus a "press the trigger once to wake" hint after a
  long idle.

### `CameraSource` — the fallback

Toggled from a persistent button on the kiosk screen: **"Scanner not working?
Use camera"**.

- `navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' } })`.
- Decoding via the **`barcode-detector` ponyfill**, which uses the native
  `BarcodeDetector` where it exists and a zxing WASM build elsewhere. Safari has
  no native implementation, so on iPad the WASM path is what actually runs
  (~400 KB, precached by the service worker so it works on a bad connection).
- Formats limited to `qr_code`, `data_matrix`, `code_128` — narrowing the format
  list measurably improves decode rate and CPU use.
- Decode at ~10 fps on a downscaled frame; full resolution only for the crop.
- **Requires HTTPS.** `getUserMedia` is unavailable on plain HTTP except on
  `localhost`. This forces a real certificate on the internal hostname — flagged
  in [09](09-security-privacy-ops.md) because it is a common late surprise.
- Permission is granted once per origin and persists in standalone mode; the
  first-run setup checklist includes granting it.
- UX: a live viewfinder with a target frame, a torch toggle where supported, and
  automatic exit back to scanner mode after a successful read.

### `ManualSource` — attendant only

Behind a PIN. A numeric/alphanumeric on-screen pad, entering either the printed
token or an asset tag / employee number. Every use is audited and counted.

### Future sources

| Source | Implementation | Effort |
|---|---|---|
| Hospital RFID/NFC ID cards via an HID reader (Phase 6) | **None** — arrives as `HidWedgeSource` keystrokes; only UID normalisation server-side | ~0 |
| Web NFC (Android tablet only; impossible in iPadOS Safari) | `NDEFReader` wrapper implementing `ScanSource` | ~40 lines |
| Bluetooth scanner in SPP/BLE mode | Web Bluetooth `ScanSource` (Chromium only, not iPad) | small |
| Employee photo / face | Out of scope, and a policy decision, not a technical one | — |

## Screens

```
┌───────────────────────────────────────────────────────────┐
│  IDLE                                                     │
│                                                           │
│              ▢  Scan your ID card                         │
│                 or a device barcode                       │
│                                                           │
│         ● Scanner ready        [ Use camera ]             │
│                                    Hito Hospital · Kiosk 1│
└───────────────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────────────┐
│  AWAITING USER              (device scanned first)        │
│   ┌──────┐                                                │
│   │ 💻   │  LAPTOP-07                                     │
│   └──────┘  Dell Latitude 5420 · Available                │
│                                                           │
│            → Now scan your ID card                        │
│                                             ◔ 38s         │
└───────────────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────────────┐
│  AWAITING DEVICE            (user scanned first)          │
│   Hello, Dr. A. Sharma  ·  Radiology                      │
│                                                           │
│   You have 2 items out:                                   │
│   ┌─────────────────────────────────────────┐             │
│   │ LAPTOP-07   out 14 Aug · due today      │ [ RETURN ]  │
│   │ PROJECTOR-02 out 10 Aug · 4 days over ⚠ │ [ RETURN ]  │
│   └─────────────────────────────────────────┘             │
│                                                           │
│            → Scan a device to borrow or return            │
│                                    [ Done ]        ◔ 21s  │
└───────────────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────────────┐
│  SUCCESS                                                  │
│                        ✓                                  │
│                 Borrowed                                  │
│           LAPTOP-07 · Dell Latitude 5420                  │
│              Please return by tomorrow, 9:00              │
│                                                           │
│      Scan another device, or tap Done      [ Done ]       │
└───────────────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────────────┐
│  BLOCKED                                                  │
│                        ⛔                                  │
│           This device is already on loan                  │
│      LAPTOP-07 is with a colleague in Radiology,          │
│              out since 14 Aug 09:20.                      │
│           Please see the equipment desk.                  │
│                                        [ OK ]      ◔ 8s   │
└───────────────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────────────┐
│  NOT REGISTERED                                           │
│                        ⓘ                                  │
│              This card is not registered                  │
│                                                           │
│     You can still take the device — the attendant will    │
│     write it in the register. Please see them to get      │
│     your card so next time you can just scan.             │
│                                                           │
│                                        [ OK ]      ◔ 8s   │
└───────────────────────────────────────────────────────────┘
```

## Feedback

Distinct, immediate, multi-channel — the borrower is often looking at the device,
not the screen.

| Outcome | Colour | Sound | Haptic | Duration |
|---|---|---|---|---|
| Borrowed | green | rising two-tone | light | 4 s then back to `ready` |
| Returned | blue | falling two-tone | light | 4 s |
| Scan accepted, waiting | neutral | single soft click | — | until next scan |
| Blocked / rejected | amber | low buzz | double | 8 s or tap OK |
| Not registered / use paper | blue-grey | soft single tone | — | 8 s or tap OK |
| Error / offline | red | none | long | until resolved |

Sounds are short and quiet — this is a hospital corridor. An admin setting
silences audio entirely per kiosk. Haptics via the Vibration API are unavailable
on iPadOS; the visual and audio channels carry the load there, and the table
above degrades gracefully.

## State management

XState v5 machine mirroring the server's states (see
[04](04-scanning-and-checkout-flows.md)), with `after` transitions for the
timeouts and an actor invoking the scan API.

```
idle ──SCAN──▶ submitting ──▶ (server response)
                              ├─ device_pending      ──▶ awaitingUser
                              ├─ user_identified     ──▶ awaitingDevice
                              ├─ borrowed | returned ──▶ ready
                              └─ rejected            ──▶ blocked ──after 8s──▶ previous
```

The machine is **presentation only**. It never decides whether a borrow is
allowed; it decides which screen to show and when to time out. Business rules
live once, on the server. A `contract.test.ts` replays the shared scenario
fixture through both this machine and the Go one to prove the mirror is accurate.

## Reliability

**Network loss.** TanStack Query with retry and a persistent mutation cache. A
full-screen non-technical banner appears — *"Reconnecting… the attendant can
record your item on the register in the meantime"* — never a raw error. The
message names the fallback rather than asking the borrower to wait indefinitely,
because the paper register is precisely what exists for this moment.

Phase 5 adds an IndexedDB queue that replays queued transactions with their
original idempotency keys on reconnect; the API is idempotent from Phase 2
specifically so this is a purely client-side addition later.

**Reload / crash.** The session id is kept in `sessionStorage`; on load, the app
calls `GET /v1/sessions/{id}` and resumes exactly where it was. If the session
expired, it starts clean.

**Wedged state.** A watchdog returns the app to `idle` after 3 minutes in any
non-idle state, whatever happened. A kiosk stuck on one person's screen all
afternoon is both a privacy incident and an outage.

## iPad setup

Documented as a runbook checklist in Phase 3; the substance:

1. Pair the Bluetooth scanner (iOS Settings → Bluetooth; the scanner's manual
   gives a pairing barcode).
2. Configure the scanner via setup barcodes: **suffix = CR/Enter**, symbologies
   QR + Data Matrix + Code 128 enabled, and note its **"show/hide iOS keyboard"
   toggle barcode** in case it is ever needed.
3. Open the kiosk URL in Safari → Share → **Add to Home Screen**. This yields
   standalone mode: no address bar, no tab bar, its own storage.
4. Register the kiosk: the admin creates it in the console, the iPad is given a
   one-time registration code, and the resulting kiosk token is stored in the
   app's local storage.
5. Grant camera permission once.
6. **Guided Access** (Settings → Accessibility → Guided Access) with a passcode,
   locking the iPad to the app. Also: Auto-Lock = Never, Screen Time app limits
   off, auto-brightness suited to the corridor.
7. Physical: powered stand or a cable to a wall outlet, and a scanner cradle.
   A kiosk at 3% battery is a down kiosk.

## Accessibility

- WCAG 2.2 AA contrast throughout; verified with automated axe checks in CI plus
  a manual pass.
- Touch targets ≥ 48 × 48 px, ≥ 64 px for primary actions.
- No information conveyed by colour alone — every state has an icon and text.
- Text scales with the iOS Dynamic Type setting up to 150% without layout breakage.
- Screens are legible at arm's length: body text ≥ 20 px, headings ≥ 36 px.
- The kiosk is mounted at a height reachable from a wheelchair; the return-by-tap
  list means a borrower who cannot manipulate the scanner can still complete the
  transaction.
- No borrower is ever asked to type. The only text entry in the app is the
  attendant's PIN-gated manual token entry, which is a large in-app keypad.
