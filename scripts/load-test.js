// k6 压测脚本：核心读路径 GET /:code
//
// 用法（PowerShell）：
//   $env:CODE = "<真实短码>"; $env:VUS = "100"; k6 run scripts/load-test.js
//
// 用法（bash）：
//   CODE=abc123 VUS=100 k6 run scripts/load-test.js
//
// 结果请如实记录到 README，不要虚构数据。
import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const CODE = __ENV.CODE;
const VUS = parseInt(__ENV.VUS || '100', 10);
const DURATION = __ENV.DURATION || '30s';
export const options = {
  vus: VUS,
  duration: DURATION,
  // 默认 summary 不含 p(99)，必须显式声明，否则 handleSummary 里取不到。
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<200', 'p(99)<500'],
  },
};

// setup 在压测开始前跑一次：先确认目标短码真的返回 302，
// 否则会白跑几十秒、测出一条与缓存无关的 404 路径。
export function setup() {
  if (!CODE) {
    throw new Error(
      'CODE 未设置。先创建一条短链接拿到 code，再设置环境变量：\n' +
      '  PowerShell: $env:CODE = "abc123"\n' +
      '  bash:       export CODE=abc123'
    );
  }

  const res = http.get(`${BASE_URL}/${CODE}`, { redirects: 0 });
  if (res.status !== 302)  {
    throw new Error(
      `预检失败：GET ${BASE_URL}/${CODE} 返回 ${res.status}，期望 302。\n` +
      `响应：${String(res.body).slice(0, 200)}\n` +
      '请确认服务已启动、且该 code 确实存在（注意短码只含 0-9a-zA-Z）。'
    );
  }

  // 预检本身也顺便预热了缓存，避免首批请求撞上冷回源。
  return { target: res.headers['Location'] };
}

export default function () {
  const res = http.get(`${BASE_URL}/${CODE}`, { redirects: 0 });
  check(res, {
    'status is 302': (r) => r.status === 302,
    'has location header': (r) => !!r.headers['Location'],
  });
}

export function handleSummary(data) {
  const m = data.metrics;
  const trend = (stat) => {
    const v = m.http_req_duration && m.http_req_duration.values[stat];
    return typeof v === 'number' ? v.toFixed(2) : 'n/a';
  };
  const num = (metric, stat, fn) => {
    const v = m[metric] && m[metric].values[stat];
    return typeof v === 'number' ? (fn ? fn(v) : v) : 'n/a';
  };
  const line = (label, value) => `  ${String(label).padEnd(16)} ${value}`;

  const summary = [
    '',
    `=== GET /:code  (VUS=${VUS}, duration=${DURATION}, code=${CODE}) ===`,
    line('requests', num('http_reqs', 'count')),
    line('RPS', num('http_reqs', 'rate', (v) => v.toFixed(2))),
    line('error rate', num('http_req_failed', 'rate', (v) => (v * 100).toFixed(2) + '%')),
    line('P50 (ms)', trend('med')),
    line('P95 (ms)', trend('p(95)')),
    line('P99 (ms)', trend('p(99)')),
    line('max (ms)', trend('max')),
    '',
  ].join('\n');

  return { stdout: summary };
}
