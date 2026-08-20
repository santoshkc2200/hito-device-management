#!/usr/bin/env bash
set -euo pipefail

# ==============================================================================
# Phase 3 Kiosk Application — Full Verification & Walkthrough
# ==============================================================================

echo "========================================================================"
echo " Starting Phase 3 (Kiosk Application) Verification Suite"
echo "========================================================================"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
FRONTEND_DIR="${REPO_ROOT}/hdms-frontend"

cd "${FRONTEND_DIR}"

echo ""
echo "[1/6] Running TypeScript Typecheck across all workspace packages..."
pnpm -r typecheck || pnpm -r exec tsc --noEmit

echo ""
echo "[2/6] Running Workspace Linter..."
pnpm -r lint

echo ""
echo "[3/6] Running Unit & Integration Tests (Vitest)..."
pnpm -r test

echo ""
echo "[4/6] Running Domain Machine Parity & Scenarios Assertion..."
pnpm --filter @hdms/domain test

echo ""
echo "[5/6] Building Production Bundles (Kiosk PWA, Admin, Scan, UI)..."
pnpm -r build

echo ""
echo "[6/6] Verifying OpenAPI Client Sync..."
cd "${REPO_ROOT}"
if command -v task &> /dev/null; then
    task generate
    if ! git diff --exit-code hdms-frontend/packages/api-client; then
        echo "ERROR: OpenAPI generated client is out of sync!"
        exit 1
    fi
    echo "OpenAPI client is in sync with schema."
else
    echo "Task not installed, skipping task generate."
fi

echo ""
echo "========================================================================"
echo " ✓ Phase 3 Verification Suite Passed Successfully!"
echo "========================================================================"
