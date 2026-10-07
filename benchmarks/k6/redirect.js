import http from 'k6/http';
import { check } from 'k6';

function positiveIntegerFromEnv(name, defaultValue) {
  const rawValue = __ENV[name];

  if (rawValue === undefined || rawValue === '') {
    return defaultValue;
  }

  const value = Number(rawValue);

  if (!Number.isInteger(value) || value <= 0) {
    throw new Error(`${name} must be a positive integer`);
  }

  return value;
}

const baseURL = __ENV.BASE_URL || 'http://127.0.0.1:8080';
const shortCode = __ENV.SHORT_CODE || '3ZB9CeC';
const expectedLocation =
  __ENV.EXPECTED_LOCATION || 'https://example.com/shortscale-phase3';

const rate = positiveIntegerFromEnv('RATE', 100);
const duration = __ENV.DURATION || '30s';
const preAllocatedVUs = positiveIntegerFromEnv('PRE_ALLOCATED_VUS', 50);
const maxVUs = positiveIntegerFromEnv('MAX_VUS', 200);

if (maxVUs < preAllocatedVUs) {
  throw new Error('MAX_VUS must be greater than or equal to PRE_ALLOCATED_VUS');
}

export const options = {
  scenarios: {
    redirect_baseline: {
      executor: 'constant-arrival-rate',
      rate,
      timeUnit: '1s',
      duration,
      preAllocatedVUs,
      maxVUs,
    },
  },

  discardResponseBodies: true,

  summaryTrendStats: [
    'avg',
    'min',
    'med',
    'p(90)',
    'p(95)',
    'p(99)',
    'max',
  ],
};

export default function () {
  const response = http.get(`${baseURL}/${shortCode}`, {
    redirects: 0,
    tags: {
      endpoint: 'redirect',
    },
  });

  check(response, {
    'redirect status is 302': (r) => r.status === 302,
    'location header is correct': (r) =>
      r.headers.Location === expectedLocation,
  });
}
