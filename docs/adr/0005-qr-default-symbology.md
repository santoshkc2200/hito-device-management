# ADR-0005 — QR (2D) as the default symbology

**Status:** Accepted · **Date:** 2026-08-18

## Context

Labels go on laptop lids, pendrives, cable bundles and ID cards. They must be
read by a Bluetooth scanner and, as a fallback, by an iPad camera.

## Decision

**QR Code** by default; **Data Matrix** for items too small for a readable QR;
**Code 128** supported but not issued. The `credentials.kind` column makes this
per-credential, and token resolution is symbology-agnostic.

Staff cards are issued by this system, not inherited: the hospital's existing ID
cards carry RFID/NFC, which is unavailable to the project in v1 and is a Phase 6
integration.

This requires a **2D imager**, not a 1D laser scanner.

## Consequences

**Good**
- Rotation-independent: no need to align a laser with a stripe.
- Error correction level M recovers from ~15% damage — labels get scuffed,
  wiped with disinfectant, and partially peeled.
- Works on curved surfaces where linear barcodes distort.
- **Readable by the iPad camera**, which linear codes on a curved laptop lid
  frequently are not. The camera fallback is only credible with 2D.
- Data Matrix reads down to ~8 mm square, covering pendrives and adapters.

**Bad**
- A 2D imager costs somewhat more than a 1D laser. The difference is trivial
  against the cost of the fallback path not working.
- Slightly more label area than a comparable linear code.

## Alternatives

- **Code 128 only** — rejected. Cheaper hardware, but it would foreclose the
  camera fallback and read poorly on the curved and small items that make up much
  of the inventory.
