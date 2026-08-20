# Runbook: Kiosk iPad Provisioning & Physical Setup

This runbook guides administrators and technicians through provisioning, pairing, locking down, and mounting an iPad running the **HDMS Kiosk PWA** at a hospital equipment counter.

---

## Prerequisites & Bill of Materials

1. **Hardware**:
   - iPad (9th generation or later running iPadOS 16+)
   - Bluetooth 2D imager barcode scanner (HID keyboard emulation mode)
   - Heavy-duty secure counter mount / enclosure with lock
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
4. Tap the scanner name under **Other Devices** (e.g., `Barcode Scanner HID` or `Opticon / Zebra / Honeywell`).
5. Confirm the status changes to **Connected** (solid blue indicator LED on scanner).

---

## Step 2: Configure Scanner Symbologies & Suffix

Scan the configuration programming barcodes from the scanner's quick-start reference:

1. **Suffix Configuration (CR / Enter)**:
   - Scan **`Enter Suffix = CR / Enter (0x0D)`** programming barcode.
   - *Rationale*: HDMS `HidWedgeSource` relies on a terminating Enter key to delimit scan sequences.
2. **Symbology Filtering**:
   - Enable **QR Code** (Default)
   - Enable **Data Matrix** (Small-format asset tags)
   - Enable **Code 128** (Staff ID legacy barcodes)
   - Disable unused symbologies (EAN/UPC, Code 39, PDF417) to optimize decode speed and eliminate false reads.
3. **Show/Hide iOS Virtual Keyboard Toggle**:
   - Note the scanner's double-click trigger gesture or dedicated barcode for **"Toggle iOS Keyboard"** in case emergency manual text entry is needed outside the PWA.

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
   - Navigate to **Kiosks → Select Kiosk Station → Actions → Generate Pairing Code**.
   - Note the 6-digit numeric code (valid for 10 minutes).
3. On the iPad Kiosk screen:
   - Enter the 6-digit pairing code using the on-screen keypad.
   - Tap **Pair Kiosk**.
4. Upon successful exchange:
   - The station token, ID, and name are stored securely in local appliance storage.
   - The screen immediately transitions to the **Idle Screen** displaying the kiosk name and **"Scanner Ready"** status.
   - *Note*: The long-lived token is never displayed or accessible to borrowers.

---

## Step 5: Grant Camera Permission for Fallback Scanner

1. On the Idle screen, tap the top-right **Camera** button.
2. When iPadOS displays the system prompt:
   > *"“HDMS Kiosk” Would Like to Access the Camera"*
3. Tap **Allow**.
4. Aim the camera viewfinder at a test asset tag to confirm decoding.
5. Tap **Close** or the X button to return to standard idle mode.
6. *Note*: In standalone PWA mode, this permission persists permanently.

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

## Maintenance & Redeployment

### Redeploying to Another Counter (Unpairing)
If the iPad needs to be moved to another location (e.g. from Emergency to Radiology):
1. On the kiosk screen, double-tap the Kiosk Name in the top header.
2. Enter the authorized Attendant PIN (default `1234`).
3. Tap **Unpair iPad** in the bottom left.
4. Tap **Confirm Unpair**.
5. The device clears its tokens and returns to the pairing screen, ready to redeem a new pairing code.
