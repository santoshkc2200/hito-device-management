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
# The host's `hdms` service user: uid 998, group 997, once useradd has run.
cat >"$stub_bin/id" <<'EOF'
#!/usr/bin/env bash
case "$*" in
-u) echo "${STUB_UID:-0}" ;;
"-u hdms") [ -f "$STUB_STATE/hdms-user" ] && echo 998 ;;
*) exec /usr/bin/id "$@" ;;
esac
EOF
cat >"$stub_bin/getent" <<'EOF'
#!/usr/bin/env bash
[ "$*" = "group hdms" ] && [ -f "$STUB_STATE/hdms-group" ] && echo "hdms:x:997:"
EOF
cat >"$stub_bin/groupadd" <<'EOF'
#!/usr/bin/env bash
printf 'groupadd %s\n' "$*" >>"$STUB_STATE/accounts.log"
touch "$STUB_STATE/hdms-group"
EOF
cat >"$stub_bin/useradd" <<'EOF'
#!/usr/bin/env bash
printf 'useradd %s\n' "$*" >>"$STUB_STATE/accounts.log"
touch "$STUB_STATE/hdms-user"
EOF
cat >"$stub_bin/chown" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$STUB_STATE/chown.log"
[ "$1" = -R ] || exit 0 # the TLS key; only the backup folder plays the worker's access
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
	touch "$state/docker.log" "$state/accounts.log" "$case_dir/certs/crt" "$case_dir/certs/key"
	chmod 600 "$case_dir/certs/key"
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
		HDMS_BACKUP_DRIVES_HOST_PATH="$drives" \
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

drives="$tmp/drives"
mkdir -p "$drives"
drives_real=$(cd "$drives" && pwd -P)

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
check "fresh: drives folder is written" has_line "$env_file" "HDMS_BACKUP_DRIVES_HOST_PATH='$drives_real'"
check "fresh: no network-drive setting left" lacks "$env_file" "HDMS_BACKUP_NAS_HOST_PATH"
check "fresh: starts the stack with the env file" grep -qF "compose -f $here/compose.yaml --env-file $env_file up -d --build" "$state/docker.log"
check "fresh: builds no separate worker image" lacks "$state/docker.log" "build --quiet"
check "fresh: points at the first administrator" says "hdms-cli admin bootstrap"
check "fresh: creates the hdms system group" grep -qx "groupadd --system hdms" "$state/accounts.log"
check "fresh: creates the hdms system user without a login" \
	grep -qx "useradd --system --gid hdms --no-create-home --shell /usr/sbin/nologin hdms" "$state/accounts.log"
check "fresh: the api runs as the hdms user" has_line "$env_file" "HDMS_UID='998'"
check "fresh: ... and its group" has_line "$env_file" "HDMS_GID='997'"
check "fresh: the TLS key belongs to root and group hdms" has_line "$state/chown.log" "root:hdms $case_dir/certs/key"
# shellcheck disable=SC2012 # ls -l is the portable way to read a mode.
check "fresh: the TLS key is readable by group hdms only" [ "$(ls -l "$case_dir/certs/key" | cut -c1-10)" = "-rw-r-----" ]

compose_json=$(HDMS_PROD_ENV_FILE="$env_file" "$real_docker" compose -f "$here/compose.yaml" --env-file "$env_file" config --format json) ||
	compose_json=""
check "compose reads the SMTP password literally" grep -qF '"HDMS_SMTP_PASSWORD": "pa$$s w #rd\"x"' <<<"$compose_json"
check "compose reads the time zone" grep -qF '"TZ": "Europe/London"' <<<"$compose_json"
check "compose runs the api as the hdms user" grep -qF '"user": "998:997"' <<<"$compose_json"
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
touch "$state/hdms-group" "$state/hdms-user"
run_install "$settings"
check "an existing hdms user is reused" exits_with 0
check "an existing hdms user is not created again" [ ! -s "$state/accounts.log" ]
check "an existing hdms user's IDs are used" has_line "$env_file" "HDMS_UID='998'"

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
nas="$drives/nas"
make_source "$nas/hdms-backups"
make_source "$nas/.snapshot/hdms-backups" # a NAS snapshot copy: never offered
mkdir -p "$drives/empty"
outside="$tmp/outside"
make_source "$outside/hdms-backups"
nas_real=$(cd "$nas" && pwd -P)

fresh_state
touch "$state/access-ok"
run_install "$(printf '%s\n' "$tmp/nowhere" "$outside" "$drives/empty" "$nas" "$wrong_key" "$good_key")
$settings" --restore
check "restore exits 0" exits_with 0
check "restore: a missing folder is asked again" says "$tmp/nowhere is not a folder on this server"
check "restore: a folder outside the drives folder is asked again" \
	says "$outside is not inside $drives_real. Mount or copy the backups under $drives_real, then try again."
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
check "restore: the worker sees the drives folder" has_line "$env_file" "HDMS_BACKUP_DRIVES_HOST_PATH='$drives_real'"
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
