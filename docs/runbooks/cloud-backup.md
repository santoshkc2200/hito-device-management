# Cloud backup (Google Drive and OneDrive)

HDMS can keep an extra copy of every backup in Google Drive or OneDrive. The copy is
encrypted with the backup key before it leaves the server, so the cloud provider cannot
read it. The administrator connects an account in **Backups → Destinations**; IT registers
the OAuth client once.

## Before anything else

Backups leave the hospital network. Get data-protection approval (a name and a date) before
connecting an account. Record it with the hospital's other sign-offs.

## 1. Register an OAuth client (IT, once)

Keep the values in the hospital password manager **and** with the printed recovery sheet.
A new server cannot download the backups without them.

### Google Drive

1. Google Cloud Console → create a project (for example "HDMS backup").
2. APIs & Services → Library → enable **Google Drive API**.
3. APIs & Services → OAuth consent screen. Choose **Internal** if the hospital uses Google
   Workspace, otherwise **External**. Add the scope `.../auth/drive.file`.
   **If External, set the publishing status to "In production".** In "Testing" Google expires
   the refresh token after 7 days and backups stop a week later.
4. Credentials → Create credentials → OAuth client ID → application type
   **TVs and Limited Input devices**.
5. Copy the **client ID** and **client secret**.

### OneDrive

1. Microsoft Entra admin center → App registrations → New registration. Name it "HDMS
   backup". Supported account types: *Accounts in this organizational directory only*.
2. Copy the **Application (client) ID** and the **Directory (tenant) ID**.
3. Authentication → Advanced settings → **Allow public client flows: Yes**.
4. API permissions → Add → Microsoft Graph → Delegated → `Files.ReadWrite` and
   `offline_access`. Grant admin consent if your tenant requires it.

## 2. Connect the account (administrator)

1. Backups → Destinations → **Connect an account**.
2. Choose Google Drive or OneDrive, give it a name, paste the client ID (and secret for
   Google, the tenant ID for OneDrive).
3. Open the address shown on any device and enter the code. Sign in with the account whose
   Drive should hold the backups.
4. Add destination → Google Drive or OneDrive → pick the account → choose a folder name
   (default `hdms-backups`). HDMS creates it; it holds `repo/` and `hdms-recovery.bin`.
5. Wait for "First copy saved". Check the folder in the cloud drive.

HDMS can only see folders it created itself (Google's `drive.file` scope). Do not move or
rename the folder in the cloud drive.

## 3. When the status says "Sign-in expired" or "Access removed"

Backups to that account have stopped (the console and the dashboard say so). Press
**Reconnect** on the account and finish the sign-in again. Its destinations are kept.
Causes: the account's password changed, someone removed HDMS under the account's security
settings, or the app registration was deleted or its secret rotated.

## 4. A lost server: restore from the cloud

On the new server, with the repository checked out and the TLS certificate in place
(production-deployment.md steps 1–4):

```bash
sudo deploy/production/install.sh --restore --cloud=google     # or --cloud=onedrive
```

The script asks for the OAuth client ID (and secret / tenant), prints an address and a code to
open on any device, downloads the backup folder into `/mnt/cloud-restore`, then asks for the
**recovery key** from the printed sheet and carries on exactly as in disaster-recovery.md.
Finish in the browser at `https://<host>/recovery`.

Keep with the recovery sheet: the provider, the OAuth client ID (and Google secret), the
OneDrive tenant ID, and the cloud folder name.

## What HDMS stores

Client secret and tokens are encrypted with `HDMS_CREDENTIAL_ENC_KEY`. During a backup the
worker writes a private rclone configuration into a temporary directory on a tmpfs and
deletes it afterwards. Disconnecting an account deletes the stored sign-in but never deletes
backups in the cloud.

The manual command `hdms-cli backup` does not copy to cloud destinations; the worker does.
