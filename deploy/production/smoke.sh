#!/bin/sh
# Production smoke check (Phase 5.3a): /v1/healthz, /v1/readyz, /recovery, a login and a scan.
#
# Usage:
#   HDMS_BASE_URL=https://hdms.hospital.local \
#   HDMS_ADMIN_EMAIL=admin@hospital.local HDMS_ADMIN_PASSWORD=... \
#   HDMS_ADMIN_TOTP_SECRET=... KIOSK_TOKEN=... \
#     sh deploy/production/smoke.sh
#
# Behind Caddy the API is same-origin, so BASE defaults to the Caddy front
# door. curl -k is used because the hospital internal CA is not in the
# operator laptop's trust store until 5.3b distributes it — the check is for
# liveness and auth, not for chain validation (verified separately on a
# real iPad in 5.3b).
set -eu

BASE="${HDMS_BASE_URL:-https://localhost:8443}"
ADMIN_EMAIL="${HDMS_ADMIN_EMAIL:-}"
ADMIN_PASSWORD="${HDMS_ADMIN_PASSWORD:-}"
ADMIN_TOTP="${HDMS_ADMIN_TOTP_SECRET:-}"
KIOSK_TOKEN="${KIOSK_TOKEN:-}"

fail() { echo "smoke: FAIL: $*" >&2; exit 1; }
pass() { echo "smoke: ok: $*"; }

echo "smoke: target ${BASE}"

health=$(curl -ksS -m 10 "${BASE}/v1/healthz") || fail "GET /v1/healthz unreachable"
echo "${health}" | grep -q '"status":"ok"' || fail "/v1/healthz body unexpected: ${health}"
pass "GET /v1/healthz"

ready=$(curl -ksS -m 10 "${BASE}/v1/readyz") || fail "GET /v1/readyz unreachable"
echo "${ready}" | grep -q '"status":"ok"' || echo "${ready}" | grep -q '"status": "ok"' || fail "/v1/readyz not ready: ${ready}"
pass "GET /v1/readyz"

recovery=$(curl -ksS -m 10 "${BASE}/recovery/api/status") || fail "GET /recovery/api/status unreachable"
echo "${recovery}" | grep -q '"database"' || fail "/recovery/api/status body unexpected: ${recovery}"
pass "GET /recovery/api/status"

curl -ksS -m 10 "${BASE}/recovery/" | grep -q '<title>HDMS Recovery</title>' || fail "/recovery/ does not serve the recovery page"
pass "GET /recovery/"

if [ -z "${ADMIN_EMAIL}" ] || [ -z "${ADMIN_PASSWORD}" ]; then
  echo "smoke: SKIP login/scan (set HDMS_ADMIN_EMAIL + HDMS_ADMIN_PASSWORD for full check)"
  echo "smoke: PASS (liveness only)"
  exit 0
fi

login_body=$(printf '{"email":%s,"password":%s}' \
  "$(printf '%s' "${ADMIN_EMAIL}" | sed 's/"/\\"/g; s/^/"/; s/$/"/')" \
  "$(printf '%s' "${ADMIN_PASSWORD}" | sed 's/"/\\"/g; s/^/"/; s/$/"/')")

if [ -n "${ADMIN_TOTP}" ]; then
  login_body=$(printf '%s' "${login_body}" | sed "s/}$/,\"totpCode\":\"${ADMIN_TOTP}\"}/")
fi

# -D dumps headers to stderr-adjacent file for the session cookies; the body
# carries the admin identity on success.
login_resp=$(curl -ksS -m 15 -D /tmp/hdms-smoke-headers.txt \
  -H 'Content-Type: application/json' -d "${login_body}" \
  "${BASE}/v1/auth/login") || fail "POST /v1/auth/login unreachable"
echo "${login_resp}" | grep -qi 'error\|unauthorized\|invalid' && fail "login rejected: ${login_resp}"
pass "POST /v1/auth/login as ${ADMIN_EMAIL}"

if [ -z "${KIOSK_TOKEN}" ]; then
  echo "smoke: SKIP scan (set KIOSK_TOKEN for the kiosk-path check)"
  echo "smoke: PASS (liveness + login)"
  exit 0
fi

session_resp=$(curl -ksS -m 15 -H "Authorization: Bearer ${KIOSK_TOKEN}" \
  -H 'Content-Type: application/json' -H "Idempotency-Key: smoke-$(date +%s)" \
  -d '{}' "${BASE}/v1/sessions") || fail "POST /v1/sessions unreachable"
echo "${session_resp}" | grep -q 'id' || fail "session open unexpected: ${session_resp}"
pass "POST /v1/sessions (kiosk scan path reachable)"

echo "smoke: PASS"
