// Benchmark scenarios. One k6 run executes one scenario (SCENARIO) at one arrival rate (RATE requests/s) for
// DURATION, against BASE_URL. Inputs come from the seeder's inputs.json.
import http from 'k6/http';
import { check } from 'k6';

const inputs = JSON.parse(open('/bench/data/inputs.json'));
const BASE_URL = __ENV.BASE_URL;
const SCENARIO = __ENV.SCENARIO;
const RATE = parseInt(__ENV.RATE, 10);
const DURATION = __ENV.DURATION || '60s';
const API = { headers: { 'X-Api-Key': inputs.apiKey, 'Accept': 'application/json' } };
const NO_REDIRECTS = { redirects: 0 };

export const options = {
  discardResponseBodies: true,
  scenarios: {
    main: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: Math.min(Math.max(RATE, 10), 500),
      maxVUs: 1000,
    },
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

const pick = (list) => list[Math.floor(Math.random() * list.length)];
let created = 0;

const requests = {
  redirect: () => http.get(`${BASE_URL}/${pick(inputs.codes)}`, NO_REDIRECTS),
  'redirect-no-tracking': () => http.get(`${BASE_URL}/${pick(inputs.codes)}?nostat`, NO_REDIRECTS),
  create: () => http.post(`${BASE_URL}/rest/v3/short-urls`, JSON.stringify({
    longUrl: `https://example.com/bench/${__VU}/${created++}/${Math.random()}`,
    tags: [pick(inputs.tags)],
  }), { headers: { ...API.headers, 'Content-Type': 'application/json' } }),
  // The first page: a tag and search term combination may have a single page, and Shlink answers 500 for a page past
  // the last one (Pagerfanta's OutOfRangeCurrentPageException)
  list: () => http.get(`${BASE_URL}/rest/v3/short-urls?searchTerm=${pick(inputs.words)}&tags[]=${pick(inputs.tags)}`
    + '&page=1&itemsPerPage=20', API),
  'short-url-visits': () => http.get(`${BASE_URL}/rest/v3/short-urls/${pick(inputs.codes)}/visits?itemsPerPage=20`, API),
  'tags-stats': () => http.get(`${BASE_URL}/rest/v3/tags/stats?itemsPerPage=20`, API),
  'global-visits': () => http.get(`${BASE_URL}/rest/v3/visits`, API),
};

const expectedStatus = { redirect: 302, 'redirect-no-tracking': 302 };

export default function () {
  let scenario = SCENARIO;
  if (scenario === 'mixed') {
    scenario = Math.random() < 0.9 ? 'redirect' : pick(['list', 'short-url-visits']);
  }
  const res = requests[scenario]();
  check(res, { 'expected status': (r) => r.status === (expectedStatus[scenario] || 200) });
}
