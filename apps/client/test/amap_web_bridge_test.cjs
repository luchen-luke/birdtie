// Local fake SDK only. No browser, loader network, provider, key or npm module.
'use strict';
const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = fs.readFileSync(path.join(__dirname, '..', 'web', 'birdtie_amap.js'), 'utf8');
class Element {
  constructor(tag) { this.tagName = tag; this.style = {}; this.dataset = {}; this.attrs = {}; this.children = []; this.events = {}; this.isConnected = true; this.clientWidth = 600; this.clientHeight = 800; this.title = ''; this._text = ''; }
  appendChild(x) { x.parent = this; this.children.push(x); return x; }
  replaceChildren() { for (const child of this.children) child.parent = undefined; this.children = []; this._text = ''; }
  remove() { if (this.parent) this.parent.children = this.parent.children.filter(x => x !== this); this.parent = undefined; }
  set textContent(x) { this.replaceChildren(); this._text = x; }
  get textContent() { return this._text; }
  setAttribute(k, v) { this.attrs[k] = v; }
  removeAttribute(k) { delete this.attrs[k]; if (k === 'src') delete this.src; }
  addEventListener(k, v) { (this.events[k] ??= []).push(v); }
  removeEventListener(k, v) { this.events[k] = (this.events[k] ?? []).filter(x => x !== v); }
  fire(k, extra = {}) { for (const f of this.events[k] ?? []) f({detail: 0, stopPropagation() {}, preventDefault() {}, ...extra}); }
}
const deferred = () => { let resolve; const promise = new Promise(r => { resolve = r; }); return {promise, resolve}; };
function fixture({lateSDK = false, lateLoader = false, holdCoordinate = false, badBatch = false} = {}) {
  const container = new Element('div'), raf = [], maps = [], conversions = [], scripts = [], timers = new global.Map();
  const timeout = (fn, delay) => { const id = setTimeout(() => { timers.delete(id); fn(); }, delay); timers.set(id, {fn, delay}); return id; };
  const clear = id => { timers.delete(id); clearTimeout(id); };
  const sdkWait = deferred(); let loadCalls = 0;
  class LngLat { constructor(lng, lat) { this.lng = lng; this.lat = lat; } getLng() { return this.lng; } getLat() { return this.lat; } }
  class Pixel { constructor(x, y) { this.x = x; this.y = y; } }
  class Map {
    constructor(element, config) { this.element = element; this.center = config.center; this.zoom = config.zoom; this.events = {}; this.shiftX = this.shiftY = 0; this.projects = 0; this.destroyed = false; maps.push(this); }
    on(k, f) { (this.events[k] ??= []).push(f); } off(k, f) { this.events[k] = (this.events[k] ?? []).filter(x => x !== f); }
    emit(k) { for (const f of this.events[k] ?? []) f(); }
    lngLatToContainer(p) { this.projects++; return new Pixel(280 + (p.lng - this.center.lng) * 400 + (p.lat - this.center.lat) ** 2 * 37 + this.shiftX, 400 - (p.lat - this.center.lat) * 600 + (p.lng - this.center.lng) ** 2 * 23 + this.shiftY); }
    getZoom() { return this.zoom; } setZoomAndCenter(zoom, p) { this.zoom = zoom; this.center = p; }
    containerToLngLat(p) { return new LngLat(this.center.lng + (p.x - 300) / 400, this.center.lat - (p.y - 400) / 600); }
    setBounds(b) { this.bounds = b; } setCenter(p) { this.center = p; }
    destroy() { this.destroyed = true; }
  }
  const sdk = {Map, Pixel, Bounds: class {constructor(sw, ne) {this.sw = sw; this.ne = ne;}},
    convertFrom: (coords, kind, callback) => {
      assert.equal(kind, 'gps');
      const batch = Array.isArray(coords[0]) ? coords : [coords]; assert.ok(batch.length <= 40);
      const record = {coords: [...coords], batch, callback, deliver() {
        const points = batch.map(c => new LngLat(c[0] + .006, c[1] + .003));
        callback('complete', {locations: badBatch && batch.length > 1 ? points.slice(0, -1) : points});
      }};
      conversions.push(record);
      if (!(holdCoordinate && coords[0] === 121.4 && coords[1] === 31.2)) record.deliver();
    }};
  const api = {load: () => { loadCalls++; return lateSDK ? sdkWait.promise : Promise.resolve(sdk); }};
  const window = lateLoader ? {} : {AMapLoader: api};
  const document = {head: {appendChild(script) { scripts.push(script); if (!lateLoader) throw new Error('external loader forbidden'); }}, getElementById: () => container, createElement: tag => new Element(tag), createElementNS: (_, tag) => new Element(tag)};
  vm.runInNewContext(source, {window, document, URL, Map: global.Map, Set, Promise, JSON, Number, Math, Object, Error,
    setTimeout: timeout, clearTimeout: clear, requestAnimationFrame: f => raf.push(f)}, {filename: 'birdtie_amap.js'});
  const frames = [], selected = [], statuses = [], viewports = [], coordinates = [];
  const handle = window.birdtieAMapMount('test', JSON.stringify({key: 'synthetic-public', serviceHost: 'https://proxy.example.invalid/_AMapService', latitude: 30, longitude: 120, zoom: 14}),
    v => frames.push(JSON.parse(v)), v => selected.push(JSON.parse(v)), v => statuses.push(JSON.parse(v)), () => {},
    v => viewports.push(JSON.parse(v)), v => coordinates.push(JSON.parse(v)));
  const update = (revision, markers, scope) => window.birdtieAMapUpdate(handle, JSON.stringify({schema: 'birdtie-amap-source-v1', revision, markers, scope}));
  const buttons = () => container.children.flatMap(x => x.children).filter(x => x.tagName === 'button');
  const layout = (frame = frames.at(-1)) => {
    if (!frame) return;
    window.birdtieAMapLayout(handle, JSON.stringify({schema: 'birdtie-amap-layout-v1', revision: frame.revision, frame: frame.frame,
      markers: frame.positions.map(p => ({id: p.id, dx: 0, dy: 0,
        selected: false, dense: true, showLabel: false}))}));
  };
  async function flush() { for (let i = 0; i < 150; i++) { await Promise.resolve(); await Promise.resolve(); for (const f of raf.splice(0)) f(); } }
  return {window, container, maps, conversions, scripts, frames, selected, statuses, viewports, coordinates, handle, update, buttons, layout, flush,
    loadCalls: () => loadCalls, finishSDK: () => sdkWait.resolve(sdk), finishLoader: () => { window.AMapLoader = api; scripts[0].onload(); },
    timerCount: () => timers.size, expire: delay => { for (const [id, timer] of [...timers]) if (timer.delay === delay) { clear(id); timer.fn(); } },
    destroy: () => window.birdtieAMapDestroy(handle)};
}
const item = (id, kind = 'moment', title = '当前合法对象') => ({id, kind, title, subtitle: '公开场地', latitude: 31.2, longitude: 121.4, precision: 'point'});
test('actual nonlinear SDK pixels and 115 original ID buttons, with stable nodes on mapmove', async () => {
  const f = fixture(); const markers = [item('place:anchor', 'place'), ...Array.from({length: 114}, (_, i) => item(`moment:${i}`))];
  try {
    f.update(1, markers); await f.flush(); f.layout(); assert.equal(f.buttons().length, 115); assert.equal(f.maps.length, 1);
    assert.equal(f.conversions.length, 2, 'one public center plus one deduplicated original point');
    const before = f.frames.at(-1), buttons = [...f.buttons()];
    for (const b of buttons) { assert.equal(b.style.width, '48px'); assert.equal(b.style.height, '48px'); b.fire('click'); }
    assert.equal(f.container.children.flatMap(x => x.children).filter(x => x.tagName === 'svg').length, 0, 'no leader SVG');
    assert.deepEqual(new Set(f.selected.map(x => x.id)), new Set(markers.map(x => x.id)));
    const current = f.maps[0], expected = current.lngLatToContainer({lng: 121.406, lat: 31.203});
    assert.ok(Math.abs(before.positions[0].x - expected.x) < 1e-8); assert.ok(Math.abs(before.positions[0].y - expected.y) < 1e-8);
    for (const b of buttons) { assert.ok(Math.abs(parseFloat(b.style.left) + 24 - expected.x) < 1e-8); assert.ok(Math.abs(parseFloat(b.style.top) + 24 - expected.y) < 1e-8); }
    current.shiftX = 93; current.shiftY = -17; current.emit('mapmove'); await f.flush(); f.layout();
    const after = f.frames.at(-1); assert.equal(after.revision, before.revision);
    assert.ok(Math.abs(after.positions[0].x - before.positions[0].x - 93) < 1e-8);
    assert.deepEqual(f.buttons(), buttons); assert.equal(f.maps.length, 1);
  } finally { f.destroy(); }
});
test('source withdrawal clears DOM/title/pixels synchronously; source and identity ABA retire saved clicks', async () => {
  const f = fixture();
  try {
    f.update(1, [item('moment:same')]); await f.flush(); f.layout();
    const old = f.buttons()[0], click = old.events.click[0], fire = () => click({detail: 0, stopPropagation() {}, preventDefault() {}});
    f.update(2, []); assert.equal(f.buttons().length, 0); assert.equal(old.title, ''); assert.equal(old.attrs['aria-label'], ''); assert.equal(old.style.left, '');
    fire(); assert.equal(f.selected.length, 0);
    f.update(3, [item('moment:same')]); await f.flush(); f.layout(); fire(); assert.equal(f.selected.length, 0);
    const restored = f.buttons()[0], restoredClick = restored.events.click[0];
    f.update(4, [item('moment:same')]); f.update(5, [item('moment:same')]); await f.flush(); f.layout();
    restoredClick({detail: 0, stopPropagation() {}, preventDefault() {}}); assert.equal(f.selected.length, 0);
    f.buttons()[0].fire('click'); assert.equal(f.selected[0].revision, 5); assert.equal(f.maps.length, 1);
  } finally { f.destroy(); }
});
test('retired nonzero ring layout is rejected and no leader nodes can return', async () => {
  const f = fixture();
  try {
    f.update(1, [item('place:anchor', 'place'), item('moment:same')]);
    await f.flush(); f.layout();
    const frame = f.frames.at(-1), before = f.buttons().map(b => [b.style.left, b.style.top]);
    f.window.birdtieAMapLayout(f.handle, JSON.stringify({schema: 'birdtie-amap-layout-v1',
      revision: frame.revision, frame: frame.frame,
      markers: frame.positions.map((p, i) => ({id: p.id, dx: i ? 28 : 0, dy: 0,
        selected: false, dense: true, showLabel: false}))}));
    assert.deepEqual(f.buttons().map(b => [b.style.left, b.style.top]), before);
    assert.equal(f.container.children.flatMap(x => x.children).filter(x => x.tagName === 'svg').length, 0);
    for (const b of f.buttons()) b.fire('click');
    assert.deepEqual(new Set(f.selected.map(x => x.id)), new Set(['place:anchor', 'moment:same']));
  } finally { f.destroy(); }
});
test('late coordinates never restore withdrawn source, and fresh source can use the same ID', async () => {
  const f = fixture({holdCoordinate: true});
  try {
    f.update(1, [item('moment:same')]); await f.flush();
    const late = f.conversions.find(x => x.coords[0] === 121.4); assert.ok(late);
    f.update(2, []); late.deliver(); await f.flush(); f.layout();
    assert.equal(f.buttons().length, 0); assert.ok(f.frames.every(x => x.revision !== 1));
    f.update(3, [item('moment:same', 'moment', '最新合法标题')]); await f.flush();
    f.conversions.at(-1).deliver(); await f.flush(); f.layout();
    assert.equal(f.buttons()[0].title, '最新合法标题');
  } finally { f.destroy(); }
});
test('dispose before SDK and loader completion creates no map or late callback', async () => {
  for (const option of ['lateSDK', 'lateLoader']) {
    const f = fixture({[option]: true}); f.update(1, [item('moment:retired')]); await f.flush(); f.destroy();
    if (option === 'lateSDK') f.finishSDK(); else f.finishLoader();
    await f.flush(); assert.equal(f.maps.length, 0); assert.equal(f.frames.length, 0); assert.equal(f.statuses.length, 0);
    if (option === 'lateLoader') assert.equal(f.loadCalls(), 0);
  }
});
test('reader covers native map interaction, cancelled drag cannot select and dispose destroys exactly once', async () => {
  const f = fixture(); f.update(1, [item('moment:one')]); await f.flush(); f.layout();
  const b = f.buttons()[0];
  f.window.birdtieAMapReaderVisible(f.handle, true); b.fire('click'); assert.equal(f.selected.length, 0); assert.equal(f.container.style.pointerEvents, 'none');
  f.window.birdtieAMapReaderVisible(f.handle, false); b.fire('pointerdown', {clientX: 0, clientY: 0}); b.fire('pointermove', {clientX: 50, clientY: 50}); b.fire('click', {detail: 1}); assert.equal(f.selected.length, 0);
  b.fire('click', {detail: 0}); assert.equal(f.selected.length, 1);
  const map = f.maps[0]; f.destroy(); f.destroy(); assert.equal(map.destroyed, true); assert.equal(f.buttons().length, 0);
  map.emit('mapmove'); await f.flush(); assert.equal(f.frames.length, 1);
});
test('114 distinct original coordinates use bounded deduplicated 40/40/34 conversion batches', async () => {
  const f = fixture();
  try {
    const markers = Array.from({length: 114}, (_, i) => ({...item(`moment:${i}`), longitude: 121.4 + i * .0001, latitude: 31.2 + i * .0001}));
    f.update(1, [...markers, {...markers[0], id: 'place:duplicate', kind: 'place'}]);
    await f.flush(); f.layout();
    assert.deepEqual(f.conversions.slice(1).map(x => x.batch.length), [40, 40, 34]);
    assert.equal(f.frames.at(-1).positions.length, 115); assert.equal(f.buttons().length, 115);
  } finally { f.destroy(); }
});
test('failed batch length never borrows prior coordinate cache or renders a partial current source', async () => {
  const f = fixture({badBatch: true});
  try {
    f.update(1, [item('moment:old')], 1); await f.flush(); f.layout(); assert.equal(f.buttons().length, 1);
    f.update(2, [item('moment:old'), {...item('moment:next'), longitude: 121.5}], 2);
    assert.equal(f.buttons().length, 0); await f.flush(); f.layout();
    assert.equal(f.buttons().length, 0); assert.equal(f.statuses.at(-1).ready, false);
    assert.ok(f.frames.every(x => x.revision !== 2));
  } finally { f.destroy(); }
});
test('same-scope source updates retain unchanged nodes and coordinate conversions; stale clicks cannot revive', async () => {
  const f = fixture();
  try {
    f.update(1, [item('owned-map:anchor:first'), item('owned-map:anchor:second')], 1);
    await f.flush(); f.layout();
    const first = f.buttons()[0], stale = first.events.click[0], conversions = f.conversions.length;
    f.update(2, [item('owned-map:anchor:first', 'moment', '新授权标题')], 1);
    assert.equal(f.buttons().length, 1); assert.equal(f.buttons()[0], first);
    await f.flush(); f.layout();
    assert.equal(f.conversions.length, conversions); assert.equal(first.title, '新授权标题');
    stale({detail: 0, stopPropagation() {}, preventDefault() {}}); assert.equal(f.selected.length, 0);
    first.fire('click'); assert.equal(f.selected.at(-1).revision, 2);
  } finally { f.destroy(); }
});
test('initial and burst wheel/rotation settled frames publish actual SDK corners and latest zoom once', async () => {
  const f = fixture();
  try {
    f.update(1, []); await f.flush(); f.expire(180);
    assert.equal(f.viewports.length, 1); assert.equal(f.viewports[0].points.length, 5);
    const map = f.maps[0]; map.zoom = 12.5;
    for (const event of ['zoomchange', 'mapmove', 'rotatechange', 'zoomend', 'rotateend']) map.emit(event);
    await f.flush(); f.expire(180);
    assert.equal(f.viewports.length, 2); assert.equal(f.viewports[1].zoom, 12.5);
    assert.deepEqual(f.viewports[1].points[0], [map.center.lng - .75, map.center.lat + 400 / 600]);
    map.emit('moveend'); f.destroy(); f.expire(180); assert.equal(f.viewports.length, 2);
  } finally { f.destroy(); }
});
test('user drag retires queued focus while marker coordinate conversion is still pending', async () => {
  const f = fixture({holdCoordinate: true});
  try {
    f.update(1, [item('owned-map:anchor:pending')]);
    f.window.birdtieAMapUpdate(f.handle, JSON.stringify({schema: 'birdtie-amap-source-v1', revision: 1,
      focus: {serial: 1, latitude: 31.2, longitude: 121.4, zoom: 14, fit: false, padding: [0, 0, 0, 0]}}));
    await f.flush();
    const pending = f.conversions.find(x => x.coords[0] === 121.4); assert.ok(pending);
    const map = f.maps[0];
    f.container.fire('pointerdown'); map.emit('movestart');
    map.center = {lng: 120.5, lat: 30.5}; map.zoom = 11;
    map.emit('mapmove'); f.container.fire('pointerup'); map.emit('moveend');
    pending.deliver(); await f.flush(); f.expire(180);
    assert.equal(map.center.lng, 120.5); assert.equal(map.center.lat, 30.5); assert.equal(map.zoom, 11);
    assert.equal(f.viewports.length, 1); assert.deepEqual(f.viewports[0].points[4], [120.5, 30.5]);
  } finally { f.destroy(); }
});
test('leased owned cover updates without null legacy photo metadata, expired bytes clear on next grant', async () => {
  const f = fixture();
  try {
    const cover = {...item('owned-map:anchor:photo'), ownedContent: true, mediaKey: 'synthetic:v1',
      mediaPNG: 'iVBORw0KGgoAAA==', mediaSelectedPNG: 'iVBORw0KGgoBBB==', mediaDPR: 2,
      mediaExpiresAt: Date.now() + 30000};
    f.update(1, [cover], 1); await f.flush(); f.layout();
    const button=f.buttons()[0], image=button.children[0].children[0];
    assert.equal(image.style.display, ''); assert.ok(image.src.startsWith('data:image/png'));
    f.update(2, [{...cover, mediaExpiresAt: Date.now()-1}], 1);
    assert.equal(f.buttons()[0],button);assert.equal(image.src,undefined);
    await f.flush();f.layout();assert.equal(image.style.display,'none');
  } finally { f.destroy(); }
});
test('display task cluster is bounded transport with exact current members and retires when a member is removed', async () => {
  const f=fixture();
  try {
    f.update(1,[item('task:a'),item('task:b')],1);await f.flush();
    const frame=f.frames.at(-1),id='display-cluster:synthetic';
    f.window.birdtieAMapLayout(f.handle,JSON.stringify({schema:'birdtie-amap-layout-v1',revision:frame.revision,frame:frame.frame,
      markers:[{id,dx:0,dy:0,selected:false,dense:false,showLabel:false,
        cluster:{id,kind:'cluster',title:'2项内容',subtitle:'',memberIDs:['task:a','task:b'],anchorID:'task:a',contentCount:2,precision:'area'}}]}));
    assert.equal(f.buttons().length,1);f.buttons()[0].fire('click');assert.equal(f.selected.at(-1).id,id);
    f.update(2,[item('task:a')],1);assert.equal(f.buttons().length,0);
    await f.flush();f.layout();assert.equal(f.buttons()[0].dataset.birdtieMapId,'task:a');
  }finally{f.destroy();}
});
test('owned content and aggregate retain square full-size targets, counts and explicit map coordinate selection', async () => {
  const f = fixture();
  try {
    f.update(1, [{...item('owned-map:anchor:first'), ownedContent: true},
      {...item('owned-map:cluster:second', 'cluster'), ownedContent: true, contentCount: 7, anchorCount: 9, precision: 'area'}]);
    await f.flush(); f.layout();
    for (const button of f.buttons()) {
      assert.equal(button.style.width, '48px'); assert.equal(button.children[0].style.width, '28px');
      assert.equal(button.children[0].style.borderRadius, '4px');
    }
    assert.equal(f.buttons()[1].children[0].children.at(-1).textContent, '7');
    f.window.birdtieAMapUpdate(f.handle, JSON.stringify({schema: 'birdtie-amap-source-v1', revision: 1, coordinateSelection: true}));
    const click = f.maps[0].events.click[0]; click({lnglat: {lng: 121.4, lat: 31.2}});
    assert.equal(f.coordinates.length, 1); assert.equal(f.coordinates[0].revision, 1);
    f.window.birdtieAMapReaderVisible(f.handle, true); click({lnglat: {lng: 121.4, lat: 31.2}});
    assert.equal(f.coordinates.length, 1);
  } finally { f.destroy(); }
});
test('loader/SDK/coordinate deadlines report failure, ignore late callbacks and dispose clears every timer', async () => {
  for (const option of ['lateSDK', 'lateLoader', 'holdCoordinate']) {
    const f = fixture({[option]: true});
    try {
      f.update(1, [item('moment:timed-out')]); await f.flush();
      f.expire(option === 'holdCoordinate' ? 8000 : 15000); await f.flush();
      assert.equal(f.statuses.at(-1).ready, false); assert.equal(f.buttons().length, 0);
      if (option === 'lateSDK') f.finishSDK();
      else if (option === 'lateLoader') f.finishLoader();
      else f.conversions.find(x => x.coords[0] === 121.4).deliver();
      await f.flush(); assert.equal(f.frames.length, 0); assert.equal(f.buttons().length, 0);
    } finally { f.destroy(); assert.equal(f.timerCount(), 0); }
  }
});
