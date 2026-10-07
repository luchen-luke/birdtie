import { execFileSync } from 'node:child_process';
import { writeFileSync } from 'node:fs';

const [serviceUrl, device, phase, output] = process.argv.slice(2);
if (!serviceUrl || !device || !['pan', 'keyboard', 'sheet', 'sheet-only'].includes(phase) || !output) {
  throw new Error('Usage: node profile_now_device.mjs <vm-service-ws> <adb-device> <pan|keyboard|sheet|sheet-only> <output-json>');
}

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const input = (...args) => execFileSync('adb', ['-s', device, 'shell', 'input', ...args], { stdio: 'ignore' });
const ws = new WebSocket(serviceUrl);
const pending = new Map();
let nextId = 1;
await new Promise((resolve, reject) => {
  ws.onopen = resolve;
  ws.onerror = reject;
});
ws.onmessage = (event) => {
  const message = JSON.parse(event.data);
  if (pending.has(message.id)) {
    const { resolve, reject } = pending.get(message.id);
    pending.delete(message.id);
    message.error ? reject(new Error(JSON.stringify(message.error))) : resolve(message.result);
  }
};
function call(method) {
  const id = nextId++;
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    ws.send(JSON.stringify({ jsonrpc: '2.0', id, method }));
  });
}

function durations(events, name) {
  const opened = new Map();
  const values = [];
  for (const event of events) {
    if (event.name !== name) continue;
    const key = `${event.tid}:${event.id ?? ''}`;
    if (event.ph === 'B' || event.ph === 'b') opened.set(key, event.ts);
    if ((event.ph === 'E' || event.ph === 'e') && opened.has(key)) {
      values.push((event.ts - opened.get(key)) / 1000);
      opened.delete(key);
    }
  }
  values.sort((a, b) => a - b);
  const percentile = (p) => values.length ? values[Math.ceil(values.length * p) - 1] : null;
  return {
    count: values.length,
    p50Ms: percentile(0.5),
    p95Ms: percentile(0.95),
    p99Ms: percentile(0.99),
    maxMs: values.at(-1) ?? null,
    over16_7Ms: values.filter((value) => value > 16.7).length,
  };
}

try {
  await sleep(1000);
  const before = await call('getVMTimeline');
  const startMicros = Math.max(
    ...before.traceEvents.map((event) => event.ts).filter(Number.isFinite),
  );

  if (phase === 'pan') {
    for (let i = 0; i < 8; i++) {
      input('swipe', '690', '850', '350', '860', '360');
      await sleep(200);
    }
  } else if (phase === 'keyboard') {
    execFileSync(
      'pwsh',
      ['-File', 'automation/verify_map_keyboard.ps1', '-Device', device, '-Cycles', '4'],
      { stdio: 'inherit' },
    );
  } else {
    if (phase === 'sheet') {
      input('tap', '460', '2460');
      for (let i = 0; i < 4; i++) input('keyevent', '67');
      input('text', 'badminton');
      input('keyevent', '66');
      await sleep(2200);
    }
    for (let i = 0; i < 5; i++) {
      input('swipe', '600', '1490', '600', '1160', '360');
      await sleep(300);
      input('swipe', '600', '680', '600', '1010', '360');
      await sleep(300);
    }
  }
  await sleep(1000);
  const after = await call('getVMTimeline');
  const events = after.traceEvents.filter((event) =>
    Number.isFinite(event.ts) && event.ts > startMicros,
  );
  const result = {
    phase,
    device,
    eventCount: events.length,
    flutterUiFrame: durations(events, 'Frame'),
    flutterRaster: durations(events, 'Rasterizer::DoDraw'),
    flutterBuild: durations(events, 'BUILD'),
    flutterLayout: durations(events, 'LAYOUT'),
    flutterPaint: durations(events, 'PAINT'),
  };
  writeFileSync(output, JSON.stringify(result, null, 2));
  console.log(JSON.stringify(result));
} finally {
  ws.close();
}
