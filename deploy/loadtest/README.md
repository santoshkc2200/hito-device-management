# HDMS Kiosk Scan Load Test

Load test script exercising the checkout engine at **50× expected peak load** (≈ 30 concurrent sessions, 500 scans/minute).

## Requirements

- [k6](https://k6.io/docs/get-started/installation/)
- HDMS backend stack running (`task dev` or docker-compose)

## Running the Load Test

```bash
# Against default local instance (http://localhost:8080)
task test:load

# Or run directly with custom parameters
k6 run -e BASE_URL=http://localhost:8080 -e KIOSK_TOKEN=your-kiosk-token deploy/loadtest/scan-load.js
```

## Target Thresholds

- `http_req_duration{name:scan}`: **p95 < 100ms**
- `error_rate`: **0%**
- `http_req_failed`: **0%**

## Things to Watch For

1. **Query Performance:** Unindexed lookups on `scan_sessions` or `loans`.
2. **Lock Contention:** Unnecessary lock waits or deadlocks on the session row.
3. **Connection Pool:** PostgreSQL connection pool exhaustion under concurrency.
4. **Memory / Goroutines:** Goroutine leaks on SSE subscriber connections.
