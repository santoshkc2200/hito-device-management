# Runbook: Production Deployment on a LAN Server

This runbook guides hospital IT engineers through installing and operating HDMS on a dedicated on-premise Linux server on the hospital LAN.

---

## 1. What you are installing

HDMS runs on a single Linux server on the hospital LAN. It operates four Docker containers orchestrated via Docker Compose:
- **`db`** (`postgres:18-alpine`): PostgreSQL database storing operational and audit records. Database ports are not published to the host or LAN.
- **`worker`**: Background job daemon running `hdms-cli worker`. It runs database migrations as the database owner at startup, provisions the restricted runtime role, and executes all scheduled jobs (nightly backups, overdue scans, reservation expiries, etc.).
- **`api`** (`hdms-api`): REST API backend connecting to the database as the restricted `hdms_app` role.
- **`caddy`**: Reverse proxy handling TLS termination and serving frontend static web applications (kiosk, admin console, staff portal).

Hospital staff and administrators access the system in their web browser at:
```
https://hdms.hospital.local
```

---

## 2. Server requirements and preparation

### Hardware & OS recommendations
- **Operating System:** Ubuntu 22.04 LTS or 24.04 LTS (or equivalent enterprise Linux)
- **CPU:** 2+ vCPU / physical cores
- **RAM:** 4 GB minimum (8 GB recommended)
- **Disk:** 50 GB SSD storage minimum, plus additional space for database backups
- **Network:** Static LAN IP address assigned by hospital network administration

### Install Docker Engine
Install Docker CE and the Docker Compose plugin using official packages:
```bash
sudo apt-get update
sudo apt-get install -y ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc

echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

sudo systemctl enable --now docker
```

### Firewall configuration
Ensure the host firewall (`ufw` or `iptables`) restricts inbound connections to ports 443 (HTTPS) and 80 (HTTP redirect) from hospital LAN subnets only:
```bash
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow 22/tcp comment 'SSH administration'
sudo ufw allow from 10.0.0.0/8 to any port 443 proto tcp comment 'HDMS HTTPS'
sudo ufw allow from 10.0.0.0/8 to any port 80 proto tcp comment 'HDMS HTTP redirect'
sudo ufw enable
```

---

## 3. Name (DNS record)

Add an internal DNS `A` record on the hospital's local DNS servers (e.g. Active Directory DNS / Infoblox):
```
hdms.hospital.local.   IN  A   <STATIC_SERVER_IP>
```

> **Note:** iPads and mobile devices cannot use local `/etc/hosts` files. The DNS hostname `hdms.hospital.local` must resolve properly across all hospital Wi-Fi networks and VLANs used by kiosks and staff devices.

---

## 4. Certificate (TLS)

HDMS requires HTTPS with a valid certificate issued by the hospital's internal Certificate Authority (CA).

1. Generate a Certificate Signing Request (CSR) for `hdms.hospital.local` or request a certificate from hospital PKI.
2. Place the issued certificate and private key at the following locations on the server:
   ```bash
   sudo mkdir -p /etc/ssl/certs /etc/ssl/private
   sudo cp hdms.hospital.crt /etc/ssl/certs/hdms.hospital.crt
   sudo cp hdms.hospital.key /etc/ssl/private/hdms.hospital.key
   sudo chmod 644 /etc/ssl/certs/hdms.hospital.crt
   sudo chmod 600 /etc/ssl/private/hdms.hospital.key
   ```
3. **CA Root Installation:** Install the hospital root CA certificate on every kiosk iPad and client PC:
   - On iPad: Install CA profile, then go to **Settings → General → About → Certificate Trust Settings** and enable full trust for the root certificate.
   - **Crucial:** Without a trusted HTTPS connection, Safari and iPad WebKit block WebRTC/getUserMedia camera access, which breaks kiosk QR code scanning.

---

## 5. Code installation

Clone the HDMS repository to `/opt/hdms`:
```bash
sudo git clone <repo-url> /opt/hdms
cd /opt/hdms
```

---

## 6. Secrets configuration

1. Create the secure configuration directory and copy the template:
   ```bash
   sudo mkdir -p /etc/hdms
   sudo cp /opt/hdms/deploy/production/production.env.example /etc/hdms/hdms.env
   sudo chmod 600 /etc/hdms/hdms.env
   ```

2. Generate strong secrets using `openssl`:
   ```bash
   # 1. Database master password (32 alphanumeric chars):
   openssl rand -hex 16

   # 2. Database app password (32 alphanumeric chars):
   openssl rand -hex 16

   # 3. Token pepper (64 hex chars / 32 bytes):
   openssl rand -hex 32

   # 4. Credential encryption key (32 bytes base64):
   openssl rand -base64 32

   # 5. TOTP encryption key (32 bytes base64):
   openssl rand -base64 32

   # 6. Backup encryption key (32 bytes base64):
   openssl rand -base64 32
   ```

3. Edit `/etc/hdms/hdms.env` with `sudo nano /etc/hdms/hdms.env` and fill in the values:
   - Set `POSTGRES_USER=hdms_prod`
   - Set `POSTGRES_PASSWORD=<master password from above>`
   - Set `HDMS_APP_DB_PASSWORD=<app password from above>`
   - Set `HDMS_DATABASE_URL=postgres://hdms_app:<app password from above>@db:5432/hdms_prod?sslmode=disable`
   - Set `HDMS_OWNER_DATABASE_URL=postgres://hdms_prod:<master password from above>@db:5432/hdms_prod?sslmode=disable`
   - Set `TZ=Asia/Tokyo` (or your local hospital timezone)
   - Set `HDMS_TOKEN_PEPPER=<token pepper>`
   - Set `HDMS_CREDENTIAL_ENC_KEY=<credential key>`
   - Set `HDMS_TOTP_ENC_KEY=<totp key>`
   - Set `HDMS_BACKUP_ENC_KEY=<backup key>`
   - Set the `HDMS_SMTP_*` values to the hospital mail relay (host, port 25/465/587, sender address). They are required: production refuses to start with the development defaults, and overdue reminders and the weekly digest are sent through this relay.

> **CRITICAL SECURITY WARNING — PASSWORD MANAGER BACKUP:**
> Store all generated keys in the hospital password manager immediately:
> - **`HDMS_TOKEN_PEPPER`**: If lost, all issued staff QR badges, session tokens, and kiosk credentials become permanently invalid and must be re-issued manually.
> - **`HDMS_BACKUP_ENC_KEY`**: If lost, **every backup snapshot is mathematically impossible to decrypt or restore**.

---

## 7. Start the services

Start the Docker Compose stack:
```bash
cd /opt/hdms
sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env up -d --build
```

Verify that all four containers are running and report `healthy`:
```bash
sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env ps
```

Run the post-deployment smoke test:
```bash
sh deploy/production/smoke.sh
```

---

## 8. Create the first administrator

Bootstrap the initial system administrator account:
```bash
sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env exec api \
  hdms-cli admin bootstrap --email admin@hospital.org --name "System Administrator" --role admin
```
The command outputs:
- The administrator ID
- An `otpauth://` URL and a raw base32 TOTP secret

Scan the QR code into your authenticator app (e.g. Google Authenticator, 1Password) immediately. Navigate to `https://hdms.hospital.local/admin/`, sign in with the temporary password prompted during bootstrap, and verify TOTP login.

---

## 9. Kiosk iPad setup

To configure counter iPads in Guided Access mode for equipment pickup and return:
Refer to the dedicated runbook: [docs/runbooks/kiosk-ipad-setup.md](kiosk-ipad-setup.md).

---

## 10. Scheduled background jobs

All scheduled jobs run automatically inside the `worker` container. The schedule follows local wall-clock time (`TZ`):

| Job | Cadence | Priority | Description |
|---|---|---|---|
| `backup` | Daily 02:00 | 1 (Highest) | Uncompressed dump piped into restic repository and remote destinations |
| `reservation-expiry` | Every 5 minutes | 2 | Expires uncollected reservations past grace period |
| `overdue-scan` | Hourly | 3 | Scans open overdue loans and enqueues reminder emails |
| `reconcile` | Daily 03:10 | 4 | Verifies device inventory statuses match active loan records |
| `retention` | Daily 03:40 | 5 | Reports or prunes expired scan events and anonymises old records |
| `directory-sync` | Daily 03:00 (`--apply`) | 6 | Synchronizes staff roster if LDAP is configured |
| `weekly-digest` | Monday 08:00 | 7 | Sends summary of unreturned devices to administrators |

### Monitoring jobs
To check job executions and outcomes, query `job_runs`:
```bash
sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env exec db \
  psql -U hdms_prod -d hdms_prod -c "SELECT job, outcome, started_at, finished_at FROM job_runs ORDER BY started_at DESC LIMIT 20;"
```

To view live worker logs:
```bash
sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env logs -f worker
```

---

## 11. Optional network-share backup copy (NAS)

To replicate backups to a hospital network share (NFS or SMB/CIFS):

1. Mount the network share on the host server:
   ```bash
   sudo mkdir -p /mnt/hospital-nas/hdms
   # Example NFS mount in /etc/fstab:
   # nas.hospital.local:/volume1/hdms-backups /mnt/hospital-nas/hdms nfs defaults 0 0
   sudo mount /mnt/hospital-nas/hdms
   ```
2. In `/etc/hdms/hdms.env`, uncomment and set:
   ```bash
   HDMS_BACKUP_NAS_HOST_PATH=/mnt/hospital-nas/hdms
   ```
3. Restart the worker service:
   ```bash
   sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env up -d worker
   ```
4. For backup destination management, see [docs/runbooks/nightly-backup.md](nightly-backup.md).

---

## 12. Updating HDMS

To upgrade to a new version:

1. Always take a manual backup before performing updates:
   ```bash
   sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env exec worker hdms-cli backup
   ```
2. Pull latest code from the repository:
   ```bash
   cd /opt/hdms
   sudo git pull
   ```
3. Rebuild and restart containers:
   ```bash
   sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env up -d --build
   ```
4. Confirm service health:
   ```bash
   sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env ps
   sh deploy/production/smoke.sh
   ```
