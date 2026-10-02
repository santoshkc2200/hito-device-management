#!/usr/bin/env bash
# Install HDMS on a new server, or rebuild a lost one from its backups.
#
#   sudo deploy/production/install.sh             fresh install
#   sudo deploy/production/install.sh --restore   new server, data from backups
#   sudo deploy/production/install.sh --restore --cloud=google   ... from Google Drive
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
drives_host_path=${HDMS_BACKUP_DRIVES_HOST_PATH:-/mnt}
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
source_dir="" service_uid="" service_gid="" drives_real="" cloud=""
backup_enc_key="" token_pepper="" credential_enc_key="" totp_enc_key=""

say() { printf '%s\n' "$*" >&2; }
die() {
	printf 'install.sh: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
Usage: install.sh [--restore [--cloud=google|onedrive]] [--force]

  (no option)  Fresh install: generate every secret, write /etc/hdms/hdms.env
               and start HDMS.
  --restore    New server for an existing HDMS: read the secrets out of the
               backups with the recovery key, write /etc/hdms/hdms.env, start
               HDMS, then finish in the browser at https://<host>/recovery.
  --cloud=google|onedrive
               With --restore: the backups are in Google Drive or OneDrive.
               Signs in on this terminal and downloads them first.
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
	for tool in getent groupadd useradd; do
		command -v "$tool" >/dev/null 2>&1 || die "$tool is not installed (apt-get install passwd libc-bin)"
	done
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
# the recovery page runs under each drive. Dot-folders and symlinks are skipped.
find_sources() {
	local dir
	{ find "$1" -maxdepth 2 \( -path "$1/*" -name '.*' -prune \) -o -type d -print 2>/dev/null || true; } |
		sort | while IFS= read -r dir; do
		if [ -f "$dir/repo/config" ] && [ -f "$dir/hdms-recovery.bin" ]; then
			printf '%s\n' "$dir"
		fi
	done
}

# resolve_drives sets drives_real: the drives folder with symlinks resolved,
# which is where the worker sees everything under /drives.
resolve_drives() {
	[ -d "$drives_host_path" ] || die "the drives folder $drives_host_path does not exist on this server; create it or set HDMS_BACKUP_DRIVES_HOST_PATH"
	drives_real=$(cd "$drives_host_path" && pwd -P)
}

# select_source LIST sets source_dir to the one folder in LIST (one per line),
# asking when there are several.
select_source() {
	local list=$1 count choice
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
	echo "Using the backups in $source_dir"
}

choose_source() {
	local folder list
	resolve_drives
	echo
	echo "Where are the backups? Mount the network drive or external disk under"
	echo "$drives_real on this server first, or copy the backup folder there."
	while :; do
		ask folder "Folder holding the HDMS backups"
		if [ ! -d "$folder" ]; then
			say "  $folder is not a folder on this server."
			continue
		fi
		folder=$(cd "$folder" && pwd -P)
		case $folder/ in
		"$drives_real"/*/*) ;;
		*)
			say "  $folder is not inside $drives_real. Mount or copy the backups under $drives_real, then try again."
			continue
			;;
		esac
		list=$(find_sources "$folder")
		if [ -z "$list" ]; then
			say "  No HDMS backups found in $folder or two folders below it (looked for repo/config next to hdms-recovery.bin)."
			continue
		fi
		select_source "$list"
		return 0
	done
}

# download_cloud PROVIDER signs in to Google Drive or OneDrive on this
# terminal and copies the HDMS backup folder into the drives folder, where
# the rest of --restore treats it like any other folder of backups.
download_cloud() {
	local provider=$1 client_id client_secret="" tenant="" folder dest ids list
	resolve_drives
	echo
	echo "The backups are in ${provider//_/ }. Use the OAuth client ID that HDMS was set up with"
	echo "(docs/runbooks/cloud-backup.md, step 1)."
	ask client_id "OAuth client ID"
	if [ "$provider" = google_drive ]; then
		while :; do
			ask_hidden client_secret "OAuth client secret (not shown)"
			[ -n "$client_secret" ] && break
			say "  Google needs the client secret."
		done
	else
		ask tenant "Directory (tenant) ID, or common" common
	fi
	ask folder "Folder name in the cloud drive" hdms-backups
	dest="$drives_real/cloud-restore"
	ids=$(docker run --rm --entrypoint sh "$worker_image" -c 'echo "$(id -u):$(id -g)"' </dev/null)
	mkdir -p "$dest"
	chown "$ids" "$dest" || die "could not give the HDMS worker ownership of $dest"
	# The client secret reaches the worker image on stdin, never in a command line.
	# shellcheck disable=SC2086 # tenant is empty for Google and a single word otherwise
	if ! printf '%s\n' "$client_secret" | docker run --rm -i -v "$dest:/download" --entrypoint hdms-cli "$worker_image" \
		cloud fetch --provider "$provider" --client-id "$client_id" ${tenant:+--tenant $tenant} --folder "$folder" --to /download; then
		die "downloading the backups failed; see the messages above, then run install.sh --restore --cloud=... again"
	fi
	client_secret=""
	list=$(find_sources "$dest")
	[ -n "$list" ] || die "nothing that looks like HDMS backups was downloaded into $dest"
	select_source "$list"
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

# ensure_service_user creates the host's `hdms` system user, which the api
# container runs as (HDMS_UID/HDMS_GID in the env file), and makes the TLS key
# readable by root and that group only. Without it the api, which is not root
# inside its container, cannot open a root-only key.
ensure_service_user() {
	if ! getent group hdms >/dev/null; then
		groupadd --system hdms
	fi
	if ! id -u hdms >/dev/null 2>&1; then
		useradd --system --gid hdms --no-create-home --shell /usr/sbin/nologin hdms
	fi
	service_uid=$(id -u hdms)
	service_gid=$(getent group hdms | cut -d: -f3)
	chown root:hdms "$key_file"
	chmod 640 "$key_file"
	echo "The API runs as the hdms system user (uid $service_uid); $key_file is readable by root and group hdms only."
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
		HDMS_UID "$service_uid"
		HDMS_GID "$service_gid"
		HDMS_BACKUP_DRIVES_HOST_PATH "$drives_host_path"
	)

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
		--cloud=google) cloud=google_drive ;;
		--cloud=onedrive) cloud=onedrive ;;
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

	if [ -n "$cloud" ] && [ "$mode" != restore ]; then
		usage >&2
		exit 2
	fi

	check_prerequisites
	work=$(mktemp -d)
	trap cleanup EXIT
	if [ "$mode" = restore ]; then
		build_worker_image
		if [ -n "$cloud" ]; then
			download_cloud "$cloud"
		else
			choose_source
		fi
		ensure_worker_access
		unlock_secrets
	else
		generate_secrets
	fi
	collect_settings
	ensure_service_user
	write_env_file
	start_and_wait
	next_steps
}

main "$@"
