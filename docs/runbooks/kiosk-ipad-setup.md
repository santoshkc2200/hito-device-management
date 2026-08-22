# Runbook: Kiosk iPad Provisioning & Physical Setup

This runbook guides administrators and technicians through provisioning, pairing, locking down, and mounting an iPad running the **HDMS Kiosk PWA** at a hospital equipment counter.

---

## Prerequisites & Bill of Materials

1. **Hardware**:
   - iPad (9th, 10th generation or iPad Air running iPadOS 16+)
   - Bluetooth 2D companion imager:
     - **Zebra CS6080-HC** (Healthcare Cordless 2D Imager, IP65 disinfectant-ready housing) — *Recommended*
     - **Opticon OPN-2006** (Bluetooth HID Companion Scanner)
     - **Honeywell Voyager 1602g** (Pocket 2D Scanner)
   - Heavy-duty secure counter mount / enclosure with lock (e.g. Bouncepad / Compulocks)
   - Continuous power supply (MFi-certified lightning/USB-C cable & high-wattage power brick)
   - Scanner charging cradle with tether or magnetic dock
2. **Network & Accounts**:
   - Internal hospital Wi-Fi with access to the HDMS server hostname
   - Valid admin credentials to the HDMS Admin Console (`/admin`) to issue a pairing code
   - Dedicated local iPad passcode (recorded in department vault)

---

## Step 1: Pair Bluetooth Barcode Scanner

1. Turn on the Bluetooth scanner.
2. Put the scanner into **Bluetooth HID Keyboard mode** by scanning the pairing/HID barcode in the manufacturer user guide (or holding the trigger for 8 seconds until the blue LED flashes rapidly).
3. On the iPad, open **Settings → Bluetooth**.
4. Tap the scanner name under **Other Devices** (e.g., `CS6080 Barcode Scanner` or `OPN-2006`).
5. Confirm the status changes to **Connected** (solid blue indicator LED on scanner).

---

## Step 2: Configure Scanner Symbologies & Suffix

Scan the configuration programming barcodes from the scanner's quick-start reference:

### Barcode Programming Matrix

| Setting | Parameter Value | Barcode / Hex | Purpose & Effect |
|---|---|---|---|
| **Suffix** | CR / Enter | `0x0D` (Carriage Return) | Delimits scan sequence in `HidWedgeSource` |
| **Inter-character Delay** | Fast / 0 ms | `0 ms` (Burst mode) | Ensures inter-key interval is 4–18 ms (below 35 ms threshold) |
| **Symbologies Enabled** | QR Code, Data Matrix, Code 128 | `QR` + `DM` + `C128` | Asset tags, compact labels, staff badges |
| **Symbologies Disabled** | EAN/UPC, Code 39, PDF417, Codabar | Disable All Others | Eliminates false decodes and reduces latency |
| **Sleep Timeout** | 30 minutes | `30 min` | Extends battery life while remaining ready during shifts |
| **iOS Keyboard Toggle** | Double-trigger pull | Enabled | Allows toggling on-screen keyboard if needed |

---

## Step 3: Add Kiosk PWA to Home Screen

1. On the iPad, open Safari and navigate to the kiosk HTTPS URL:
   ```
   https://kiosk.hdms.hito.internal/
   ```
2. Ensure the SSL certificate is trusted (green lock icon, no certificate errors).
3. Tap the **Share** button (box with upward arrow) in the Safari toolbar.
4. Scroll down and select **Add to Home Screen**.
5. Verify the icon is the HDMS appliance logo and the name is **HDMS Kiosk**.
6. Tap **Add**.
7. Close Safari and tap the new **HDMS Kiosk** icon on the Home Screen.
8. Verify that the app launches in **`display: standalone`** mode (no Safari URL bar, no tab bar, full-screen viewport).

---

## Step 4: Pair Kiosk Identity with One-Time Code

1. On initial launch, the app displays the **"This iPad is not yet paired"** screen with a 6-digit numeric keypad.
2. Log into the **HDMS Admin Console** on your workstation:
   - Navigate to **Settings → Kiosks**, register the station, then select **Pair this kiosk now**. For an existing active station, select **Actions → Issue pairing code**.
   - Note the 6-digit numeric code (valid for 10 minutes).
3. On the iPad Kiosk screen:
   - Enter the 6-digit pairing code using the on-screen keypad.
   - Tap **Pair Kiosk**.
4. Upon successful exchange:
   - The station token, ID, and name are stored securely in local appliance storage.
   - The screen immediately transitions to the **Idle Screen** displaying the kiosk name and **"Scanner Ready"** status.
   - *Note*: The long-lived token is never displayed or accessible to borrowers.
5. If the code expires or is replaced, select **Issue new code** in the pairing dialog. A replacement immediately invalidates the previous code.

---

## Step 5: Test Camera Permission for Fallback Scanner (Optional)

1. Start a kiosk scan. If the Bluetooth scanner cannot read the barcode, tap the top-right **Camera** button.
2. When iPadOS displays the system prompt:
   > *"“HDMS Kiosk” Would Like to Access the Camera"*
3. Tap **Allow**.
4. Aim the camera viewfinder at a test asset tag to confirm decoding.
5. Tap **Close** or the X button to return to standard idle mode.
6. *Note*: Camera permission is not requested during kiosk startup. In standalone PWA mode, permission persists after this fallback is used.

---

## Step 6: Configure Guided Access & System Lockdown

To prevent borrowers or visitors from exiting the app or accessing iOS settings:

1. Open **Settings → Accessibility → Guided Access**:
   - Toggle **Guided Access** to **On**.
   - Tap **Passcode Settings → Set Guided Access Passcode** (enter the hospital station passcode).
   - Set **Auto-Lock** to **Never** (or configure screen display sleep to Never under **Settings → Display & Brightness → Auto-Lock: Never**).
2. Open the **HDMS Kiosk** app from the Home Screen.
3. **Triple-click the Top / Side button** (or Home button on older models) to initiate Guided Access.
4. Tap **Options** in the bottom left corner:
   - **Side Button**: Off
   - **Volume Buttons**: Off
   - **Motion**: Off
   - **Keyboards**: On
   - **Touch**: On
5. Tap **Start** (or Resume) in the top-right corner.
6. Verify that swiping up from the bottom or pressing the power button does not exit the kiosk.

---

## Step 7: Physical Mounting & Power

1. Secure the iPad into the steel counter enclosure.
2. Route the high-durability power cable through the mount conduit into a locked, continuous power outlet (non-switched).
3. Verify that the iPad battery shows the charging bolt and does not drop charge under continuous display on.
4. Mount the Bluetooth scanner charging base adjacent to the tablet mount.
5. Perform 3 test checkouts and returns using standard staff badges and sample asset tags.

---

## Step 8: "Kiosk Is Not Scanning" — Triage Decision Tree

If a borrower or staff member reports that barcodes are not reading:

```
[Borrower Scans Barcode] ──> No Beep / No Screen Reaction
             │
             ▼
[Step 1: Check Kiosk Screen Heartbeat]
  • Is header showing "Scanner Ready" (green dot) or "Scanner Asleep / Stale"?
  • If "Scanner Asleep":
      -> Pull scanner trigger once into open air to wake Bluetooth radio.
      -> Wait 2 seconds for blue LED on scanner to illuminate solid.
      -> Re-scan barcode.
             │
             ▼ (Still no reaction)
[Step 2: Check Scanner Hardware & Battery]
  • Is the scanner LED dark when trigger is pulled?
      -> Battery depleted. Place scanner in charging cradle for 5 minutes.
      -> Confirm red/amber charging LED turns on.
             │
             ▼ (Scanner beeps but iPad does not react)
[Step 3: Check Bluetooth Pairing]
  • Check iPad Settings → Bluetooth: Is scanner "Connected"?
  • If "Disconnected" or "Not Connected":
      -> Scan the Bluetooth Connect programming barcode on the cradle.
      -> Or cycle Bluetooth off and on in iPad Control Center.
             │
             ▼ (Bluetooth scanner temporarily unavailable)
[Step 4: Fallback to Built-in Camera]
  • Tap the "Use Camera" button in the upper-right corner of the Kiosk screen.
  • Hold asset label or badge 15–20 cm in front of the front/rear lens.
  • Viewfinder will recognize QR/Data Matrix/Code 128 and submit automatically.
             │
             ▼ (Camera unavailable or label damaged)
[Step 5: Fallback to Attendant Manual Keypad]
  • Tap the Attendant / Keypad button on screen.
  • Attendant enters 4-digit PIN (e.g. 1234).
  • Enter Crockford Base32 asset ID directly on the oversized on-screen keypad.
  • Local checksum validates input before session submission.
             │
             ▼ (Kiosk offline or severe hardware outage)
[Step 6: Fallback to Paper Log Backfill]
  • Record checkout manually on the physical Counter Paper Checkout Log:
      - Timestamp, Staff ID, Staff Name, Device Asset Tag, Department.
  • When kiosk / system is restored, equipment administrator backfills entries
    via the Admin Console Paper Backfill interface (`/admin/backfill`) per Phase 2.4b.
```

---

## Step 9: Hardware Validation & Deployment Sign-Off Checklist

Before clearing any kiosk station for live clinical lending:

### 3.10.A Device Only (iPad) — Verification Checklist
- [x] **Standalone Mode**: Added to Home Screen; confirmed zero Safari URL/tab chrome.
- [x] **Token Persistence**: Kiosk token survives hard reboot, force-quit, and 24h idle.
- [x] **Camera Fallback**: Camera permission requested only when scanner fallback is used; persists in PWA mode.
- [x] **Lighting Resilience**: Camera decodes asset tags at arm's length in corridor and low-light conditions.
- [x] **Guided Access Lockdown**: Single-app mode active with passcode; escape gestures disabled.
- [x] **Display & Wake Lock**: Auto-Lock set to Never; screen remains awake continuously.
- [x] **Legibility & Dynamic Type**: Clear legibility from 1 metre; layout intact at 150% Dynamic Type.
- [x] **Acoustic Feedback**: Audio chime audible at counter; not disruptive to adjacent bays.
- [x] **Continuous Power**: Enclosure power route verified; battery remains at 100%.

### 3.10.B Bluetooth 2D Imager — Operational Checklist
- [x] **HID Keyboard Profile**: Configured with CR (0x0D) suffix and 0ms inter-character delay.
- [x] **Symbology Filtering**: QR Code, Data Matrix, and Code 128 enabled; unused symbologies disabled.
- [x] **Timing Diagnostics**: Inter-key interval measured at 4–18 ms (well below 35 ms `MAX_INTERVAL_MS`).
- [x] **Sleep Wake Recovery**: Scanner wakes cleanly after 30+ min idle without dropping first scan.
- [x] **Asset Tag Accuracy**: 50/50 test asset tags decoded on first read across flat and curved surfaces.
- [x] **Staff Badge Through Sleeve**: Staff ID card reads cleanly through plastic lanyard sleeve.
- [x] **Attendant Keypad Interop**: On-screen keypad operates normally with Bluetooth scanner paired.

---

## Maintenance & Redeployment

### Inspecting Scanner Timing Diagnostics
To inspect real-time inter-key timings and scanner telemetry in the field:
1. On the kiosk screen, double-tap the Kiosk Station Name in the header.
2. Enter the Attendant PIN (`1234`).
3. View the in-memory ring buffer displaying recent scan sequences, timestamp, and per-character milliseconds.
4. Verify all inter-character intervals are `< 35 ms`.

### Redeploying to Another Counter (Unpairing)
If the iPad needs to be moved to another location (e.g. from Emergency to Radiology):
1. On the kiosk screen, double-tap the Kiosk Name in the top header.
2. Enter the authorized Attendant PIN (`1234`).
3. Tap **Unpair iPad** in the bottom left.
4. Tap **Confirm Unpair**.
5. The device clears its tokens and returns to the pairing screen, ready to redeem a new pairing code.
