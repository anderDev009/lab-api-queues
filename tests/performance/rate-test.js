import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';

const SYNC_URL = 'http://api-sync.lan/reservations';
const ASYNC_URL = 'http://api-async.lan/reservations';
const headers = { 'Content-Type': 'application/json' };

const RUN_ID = __ENV.RUN_ID || Date.now().toString();
const STEP_DURATION = '30s';
const STEP_GAP_SECONDS = 35;
const RATES = [100, 500, 1000];

function buildScenarios() {
  const scenarios = {};
  RATES.forEach((rate, index) => {
    ['sync', 'async'].forEach((api) => {
      scenarios[`${api}_${rate}rps`] = {
        executor: 'constant-arrival-rate',
        exec: `${api}Api`,
        rate,
        timeUnit: '1s',
        duration: STEP_DURATION,
        startTime: `${index * STEP_GAP_SECONDS}s`,
        preAllocatedVUs: Math.ceil(rate / 2),
        maxVUs: rate * 2,
        tags: { api, rate: String(rate) },
      };
    });
  });
  return scenarios;
}

export const options = {
  scenarios: buildScenarios(),
  thresholds: {
    'http_req_duration{api:sync}': ['p(95)<1500'],
    'http_req_duration{api:async}': ['p(95)<800'],
    'http_req_failed{api:sync}': ['rate<0.05'],
    'http_req_failed{api:async}': ['rate<0.05'],
    'checks{api:sync}': ['rate>0.95'],
    'checks{api:async}': ['rate>0.95'],
  },
};

function payload(api) {
  const n = exec.scenario.iterationInTest;
  const scenario = exec.scenario.name;
  return JSON.stringify({
    email: `load.${RUN_ID}.${scenario}.${n}@${api}.example.com`,
    name: `Load ${api} ${n}`,
  });
}

export function syncApi() {
  const res = http.post(SYNC_URL, payload('sync'), { headers });
  check(res, { 'sync: status is 201': (r) => r.status === 201 });
}

export function asyncApi() {
  const res = http.post(ASYNC_URL, payload('async'), { headers });
  check(res, { 'async: status is 202': (r) => r.status === 202 });
}
