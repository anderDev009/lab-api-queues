import http from 'k6/http';
import { check, sleep } from 'k6';
import exec from 'k6/execution';
import { largeDataset as syncData } from './mails-sync.js';
import { largeDataset as asyncData } from './mails-async.js';

const SYNC_URL = 'http://api-sync.lan/reservations';
const ASYNC_URL = 'http://api-async.lan/reservations';
const headers = { 'Content-Type': 'application/json' };

export const options = {
  scenarios: {
    reservations: {
      executor: 'shared-iterations',
      vus: 10,
      iterations: syncData.length,
    },
  },
  thresholds: {
    'http_req_duration{api:sync}': ['p(95)<1000'],
    'http_req_duration{api:async}': ['p(95)<500'],
    'http_req_failed{api:sync}': ['rate<0.05'],
    'http_req_failed{api:async}': ['rate<0.05'],
    'checks{api:sync}': ['rate>0.95'],
    'checks{api:async}': ['rate>0.95'],
  },
};

export default function () {
  const i = exec.scenario.iterationInTest;

  const resSync = http.post(
    SYNC_URL,
    JSON.stringify(syncData[i]),
    { headers, tags: { api: 'sync' } },
  );
  check(
    resSync,
    {
      'sync: status is 201': (r) => r.status === 201,
      'sync: responds in < 1s': (r) => r.timings.duration < 1000,
    },
    { api: 'sync' },
  );

  const resAsync = http.post(
    ASYNC_URL,
    JSON.stringify(asyncData[i]),
    { headers, tags: { api: 'async' } },
  );
  check(
    resAsync,
    {
      'async: status is 202': (r) => r.status === 202,
      'async: responds in < 500ms': (r) => r.timings.duration < 500,
    },
    { api: 'async' },
  );

  sleep(1);
}
