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

const baseURL = __ENV.BASE_URL || 'http://127.0.0.1:18080';
const shortCode = __ENV.SHORT_CODE;
const sessionCookie = __ENV.SESSION_COOKIE;

if (!shortCode) {
  throw new Error('SHORT_CODE is required');
}

if (!sessionCookie) {
  throw new Error('SESSION_COOKIE is required');
}

const days = positiveIntegerFromEnv('DAYS', 7);
if (days < 1 || days > 90) {
  throw new Error('DAYS must be between 1 and 90');
}

const rate = positiveIntegerFromEnv('RATE', 50);
const duration = __ENV.DURATION || '10s';
const preAllocatedVUs = positiveIntegerFromEnv('PRE_ALLOCATED_VUS', 50);
const maxVUs = positiveIntegerFromEnv('MAX_VUS', 400);

if (maxVUs < preAllocatedVUs) {
  throw new Error('MAX_VUS must be greater than or equal to PRE_ALLOCATED_VUS');
}

export const options = {
  scenarios: {
    analytics_read: {
      executor: 'constant-arrival-rate',
      rate,
      timeUnit: '1s',
      duration,
      preAllocatedVUs,
      maxVUs,
    },
  },

  discardResponseBodies: false,

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
  const response = http.get(
    `${baseURL}/api/v1/urls/${encodeURIComponent(shortCode)}/analytics?days=${days}`,
    {
      headers: {
        Cookie: `shortscale_session=${sessionCookie}`,
      },
      tags: {
        endpoint: 'analytics',
        days: String(days),
      },
    },
  );

  let payload = null;

  if (response.status === 200) {
    try {
      payload = response.json();
    } catch (_) {
      payload = null;
    }
  }

  check(response, {
    'analytics status is 200': (r) => r.status === 200,
    'analytics short code matches': () =>
      payload !== null && payload.short_code === shortCode,
    'analytics window matches': () =>
      payload !== null &&
      payload.window !== undefined &&
      payload.window.days === days,
  });
}
