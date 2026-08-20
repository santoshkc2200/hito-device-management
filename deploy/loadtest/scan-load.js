import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Rate } from 'k6/metrics';

// Load test at 50× expected peak (≈ 30 concurrent sessions, 500 scans/min)
// Target: p95 scan latency < 100 ms locally and 0 error rate.
//
// Failure modes to watch for during runs:
//  - Accidental O(n²) queries on loan lookups
//  - Missing indexes on scan_sessions or loans tables
//  - Lock waits / deadlocks on the session row
//  - Postgres connection-pool exhaustion
//  - Goroutine / memory growth on SSE stream endpoints

const scanDuration = new Trend('scan_duration', true);
const errorRate = new Rate('error_rate');

export const options = {
  scenarios: {
    scan_checkout_flow: {
      executor: 'ramping-vus',
      startVUs: 1,
      stages: [
        { duration: '15s', target: 10 },
        { duration: '30s', target: 30 }, // 50× expected peak (~30 concurrent sessions)
        { duration: '15s', target: 0 },
      ],
      gracefulRampDown: '5s',
    },
  },
  thresholds: {
    'http_req_duration{name:scan}': ['p(95)<100'], // p95 scan latency < 100ms
    'error_rate': ['rate==0'],                    // 0% error rate
    'http_req_failed': ['rate==0'],
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const KIOSK_TOKEN = __ENV.KIOSK_TOKEN || 'kiosk-loadtest-bearer-token';

// Seed test tokens (configured during test setup or fixtures)
const USER_TOKENS = [
  'HDMS:U:LOADTEST-USER-0001:SIG1',
  'HDMS:U:LOADTEST-USER-0002:SIG2',
  'HDMS:U:LOADTEST-USER-0003:SIG3',
];

const DEVICE_TOKENS = [
  'HDMS:D:LOADTEST-DEV-0001:SIG1',
  'HDMS:D:LOADTEST-DEV-0002:SIG2',
  'HDMS:D:LOADTEST-DEV-0003:SIG3',
];

export default function () {
  const userToken = USER_TOKENS[__VU % USER_TOKENS.length];
  const devToken = DEVICE_TOKENS[__VU % DEVICE_TOKENS.length];

  const authHeaders = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${KIOSK_TOKEN}`,
  };

  // 1. Create session
  const createRes = http.post(
    `${BASE_URL}/v1/sessions`,
    JSON.stringify({}),
    { headers: authHeaders, tags: { name: 'create_session' } }
  );

  const createOk = check(createRes, {
    'session created 201': (r) => r.status === 201,
    'has session id': (r) => r.json('id') !== undefined,
  });
  errorRate.add(!createOk);

  if (!createOk) {
    sleep(1);
    return;
  }

  const sessionID = createRes.json('id');

  // 2. Scan User card (identifies user -> awaiting_device)
  const userScanRes = http.post(
    `${BASE_URL}/v1/sessions/${sessionID}/scans`,
    JSON.stringify({ token: userToken, source: 'scanner' }),
    { headers: authHeaders, tags: { name: 'scan' } }
  );
  scanDuration.add(userScanRes.timings.duration);
  const userScanOk = check(userScanRes, {
    'user scan 200': (r) => r.status === 200,
    'user outcome user_identified': (r) => r.json('outcome.kind') === 'user_identified',
  });
  errorRate.add(!userScanOk);

  sleep(0.1);

  // 3. Scan Device (borrows device -> ready)
  const borrowScanRes = http.post(
    `${BASE_URL}/v1/sessions/${sessionID}/scans`,
    JSON.stringify({ token: devToken, source: 'scanner' }),
    { headers: authHeaders, tags: { name: 'scan' } }
  );
  scanDuration.add(borrowScanRes.timings.duration);
  const borrowOk = check(borrowScanRes, {
    'borrow scan 200': (r) => r.status === 200,
  });
  errorRate.add(!borrowOk);

  sleep(0.1);

  // 4. Scan Device again (returns device -> ready)
  const returnScanRes = http.post(
    `${BASE_URL}/v1/sessions/${sessionID}/scans`,
    JSON.stringify({ token: devToken, source: 'scanner' }),
    { headers: authHeaders, tags: { name: 'scan' } }
  );
  scanDuration.add(returnScanRes.timings.duration);
  const returnOk = check(returnScanRes, {
    'return scan 200': (r) => r.status === 200,
  });
  errorRate.add(!returnOk);

  sleep(0.1);

  // 5. Close session ("Done")
  const closeRes = http.post(
    `${BASE_URL}/v1/sessions/${sessionID}/close`,
    JSON.stringify({}),
    { headers: authHeaders, tags: { name: 'close_session' } }
  );
  const closeOk = check(closeRes, {
    'close session 200': (r) => r.status === 200,
  });
  errorRate.add(!closeOk);

  sleep(0.5);
}
