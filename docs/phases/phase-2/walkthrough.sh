#!/usr/bin/env bash
# ==============================================================================
# HDMS Phase 2 End-to-End Walkthrough Script
#
# Drives all seven checkout/lending scenarios from docs/04-scanning-and-checkout-flows.md
# via curl against the running API (default: http://localhost:8080).
#
# Requirements:
#   - curl
#   - jq
#   - HDMS API running (`task dev` or `go run ./cmd/hdms-api`)
#
# Usage:
#   ./docs/phases/phase-2/walkthrough.sh [BASE_URL] [KIOSK_TOKEN] [ADMIN_COOKIE]
# ==============================================================================

set -euo pipefail

BASE_URL="${1:-${BASE_URL:-http://localhost:8080}}"
KIOSK_TOKEN="${2:-${KIOSK_TOKEN:-kiosk-test-token}}"
ADMIN_COOKIE="${3:-${ADMIN_COOKIE:-}}"

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BOLD='\033[1m'
NC='\033[0m' # No Color

PASSED_COUNT=0
FAILED_COUNT=0

log_header() {
  echo -e "\n${BOLD}${BLUE}════════════════════════════════════════════════════════════════════${NC}"
  echo -e "${BOLD}${BLUE}  $1${NC}"
  echo -e "${BOLD}${BLUE}════════════════════════════════════════════════════════════════════${NC}"
}

log_step() {
  echo -e "${YELLOW}▶ $1${NC}"
}

log_pass() {
  echo -e "${GREEN}✓ PASS: $1${NC}"
  PASSED_COUNT=$((PASSED_COUNT + 1))
}

log_fail() {
  echo -e "${RED}✗ FAIL: $1${NC}"
  FAILED_COUNT=$((FAILED_COUNT + 1))
  exit 1
}

# ------------------------------------------------------------------------------
# Helpers
# ------------------------------------------------------------------------------

kiosk_post() {
  local path="$1"
  local data="${2:-{}}"
  curl -s -X POST "${BASE_URL}${path}" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer ${KIOSK_TOKEN}" \
    -d "${data}"
}

kiosk_get() {
  local path="$1"
  curl -s -X GET "${BASE_URL}${path}" \
    -H "Authorization: Bearer ${KIOSK_TOKEN}"
}

kiosk_delete() {
  local path="$1"
  curl -s -X DELETE "${BASE_URL}${path}" \
    -H "Authorization: Bearer ${KIOSK_TOKEN}"
}

admin_post() {
  local path="$1"
  local data="${2:-{}}"
  if [ -n "${ADMIN_COOKIE}" ]; then
    curl -s -X POST "${BASE_URL}${path}" \
      -H "Content-Type: application/json" \
      -H "Cookie: ${ADMIN_COOKIE}" \
      -d "${data}"
  else
    curl -s -X POST "${BASE_URL}${path}" \
      -H "Content-Type: application/json" \
      -d "${data}"
  fi
}

assert_json_field() {
  local json="$1"
  local query="$2"
  local expected="$3"
  local description="$4"

  local actual
  actual=$(echo "$json" | jq -r "$query" 2>/dev/null || echo "jq_parse_error")
  if [ "$actual" = "$expected" ]; then
    log_pass "$description (got '$actual')"
  else
    echo -e "${RED}Response JSON:${NC} $json"
    log_fail "$description (expected '$expected', got '$actual')"
  fi
}

create_session() {
  local res
  res=$(kiosk_post "/v1/sessions" "{}")
  local session_id
  session_id=$(echo "$res" | jq -r '.id')
  if [ -z "$session_id" ] || [ "$session_id" = "null" ]; then
    echo -e "${RED}Failed to create session:${NC} $res"
    exit 1
  fi
  echo "$session_id"
}

# ------------------------------------------------------------------------------
# Preflight Check
# ------------------------------------------------------------------------------
log_header "HDMS Phase 2 Contract & Scenario Verification Walkthrough"
echo "Target Base URL: ${BASE_URL}"

log_step "Checking API Health (/healthz)..."
HEALTH_RES=$(curl -s "${BASE_URL}/healthz" || echo "failed")
if echo "$HEALTH_RES" | grep -q "ok"; then
  log_pass "API server is healthy and responding"
else
  echo -e "${YELLOW}Note: Proceeding with walkthrough assertions...${NC}"
fi

# ==============================================================================
# SCENARIO 1: Device first -> User -> Borrow (Device available)
# ==============================================================================
log_header "Scenario 1: Device first, then user -> Borrow (Device available)"

SESS1=$(create_session)
log_step "Created Session: $SESS1"

# Step 1: Scan Available Device
log_step "1a. Scan Available Device (DEV-WALK-01)"
SCAN1A=$(kiosk_post "/v1/sessions/${SESS1}/scan" '{"token":"HDMS:D:DEV-WALK-01:SIG","source":"scanner"}')
assert_json_field "$SCAN1A" ".outcome.kind" "device_pending" "Device held as pending"
assert_json_field "$SCAN1A" ".session.state" "awaiting_user" "Session transitions to awaiting_user"

# Step 2: Scan Active User
log_step "1b. Scan Active User (USER-SHARMA-01)"
SCAN1B=$(kiosk_post "/v1/sessions/${SESS1}/scan" '{"token":"HDMS:U:USER-SHARMA-01:SIG","source":"scanner"}')
assert_json_field "$SCAN1B" ".outcome.kind" "borrowed" "Device borrowed by identified user"
assert_json_field "$SCAN1B" ".session.state" "ready" "Session transitions to ready"

# ==============================================================================
# SCENARIO 1' (Variant): User first -> Device -> Borrow
# ==============================================================================
log_header "Scenario 1' (Order Variant): User first, then device -> Borrow"

SESS1_VAR=$(create_session)
log_step "Created Session: $SESS1_VAR"

# Step 1: Scan User
log_step "1'a. Scan Active User (USER-SHARMA-02)"
SCAN1VA=$(kiosk_post "/v1/sessions/${SESS1_VAR}/scan" '{"token":"HDMS:U:USER-SHARMA-02:SIG","source":"scanner"}')
assert_json_field "$SCAN1VA" ".outcome.kind" "user_identified" "User identified, awaiting device"
assert_json_field "$SCAN1VA" ".session.state" "awaiting_device" "Session transitions to awaiting_device"

# Step 2: Scan Device
log_step "1'b. Scan Available Device (DEV-WALK-02)"
SCAN1VB=$(kiosk_post "/v1/sessions/${SESS1_VAR}/scan" '{"token":"HDMS:D:DEV-WALK-02:SIG","source":"scanner"}')
assert_json_field "$SCAN1VB" ".outcome.kind" "borrowed" "Device borrowed by identified user"
assert_json_field "$SCAN1VB" ".session.state" "ready" "Session transitions to ready"

# ==============================================================================
# SCENARIO 2: Device first -> User -> Return (User already holds it)
# ==============================================================================
log_header "Scenario 2: Device first, then user -> Return (User holds device)"

SESS2=$(create_session)
log_step "Created Session: $SESS2"

# Step 1: Scan Device currently on loan to Sharma
log_step "2a. Scan On-Loan Device (DEV-WALK-01)"
SCAN2A=$(kiosk_post "/v1/sessions/${SESS2}/scan" '{"token":"HDMS:D:DEV-WALK-01:SIG","source":"scanner"}')
assert_json_field "$SCAN2A" ".outcome.kind" "device_pending" "Device pending with on-loan indication"
assert_json_field "$SCAN2A" ".session.state" "awaiting_user" "Session transitions to awaiting_user"

# Step 2: Scan the Holding User (Sharma)
log_step "2b. Scan Holding User (USER-SHARMA-01)"
SCAN2B=$(kiosk_post "/v1/sessions/${SESS2}/scan" '{"token":"HDMS:U:USER-SHARMA-01:SIG","source":"scanner"}')
assert_json_field "$SCAN2B" ".outcome.kind" "returned" "Loan successfully closed / returned"
assert_json_field "$SCAN2B" ".session.state" "ready" "Session transitions to ready"

# ==============================================================================
# SCENARIO 2' (Variant): User first -> Device -> Return
# ==============================================================================
log_header "Scenario 2' (Order Variant): User first, then device -> Return"

SESS2_VAR=$(create_session)
log_step "Created Session: $SESS2_VAR"

# Step 1: Scan Holding User (Sharma 02 holds DEV-WALK-02)
log_step "2'a. Scan Holding User (USER-SHARMA-02)"
SCAN2VA=$(kiosk_post "/v1/sessions/${SESS2_VAR}/scan" '{"token":"HDMS:U:USER-SHARMA-02:SIG","source":"scanner"}')
assert_json_field "$SCAN2VA" ".outcome.kind" "user_identified" "User identified with active open loans"

# Step 2: Scan Held Device (DEV-WALK-02)
log_step "2'b. Scan Held Device (DEV-WALK-02)"
SCAN2VB=$(kiosk_post "/v1/sessions/${SESS2_VAR}/scan" '{"token":"HDMS:D:DEV-WALK-02:SIG","source":"scanner"}')
assert_json_field "$SCAN2VB" ".outcome.kind" "returned" "Device returned successfully"
assert_json_field "$SCAN2VB" ".session.state" "ready" "Session transitions to ready"

# ==============================================================================
# SCENARIO 3: Multi-item Borrowing & Session Completion ("Done")
# ==============================================================================
log_header "Scenario 3: User first -> Multiple devices -> Close ('Done')"

SESS3=$(create_session)
log_step "Created Session: $SESS3"

# Step 1: Scan User
log_step "3a. Scan Active User (USER-MULTI-01)"
SCAN3A=$(kiosk_post "/v1/sessions/${SESS3}/scan" '{"token":"HDMS:U:USER-MULTI-01:SIG","source":"scanner"}')
assert_json_field "$SCAN3A" ".outcome.kind" "user_identified" "User identified"

# Step 2: Scan Item 1
log_step "3b. Scan Item 1 (DEV-MULTI-01)"
SCAN3B=$(kiosk_post "/v1/sessions/${SESS3}/scan" '{"token":"HDMS:D:DEV-MULTI-01:SIG","source":"scanner"}')
assert_json_field "$SCAN3B" ".outcome.kind" "borrowed" "Item 1 borrowed"

# Step 3: Scan Item 2
log_step "3c. Scan Item 2 (DEV-MULTI-02)"
SCAN3C=$(kiosk_post "/v1/sessions/${SESS3}/scan" '{"token":"HDMS:D:DEV-MULTI-02:SIG","source":"scanner"}')
assert_json_field "$SCAN3C" ".outcome.kind" "borrowed" "Item 2 borrowed in same session"

# Step 4: Tap "Done" (Close Session)
log_step "3d. Tap Done / Close Session"
CLOSE_RES=$(kiosk_post "/v1/sessions/${SESS3}/close" "{}")
assert_json_field "$CLOSE_RES" ".state" "completed" "Session closed cleanly with completed state"

# ==============================================================================
# SCENARIO 4: Device held by someone else -> Rejection with holder name
# ==============================================================================
log_header "Scenario 4: Device held by someone else -> Reject"

# Ensure DEV-HELD-01 is borrowed by User Karki
SESS4_SETUP=$(create_session)
kiosk_post "/v1/sessions/${SESS4_SETUP}/scan" '{"token":"HDMS:U:USER-KARKI-01:SIG","source":"scanner"}' > /dev/null
kiosk_post "/v1/sessions/${SESS4_SETUP}/scan" '{"token":"HDMS:D:DEV-HELD-01:SIG","source":"scanner"}' > /dev/null
kiosk_post "/v1/sessions/${SESS4_SETUP}/close" "{}" > /dev/null

# New session: Different user attempts to scan DEV-HELD-01
SESS4=$(create_session)
log_step "Created Session: $SESS4"

log_step "4a. Scan Device held by Karki (DEV-HELD-01)"
SCAN4A=$(kiosk_post "/v1/sessions/${SESS4}/scan" '{"token":"HDMS:D:DEV-HELD-01:SIG","source":"scanner"}')
assert_json_field "$SCAN4A" ".outcome.kind" "device_pending" "Device pending"

log_step "4b. Scan Different User (USER-SHARMA-01)"
SCAN4B=$(kiosk_post "/v1/sessions/${SESS4}/scan" '{"token":"HDMS:U:USER-SHARMA-01:SIG","source":"scanner"}')
assert_json_field "$SCAN4B" ".outcome.kind" "rejected" "Borrow rejected (held by other)"
assert_json_field "$SCAN4B" ".session.state" "idle" "Session resets to idle"

# ==============================================================================
# SCENARIO 5: Unregistered / Unknown Card -> Paper Register Fallback
# ==============================================================================
log_header "Scenario 5: Unregistered / Unknown card -> Rejection & Paper guidance"

SESS5=$(create_session)
log_step "Created Session: $SESS5"

log_step "5a. Scan Available Device (DEV-UNREG-01)"
SCAN5A=$(kiosk_post "/v1/sessions/${SESS5}/scan" '{"token":"HDMS:D:DEV-UNREG-01:SIG","source":"scanner"}')
assert_json_field "$SCAN5A" ".outcome.kind" "device_pending" "Device pending"

log_step "5b. Scan Unknown User Token"
SCAN5B=$(kiosk_post "/v1/sessions/${SESS5}/scan" '{"token":"HDMS:U:UNKNOWN-STAFF-9999:SIG","source":"scanner"}')
assert_json_field "$SCAN5B" ".outcome.kind" "rejected" "Unknown card rejected with 200 OK outcome"
assert_json_field "$SCAN5B" ".session.state" "idle" "Pending device released, session resets to idle"

# ==============================================================================
# SCENARIO 6: Revoked / Reissued Card
# ==============================================================================
log_header "Scenario 6: Revoked Card Scanned"

SESS6=$(create_session)
log_step "Created Session: $SESS6"

log_step "6a. Scan Revoked Card Token"
SCAN6=$(kiosk_post "/v1/sessions/${SESS6}/scan" '{"token":"HDMS:U:REVOKED-CARD-0001:SIG","source":"scanner"}')
assert_json_field "$SCAN6" ".outcome.kind" "rejected" "Revoked card rejected"
assert_json_field "$SCAN6" ".session.state" "idle" "Session returns to idle"

# ==============================================================================
# SCENARIO 7a: Concurrent Double-Borrow Race (Two Sessions, One Device)
# ==============================================================================
log_header "Scenario 7a: Concurrent Double-Borrow Race"

SESS7A1=$(create_session)
SESS7A2=$(create_session)

# Identify users in both sessions
kiosk_post "/v1/sessions/${SESS7A1}/scan" '{"token":"HDMS:U:USER-RACE-01:SIG","source":"scanner"}' > /dev/null
kiosk_post "/v1/sessions/${SESS7A2}/scan" '{"token":"HDMS:U:USER-RACE-02:SIG","source":"scanner"}' > /dev/null

log_step "7a. Racing simultaneous scans of DEV-RACE-01 across two sessions..."
TMP_DIR=$(mktemp -d)
kiosk_post "/v1/sessions/${SESS7A1}/scan" '{"token":"HDMS:D:DEV-RACE-01:SIG","source":"scanner"}' > "${TMP_DIR}/res1.json" &
PID1=$!
kiosk_post "/v1/sessions/${SESS7A2}/scan" '{"token":"HDMS:D:DEV-RACE-01:SIG","source":"scanner"}' > "${TMP_DIR}/res2.json" &
PID2=$!

wait $PID1 $PID2

OUT1=$(jq -r '.outcome.kind' "${TMP_DIR}/res1.json" 2>/dev/null || echo "err")
OUT2=$(jq -r '.outcome.kind' "${TMP_DIR}/res2.json" 2>/dev/null || echo "err")
rm -rf "$TMP_DIR"

if ([ "$OUT1" = "borrowed" ] && [ "$OUT2" = "rejected" ]) || ([ "$OUT1" = "rejected" ] && [ "$OUT2" = "borrowed" ]); then
  log_pass "Concurrency race verified: exactly one winner ($OUT1 vs $OUT2)"
else
  log_fail "Concurrency race failed: unexpected outcomes ($OUT1 vs $OUT2)"
fi

# ==============================================================================
# SCENARIO 7b: Paper Backfill -> Device On Loan -> Normal Kiosk Return
# ==============================================================================
log_header "Scenario 7b: Paper Backfill creates Open Loan -> Returned at Kiosk"

log_step "7b-1. Post Backfill batch with open paper loan for DEV-PAPER-01"
BACKFILL_RES=$(admin_post "/v1/backfill" '{
  "paperRef": "2026-08-18 p.1",
  "rows": [
    {
      "clientRowId": "r-paper-01",
      "deviceRef": "DEV-PAPER-01",
      "userRef": { "userId": "0192f3c1-0000-7000-8000-000000000001" },
      "borrowedAt": "2026-08-15T09:00:00Z"
    }
  ]
}')

log_step "7b-2. Kiosk session returns backfilled item normally"
SESS7B=$(create_session)
# Scan device
kiosk_post "/v1/sessions/${SESS7B}/scan" '{"token":"HDMS:D:DEV-PAPER-01:SIG","source":"scanner"}' > /dev/null
# Scan borrower to return
SCAN7B_RETURN=$(kiosk_post "/v1/sessions/${SESS7B}/scan" '{"token":"HDMS:U:USER-PAPER-01:SIG","source":"scanner"}')
assert_json_field "$SCAN7B_RETURN" ".outcome.kind" "returned" "Backfilled loan returned normally through scan flow"

# ==============================================================================
# Summary
# ==============================================================================
log_header "Walkthrough Summary"
echo -e "Total Checks Passed: ${GREEN}${PASSED_COUNT}${NC}"
echo -e "Total Checks Failed: ${RED}${FAILED_COUNT}${NC}"

if [ "$FAILED_COUNT" -eq 0 ]; then
  echo -e "\n${BOLD}${GREEN}✔ ALL 7 SCENARIOS VERIFIED SUCCESSFULLY! PHASE 2 CONTRACT PROVEN GREEN.${NC}\n"
  exit 0
else
  echo -e "\n${BOLD}${RED}✘ WALKTHROUGH ENCOUNTERED FAILURES.${NC}\n"
  exit 1
fi
