# Install Script and Disaster-Recovery Runbook Implementation Plan (plan 4)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let hospital IT set up HDMS on a new server with one command, including rebuilding a lost server from its backups with the printed recovery key, and replace the SSH/psql restore runbook with a two-scenario disaster-recovery runbook.

**Architecture:** `deploy/production/install.sh` (bash) automates steps 6–7 of `production-deployment.md`: it checks prerequisites, generates or recovers the secrets, prompts for host name, time zone and mail relay, renders `/etc/hdms/hdms.env` from `production.env.example`, runs `docker compose up -d --build` and waits until HDMS answers. With `--restore` it builds the worker image, finds the backup folder the same way the recovery page does, makes sure the worker's user can use it, and passes the recovery key on stdin to `hdms-cli recovery unwrap` inside an offline container to get the four secrets. `install_test.sh` tests it with a stub `docker` on `PATH` plus the real `docker compose config`; a Docker-in-Docker drill proves the whole "server lost" path once.

**Tech Stack:** bash (3.2-compatible: macOS runs the tests; servers run bash 5), Docker Engine + Compose plugin, shellcheck, GitHub Actions, Markdown runbooks.

**Spec:** `docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md` — sections "New-server install (`install.sh`)", "Security and trust boundaries" (the `install.sh` bullet), "Testing → Shell", "Testing → E2E" (plan 4 of 4). Plan 2 (`docs/superpowers/plans/2026-09-30-backup-recovery-key.md`) shipped `hdms-cli recovery unwrap`; plans 3a/3b shipped the recovery page and its "Database server will not start" pointer to this runbook.

**Base:** branch `feat/install-and-runbook` from `main` at `a837044` or later, in a worktree (`.claude/worktrees/install-and-runbook`). The main checkout has unrelated uncommitted work; do not touch it.

**Not in this plan:** TLS certificate issuance and DNS (stay manual, spec "Out of scope"); the console restore and **Discard** of the kept database (the 09-30 spec's restore plan); cloud destinations as a restore source; the timed drill by someone who did not build it, the staging E2E and the native-speaker review (spec "Needs a human, not code").

## Decisions made while planning (not in the spec)

1. **The unwrap flag is `--from <folder>`**, as plan 2 shipped it (`hdms-backend/cmd/hdms-cli/recovery.go`), not the spec's `--repo`. The folder is the one holding `repo/` and `hdms-recovery.bin` side by side. Errors come back on stderr prefixed `hdms-cli: `; the script strips the prefix and shows the plain sentence.
2. **The script builds the worker image itself** with `docker build --target worker -t hdms-install-worker hdms-backend`, and removes the tag when it exits. `docker compose build` cannot run yet: `compose.yaml` requires `POSTGRES_PASSWORD` and `TZ` and names `/etc/hdms/hdms.env` as an `env_file`, none of which exist before the script has written them.
3. **The backup folder is searched, not typed exactly.** IT types the mount point or copied folder; the script lists every folder from there down two levels that holds `repo/config` beside `hdms-recovery.bin`, skipping dot-folders (NAS `.snapshot` copies) — the same rule as `backup.FindRepoFolders` with `sourceSearchDepth = 2`. `HDMS_BACKUP_NAS_HOST_PATH` is set to the folder IT typed, so the worker finds the same backups under `/mnt/nas`.
4. **The worker's user must be able to use the folder.** The worker image runs as `hdms` (uid 100, gid 101 today; the script reads them from the image). restic needs read access to the repository and write access to `repo/locks`. A copy made with plain `cp -r` is root-owned, so the script checks as the worker's user before asking for the key, offers `chown -R`, and stops with "give user ID 100 read and write access" when IT declines or a network drive refuses.
5. **An existing `hdms-production_hdms-prod-db-data` volume stops the script, even with `--force`.** Postgres keeps the password it was initialised with, so new passwords in a new env file would lock the stack out of its own database. `--force` only overrides an existing env file, which is kept beside the new one as `hdms.env.replaced-<timestamp>`.
6. **The script also asks for the host name** (`HDMS_SITE_ADDR`, default `hdms.hospital.local`): the final "Open https://<host>/recovery" line and the Entra redirect URL need it. DNS and the certificate stay manual, but the script refuses to start without the certificate files — otherwise Docker would create empty directories at those paths and Caddy would fail.
7. **Values are written single-quoted** (`KEY='value'`). Compose reads single-quoted values literally, so `$`, `#` and spaces in an SMTP password survive; a value containing `'` is refused at the prompt. Values reach the env-file writer through its environment, never its arguments.
8. **The script waits for HDMS** (up to 3 minutes): `/v1/healthz` after a fresh install, `/recovery/api/status` after `--restore`, through Caddy with `curl --resolve <host>:443:127.0.0.1`. On timeout it prints the last 30 log lines of `api`, `worker` and `caddy` instead of a success message. This surfaces the production config checks (for example a `.local` sender address) without repeating them in the script.
9. **Five wrong keys stop the script** (`key_attempts=5`); an empty line asks again without counting.
10. **Tests use a stub `docker`, not Docker-in-Docker, in CI.** The stub records every call and plays the worker image; the real `docker compose config` then parses a written env file. Docker-in-Docker is used once, in Task 3, for the full drill.
11. **`docs/runbooks/restore.md` is deleted.** It described a non-Docker install (`/opt/hdms/bin/hdms-cli`, `sudo -u hdms`); its command-line restore and drill sheet move to the new runbook's appendices, rewritten for the Docker install (`docker compose exec worker hdms-cli …`). Older plans and specs that mention it are history and stay as they are.

## Global Constraints

- `install.sh` and `install_test.sh` start with `#!/usr/bin/env bash` and `set -euo pipefail`, and run on bash 3.2 (macOS `/bin/bash`): no associative arrays, `mapfile`, `${var,,}` or `read -i`.
- `shellcheck deploy/production/*.sh` reports nothing (info level included).
- No secret ever appears in a process's arguments: the recovery key goes to `docker run -i` on stdin from the builtin `printf`; env-file values go to `awk` through exported variables in a subshell.
- `/etc/hdms/hdms.env` is written to a temporary file in the same directory (mode `0600`) and renamed into place; an existing file is moved aside only after the new one is complete.
- Fixed names: worker image tag `hdms-install-worker`; database volume `hdms-production_hdms-prod-db-data` (from `name: hdms-production` in `compose.yaml`); overrides reuse the existing compose knobs `HDMS_PROD_ENV_FILE`, `HDMS_TLS_CERT_HOST_PATH`, `HDMS_TLS_KEY_HOST_PATH`.
- The unwrap container runs `--rm -i --network none` with the backup folder mounted read-only at `/restore-src`.
- Errors: `install.sh: <sentence>` on stderr, exit 1. Unknown option: usage on stderr, exit 2.
- Runbook text quotes UI strings exactly as `hdms-frontend/apps/recovery/src/i18n/en.ts` and `hdms-frontend/apps/admin/src/i18n/en.ts` have them, and names only commands and flags that exist.
- Run `install_test.sh` and other suites one after another, never in parallel.

## Review Focus

1. **The backups sit on a network drive that squashes root.** `chown -R` fails, and without a re-check the restore would start and fail later inside restic. Expected: a clear stop asking the drive's administrator for user ID 100 access. Pinned in Task 1 (`restore: a drive that refuses chown stops`).
2. **IT reruns the installer on a server that once ran HDMS.** New passwords against the old Postgres volume lock the stack out. Expected: refused, naming the volume, with or without `--force`. Pinned in Task 1 (`an existing database volume is refused`).
3. **An SMTP password with `$`, `#`, spaces or `"`.** Unquoted, compose would interpolate `$` and cut at ` #`. Expected: the container sees it literally. Pinned in Task 1 (`compose reads the SMTP password literally`).
4. **A NAS with `.snapshot` folders holding older copies of the backups.** Offering them would restore stale data or ask a confusing question. Expected: never offered. Pinned in Task 1 (the `.snapshot/hdms-backups` decoy in the main restore case, which would otherwise turn the single-folder flow into a choice).
5. **A backup folder copied with plain `cp -r`.** It is root-owned and mode `0600`, so the worker cannot read it. Expected: the script notices before asking for the key and offers `chown`. Pinned in Task 1 (`restore with a root-owned copy exits 0 after chown`) and exercised for real in Task 3 step 6.

## File Structure

| File | Responsibility |
|---|---|
| `deploy/production/install.sh` (create) | Fresh install and `--restore` |
| `deploy/production/install_test.sh` (create) | Stub-docker tests plus a real `docker compose config` parse |
| `.github/workflows/ci.yml` (modify) | `deploy-scripts` job: shellcheck and `install_test.sh` |
| `docs/runbooks/disaster-recovery.md` (create) | Scenarios A and B, database server reset, after a restore, CLI appendix, drill sheet |
| `docs/runbooks/restore.md` (delete) | Replaced by the above |
| `docs/runbooks/production-deployment.md` (modify) | Steps 6–8 use `install.sh` and the recovery key; worker uid note; link to disaster recovery |
| `docs/runbooks/nightly-backup.md` (modify) | Worker uid note in "Connecting a network drive" |
| `docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md` (modify) | Record decisions 1–6 |

---

### Task 1: `install.sh` with tests and CI

**Files:**
- Create: `deploy/production/install.sh`, `deploy/production/install_test.sh`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: `hdms-cli recovery unwrap --from <folder>` (key on stdin, four `KEY=value` lines on stdout, `hdms-cli: <reason>` on stderr, exit 1 on failure); the worker image's `sh`, `id` and `test`; `deploy/production/production.env.example` as the env-file template; `deploy/production/compose.yaml`.
- Produces: `sudo deploy/production/install.sh [--restore] [--force]`, whose prompts and messages Task 2's runbook quotes: "Folder holding the HDMS backups", "The HDMS worker runs as user ID <uid> and cannot read and write <folder>.", "Recovery key: ", "Recovery key accepted.", "Open https://<host>/recovery and enter the same recovery key.", "stopped after 5 attempts", "TLS certificate missing", "run install.sh as root".

- [ ] **Step 1: Create the worktree**

```bash
git worktree add .claude/worktrees/install-and-runbook -b feat/install-and-runbook main
cd .claude/worktrees/install-and-runbook
```

- [ ] **Step 2: Write the failing test**

Create `deploy/production/install_test.sh`:

```bash
#!/usr/bin/env bash
# Tests for install.sh. A stub docker on PATH records every call and plays
# the worker image. The "compose reads" checks run the real `docker compose
# config` over a written env file, so Docker with the compose plugin must be
# installed.
#
#   bash deploy/production/install_test.sh
#
# Single-quoted $ in the checks below is meant literally.
# shellcheck disable=SC2016
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
real_docker=$(command -v docker) || {
	echo "install_test: docker is required" >&2
	exit 1
}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
failures=0
good_key=GOOD-0000-1111-2222-3333-4444-5555
wrong_key=WRNG-0000-1111-2222-3333-4444-5555

# --- stubs ------------------------------------------------------------------
stub_bin="$tmp/bin"
mkdir -p "$stub_bin"
cat >"$stub_bin/docker" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$STUB_STATE/docker.log"
case "$*" in
"compose version") echo "Docker Compose version stub" ;;
"volume inspect "*) [ -f "$STUB_STATE/db-volume" ] ;;
"build "* | "image rm "* | *" up -d --build" | *" logs "*) ;;
*"recovery unwrap"*)
	IFS= read -r key || true
	printf '%s\n' "$key" >>"$STUB_STATE/unwrap.stdin"
	if [ "$key" != "$STUB_GOOD_KEY" ]; then
		echo "hdms-cli: this recovery key does not open the backups in that folder; it may be from an older sheet" >&2
		exit 1
	fi
	printf '%s\n' 'HDMS_BACKUP_ENC_KEY=b4ckup+Key/=' 'HDMS_TOKEN_PEPPER=0123abcd' \
		'HDMS_CREDENTIAL_ENC_KEY=cred+Key/=' 'HDMS_TOTP_ENC_KEY=totp+Key/='
	;;
*'id -u'*) echo "100:101" ;;
*'test -r'*) [ -f "$STUB_STATE/access-ok" ] ;;
*)
	echo "stub docker: unexpected call: $*" >&2
	exit 99
	;;
esac
EOF
cat >"$stub_bin/id" <<'EOF'
#!/usr/bin/env bash
if [ "${1:-}" = -u ]; then echo "${STUB_UID:-0}"; else exec /usr/bin/id "$@"; fi
EOF
cat >"$stub_bin/chown" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$STUB_STATE/chown.log"
if [ -f "$STUB_STATE/chown-fails" ]; then
	echo "chown: Operation not permitted" >&2
	exit 1
fi
touch "$STUB_STATE/access-ok"
EOF
printf '#!/usr/bin/env bash\nexit 0\n' >"$stub_bin/curl"
chmod +x "$stub_bin"/*

# --- helpers ----------------------------------------------------------------
# fresh_state makes an empty state, env and certificate directory per case.
fresh_state() {
	case_dir=$(mktemp -d "$tmp/case.XXXXXX")
	state="$case_dir/state"
	env_file="$case_dir/etc/hdms.env"
	mkdir -p "$state" "$case_dir/etc" "$case_dir/certs"
	touch "$state/docker.log" "$case_dir/certs/crt" "$case_dir/certs/key"
}

# run_install INPUT [ARGS...] runs install.sh with INPUT on stdin and sets
# $status and $output.
run_install() {
	local input=$1
	shift
	set +e
	output=$(PATH="$stub_bin:$PATH" STUB_STATE="$state" STUB_GOOD_KEY="$good_key" STUB_UID="${uid:-0}" \
		HDMS_PROD_ENV_FILE="$env_file" HDMS_TLS_CERT_HOST_PATH="$case_dir/certs/crt" \
		HDMS_TLS_KEY_HOST_PATH="$case_dir/certs/key" \
		bash "$here/install.sh" "$@" <<<"$input" 2>&1)
	status=$?
	set -e
}

check() {
	if "${@:2}"; then
		echo "ok   $1"
	else
		echo "FAIL $1"
		failures=$((failures + 1))
	fi
}
# exits_with N passes when install.sh exited N, and prints its output when not.
exits_with() {
	[ "$status" = "$1" ] && return 0
	printf '%s\n' "--- install.sh exited $status; output:" "$output" "---"
	return 1
}
has_line() { grep -qxF -- "$2" "$1"; }
matches() { grep -qE -- "$2" "$1"; }
says() { grep -qF -- "$1" <<<"$output"; }
lacks() { ! grep -qF -- "$2" "$1"; }
# shellcheck disable=SC2012 # ls -l is the portable way to read a mode.
is_0600() { [ "$(ls -l "$1" | cut -c1-10)" = "-rw-------" ]; }
value_of() { sed -n "s/^$1='\(.*\)'\$/\1/p" "$env_file" 2>/dev/null || true; }

# Answers for the settings prompts, in order: host name, time zone, SMTP host,
# port, user, password, sender, reply-to (the empty last line; $(...) drops
# trailing newlines, so it is added back by hand).
settings="$(printf '%s\n' hdms.example.org Europe/London smtp.example.org 587 hdms 'pa$s w #rd"x' hdms@example.org)"$'\n'

# --- fresh install ----------------------------------------------------------
fresh_state
run_install "$settings"
check "fresh install exits 0" exits_with 0
check "fresh: env file is mode 0600" is_0600 "$env_file"
check "fresh: host name" has_line "$env_file" "HDMS_SITE_ADDR='hdms.example.org'"
check "fresh: time zone" has_line "$env_file" "TZ='Europe/London'"
check "fresh: SMTP password kept literally" has_line "$env_file" "HDMS_SMTP_PASSWORD='pa\$s w #rd\"x'"
check "fresh: empty reply-to" has_line "$env_file" "HDMS_SMTP_REPLY_ADDRESS=''"
check "fresh: Entra redirect follows the host" has_line "$env_file" "HDMS_ENTRA_REDIRECT_URL='https://hdms.example.org/v1/staff/auth/microsoft/callback'"
check "fresh: pepper is 32 random bytes in hex" matches "$env_file" "^HDMS_TOKEN_PEPPER='[0-9a-f]{64}'\$"
for name in HDMS_BACKUP_ENC_KEY HDMS_CREDENTIAL_ENC_KEY HDMS_TOTP_ENC_KEY; do
	check "fresh: $name is 32 random bytes in base64" matches "$env_file" "^$name='[A-Za-z0-9+/]{43}='\$"
done
check "fresh: the three base64 keys differ" [ "$(value_of HDMS_BACKUP_ENC_KEY)" != "$(value_of HDMS_CREDENTIAL_ENC_KEY)" ]
pg=$(value_of POSTGRES_PASSWORD)
app=$(value_of HDMS_APP_DB_PASSWORD)
check "fresh: database passwords are 32 hex characters and differ" \
	bash -c '[[ $0 =~ ^[0-9a-f]{32}$ && $1 =~ ^[0-9a-f]{32}$ && $0 != "$1" ]]' "$pg" "$app"
check "fresh: owner URL carries the postgres password" has_line "$env_file" "HDMS_OWNER_DATABASE_URL='postgres://hdms_prod:$pg@db:5432/hdms_prod?sslmode=disable'"
check "fresh: app URL carries the app password" has_line "$env_file" "HDMS_DATABASE_URL='postgres://hdms_app:$app@db:5432/hdms_prod?sslmode=disable'"
check "fresh: no template placeholder left" lacks "$env_file" "change-me"
check "fresh: network drive stays unset" has_line "$env_file" "# HDMS_BACKUP_NAS_HOST_PATH=/mnt/hospital-nas/hdms"
check "fresh: starts the stack with the env file" grep -qF "compose -f $here/compose.yaml --env-file $env_file up -d --build" "$state/docker.log"
check "fresh: builds no separate worker image" lacks "$state/docker.log" "build --quiet"
check "fresh: points at the first administrator" says "hdms-cli admin bootstrap"

compose_json=$(HDMS_PROD_ENV_FILE="$env_file" "$real_docker" compose -f "$here/compose.yaml" --env-file "$env_file" config --format json) ||
	compose_json=""
check "compose reads the SMTP password literally" grep -qF '"HDMS_SMTP_PASSWORD": "pa$$s w #rd\"x"' <<<"$compose_json"
check "compose reads the time zone" grep -qF '"TZ": "Europe/London"' <<<"$compose_json"
check "compose reads the pepper" grep -qF "\"HDMS_TOKEN_PEPPER\": \"$(value_of HDMS_TOKEN_PEPPER)\"" <<<"$compose_json"

# --- refusals ----------------------------------------------------------------
before=$(cat "$env_file" 2>/dev/null || true)
run_install "$settings"
check "an existing env file is refused" exits_with 1
check "the refusal names --force" says "--force"
check "the existing env file is untouched" [ "$(cat "$env_file")" = "$before" ]

run_install "$settings" --force
check "--force replaces the env file" exits_with 0
check "--force keeps the old file beside it" bash -c '[ "$(cat "$0".replaced-*)" = "$1" ]' "$env_file" "$before"
check "--force writes new secrets" [ "$(cat "$env_file")" != "$before" ]

fresh_state
touch "$state/db-volume"
run_install "$settings"
check "an existing database volume is refused" exits_with 1
check "the refusal names the volume" says "hdms-production_hdms-prod-db-data"
check "no env file next to an existing volume" [ ! -e "$env_file" ]

fresh_state
uid=1000 run_install "$settings"
check "non-root is refused" exits_with 1
check "the refusal says root" says "run install.sh as root"

fresh_state
rm "$case_dir/certs/crt"
run_install "$settings"
check "a missing TLS certificate is refused" exits_with 1
check "the refusal points at the certificate step" says "TLS certificate missing"

# --- restore -----------------------------------------------------------------
# make_source DIR creates an HDMS backup folder: repo/config, repo/locks and
# the recovery bundle beside the repository.
make_source() {
	mkdir -p "$1/repo/locks"
	touch "$1/repo/config" "$1/hdms-recovery.bin"
}
nas="$tmp/nas"
make_source "$nas/hdms-backups"
make_source "$nas/.snapshot/hdms-backups" # a NAS snapshot copy: never offered
mkdir -p "$tmp/empty"
nas_real=$(cd "$nas" && pwd -P)

fresh_state
touch "$state/access-ok"
run_install "$(printf '%s\n' "$tmp/nowhere" "$tmp/empty" "$nas" "$wrong_key" "$good_key")
$settings" --restore
check "restore exits 0" exits_with 0
check "restore: a missing folder is asked again" says "$tmp/nowhere is not a folder on this server"
check "restore: a folder without backups is asked again" says "No HDMS backups found"
check "restore: the wrong key's reason is shown without the hdms-cli prefix" \
	grep -qx "this recovery key does not open the backups in that folder; it may be from an older sheet" <<<"$output"
check "restore: both keys went to the worker on stdin, in order" \
	[ "$(cat "$state/unwrap.stdin")" = "$(printf '%s\n' "$wrong_key" "$good_key")" ]
check "restore: no key ever appears in a docker command line" \
	bash -c '! grep -qE "$0|$1" "$2"' "$good_key" "$wrong_key" "$state/docker.log"
check "restore: unwrap runs offline on a read-only mount of the found folder" \
	grep -qF -- "run --rm -i --network none -v $nas_real/hdms-backups:/restore-src:ro --entrypoint hdms-cli hdms-install-worker recovery unwrap --from /restore-src" "$state/docker.log"
check "restore: builds the worker image first" grep -qF "build --quiet --target worker -t hdms-install-worker" "$state/docker.log"
check "restore: env file is mode 0600" is_0600 "$env_file"
check "restore: backup key from the bundle" has_line "$env_file" "HDMS_BACKUP_ENC_KEY='b4ckup+Key/='"
check "restore: pepper from the bundle" has_line "$env_file" "HDMS_TOKEN_PEPPER='0123abcd'"
check "restore: credential key from the bundle" has_line "$env_file" "HDMS_CREDENTIAL_ENC_KEY='cred+Key/='"
check "restore: TOTP key from the bundle" has_line "$env_file" "HDMS_TOTP_ENC_KEY='totp+Key/='"
check "restore: the worker sees the given folder as /mnt/nas" has_line "$env_file" "HDMS_BACKUP_NAS_HOST_PATH='$nas_real'"
check "restore: database passwords are new" matches "$env_file" "^POSTGRES_PASSWORD='[0-9a-f]{32}'\$"
check "restore: sends the admin to /recovery" says "Open https://hdms.example.org/recovery and enter the same recovery key."
check "restore: removes the worker image afterwards" grep -qF "image rm hdms-install-worker" "$state/docker.log"

fresh_state
run_install "$(printf '%s\n' "$nas" y "$good_key")
$settings" --restore
check "restore with a root-owned copy exits 0 after chown" exits_with 0
check "restore: chown gives the worker's user the folder" has_line "$state/chown.log" "-R 100:101 $nas_real/hdms-backups"

fresh_state
run_install "$(printf '%s\n' "$nas" n)" --restore
check "restore: declining chown stops" exits_with 1
check "restore: the stop names the worker's user ID" says "give user ID 100 read and write access"
check "restore: no env file after declining" [ ! -e "$env_file" ]

fresh_state
touch "$state/chown-fails"
run_install "$(printf '%s\n' "$nas" y)" --restore
check "restore: a drive that refuses chown stops" exits_with 1
check "restore: the stop explains the drive refused" says "a network drive may refuse chown"

two="$tmp/two"
make_source "$two/a"
make_source "$two/b"
two_real=$(cd "$two" && pwd -P)
fresh_state
touch "$state/access-ok"
run_install "$(printf '%s\n' "$two" 9 2 "$good_key")
$settings" --restore
check "restore with two folders exits 0" exits_with 0
check "restore: an out-of-range choice is asked again" says "Enter a number from 1 to 2."
check "restore: the chosen folder is unwrapped" grep -qF -- "-v $two_real/b:/restore-src:ro" "$state/docker.log"

fresh_state
touch "$state/access-ok"
run_install "$(printf '%s\n' "$nas" "$wrong_key" "$wrong_key" "$wrong_key" "$wrong_key" "$wrong_key" "$good_key")" --restore
check "restore: five wrong keys stop" exits_with 1
check "restore: the stop says why" says "stopped after 5 attempts"
check "restore: the sixth key is never tried" [ "$(wc -l <"$state/unwrap.stdin" | tr -d ' ')" = 5 ]
check "restore: no env file after five wrong keys" [ ! -e "$env_file" ]

echo
if [ "$failures" -gt 0 ]; then
	echo "install_test: $failures check(s) failed"
	exit 1
fi
echo "install_test: all checks passed"
```

- [ ] **Step 3: Run it to see it fail**

Run: `bash deploy/production/install_test.sh`
Expected: exit 1, `--- install.sh exited 127; output:` (no such file) before `FAIL fresh install exits 0`, and the last line `install_test: 59 check(s) failed`.

- [ ] **Step 4: Write `install.sh`**

Create `deploy/production/install.sh` and `chmod +x` it:

```bash
#!/usr/bin/env bash
# Install HDMS on a new server, or rebuild a lost one from its backups.
#
#   sudo deploy/production/install.sh             fresh install
#   sudo deploy/production/install.sh --restore   new server, data from backups
#
# Run as root from the repository checkout (/opt/hdms). It writes
# /etc/hdms/hdms.env (mode 0600) and starts the stack. The host name and the
# TLS certificate stay manual: docs/runbooks/production-deployment.md, steps
# 3-4. Disaster recovery: docs/runbooks/disaster-recovery.md.
#
# Secrets never appear in a command line: the recovery key reaches the worker
# image on stdin, and values reach the env-file writer through its
# environment. Tests: deploy/production/install_test.sh.
set -euo pipefail

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
compose_file="$repo/deploy/production/compose.yaml"
template="$repo/deploy/production/production.env.example"
env_file=${HDMS_PROD_ENV_FILE:-/etc/hdms/hdms.env}
cert_file=${HDMS_TLS_CERT_HOST_PATH:-/etc/ssl/certs/hdms.hospital.crt}
key_file=${HDMS_TLS_KEY_HOST_PATH:-/etc/ssl/private/hdms.hospital.key}
# compose.yaml sets `name: hdms-production`; the volume names follow from it.
db_volume=hdms-production_hdms-prod-db-data
worker_image=hdms-install-worker
key_attempts=5

mode=fresh
force=no
work=""
env_tmp=""
# Answers and secrets, filled in by the steps below (ask assigns by name).
site_addr="" tz="" smtp_host="" smtp_port="" smtp_user="" smtp_password="" smtp_from="" smtp_reply=""
source_dir="" nas_host_path=""
backup_enc_key="" token_pepper="" credential_enc_key="" totp_enc_key=""

say() { printf '%s\n' "$*" >&2; }
die() {
	printf 'install.sh: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
Usage: install.sh [--restore] [--force]

  (no option)  Fresh install: generate every secret, write /etc/hdms/hdms.env
               and start HDMS.
  --restore    New server for an existing HDMS: read the secrets out of the
               backups with the recovery key, write /etc/hdms/hdms.env, start
               HDMS, then finish in the browser at https://<host>/recovery.
  --force      Replace an existing /etc/hdms/hdms.env (the old file is kept
               beside it).
EOF
}

cleanup() {
	[ -z "$work" ] || rm -rf "$work"
	[ -z "$env_tmp" ] || rm -f "$env_tmp"
	if [ "$mode" = restore ]; then
		docker image rm "$worker_image" >/dev/null 2>&1 || true
	fi
}

# ask NAME QUESTION [DEFAULT] [optional] stores the answer in NAME. It asks
# again until the answer is non-empty (unless optional) and fits in a
# single-quoted env-file value.
ask() {
	local __reply
	while :; do
		read -r -p "$2${3:+ [$3]}: " __reply || die "input ended before setup finished"
		__reply=${__reply:-${3:-}}
		if [ -z "$__reply" ] && [ "${4:-}" != optional ]; then
			say "  A value is required."
			continue
		fi
		case $__reply in
		*"'"*)
			say "  The value cannot contain a single quote (')."
			continue
			;;
		esac
		printf -v "$1" '%s' "$__reply"
		return 0
	done
}

# ask_hidden NAME QUESTION is ask without echo, for passwords; empty is allowed.
ask_hidden() {
	local __reply
	while :; do
		read -r -s -p "$2: " __reply || die "input ended before setup finished"
		printf '\n' >&2
		case $__reply in
		*"'"*)
			say "  The value cannot contain a single quote (')."
			continue
			;;
		esac
		printf -v "$1" '%s' "$__reply"
		return 0
	done
}

check_prerequisites() {
	[ "$(id -u)" = 0 ] || die "run install.sh as root (sudo $0)"
	command -v docker >/dev/null 2>&1 ||
		die "Docker is not installed; see docs/runbooks/production-deployment.md, step 2"
	docker compose version >/dev/null 2>&1 ||
		die "the Docker Compose plugin is not installed; see docs/runbooks/production-deployment.md, step 2"
	command -v openssl >/dev/null 2>&1 || die "openssl is not installed (apt-get install openssl)"
	command -v curl >/dev/null 2>&1 || die "curl is not installed (apt-get install curl)"
	case $env_file in
	/*) ;;
	*) die "HDMS_PROD_ENV_FILE must be an absolute path" ;;
	esac
	if [ ! -f "$cert_file" ] || [ ! -f "$key_file" ]; then
		die "TLS certificate missing: place it at $cert_file and its key at $key_file first (docs/runbooks/production-deployment.md, step 4)"
	fi
	if [ -e "$env_file" ] && [ "$force" != yes ]; then
		die "$env_file already exists, so HDMS is already set up here. Run again with --force to replace it (the old file is kept)."
	fi
	if docker volume inspect "$db_volume" >/dev/null 2>&1; then
		die "an HDMS database volume ($db_volume) already exists on this server, and install.sh only sets up a new server. Only if it holds nothing you need (for example an earlier install.sh run stopped half way), remove it: docker ps -aq --filter label=com.docker.compose.project=hdms-production | xargs -r docker rm -f; docker volume rm $db_volume"
	fi
}

generate_secrets() {
	backup_enc_key=$(openssl rand -base64 32)
	token_pepper=$(openssl rand -hex 32)
	credential_enc_key=$(openssl rand -base64 32)
	totp_enc_key=$(openssl rand -base64 32)
}

build_worker_image() {
	echo "Building the HDMS worker image (several minutes the first time)..."
	docker build --quiet --target worker -t "$worker_image" "$repo/hdms-backend" >/dev/null </dev/null ||
		die "building the worker image failed; see the messages above"
}

# find_sources ROOT prints each folder from ROOT down to two levels below it
# that holds an HDMS repository beside its recovery bundle — the same search
# the recovery page runs under /mnt/nas. Dot-folders and symlinks are skipped.
find_sources() {
	local dir
	{ find "$1" -maxdepth 2 \( -path "$1/*" -name '.*' -prune \) -o -type d -print 2>/dev/null || true; } |
		sort | while IFS= read -r dir; do
		if [ -f "$dir/repo/config" ] && [ -f "$dir/hdms-recovery.bin" ]; then
			printf '%s\n' "$dir"
		fi
	done
}

choose_source() {
	local folder list count choice
	echo
	echo "Where are the backups? Mount the network drive or external disk on this"
	echo "server first, or copy the backup folder onto it."
	while :; do
		ask folder "Folder holding the HDMS backups"
		if [ ! -d "$folder" ]; then
			say "  $folder is not a folder on this server."
			continue
		fi
		folder=$(cd "$folder" && pwd -P)
		list=$(find_sources "$folder")
		if [ -z "$list" ]; then
			say "  No HDMS backups found in $folder or two folders below it (looked for repo/config next to hdms-recovery.bin)."
			continue
		fi
		count=$(printf '%s\n' "$list" | wc -l | tr -d ' ')
		if [ "$count" = 1 ]; then
			source_dir=$list
		else
			echo "Found $count backup folders:"
			printf '%s\n' "$list" | awk '{ printf "  %d) %s\n", NR, $0 }'
			while :; do
				ask choice "Which one (1-$count)"
				if [[ $choice =~ ^[0-9]+$ ]] && [ "$choice" -ge 1 ] && [ "$choice" -le "$count" ]; then
					break
				fi
				say "  Enter a number from 1 to $count."
			done
			source_dir=$(printf '%s\n' "$list" | sed -n "${choice}p")
		fi
		# The worker sees this folder as /mnt/nas and finds the backups there.
		nas_host_path=$folder
		echo "Using the backups in $source_dir"
		return 0
	done
}

# worker_can_use: the worker runs as its own user and must read the bundle and
# repository and take restic's lock in repo/locks.
worker_can_use() {
	docker run --rm --network none -v "$source_dir:/restore-src" --entrypoint sh "$worker_image" \
		-c 'test -r /restore-src/hdms-recovery.bin && test -r /restore-src/repo/config && test -w /restore-src/repo/locks' \
		</dev/null >/dev/null 2>&1
}

ensure_worker_access() {
	local ids uid reply
	if worker_can_use; then
		return 0
	fi
	ids=$(docker run --rm --entrypoint sh "$worker_image" -c 'echo "$(id -u):$(id -g)"' </dev/null)
	uid=${ids%%:*}
	say "The HDMS worker runs as user ID $uid and cannot read and write $source_dir."
	ask reply "Give it ownership now with chown -R $ids $source_dir? (y/n)" n
	case $reply in
	y | Y | yes | YES) chown -R "$ids" "$source_dir" || true ;;
	*) die "ask whoever manages that drive to give user ID $uid read and write access to $source_dir, then run install.sh --restore again" ;;
	esac
	worker_can_use ||
		die "the worker still cannot use $source_dir (a network drive may refuse chown); ask whoever manages it to give user ID $uid read and write access, then run install.sh --restore again"
}

# secret_value NAME TEXT prints NAME's value from KEY=value lines.
secret_value() { printf '%s\n' "$2" | sed -n "s/^$1=//p" | head -n 1; }

# parse_secrets reads the four KEY=value lines `hdms-cli recovery unwrap`
# prints, and rejects anything that is not a hex or base64 secret.
parse_secrets() {
	local value
	backup_enc_key=$(secret_value HDMS_BACKUP_ENC_KEY "$1")
	token_pepper=$(secret_value HDMS_TOKEN_PEPPER "$1")
	credential_enc_key=$(secret_value HDMS_CREDENTIAL_ENC_KEY "$1")
	totp_enc_key=$(secret_value HDMS_TOTP_ENC_KEY "$1")
	for value in "$backup_enc_key" "$token_pepper" "$credential_enc_key" "$totp_enc_key"; do
		[[ $value =~ ^[A-Za-z0-9+/=_-]+$ ]] || return 1
	done
}

unlock_secrets() {
	local key out attempt=1
	echo
	echo "Type the recovery key from the printed sheet. It is not shown while you type."
	while :; do
		read -r -s -p "Recovery key: " key || die "input ended before setup finished"
		printf '\n' >&2
		if [ -z "$key" ]; then
			say "  Type the key, or press Ctrl-C to stop."
			continue
		fi
		# printf is a shell builtin, so the key is never in a process's arguments.
		if out=$(printf '%s\n' "$key" | docker run --rm -i --network none \
			-v "$source_dir:/restore-src:ro" --entrypoint hdms-cli "$worker_image" \
			recovery unwrap --from /restore-src 2>"$work/unwrap.err"); then
			key=""
			parse_secrets "$out" || die "the recovery bundle in $source_dir gave unexpected output"
			out=""
			echo "Recovery key accepted."
			return 0
		fi
		sed 's/^hdms-cli: //' "$work/unwrap.err" >&2
		if [ "$attempt" -ge "$key_attempts" ]; then
			die "stopped after $key_attempts attempts; check the key against the printed sheet and run install.sh --restore again"
		fi
		attempt=$((attempt + 1))
	done
}

default_tz() {
	local tz=""
	if command -v timedatectl >/dev/null 2>&1; then
		tz=$(timedatectl show -p Timezone --value 2>/dev/null || true)
	fi
	printf '%s' "${tz:-Asia/Tokyo}"
}

collect_settings() {
	echo
	echo "Server"
	ask site_addr "Host name staff use to open HDMS" hdms.hospital.local
	ask tz "Time zone for scheduled jobs" "$(default_tz)"
	echo
	echo "Hospital mail relay (overdue reminders and the weekly digest go through it)"
	ask smtp_host "SMTP host"
	while :; do
		ask smtp_port "SMTP port" 587
		[[ $smtp_port =~ ^[0-9]+$ ]] && break
		say "  The port is a number, usually 25, 465 or 587."
	done
	ask smtp_user "SMTP user name (empty if the relay needs none)" "" optional
	ask_hidden smtp_password "SMTP password (empty if none; not shown)"
	ask smtp_from "Sender address for HDMS mail"
	ask smtp_reply "Reply-to address (empty for none)" "" optional
}

# render_env NAME VALUE ... prints the template with each NAME's line (or its
# commented-out line) replaced by NAME='VALUE'. Values reach awk through its
# environment, never its arguments.
render_env() {
	(
		names=""
		while [ $# -gt 0 ]; do
			export "HDMS_SET_$1=$2"
			names="$names $1"
			shift 2
		done
		awk -v names="$names" -v q="'" '
			BEGIN { n = split(names, list, " "); for (i = 1; i <= n; i++) want[list[i]] = 1 }
			{
				line = $0
				sub(/^# /, "", line)
				eq = index(line, "=")
				name = eq > 1 ? substr(line, 1, eq - 1) : ""
				if (name in want && !(name in done)) {
					print name "=" q ENVIRON["HDMS_SET_" name] q
					done[name] = 1
					next
				}
				print
			}
			END { for (i = 1; i <= n; i++) if (!(list[i] in done)) print list[i] "=" q ENVIRON["HDMS_SET_" list[i]] q }
		' "$template"
	)
}

write_env_file() {
	local dir postgres_password app_db_password replaced
	local -a settings
	postgres_password=$(openssl rand -hex 16)
	app_db_password=$(openssl rand -hex 16)
	settings=(
		POSTGRES_PASSWORD "$postgres_password"
		HDMS_APP_DB_PASSWORD "$app_db_password"
		HDMS_DATABASE_URL "postgres://hdms_app:$app_db_password@db:5432/hdms_prod?sslmode=disable"
		HDMS_OWNER_DATABASE_URL "postgres://hdms_prod:$postgres_password@db:5432/hdms_prod?sslmode=disable"
		TZ "$tz"
		HDMS_SITE_ADDR "$site_addr"
		HDMS_ENTRA_REDIRECT_URL "https://$site_addr/v1/staff/auth/microsoft/callback"
		HDMS_TOKEN_PEPPER "$token_pepper"
		HDMS_CREDENTIAL_ENC_KEY "$credential_enc_key"
		HDMS_TOTP_ENC_KEY "$totp_enc_key"
		HDMS_BACKUP_ENC_KEY "$backup_enc_key"
		HDMS_SMTP_HOST "$smtp_host"
		HDMS_SMTP_PORT "$smtp_port"
		HDMS_SMTP_USERNAME "$smtp_user"
		HDMS_SMTP_PASSWORD "$smtp_password"
		HDMS_SMTP_FROM_ADDRESS "$smtp_from"
		HDMS_SMTP_REPLY_ADDRESS "$smtp_reply"
	)
	if [ "$mode" = restore ]; then
		settings+=(HDMS_BACKUP_NAS_HOST_PATH "$nas_host_path")
	fi

	dir=$(dirname "$env_file")
	mkdir -p "$dir"
	env_tmp=$(mktemp "$dir/.hdms.env.XXXXXX")
	chmod 600 "$env_tmp"
	render_env "${settings[@]}" >"$env_tmp"
	if [ -e "$env_file" ]; then
		replaced="$env_file.replaced-$(date +%Y%m%d%H%M%S)"
		mv "$env_file" "$replaced"
		echo "Kept the previous settings as $replaced"
	fi
	mv "$env_tmp" "$env_file"
	env_tmp=""
	echo "Wrote $env_file"
}

compose() {
	HDMS_PROD_ENV_FILE=$env_file docker compose -f "$compose_file" --env-file "$env_file" "$@" </dev/null
}

start_and_wait() {
	local path=/v1/healthz tries=0
	if [ "$mode" = restore ]; then
		path=/recovery/api/status
	fi
	echo
	echo "Starting HDMS (building the images takes several minutes the first time)..."
	compose up -d --build
	until curl -ksfS -m 5 -o /dev/null --resolve "$site_addr:443:127.0.0.1" "https://$site_addr$path" 2>/dev/null; do
		tries=$((tries + 1))
		if [ "$tries" -ge 60 ]; then
			say "HDMS did not answer within 3 minutes. Recent logs:"
			compose logs --tail 30 api worker caddy >&2 || true
			die "fix the problem shown above, then start HDMS with: docker compose -f $compose_file --env-file $env_file up -d"
		fi
		sleep 3
	done
}

next_steps() {
	echo
	if [ "$mode" = restore ]; then
		echo "HDMS is running with the secrets from your backups."
		echo "Open https://$site_addr/recovery and enter the same recovery key."
		return 0
	fi
	echo "HDMS is running at https://$site_addr/"
	echo "Next: create the first administrator, then print the recovery key in the console."
	echo "  1. sudo docker compose -f $compose_file --env-file $env_file exec api \\"
	echo "       hdms-cli admin bootstrap --email <email> --name \"<full name>\" --role admin"
	echo "  2. Sign in at https://$site_addr/admin/, open Backups and create the recovery key."
	echo "  3. Store the printed sheet away from the server room, and copy HDMS_BACKUP_ENC_KEY,"
	echo "     HDMS_TOKEN_PEPPER, HDMS_CREDENTIAL_ENC_KEY and HDMS_TOTP_ENC_KEY from"
	echo "     $env_file into the hospital password manager."
}

main() {
	local arg
	for arg in "$@"; do
		case $arg in
		--restore) mode=restore ;;
		--force) force=yes ;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			usage >&2
			exit 2
			;;
		esac
	done

	check_prerequisites
	work=$(mktemp -d)
	trap cleanup EXIT
	if [ "$mode" = restore ]; then
		build_worker_image
		choose_source
		ensure_worker_access
		unlock_secrets
	else
		generate_secrets
	fi
	collect_settings
	write_env_file
	start_and_wait
	next_steps
}

main "$@"
```

```bash
chmod +x deploy/production/install.sh
```

- [ ] **Step 5: Run shellcheck and the tests**

Run: `shellcheck deploy/production/*.sh && bash deploy/production/install_test.sh | grep -v '^ok'`
Expected: no shellcheck output, then only a blank line and `install_test: all checks passed` (67 `ok` lines filtered out).

- [ ] **Step 6: Mutation-check the tests**

First keep a copy: `GOOD=$(mktemp) && cp deploy/production/install.sh "$GOOD"`. Then make each change below to `install.sh` on its own, run `bash deploy/production/install_test.sh | grep FAIL`, confirm at least the named check fails, and put the file back with `cp "$GOOD" deploy/production/install.sh`.

| Change | Must fail |
|---|---|
| Drop `--network none \` from the unwrap `docker run` | `restore: unwrap runs offline on a read-only mount of the found folder` |
| Add `--key "$key"` before `--from /restore-src` in the unwrap call | `restore: no key ever appears in a docker command line` |
| Delete `\( -path "$1/*" -name '.*' -prune \) -o ` from `find_sources` | `restore exits 0` |
| In `render_env`'s awk, print `name "=" ENVIRON["HDMS_SET_" name]` without `q` | `fresh: SMTP password kept literally` |
| Replace `if [ "$attempt" -ge "$key_attempts" ]; then` with `if false; then` | `restore: the sixth key is never tried` |
| Replace `if docker volume inspect "$db_volume" >/dev/null 2>&1; then` with `if false; then` | `an existing database volume is refused` |

After the last one, run the full test again: `install_test: all checks passed`.

- [ ] **Step 7: Add the CI job**

In `.github/workflows/ci.yml`, add this job after the `security` job (same indentation as `security:`):

```yaml
  deploy-scripts:
    name: deploy scripts
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5

      - name: shellcheck
        run: shellcheck deploy/production/*.sh

      - name: install.sh tests
        run: bash deploy/production/install_test.sh
```

`ubuntu-latest` ships shellcheck and Docker with the compose plugin. Check the YAML parses: `python3 -c 'import yaml,sys; yaml.safe_load(open(".github/workflows/ci.yml"))'` (no output).

- [ ] **Step 8: Commit**

```bash
git add deploy/production/install.sh deploy/production/install_test.sh .github/workflows/ci.yml
git commit -m "feat(deploy): install.sh for a fresh install and --restore from backups"
```

---

### Task 2: Disaster-recovery runbook and deployment docs

**Files:**
- Create: `docs/runbooks/disaster-recovery.md`
- Delete: `docs/runbooks/restore.md`
- Modify: `docs/runbooks/production-deployment.md`, `docs/runbooks/nightly-backup.md`, `docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md`

**Interfaces:**
- Consumes: Task 1's `install.sh` prompts and messages (listed in Task 1's Produces); the recovery page strings in `hdms-frontend/apps/recovery/src/i18n/en.ts` (`HDMS database is damaged`, `HDMS database is empty`, `The database server is not running`, `This server was set up with different keys`, `Undo this restore`, `Restored`, `Open HDMS admin`); `serverDownHint` there and in `ja.ts` names the section "Database server will not start" — that heading must exist verbatim. Admin strings `Create recovery key`, `No recovery key printed yet`.
- Produces: `docs/runbooks/disaster-recovery.md` with headings `## A. Database broken, server fine`, `## B. Server lost`, `## Database server will not start`, `## After a restore`.

- [ ] **Step 1: Write `docs/runbooks/disaster-recovery.md`**

````markdown
# Runbook: Disaster recovery

**Who:** the HDMS administrator, with hospital IT when the server itself is
lost or the database server will not start.
**Recovery time objective:** 4 hours. **Recovery point:** the time of the backup
you restore. Loans and returns after it come back only from the paper register.

You need the **recovery sheet**: the page printed from **Backups → Recovery
key**, with a 28-character key in seven groups of four. Without it, IT needs
`HDMS_BACKUP_ENC_KEY`, `HDMS_TOKEN_PEPPER`, `HDMS_CREDENTIAL_ENC_KEY` and
`HDMS_TOTP_ENC_KEY` from the hospital password manager (Appendix A).

Every command below runs on the HDMS server from `/opt/hdms`, and uses this
shorthand:

```bash
cd /opt/hdms
DC="docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env"
```

> **Never run `$DC down -v`.** `-v` also deletes `hdms-prod-backups`, the
> server's own copy of every backup.

## Which situation is this?

| What you see | Go to |
|---|---|
| HDMS pages show errors or nobody can sign in, but the server is running | [A. Database broken, server fine](#a-database-broken-server-fine) |
| The server is gone: hardware failure, disk lost, machine replaced | [B. Server lost](#b-server-lost) |
| The recovery page says "The database server is not running" | [Database server will not start](#database-server-will-not-start), then A |

## A. Database broken, server fine

No IT needed. Start the paper register at the counter first.

1. Open `https://hdms.hospital.local/recovery` (your HDMS address followed by
   `/recovery`).
2. The page shows what the server sees; "HDMS database is damaged" (or "… is
   empty") is expected here.
3. Choose where the backups are: **This server**, the network drive or
   external disk, or a named backup location.
4. Type the recovery key from the sheet. Capitals, spaces and hyphens do not
   matter. If the page reports a typing mistake, compare it group by group.
5. Choose a backup. The newest is highlighted.
6. Read what will be replaced, type `RESTORE` and start. Kiosks show a
   maintenance notice until it finishes, usually a few minutes.
7. When the page says "Restored", choose **Open HDMS admin** and sign in with
   the accounts as they were on the date of that backup.
8. Enter the loans and returns from the paper register.

If the page says **This server was set up with different keys**, the server's
secrets do not match the backups; IT follows B from step 3 with this recovery
key.

**Undo.** While the replaced database was working and is still kept, the page
offers **Undo this restore** (type `RESTORE` again). It puts the replaced
database back.

Then read [After a restore](#after-a-restore).

## B. Server lost

**IT:**

1. Prepare the new server with the same host name:
   [production-deployment.md](production-deployment.md) steps 2–5 (Docker,
   firewall, DNS record pointing at the new address, TLS certificate, code in
   `/opt/hdms`). Stop before step 6.
2. Make the backups visible on the new server, in one of three ways:
   - **Network drive:** mount the share, for example at `/srv/hdms-nas`, and add
     it to `/etc/fstab`
     ([nightly-backup.md](nightly-backup.md), "Connecting a network drive").
   - **External disk:** mount it, for example at `/mnt/hdms-disk`.
   - **Copied folder:** copy the backup folder (the one holding `repo` and
     `hdms-recovery.bin`) onto the server with `cp -a`.
3. Run the installer and answer its questions:

   ```bash
   cd /opt/hdms
   sudo deploy/production/install.sh --restore
   ```

   - **Folder holding the HDMS backups:** the mount point or copied folder. It
     searches two folders down and asks which one when it finds several.
   - **"The HDMS worker runs as user ID 100 and cannot read and write …":**
     answer `y` for a disk or a copy. On a network drive that refuses, ask
     the drive's administrator to give user ID 100 read and write access to
     that folder, then run the installer again.
   - **Recovery key:** typed without being shown. "This recovery key does not
     open the backups in that folder" means an older sheet or another
     folder. It stops after five wrong keys.
   - **Host name, time zone, mail relay:** as on the old server.

   It writes `/etc/hdms/hdms.env` with the secrets from the backups and new
   database passwords, starts HDMS, and ends with
   "Open https://…/recovery and enter the same recovery key".
4. Hand over to the administrator, who follows A from step 1. On a new server
   the page shows "HDMS database is empty"; that is expected.
5. Once sign-in works, check that a kiosk scan resolves a badge (kiosks keep
   their pairing because the secrets came back with the backups).
6. If the backups came from a **copied folder** or a temporary disk, point
   `HDMS_BACKUP_NAS_HOST_PATH` in `/etc/hdms/hdms.env` at the real network
   drive ([nightly-backup.md](nightly-backup.md), "Connecting a network
   drive"); the installer set it to the folder you gave.

## Database server will not start

The recovery page says "The database server is not running", and
`sudo $DC ps db` shows the `db` container restarting.

1. Read why:

   ```bash
   sudo $DC logs --tail 50 db
   ```

   If it says `No space left on device`, free disk space and run
   `sudo $DC up -d`. Do not reset anything.
2. Keep a copy of the damaged database files:

   ```bash
   sudo $DC stop db
   sudo tar -C "$(sudo docker volume inspect -f '{{.Mountpoint}}' hdms-production_hdms-prod-db-data)" \
     -czf "/var/backups/hdms-damaged-db-$(date +%Y%m%d).tgz" .
   ```

3. Reset the database volume and start again. The database server starts
   empty, with the same passwords:

   ```bash
   sudo $DC rm -sf db
   sudo docker volume rm hdms-production_hdms-prod-db-data
   sudo $DC up -d
   ```

4. Within a minute the recovery page shows "HDMS database is empty". The
   administrator restores as in A.
5. Delete `/var/backups/hdms-damaged-db-*.tgz` after a week of normal running.

## After a restore

- **Kept databases.** A restore keeps the database it replaced, named
  `hdms_prod_before_<UTC date and time>`; an undo keeps the restored one as
  `hdms_prod_rolledback_<…>`. List them:

  ```bash
  sudo $DC exec db psql -U hdms_prod -d postgres -c '\l hdms_prod_*'
  ```

  Undo is offered only while the `_before_` database exists. After a week of
  normal running, drop it (this ends Undo):

  ```bash
  sudo $DC exec db dropdb -U hdms_prod hdms_prod_before_20261001t020000
  ```

- **A failed restore** leaves the database as it was; the page shows the step
  that failed. Details: `sudo $DC logs worker | grep recovery`.
- **Backups** continue on their schedule with the restored settings.

## Appendix A: restore from the command line

For drills, or when the recovery sheet is lost but the four secrets are in the
password manager (put them in `/etc/hdms/hdms.env` first). These commands
restore into a separate **scratch** database and never touch the live one.

1. List backups, from this server or from a named backup location:

   ```bash
   sudo $DC exec worker hdms-cli snapshots
   sudo $DC exec worker hdms-cli snapshots --from "Network drive"
   ```

2. Restore one into a scratch database (`latest`, or an ID from the list):

   ```bash
   sudo $DC exec db createdb -U hdms_prod hdms_scratch
   sudo $DC exec worker sh -c \
     'hdms-cli restore --snapshot latest --into "${HDMS_OWNER_DATABASE_URL%/*}/hdms_scratch?sslmode=disable"'
   ```

   Add `--from "Network drive"` to read from that location instead.
3. Check it:

   ```bash
   sudo $DC exec db psql -U hdms_prod -d hdms_scratch -c \
     "SELECT count(*) AS total_loans FROM loans;"
   sudo $DC exec db psql -U hdms_prod -d hdms_scratch -c \
     "SELECT (SELECT count(*) FROM loans WHERE status = 'open' AND NOT disputed) AS open_loans,
             (SELECT count(*) FROM devices WHERE status = 'on_loan') AS devices_on_loan;"
   sudo $DC exec worker sh -c \
     'HDMS_DATABASE_URL="${HDMS_OWNER_DATABASE_URL%/*}/hdms_scratch?sslmode=disable" hdms-cli reconcile'
   ```

   The two counts must match and `reconcile` must report 0 mismatches.
4. Drop it:

   ```bash
   sudo $DC exec db dropdb -U hdms_prod hdms_scratch
   ```

## Appendix B: drill record

Run a drill at least once a year, by someone who did not set the server up:
A against a healthy server (restore, then **Undo**), and B on a spare
virtual machine.

```text
================================================================================
HDMS RECOVERY DRILL RECORD
================================================================================
Date:                      ______________________________
Operator:                  ______________________________
Scenario:                  [ ] A (database)   [ ] B (server lost)
Backup location:           [ ] this server  [ ] network drive  [ ] external disk
Backup restored (date):    ______________________________

Start time:                ______________________________
Signed in again at:        ______________________________
Elapsed:                   ______________________________ (must be 4h 00m or less)

[ ] Recovery key opened the backups
[ ] Restore finished without warnings
[ ] Administrator signed in with TOTP
[ ] Kiosk scan resolved a badge
[ ] Undo worked (scenario A only)

Result:                    [ ] PASS    [ ] FAIL
Signature:                 ______________________________
================================================================================
```
````

- [ ] **Step 2: Delete the old runbook**

```bash
git rm docs/runbooks/restore.md
```

- [ ] **Step 3: Update `docs/runbooks/production-deployment.md`**

1. In `## 1. What you are installing`, change the `caddy` bullet's `(kiosk, admin console, staff portal)` to `(kiosk, admin console, staff portal, recovery page)`.
2. Replace everything from the line `## 6. Secrets configuration` up to, not including, the line `## 9. Kiosk iPad setup` with:

````markdown
## 6. Install and start

Run the installer from the checkout:

```bash
cd /opt/hdms
sudo deploy/production/install.sh
```

It checks for Docker and for the certificate from step 4, generates every
secret, and asks for:

- the host name staff use (default `hdms.hospital.local`),
- the time zone for scheduled jobs (for example `Asia/Tokyo`),
- the hospital mail relay: host, port (25, 465 or 587), user name and password
  if the relay needs them, sender address and reply-to address. Production
  refuses to start with the development defaults; overdue reminders and the
  weekly digest go through this relay.

It writes `/etc/hdms/hdms.env` (mode `0600`, root only), starts the four
containers and waits until HDMS answers; if it does not, it shows the recent
logs. It refuses to run when `/etc/hdms/hdms.env` already exists; `--force`
replaces the file and keeps the old one beside it as
`hdms.env.replaced-<date and time>`.

To rebuild a lost server from its backups, run `install.sh --restore` instead:
see [disaster-recovery.md](disaster-recovery.md), section B.

> **CRITICAL SECURITY WARNING — PASSWORD MANAGER BACKUP:**
> Copy these four values from `/etc/hdms/hdms.env` into the hospital password
> manager now. The recovery key (step 8) also unlocks them; the password
> manager is the fallback if the recovery sheet is lost.
> - **`HDMS_BACKUP_ENC_KEY`**: if lost, **no backup can ever be decrypted or restored**.
> - **`HDMS_TOKEN_PEPPER`**: if lost, every staff QR badge, session token and kiosk credential becomes invalid and must be re-issued.
> - **`HDMS_CREDENTIAL_ENC_KEY`** and **`HDMS_TOTP_ENC_KEY`**: if lost, stored integration secrets and every administrator's TOTP enrolment cannot be read.

---

## 7. Check the installation

Verify that all four containers are running and report `healthy` (the worker
can take two minutes):
```bash
sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env ps
```

Run the post-deployment smoke test:
```bash
HDMS_BASE_URL=https://hdms.hospital.local sh deploy/production/smoke.sh
```

---

## 8. Create the first administrator and the recovery key

Bootstrap the initial system administrator account:
```bash
sudo docker compose -f deploy/production/compose.yaml --env-file /etc/hdms/hdms.env exec api \
  hdms-cli admin bootstrap --email admin@hospital.org --name "System Administrator" --role admin
```
The command outputs:
- The administrator ID
- An `otpauth://` URL and a raw base32 TOTP secret

Scan the QR code into your authenticator app (e.g. Google Authenticator, 1Password) immediately. Navigate to `https://hdms.hospital.local/admin/`, sign in with the password prompted during bootstrap, and verify TOTP login.

Then create the recovery key. It lets the administrator restore HDMS from a
browser if the database breaks, and lets IT rebuild a lost server:

1. In the admin console open **Backups** and choose **Create recovery key**.
   Enter your password and a TOTP code.
2. Print the sheet that appears. The key is shown only once.
3. Type the last group of the key to confirm it was printed.
4. Store the sheet away from the server room, with a named custodian. Until
   step 3 is done the dashboard shows "No recovery key printed yet".

---

````

3. In "## 11. Optional network-share backup copy (NAS)", step 1, add this sentence after the code block: `The worker writes as user ID 100, so the share must give that user read and write access.`
4. Append at the end of the file:

```markdown

---

## 13. Disaster recovery

If HDMS stops working, the database server will not start, or the server is
lost, follow [disaster-recovery.md](disaster-recovery.md).
```

- [ ] **Step 4: Update `docs/runbooks/nightly-backup.md`**

In "## Connecting a network drive", replace step 1:

```markdown
1. Mount the share on the host (NFS or SMB), for example at `/srv/hdms-nas`,
   and add it to `/etc/fstab` so it is mounted again after a reboot.
```

with:

```markdown
1. Mount the share on the host (NFS or SMB), for example at `/srv/hdms-nas`,
   and add it to `/etc/fstab` so it is mounted again after a reboot. The
   worker writes as user ID 100, so the share must give that user read and
   write access.
```

- [ ] **Step 5: Record the planning decisions in the spec**

In `docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md`, section "## New-server install (`install.sh`)":

1. In `**`--restore`.**` step 2, replace
   `` `docker run --rm -i --network none -v <path>:/restore-src:ro <worker image> hdms-cli recovery unwrap --repo /restore-src`. ``
   with
   `` `docker run --rm -i --network none -v <folder>:/restore-src:ro --entrypoint hdms-cli hdms-install-worker recovery unwrap --from /restore-src`, where `<folder>` is the backup folder found under the path (the one holding `repo` and `hdms-recovery.bin`). ``
2. After the paragraph ending `TLS certificate placement and host naming stay manual runbook steps.`, add:

```markdown
Settled while planning (plan 4):

- The script builds the worker image itself (`docker build --target worker`,
  tagged `hdms-install-worker`, removed afterwards): `docker compose build`
  needs the env file the script has not written yet.
- It searches the given path two folders down for `repo/config` beside
  `hdms-recovery.bin`, skipping dot-folders, as the recovery page does under
  `/mnt/nas`; `HDMS_BACKUP_NAS_HOST_PATH` is the path IT typed.
- Before asking for the key it checks, as the worker's user (uid 100), that
  the folder can be read and `repo/locks` written; otherwise it offers
  `chown -R` and stops if that is declined or refused.
- It refuses when the `hdms-production_hdms-prod-db-data` volume exists, even
  with `--force` (Postgres keeps its first password); `--force` keeps the old
  env file as `hdms.env.replaced-<timestamp>`.
- It also asks for the host name (`HDMS_SITE_ADDR`) and refuses to run without
  the certificate files; values are written single-quoted; it stops after five
  wrong keys; it waits up to 3 minutes for HDMS and shows the logs if it does
  not answer.
- Tests use a stub `docker` plus the real `docker compose config`;
  Docker-in-Docker is used once for the full drill, not in CI.
```

- [ ] **Step 6: Check every link, command and quoted string**

Run from the worktree root:

```bash
# 1. Relative links in the touched runbooks point at files that exist.
for f in docs/runbooks/disaster-recovery.md docs/runbooks/production-deployment.md docs/runbooks/nightly-backup.md; do
  grep -oE '\]\([a-z0-9-]+\.md' "$f" | sed 's/](//' | sort -u | while read -r l; do
    [ -f "docs/runbooks/$l" ] || echo "BROKEN LINK in $f: $l"
  done
done
# 2. Nothing still links to the deleted runbook (history under docs/superpowers is fine).
git grep -n 'restore\.md' -- ':!docs/superpowers' || true
# 3. Every quoted UI string exists.
for s in "HDMS database is damaged" "HDMS database is empty" "The database server is not running" \
  "This server was set up with different keys" "Undo this restore" "Open HDMS admin" "This server"; do
  grep -qF "$s" hdms-frontend/apps/recovery/src/i18n/en.ts || echo "MISSING recovery string: $s"
done
for s in "Create recovery key" "No recovery key printed yet"; do
  grep -qF "$s" hdms-frontend/apps/admin/src/i18n/en.ts || echo "MISSING admin string: $s"
done
grep -qF 'section \"Database server will not start\"' hdms-frontend/apps/recovery/src/i18n/en.ts || echo "MISSING section pointer"
grep -qx '## Database server will not start' docs/runbooks/disaster-recovery.md || echo "MISSING heading"
# 4. Every hdms-cli flag the runbook names exists.
for flag in '"from"' '"snapshot"' '"into"'; do
  grep -q "fs.String($flag" hdms-backend/cmd/hdms-cli/main.go || echo "MISSING flag $flag"
done
grep -q 'case "reconcile"' hdms-backend/cmd/hdms-cli/main.go || echo "MISSING reconcile"
```

Expected: no output at all.

- [ ] **Step 7: Commit**

```bash
git add docs/runbooks/disaster-recovery.md docs/runbooks/production-deployment.md docs/runbooks/nightly-backup.md \
  docs/superpowers/specs/2026-09-30-guided-backup-and-disaster-recovery-design.md
git commit -m "docs(runbooks): disaster recovery runbook replaces restore.md; deployment uses install.sh"
```

(`git rm` in Step 2 already staged the deletion.)

---

### Task 3: "Server lost" drill in Docker-in-Docker

A throwaway `docker:dind` container plays the new server. Nothing here is committed unless the drill finds a bug: then fix it in the task that owns the code (Task 1 for the script, with a new failing check in `install_test.sh` first; Task 2 for the runbook), commit the fix, and record it under "Changes made during execution" at the end of this plan.

**Files:** none (verification); fixes go to Task 1's or Task 2's files.

**Interfaces:**
- Consumes: Task 1's `install.sh`, Task 2's runbook commands, the recovery API (`GET /recovery/api/status` → `{"database": "working|empty|damaged|server_down", …}`; `GET /recovery/api/sources` → `{"sources":[{"id":"path:/mnt/nas/hdms-backups",…}]}`; `POST /recovery/api/unlock {source,key}` sets the session cookie; `GET /recovery/api/snapshots` → `{"snapshots":[{"id","takenAt","sizeBytes"}]}` newest first; `POST /recovery/api/restore {snapshotId, confirmation:"RESTORE"}` → 202; `GET /recovery/api/restore` → `{"phase":"running|completed|failed",…}`), the admin API (`POST /v1/auth/login {email,password,totpCode}`; `POST /v1/backup/recovery-key {password,totpCode}` → `{"key"}`; `POST /v1/backup/recovery-key/confirm`; `POST /v1/backup/run`; mutating calls send `X-CSRF-Token` from the `hdms_csrf` cookie).
- Produces: a filled-in drill record in "Changes made during execution".

Run everything from the worktree root on the Mac. The drill builds every image inside the container; allow 15 minutes for the first `install.sh`.

- [ ] **Step 1: Start the drill server**

```bash
S=$(mktemp -d)        # scratch for cookies and the key
docker run -d --privileged --name hdms-drill -p 9443:443 docker:dind
until docker exec hdms-drill docker info >/dev/null 2>&1; do sleep 2; done
docker exec hdms-drill apk add --no-cache bash curl openssl
git archive --format=tar HEAD | docker exec -i hdms-drill sh -c 'mkdir -p /opt/hdms && tar -x -C /opt/hdms'
# The main checkout's mkcert pair for "localhost", trusted by this Mac's browser.
MAIN=$(git worktree list | head -1 | awk '{print $1}')
docker exec hdms-drill mkdir -p /etc/ssl/certs /etc/ssl/private
docker cp "$MAIN/certs/localhost.pem" hdms-drill:/etc/ssl/certs/hdms.hospital.crt
docker cp "$MAIN/certs/localhost-key.pem" hdms-drill:/etc/ssl/private/hdms.hospital.key
DC='docker compose -f /opt/hdms/deploy/production/compose.yaml --env-file /etc/hdms/hdms.env'
B=https://localhost:9443
totp() { python3 -c 'import base64,hmac,struct,time,hashlib;k=base64.b32decode("JBSWY3DPEHPK3PXP");h=hmac.new(k,struct.pack(">Q",int(time.time())//30),hashlib.sha1).digest();o=h[-1]&15;print("%06d"%((struct.unpack(">I",h[o:o+4])[0]&0x7fffffff)%1000000))'; }
```

If port 9443 is taken, pick another and change `B` to match.

- [ ] **Step 2: Fresh install**

Answers: host `localhost`, time zone, SMTP host, port, empty user, empty password, sender, empty reply-to.

```bash
printf '%s\n' localhost Asia/Tokyo smtp.example.org 587 '' '' hdms@example.org '' |
  docker exec -i hdms-drill bash /opt/hdms/deploy/production/install.sh
curl -ksS "$B/v1/readyz"
```

Expected: the script ends with `HDMS is running at https://localhost/` and the bootstrap instructions; `readyz` returns `"status":"ok"`.

- [ ] **Step 3: Administrator, kiosk, recovery key, first backup**

```bash
docker exec hdms-drill sh -c "$DC exec -T api hdms-cli admin bootstrap --email drill@example.org \
  --name 'Drill Admin' --role admin --password drill-password-1 --totp-secret JBSWY3DPEHPK3PXP"
docker exec hdms-drill sh -c "$DC exec -T api hdms-cli kiosk register --name drill-kiosk" | tee "$S/kiosk.txt"
KIOSK=$(awk 'f && NF {print $1; exit} /Bearer token/ {f=1}' "$S/kiosk.txt")
curl -ksS -c "$S/jar" -H 'Content-Type: application/json' \
  -d "{\"email\":\"drill@example.org\",\"password\":\"drill-password-1\",\"totpCode\":\"$(totp)\"}" "$B/v1/auth/login"
CSRF=$(awk '$6=="hdms_csrf"{print $7}' "$S/jar")
sleep 31   # a TOTP code is accepted once
curl -ksS -b "$S/jar" -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' \
  -d "{\"password\":\"drill-password-1\",\"totpCode\":\"$(totp)\"}" "$B/v1/backup/recovery-key" | tee "$S/key.json"
KEY=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["key"])' "$S/key.json")
curl -ksS -b "$S/jar" -H "X-CSRF-Token: $CSRF" -X POST "$B/v1/backup/recovery-key/confirm"
curl -ksS -b "$S/jar" -H "X-CSRF-Token: $CSRF" -X POST "$B/v1/backup/run"
until docker exec hdms-drill sh -c "$DC exec -T worker hdms-cli snapshots" | grep -qE '^[0-9a-f]{8}'; do sleep 10; done
docker exec hdms-drill sh -c "$DC exec -T worker ls -l /var/backups/hdms"
```

Expected: `KIOSK` is non-empty; the key has seven groups of four; the backup lists one snapshot; `/var/backups/hdms` holds `repo` and `hdms-recovery.bin`.

- [ ] **Step 4: Appendix A commands on the healthy server**

Run the runbook's Appendix A, steps 1–4, verbatim except `sudo $DC` becomes `docker exec hdms-drill sh -c "$DC …"` (use `exec -T`), and skip the `--from "Network drive"` line (no destination is configured). Expected: one snapshot listed; restore into `hdms_scratch` succeeds; both counts print (equal); `reconcile` prints `Reconcile: … 0 mismatches`; `dropdb` succeeds.

- [ ] **Step 5: Lose the server**

```bash
docker exec hdms-drill sh -c '
  set -e
  vol=$(docker volume inspect -f "{{.Mountpoint}}" hdms-production_hdms-prod-backups)
  mkdir -p /srv/nas
  cp -r "$vol" /srv/nas/hdms-backups      # plain cp -r: root-owned, as a hurried copy would be
  '"$DC"' down -v                          # drill only: also deletes the server-side backups
  rm /etc/hdms/hdms.env'
```

- [ ] **Step 6: `install.sh --restore`**

First a key with one character changed (a typo the check characters catch), then the real key:

```bash
TYPO="${KEY%?}$( [ "${KEY: -1}" = A ] && echo B || echo A )"
printf '%s\n' /srv/nas y "$TYPO" "$KEY" localhost Asia/Tokyo smtp.example.org 587 '' '' hdms@example.org '' |
  docker exec -i hdms-drill bash /opt/hdms/deploy/production/install.sh --restore
docker exec hdms-drill grep -E '^(HDMS_BACKUP_NAS_HOST_PATH|HDMS_TOKEN_PEPPER)=' /etc/hdms/hdms.env
```

Expected, in order: `The HDMS worker runs as user ID 100 and cannot read and write /srv/nas/hdms-backups.`; `the recovery key has a typo; check it against the printed sheet`; `Recovery key accepted.`; `Open https://localhost/recovery and enter the same recovery key.` The env file has `HDMS_BACKUP_NAS_HOST_PATH='/srv/nas'` and a pepper.

- [ ] **Step 7: Restore through the recovery API**

(Or do this step in Chrome at `https://localhost:9443/recovery/`; the curl version is below.)

```bash
R=$B/recovery/api
curl -ksS "$R/status"
curl -ksS "$R/sources"
curl -ksS -c "$S/rjar" -H 'Content-Type: application/json' \
  -d "{\"source\":\"path:/mnt/nas/hdms-backups\",\"key\":\"$KEY\"}" "$R/unlock"
SNAP=$(curl -ksS -b "$S/rjar" "$R/snapshots" | python3 -c 'import json,sys; print(json.load(sys.stdin)["snapshots"][0]["id"])')
curl -ksS -b "$S/rjar" -H 'Content-Type: application/json' \
  -d "{\"snapshotId\":\"$SNAP\",\"confirmation\":\"RESTORE\"}" "$R/restore"
until curl -ksS -b "$S/rjar" "$R/restore" | grep -qE '"phase":"(completed|failed)"'; do sleep 2; done
curl -ksS -b "$S/rjar" "$R/restore"
```

Expected: status `"database":"empty"`; the sources list `path:/mnt/nas/hdms-backups`; unlock succeeds (no `keys_mismatch`); the final state is `"phase":"completed"` with no `error`.

- [ ] **Step 8: Sign in and scan**

```bash
sleep 31
HDMS_BASE_URL=$B HDMS_ADMIN_EMAIL=drill@example.org HDMS_ADMIN_PASSWORD=drill-password-1 \
  HDMS_ADMIN_TOTP_SECRET=$(totp) KIOSK_TOKEN=$KIOSK sh deploy/production/smoke.sh
```

Expected: `smoke: PASS`. The login proves the TOTP key came back; the kiosk session opening with a token issued before the "loss" proves the pepper did.

- [ ] **Step 9: After-a-restore commands**

Run the runbook's "After a restore" list and drop commands through `docker exec hdms-drill sh -c "$DC exec -T db …"`, using the real `hdms_prod_before_…` name from the list. Expected: the list shows one `hdms_prod_before_<stamp>`; `dropdb` removes it.

- [ ] **Step 10: "Database server will not start"**

Break Postgres, then follow the runbook section verbatim (through `docker exec hdms-drill sh -c "…"`, with `DC` as above and without `sudo`):

```bash
docker exec hdms-drill sh -c "$DC stop db && rm \"\$(find \"\$(docker volume inspect -f '{{.Mountpoint}}' hdms-production_hdms-prod-db-data)\" -name pg_control)\" && $DC up -d db"
sleep 20
curl -ksS "$R/status"
```

Expected: `"database":"server_down"`. Then runbook steps 1–3; `curl -ksS "$R/status"` shows `"database":"empty"` within a minute; repeat Step 7 (unlock again) and Step 8 (`smoke: PASS`); `/var/backups/hdms-damaged-db-<date>.tgz` exists in the drill container.

- [ ] **Step 11: Clean up and record**

```bash
docker rm -f -v hdms-drill
rm -rf "$S"
```

Add a "Changes made during execution" section at the end of this plan with the drill date, the elapsed time from Step 5 to `smoke: PASS` in Step 8, and every deviation or fix. Commit it:

```bash
git add docs/superpowers/plans/2026-10-01-install-and-disaster-recovery-runbook.md
git commit -m "docs(plans): record the plan 4 drill"
```

## Changes made during execution

- **Drill Date:** 2026-10-01
- **Status:** Stopped at Task 3 Step 2 per Rule 2 and Rule 6.
- **Details:**
  - Task 1 and Task 2 completed exactly as specified and were committed (`a015849`, `d6f7ef6`).
  - Task 3 Step 1 started the `hdms-drill` container and provisioned tools and certificates.
  - Task 3 Step 2 ran `install.sh` for a fresh install. Container images built successfully. During service startup and health checking, `install.sh` timed out after 3 minutes waiting for `https://localhost/v1/healthz`.
  - Container logs showed `hdms-production-api-1` failed to start with `open /etc/ssl/private/hdms.hospital.key: permission denied`. The key file copied from the host into `hdms-drill` had host UID 501 and mode `0600`; inside the `api` container the process runs as non-root user `hdms` (UID 100), which cannot read `0600` files owned by UID 501.
  - Per Rule 2 ("If a plan step's command does not give the plan's Expected result, STOP that task, do not work around it, and report the exact command and its exact output") and Rule 6 ("If a step fails, stop and report; do not invent alternative commands. At the end always run: docker rm -f -v hdms-drill. Write the 'Changes made during execution' section the plan asks for only with what really happened, then make the plan's final commit"), execution stopped without inventing alternative commands, `docker rm -f -v hdms-drill` was executed, and the plan is committed.
