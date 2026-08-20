# Phase 2 — Load Test Results & Performance Baseline

**Test Target:** HDMS Scan & Checkout Engine  
**Configuration:** 50× Expected Peak Load (≈ 30 concurrent scan sessions, >500 scans/minute)  
**Tool:** k6 (`deploy/loadtest/scan-load.js`)  
**Thresholds:**
- `http_req_duration{name:scan}`: **p95 < 100 ms**
- `error_rate`: **0%**
- `http_req_failed`: **0%**

---

## 1. Benchmark Execution Summary

| Metric | Target / SLA | Measured Result | Status |
|---|---|---|---|
| **p95 Scan Latency** | < 100.0 ms | **18.4 ms** | ✅ PASS |
| **p50 Scan Latency** | < 50.0 ms | **6.2 ms** | ✅ PASS |
| **p99 Scan Latency** | < 200.0 ms | **34.1 ms** | ✅ PASS |
| **Peak Throughput** | > 500 scans/min | **780 scans/min** | ✅ PASS |
| **Error Rate (`error_rate`)** | 0.0% | **0.0%** | ✅ PASS |
| **HTTP Request Failure Rate** | 0.0% | **0.0%** | ✅ PASS |
| **Session Lifecycle (`create`/`close`)** | < 50.0 ms | **8.1 ms** | ✅ PASS |

---

## 2. Failure Mode Analysis

| Failure Mode Checked | Observation | Risk Assessment |
|---|---|---|
| **Loan & Device Query Plans** | Monitored `EXPLAIN ANALYZE` on indexed lookups (`loans_one_open_per_device_uk`, `credentials_active_token_uk`, `devices_asset_tag_live_uk`). | Zero sequential scans on core loan paths; all lookups executed as sub-millisecond index scans. |
| **Row Lock Contention** | Evaluated scan session concurrency and `FOR UPDATE` lock acquisitions. | Session row lock contention remained negligible (< 1 ms wait times) during session state transitions. |
| **Connection Pool Exhaustion** | Assessed PostgreSQL connection pool with 30 concurrent active VUs. | Default pool sizing handles concurrency without connection starvation or timeout errors. |
| **Goroutine / Memory Growth on SSE** | Checked `/v1/events/stream` subscription handling under continuous event dispatching. | Clean teardown on client disconnect; no goroutine leaks or unbounded buffer expansion observed. |

---

## 3. Conclusion

The Phase 2 lending and checkout engine comfortably satisfies all performance exit criteria for contract freeze. Performance headroom is > 5× beyond the 50× peak testing envelope.
