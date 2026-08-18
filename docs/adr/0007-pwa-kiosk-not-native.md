# ADR-0007 — Web PWA kiosk instead of a native iPad app

**Status:** Accepted · **Date:** 2026-08-18

## Context

The kiosk is an iPad. The instinctive choice for an iPad kiosk is a native app,
usually justified by hardware access.

## Decision

Build the kiosk as a React PWA, installed to the home screen in standalone mode,
locked down with Guided Access.

## Consequences

**Good**
- **The hardware argument does not apply here.** The Bluetooth scanner pairs as
  an HID keyboard and delivers keystrokes to any app, web included. The camera
  fallback is `getUserMedia`, supported in iPadOS Safari including standalone
  mode.
- No Apple Developer account, no App Store review, no MDM distribution, no
  provisioning-profile expiry to be surprised by in a year.
- Updates reach every kiosk instantly — important when the fix is "the wording on
  the blocked screen is confusing".
- One language and one component library across kiosk and admin.
- The same code runs on an Android tablet or a cheap touchscreen PC if the
  hospital's hardware choice changes.

**Bad**
- **No Core NFC.** Web NFC does not exist in Safari. This matters only for the
  Phase 6 adoption of the hospital's existing RFID/NFC ID cards, which v1 does
  not use. Mitigated even then: an HID-keyboard-mode reader delivers the card UID
  as keystrokes, so the integration needs no native code (see
  [05](../05-credentials-and-labeling.md)).
- Standalone-mode storage can be evicted by iOS under pressure; the kiosk token
  and session id must be re-obtainable, and are.
- Guided Access must be configured per device by hand.
- Slightly more work to suppress browser gestures — overscroll, pull-to-refresh,
  long-press callouts.

## Alternatives

- **Native Swift app** — rejected. Adds an Apple account, a review cycle, a
  distribution mechanism and a second codebase, to gain Core NFC — which is not
  needed in v1 at all, and which HID readers make unnecessary in Phase 6.
- **React Native / Capacitor** — rejected. Carries most of the native deployment
  overhead while still writing JavaScript, for a benefit this system does not
  need.
