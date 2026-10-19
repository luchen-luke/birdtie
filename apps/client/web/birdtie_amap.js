// AMap 2.0 transport/projection only. Original WGS84 objects remain in Dart.
// serviceHost owns the security key; this file contains no key or credentials.
(() => {
  let loaderPromise, loaderCancel, nextHandle = 1;
  const maps = new Map();
  const finite = x => typeof x === 'number' && Number.isFinite(x);
  const alive = s => !s.retired && maps.get(s.handle) === s;
  const current = (s, revision, work) => alive(s) && s.revision === revision && s.work === work;
  const pointValid = p => p && finite(p.longitude) && finite(p.latitude) &&
    Math.abs(p.longitude) <= 180 && Math.abs(p.latitude) <= 90;
  function status(s, ready) {
    if (alive(s)) s.onStatus(JSON.stringify({schema: 'birdtie-amap-status-v1', revision: s.revision, ready}));
  }
  function loader() {
    if (window.AMapLoader) return Promise.resolve(window.AMapLoader);
    if (!loaderPromise) {
      let resolve, reject, settled = false;
      const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
      const script = document.createElement('script');
      const finish = error => {
        if (settled) return; settled = true; clearTimeout(timer);
        if (loaderPromise === promise) { loaderPromise = undefined; loaderCancel = undefined; }
        if (error) { script.remove(); reject(new Error('SDK unavailable')); }
        else resolve(window.AMapLoader);
      };
      const timer = setTimeout(() => finish(new Error('SDK timeout')), 15000);
      loaderPromise = promise; loaderCancel = () => finish(new Error('retired'));
      script.src = 'https://webapi.amap.com/loader.js';
      script.onload = () => finish(window.AMapLoader ? null : new Error('SDK unavailable'));
      script.onerror = () => finish(new Error('SDK unavailable'));
      try { document.head.appendChild(script); } catch (_) { finish(new Error('SDK unavailable')); }
      return promise;
    }
    return loaderPromise;
  }
  function attached(s) {
    return new Promise((resolve, reject) => {
      let attempts = 0;
      const check = () => {
        if (!alive(s)) return reject(new Error('retired'));
        const element = document.getElementById(s.elementID);
        if (element?.isConnected && element.clientWidth > 0 && element.clientHeight > 0) resolve(element);
        else if (++attempts < 120) requestAnimationFrame(check);
        else reject(new Error('container unavailable'));
      };
      check();
    });
  }
  function mountWait(s, promise, milliseconds = 15000) {
    return new Promise((resolve, reject) => {
      let settled = false;
      const finish = (error, result) => {
        if (settled) return; settled = true; clearTimeout(timer); s.mountPending.delete(cancel);
        if (error || !alive(s)) reject(new Error('mount unavailable')); else resolve(result);
      };
      const cancel = () => finish(new Error('retired'));
      const timer = setTimeout(() => finish(new Error('mount timeout')), milliseconds);
      s.mountPending.add(cancel);
      Promise.resolve(promise).then(value => finish(null, value), error => finish(error));
    });
  }
  function removeRoute(row) {
    try { row.line?.setMap(null); } catch (_) { /* Retired source cannot be retained. */ }
  }
  function clearRoutes(s) {
    s.pendingRoutes=[];
    clearTimeout(s.routeTimer); s.routeTimer = undefined;
    for (const row of s.routes?.values() ?? []) removeRoute(row);
    s.routes?.clear();
  }
  function routeLive(s, row) {
    return alive(s) && row.expiresAt > Date.now() &&
      s.source.get(row.fromId)?.sourceVersion === row.fromSourceVersion &&
      s.source.get(row.toId)?.sourceVersion === row.toSourceVersion;
  }
  function armRoutes(s) {
    clearTimeout(s.routeTimer); s.routeTimer = undefined;
    let deadline = Infinity;
    for (const [id, row] of s.routes) {
      if (!routeLive(s,row)) { removeRoute(row); s.routes.delete(id); }
      else deadline = Math.min(deadline,row.expiresAt);
    }
    if (Number.isFinite(deadline)) s.routeTimer = setTimeout(() => armRoutes(s), Math.max(1,deadline-Date.now()));
  }
  function syncRoutes(s, routes) {
    if (!Array.isArray(routes) || routes.length > 1536) { clearRoutes(s); return; }
    let points = 0;
    const ids = new Set();
    for (const row of routes) {
      if (typeof row.id !== 'string' || row.id.length > 1024 || ids.has(row.id) ||
        typeof row.fingerprint !== 'string' || row.fingerprint.length > 512 || row.coordinateSystem !== 'gcj02' ||
        !Number.isSafeInteger(row.expiresAt) || row.expiresAt <= Date.now() || row.expiresAt > Date.now()+31000 ||
        typeof row.selected !== 'boolean' || !Array.isArray(row.points) || row.points.length < 2 ||
        (points += row.points.length) > 24000 || row.points.some(p => !Array.isArray(p) || p.length !== 2 ||
          !finite(p[0]) || !finite(p[1]) || Math.abs(p[0]) > 180 || Math.abs(p[1]) > 90) || !routeLive(s,row)) {
        clearRoutes(s); return;
      }
      ids.add(row.id);
    }
    s.pendingRoutes=routes;
    for (const [id,row] of s.routes) if (!ids.has(id)) { removeRoute(row); s.routes.delete(id); }
    for (const value of routes) {
      const previous = s.routes.get(value.id);
      if (previous?.fingerprint === value.fingerprint && previous.selected === value.selected) {
        Object.assign(previous,value); continue;
      }
      if (typeof s.sdk?.Polyline !== 'function') continue;
      const options = {path:value.points,strokeColor:'#2c6b58',strokeWeight:3,strokeOpacity:value.selected?1:.7,
        strokeStyle:'dashed',strokeDasharray:[7,5],showDir:true,lineJoin:'round',lineCap:'round',bubble:true,zIndex:50};
      try {
        if (previous) { previous.line.setOptions(options); Object.assign(previous,value); }
        else { const line = new s.sdk.Polyline(options); line.setMap(s.map); s.routes.set(value.id,{...value,line}); }
      } catch (_) { if(previous)removeRoute(previous);s.routes.delete(value.id); }
    }
    armRoutes(s);
  }
  function clearNodes(s) {
    clearRoutes(s);
    clearTimeout(s.mediaTimer); s.mediaTimer = undefined;
    for (const row of s.nodes.values()) {
      removeNode(row);
    }
    s.nodes.clear(); s.overlay?.replaceChildren(); s.projections.clear();
  }
  function removeNode(row) {
    row.image?.removeAttribute('src');
    row.button.disabled = true; row.button.tabIndex = -1;
    row.button.title = ''; row.button.textContent = '';
    row.button.setAttribute('aria-label', '');
    row.button.style.left = ''; row.button.style.top = '';
    row.button.remove();
  }
  function remember(s, key, point) {
    s.conversions.delete(key); s.conversions.set(key, point);
    while (s.conversions.size > 320) s.conversions.delete(s.conversions.keys().next().value);
  }
  function retireSource(s) {
    ++s.work;
    for (const cancel of [...s.pending]) cancel();
    s.pending.clear(); s.conversions.clear(); clearNodes(s);
    for (const row of s.source.values()) {
      row.title = ''; row.subtitle = ''; row.latitude = null; row.longitude = null; row.converted = null;
      delete row.mediaPNG; delete row.mediaSelectedPNG; delete row.mediaKey; delete row.recentLabel;
    }
    s.source.clear(); s.displayClusters.clear(); s.latestFrame = 0;
  }
  function convert(s, longitude, latitude, revision, work) {
    if (!current(s, revision, work)) return Promise.reject(new Error('retired'));
    const key = `${longitude},${latitude}`;
    if (s.conversions.has(key)) return Promise.resolve(s.conversions.get(key));
    return convertBatch(s, [[longitude, latitude]], revision, work).then(rows => {
      if (current(s, revision, work)) remember(s, key, rows[0]);
      return rows[0];
    });
  }
  // Official JS API 2.0 maximum: 40 coordinate pairs per convertFrom call.
  // https://lbs.amap.com/api/javascript-api-v2/guide/transform/convertfrom
  function convertBatch(s, coordinates, revision, work) {
    if (!current(s, revision, work) || coordinates.length < 1 || coordinates.length > 40)
      return Promise.reject(new Error('retired or invalid conversion batch'));
    return new Promise((resolve, reject) => {
      let settled = false;
      const finish = (error, value) => {
        if (settled) return; settled = true; clearTimeout(timer); s.pending.delete(cancel);
        if (error || !current(s, revision, work)) reject(new Error('conversion unavailable'));
        else resolve(value);
      };
      const cancel = () => finish(new Error('retired'));
      const timer = setTimeout(() => finish(new Error('conversion timeout')), 8000);
      s.pending.add(cancel);
      s.sdk.convertFrom(coordinates.length === 1 ? coordinates[0] : coordinates, 'gps', (result, response) => {
        const rows = response?.locations;
        const valid = Array.isArray(rows) && rows.length === coordinates.length && rows.every(p => {
          const lng = p?.getLng?.() ?? p?.lng, lat = p?.getLat?.() ?? p?.lat;
          return finite(lng) && finite(lat) && Math.abs(lng) <= 180 && Math.abs(lat) <= 90;
        });
        finish(result !== 'complete' || !valid ? new Error('conversion failed') : null, rows);
      });
    });
  }
  function queueFrame(s) {
    if (!alive(s) || !s.map || s.framePending) return;
    s.framePending = true;
    requestAnimationFrame(() => {
      s.framePending = false;
      if (!alive(s) || !s.map || [...s.source.values()].some(x => !x.converted)) return;
      const revision = s.revision, positions = [], points = new Map();
      for (const item of s.source.values()) {
        // Exact current SDK container pixels. No Mercator, WGS/GCJ estimate or DPR division.
        const pixel = s.map.lngLatToContainer(item.converted);
        const x = pixel?.getX?.() ?? pixel?.x, y = pixel?.getY?.() ?? pixel?.y;
        if (!finite(x) || !finite(y)) return status(s, false);
        positions.push({id: item.id, x, y}); points.set(item.id, {x, y});
      }
      const zoom = s.map.getZoom();
      if (!finite(zoom) || zoom < 1 || zoom > 20) return status(s, false);
      s.projections = points; const frame = ++s.frameSerial; s.latestFrame = frame;
      const element = document.getElementById(s.elementID), cx = element.clientWidth / 2, cy = element.clientHeight / 2;
      const scalePoints = [-24, 24].map(dx => {
        const p = s.map.containerToLngLat(new s.sdk.Pixel(cx + dx, cy));
        return [p?.getLng?.() ?? p?.lng, p?.getLat?.() ?? p?.lat];
      });
      if (scalePoints.some(p => !finite(p[0]) || !finite(p[1]))) return status(s, false);
      s.onFrame(JSON.stringify({schema: 'birdtie-amap-frame-v1', revision, frame, zoom, positions,
        scalePoints, scalePixels: 48}));
    });
  }
  // One replaceable quiet-period slot covers drag, wheel, rotation, resize and
  // programmatic camera changes. Dart publishes both readers from this frame.
  function queueViewport(s) {
    if (!alive(s) || !s.map) return;
    clearTimeout(s.viewportTimer);
    const motion = s.motion, scope = s.scope;
    s.viewportTimer = setTimeout(() => {
      s.viewportTimer = undefined;
      if (!alive(s) || !s.map || motion !== s.motion || scope !== s.scope) return;
      try {
        const element = document.getElementById(s.elementID), w = element.clientWidth, h = element.clientHeight;
        if (!(w > 0 && h > 0) || !s.map.containerToLngLat) return;
        const points = [[0, 0], [w, 0], [w, h], [0, h], [w / 2, h / 2]].map(([x, y]) => {
          const p = s.map.containerToLngLat(new s.sdk.Pixel(x, y));
          return [p?.getLng?.() ?? p?.lng, p?.getLat?.() ?? p?.lat];
        });
        const zoom = s.map.getZoom();
        if (!finite(zoom) || zoom < 1 || zoom > 20 || points.some(p =>
          !finite(p[0]) || !finite(p[1]) || Math.abs(p[0]) > 180 || Math.abs(p[1]) > 90)) return;
        s.onViewport?.(JSON.stringify({schema: 'birdtie-amap-viewport-v1', revision: s.revision, zoom, points, bearing: s.map.getRotation?.() ?? 0, pitch: s.map.getPitch?.() ?? 0}));
      } catch (_) { status(s, false); }
    }, 180);
  }
  async function focus(s) {
    const request = s.focus;
    if (!request || !s.map || request.serial <= s.appliedFocus) return;
    const revision = s.revision, work = s.work, motion = s.motion;
    const target = await convert(s, request.longitude, request.latitude, revision, work);
    if (!current(s, revision, work) || s.focus !== request || s.motion !== motion || request.serial <= s.appliedFocus) return;
    s.appliedFocus = request.serial;
    let positions = [...s.source.values()].map(x => x.converted).filter(Boolean);
    const bounds = request.bounds;
    if (request.fit && bounds && [bounds.west, bounds.south, bounds.east, bounds.north].every(finite)) {
      positions = await Promise.all([
        convert(s, bounds.west, bounds.south, revision, work),
        convert(s, bounds.east, bounds.north, revision, work)]);
      if (!current(s, revision, work) || s.focus !== request || s.motion !== motion) return;
    }
    s.programmatic = true;
    try {
    if (request.fit && positions.length > 1) {
      const lng = positions.map(x => x.getLng?.() ?? x.lng), lat = positions.map(x => x.getLat?.() ?? x.lat);
      const p = request.padding;
      // Official setBounds avoid order is top,bottom,left,right.
      s.map.setBounds(new s.sdk.Bounds([Math.min(...lng), Math.min(...lat)], [Math.max(...lng), Math.max(...lat)]),
        true, [p[0], p[2], p[3], p[1]]);
      if (s.map.getZoom() > 14 && s.map.setZoom) s.map.setZoom(14, true);
    } else s.map.setZoomAndCenter(request.zoom, target, true);
    if (finite(request.bearing)) s.map.setRotation?.(request.bearing, true);
    if (finite(request.pitch)) s.map.setPitch?.(request.pitch, true);
    // Camera-only adjustment through the SDK's inverse pixel projection. It
    // never changes an object's geographic coordinate or declares WGS bounds.
    const element = document.getElementById(s.elementID), p = request.padding;
    if (!request.fit && p && element && s.map.containerToLngLat && s.map.setCenter) {
      const dx = (p[3] - p[1]) / 2, dy = (p[0] - p[2]) / 2;
      const center = s.map.containerToLngLat(new s.sdk.Pixel(element.clientWidth / 2 - dx,
        element.clientHeight / 2 - dy));
      s.map.setCenter(center, true);
    }
    } finally { s.programmatic = false; }
    queueFrame(s);
    queueViewport(s);
  }
  async function refresh(s) {
    if (!alive(s) || !s.sdk || !s.map) return;
    const revision = s.revision, work = s.work, source = [...s.source.values()];
    if (s.refreshingWork === work) return;
    s.refreshingWork = work;
    try {
      const unique = new Map();
      for (const item of source) if (!item.converted) {
        const key = `${item.longitude},${item.latitude}`;
        if (s.conversions.has(key)) { item.converted = s.conversions.get(key); continue; }
        if (!unique.has(key)) unique.set(key, {coordinates: [item.longitude, item.latitude], rows: []});
        unique.get(key).rows.push(item);
      }
      const entries = [...unique.entries()], batches = [];
      for (let start = 0; start < entries.length; start += 40) batches.push(entries.slice(start, start + 40));
      let next = 0;
      await Promise.all([0, 1].map(async () => {
        while (next < batches.length && current(s, revision, work)) {
          const batch = batches[next++];
          const result = await convertBatch(s, batch.map(x => x[1].coordinates), revision, work);
          if (!current(s, revision, work)) return;
          batch.forEach(([key, entry], index) => {
            for (const item of entry.rows) item.converted = result[index];
            remember(s, key, result[index]);
          });
        }
      }));
      if (!current(s, revision, work)) return;
      await focus(s);
      if (current(s, revision, work)) { status(s, true); queueFrame(s); }
    } catch (_) {
      if (current(s, revision, work)) {
        ++s.work; // Retire other parallel completions before clearing partial data.
        for (const cancel of [...s.pending]) cancel();
        for (const item of s.source.values()) item.converted = null;
        s.conversions.clear(); clearNodes(s); status(s, false);
      }
    } finally {
      if (s.refreshingWork === work) s.refreshingWork = undefined;
    }
  }
  function requestRefresh(s) {
    if (!alive(s) || !s.map) return Promise.resolve();
    s.refreshRequested = true;
    if (s.refreshFlight) return s.refreshFlight;
    s.refreshFlight = (async () => {
      while (alive(s) && s.refreshRequested) {
        s.refreshRequested = false;
        await refresh(s);
      }
    })().finally(() => {
      s.refreshFlight = undefined;
      if (alive(s) && s.refreshRequested) void requestRefresh(s);
    });
    return s.refreshFlight;
  }
  const labels = {place: '地点', moment: '动态', person: '大致区域', activity: '活动',
    opportunity: '活动机会', community: '社群', group: '群组', organization: '组织', business: '经营场地'};
  function svg(tag) { return document.createElementNS('http://www.w3.org/2000/svg', tag); }
  function node(s, item) {
    const id = item.id, kind = item.kind;
    const button = document.createElement('button'); button.type = 'button';
    button.dataset.birdtieMapId = id;
    Object.assign(button.style, {position: 'absolute', width: '48px', height: '48px', padding: '0',
      border: '0', background: 'transparent', display: 'grid', placeItems: 'center', cursor: 'pointer',
      pointerEvents: 'auto', touchAction: 'pan-x pan-y', outlineOffset: '2px'});
    button.title = item.title;
    button.setAttribute('aria-label', `${labels[kind] ?? '内容'} · ${item.title} · ${item.subtitle}`);
    const shape = document.createElement('span'), text = document.createElement('span'), image = document.createElement('img');
    const name = document.createElement('span'), recent = document.createElement('span'), count = document.createElement('span');
    text.appendChild(name); text.appendChild(recent);
    Object.assign(name.style, {display: '-webkit-box', WebkitBoxOrient: 'vertical', overflow: 'hidden'});
    Object.assign(recent.style, {display: 'block', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis'});
    image.alt = ''; image.style.pointerEvents = 'none'; image.style.display = 'none';
    shape.appendChild(image);
    const icon = svg('svg'), path = svg('path'); icon.setAttribute('viewBox', '0 0 24 24');
    path.setAttribute('d', kind === 'moment' ? 'M4 5h16v2H4zm0 6h16v2H4zm0 6h10v2H4z'
      : 'M12 2a7 7 0 0 0-7 7c0 5 7 13 7 13s7-8 7-13a7 7 0 0 0-7-7zm0 10a3 3 0 1 1 0-6 3 3 0 0 1 0 6z');
    path.setAttribute('fill', 'white'); icon.appendChild(path); shape.appendChild(icon);
    Object.assign(count.style, {color: 'white', fontSize: '12px', fontWeight: '600', lineHeight: '1', pointerEvents: 'none'});
    shape.appendChild(count);
    Object.assign(shape.style, {position: 'relative', display: 'grid', placeItems: 'center', boxSizing: 'border-box', border: '1.5px solid white'});
    Object.assign(text.style, {position: 'absolute', top: '46px', left: '-48px', width: '144px',
      fontSize: '12px', lineHeight: '16px', color: '#193B32', whiteSpace: 'nowrap', overflow: 'hidden',
      textOverflow: 'ellipsis', pointerEvents: 'none'});
    name.textContent = item.title; button.appendChild(shape); button.appendChild(text);
    let pointer, cancelled = false;
    button.addEventListener('pointerdown', e => { pointer = {x: e.clientX, y: e.clientY}; cancelled = false; });
    button.addEventListener('pointermove', e => { if (pointer && Math.hypot(e.clientX - pointer.x, e.clientY - pointer.y) > 8) cancelled = true; });
    button.addEventListener('pointercancel', () => { pointer = undefined; cancelled = true; });
    button.addEventListener('blur', () => { pointer = undefined; cancelled = true; button.style.outline = ''; });
    button.addEventListener('focus', () => { button.style.outline = '2px solid #193B32'; });
    let click;
    const bind = () => {
      if (click) button.removeEventListener('click', click);
      const revision = s.revision;
      click = e => {
      e.stopPropagation(); e.preventDefault();
      if (!alive(s) || s.revision !== revision || !(s.source.has(id) || s.displayClusters.has(id)) || s.reader ||
        (e.detail !== 0 && cancelled)) return;
      pointer = undefined;
      s.onSelect(JSON.stringify({schema: 'birdtie-amap-select-v1', revision,
        frame: s.latestFrame, id, kind}));
      };
      button.addEventListener('click', click);
    };
    bind();
    s.overlay.appendChild(button);
    return {button, shape, icon, text, image, name, recent, count, bind, kind};
  }
  function mediaValid(item) {
    const png = s => typeof s === 'string' && s.length <= 360000 &&
      /^iVBORw0KGgo[A-Za-z0-9+/]*={0,2}$/.test(s);
    return item.kind === 'moment' && ['point', 'approximate'].includes(item.precision) &&
      typeof item.mediaKey === 'string' && item.mediaKey.length <= 2048 &&
      png(item.mediaPNG) && png(item.mediaSelectedPNG) &&
      finite(item.mediaDPR) && item.mediaDPR > 0 && item.mediaDPR <= 8 &&
      Number.isSafeInteger(item.mediaExpiresAt) && item.mediaExpiresAt > Date.now() &&
      item.mediaExpiresAt <= Date.now() + 61000;
  }
  function paintMedia(s, item, row, selected, pale) {
    const micro = row.micro === true;
    const current = !micro && (!pale || item.kind === 'moment') && mediaValid(item);
    const size = row.logicalSize ?? (item.kind === 'moment' ? (selected ? 20 : 18) : selected ? 28 : pale ? 20 : 24);
    row.shape.style.border = current ? '0' : micro ? '1px solid white' : '1.5px solid white';
    const cluster = item.kind === 'cluster';
    row.icon.style.display = current || cluster || micro ? 'none' : '';
    row.count.textContent = cluster ? String(row.clusterCount ?? (item.categoryCode === 'moment_cluster' ? item.contentCount : item.anchorCount || item.memberIDs?.length || 0)) : '';
    row.count.style.display = cluster ? '' : 'none';
    if (current) {
      const src = `data:image/png;base64,${selected ? item.mediaSelectedPNG : item.mediaPNG}`;
      if (row.image.src !== src) row.image.src = src;
      const pixels = 48; // MapMarkerBitmap.pointCanvasSize logical pixels.
      Object.assign(row.image.style, {position: 'absolute', display: '', width: `${pixels}px`, height: `${pixels}px`,
        maxWidth: 'none', left: `${(size - pixels) / 2}px`, top: `${(size - pixels) / 2}px`});
    } else { row.image.removeAttribute('src'); row.image.style.display = 'none'; }
    // Layout names/counts come from the same measured Dart plan as native.
    const label = row.layoutLabel;
    row.name.textContent = label?.name ?? '';
    row.name.style.WebkitLineClamp = '1';
    row.name.style.whiteSpace = 'nowrap';
    row.recent.textContent = item.recentExpiresAt > Date.now() ? label?.auxiliary ?? '' : '';
    row.recent.style.fontSize = '11px'; row.recent.style.lineHeight = '13.75px';
    row.recent.style.color = '#69717A';
    row.text.style.whiteSpace = 'nowrap';
    row.text.style.maxHeight = label ? `${label.height}px` : '0';
    row.selected = selected; row.pale = pale;
  }
  function scheduleMedia(s) {
    clearTimeout(s.mediaTimer);
    let deadline = Infinity;
    for (const item of s.source.values()) for (const end of [item.mediaExpiresAt, item.recentExpiresAt])
      if (Number.isSafeInteger(end) && end > Date.now()) deadline = Math.min(deadline, end);
    if (!Number.isFinite(deadline)) return;
    const revision = s.revision;
    s.mediaTimer = setTimeout(() => {
      if (!alive(s) || revision !== s.revision) return;
      for (const item of s.source.values()) {
        if (!(item.mediaExpiresAt > Date.now())) { delete item.mediaPNG; delete item.mediaSelectedPNG; }
        if (!(item.recentExpiresAt > Date.now())) delete item.recentLabel;
        const row = s.nodes.get(item.id);
        if (row) paintMedia(s, item, row, row.selected === true, row.pale === true);
      }
      scheduleMedia(s);
    }, Math.max(1, deadline - Date.now()));
  }
  window.birdtieAMapMount = (elementID, json, onFrame, onSelect, onStatus, onMotion, onViewport, onCoordinate) => {
    const config = JSON.parse(json), host = new URL(config.serviceHost);
    if (!config.key || !pointValid(config) || !finite(config.zoom) || config.zoom < 1 || config.zoom > 20 ||
        !host.pathname.endsWith('/_AMapService') || !(host.protocol === 'https:' ||
          (host.protocol === 'http:' && ['localhost', '127.0.0.1'].includes(host.hostname)))) throw new Error('configuration unavailable');
    const s = {handle: nextHandle++, elementID, retired: false, revision: 0, work: 0, motion: 0,
      pending: new Set(), mountPending: new Set(), conversions: new Map(), source: new Map(), nodes: new Map(), projections: new Map(),
      routes: new Map(), displayClusters: new Map(), frameSerial: 0, latestFrame: 0, appliedFocus: 0, reader: false,
      onFrame, onSelect, onStatus, onMotion, onViewport, onCoordinate};
    maps.set(s.handle, s);
    (async () => {
      try {
        window._AMapSecurityConfig = {serviceHost: config.serviceHost};
        const api = await mountWait(s, loader()); if (!alive(s)) return;
        s.sdk = await mountWait(s, api.load({key: config.key, version: '2.0'})); if (!alive(s)) return;
        const element = await mountWait(s, attached(s), 8000); if (!alive(s)) return;
        // Default camera is public city configuration, independent of sources.
        const center = await mountWait(s, new Promise((resolve, reject) => s.sdk.convertFrom([config.longitude, config.latitude], 'gps',
          (state, result) => {
            const p = result?.locations?.[0], lng = p?.getLng?.() ?? p?.lng, lat = p?.getLat?.() ?? p?.lat;
            if (state === 'complete' && result.locations.length === 1 && finite(lng) && finite(lat) && Math.abs(lng) <= 180 && Math.abs(lat) <= 90) resolve(p);
            else reject(new Error('center unavailable'));
          })), 8000);
        if (!alive(s)) return;
        s.map = new s.sdk.Map(element, {center, zoom: config.zoom, resizeEnable: true, viewMode: '2D', features: ['bg', 'road', 'building']});
        syncRoutes(s,s.pendingRoutes ?? []);
        s.overlay = document.createElement('div');
        Object.assign(s.overlay.style, {position: 'absolute', inset: '0', pointerEvents: 'none', zIndex: '1000'});
        element.appendChild(s.overlay);
        s.move = () => { queueFrame(s); queueViewport(s); };
        s.gestureStart = () => {
          if (alive(s)) {
            ++s.motion;
            // A focus can be waiting behind marker conversions before focus()
            // captures a motion fence. Retire it at the actual input boundary.
            s.focus = undefined;
            s.userGesture = true;
          }
        };
        s.gestureEnd = () => { s.userGesture = false; s.motionNotified = false; };
        for (const event of ['pointerdown', 'wheel', 'touchstart', 'keydown']) element.addEventListener(event, s.gestureStart, true);
        for (const event of ['pointerup', 'pointercancel', 'touchend', 'keyup']) element.addEventListener(event, s.gestureEnd, true);
        s.motionStart = () => { if (alive(s) && s.userGesture && !s.programmatic && !s.motionNotified) { s.motionNotified = true; s.onMotion(); } };
        for (const event of ['mapmove', 'zoomchange', 'rotatechange', 'resize', 'moveend', 'zoomend', 'rotateend', 'complete']) s.map.on(event, s.move);
        for (const event of ['movestart', 'zoomstart']) s.map.on(event, s.motionStart);
        s.click = event => {
          if (!alive(s) || s.reader || !s.coordinateSelection || !event?.lnglat) return;
          const longitude = event.lnglat.getLng?.() ?? event.lnglat.lng, latitude = event.lnglat.getLat?.() ?? event.lnglat.lat;
          if (pointValid({longitude, latitude})) s.onCoordinate?.(JSON.stringify({
            schema: 'birdtie-amap-coordinate-v1', revision: s.revision, latitude, longitude}));
        };
        s.map.on('click', s.click);
        await requestRefresh(s);
        queueViewport(s);
      } catch (_) { if (alive(s)) { retireSource(s); s.focus = undefined; status(s, false); } }
    })();
    // Immediate handle permits retirement while loader/SDK/conversion is pending.
    return s.handle;
  };
  window.birdtieAMapUpdate = (handle, json) => {
    const s = maps.get(handle); if (!s || !alive(s)) return;
    const v = JSON.parse(json);
    if (v.schema !== 'birdtie-amap-source-v1' || !Number.isSafeInteger(v.revision) || v.revision < s.revision) return;
    if (v.revision > s.revision) {
      const scopeChanged = s.scope !== v.scope;
      if (scopeChanged) { retireSource(s); clearTimeout(s.viewportTimer); }
      else { ++s.work; for (const cancel of [...s.pending]) cancel(); s.pending.clear(); }
      s.revision = v.revision; s.scope = v.scope; s.focus = undefined; s.latestFrame = 0;
      s.displayClusters.clear();
      if (!Array.isArray(v.markers) || v.markers.length > 10000) { retireSource(s); return status(s, false); }
      const rows = new Map();
      for (const item of v.markers) {
        if (!pointValid(item) || typeof item.id !== 'string' || !item.id || rows.has(item.id) ||
          typeof item.title !== 'string' || typeof item.subtitle !== 'string' || typeof item.kind !== 'string' ||
          !['point', 'area', 'approximate'].includes(item.precision)) { retireSource(s); return status(s, false); }
        const row = {...item};
        const old = s.source.get(item.id);
        if (old && old.longitude === row.longitude && old.latitude === row.latitude) row.converted = old.converted;
        if (!mediaValid(row)) { delete row.mediaPNG; delete row.mediaSelectedPNG; delete row.mediaKey; }
        if (row.kind !== 'place' || typeof row.recentLabel !== 'string' || row.recentLabel.length > 80 ||
          !Number.isSafeInteger(row.recentExpiresAt) || row.recentExpiresAt > Date.now() + 61000)
          delete row.recentLabel;
        rows.set(item.id, row);
      }
      for (const [id, node] of s.nodes) {
        const item = rows.get(id);
        if (!item || !item.converted || node.kind !== item.kind) { removeNode(node); s.nodes.delete(id); continue; }
        node.bind(); node.button.title = item.title;
        node.button.setAttribute('aria-label', `${labels[item.kind] ?? '内容'} · ${item.title} · ${item.subtitle}`);
        paintMedia(s, item, node, node.selected === true, node.pale === true);
      }
      s.source = rows;
      syncRoutes(s,v.routes ?? []);
      if (rows.size === 0) s.conversions.clear();
      scheduleMedia(s);
    } else if (Object.hasOwn(v, 'markers')) return;
    if (typeof v.coordinateSelection === 'boolean') s.coordinateSelection = v.coordinateSelection;
    if (v.focus && pointValid(v.focus) && Number.isSafeInteger(v.focus.serial) &&
      finite(v.focus.zoom) && v.focus.zoom >= 1 && v.focus.zoom <= 20 &&
      Array.isArray(v.focus.padding) && v.focus.padding.length === 4 && v.focus.padding.every(finite)) s.focus = v.focus;
    void requestRefresh(s);
  };
  window.birdtieAMapLayout = (handle, json) => {
    const s = maps.get(handle); if (!s || !alive(s) || !s.overlay) return;
    const v = JSON.parse(json);
    if (v.schema !== 'birdtie-amap-layout-v1' || v.revision !== s.revision || v.frame !== s.latestFrame ||
      !Array.isArray(v.markers) || v.markers.length > s.source.size) return;
    const ids = new Set();
    const clusters = new Map();
    for (const m of v.markers) {
      const c = m.cluster;
      // The layout cannot invent spatial/same-coordinate aggregates.
      if (c) return;
      if (ids.has(m.id) || (!c && (!s.source.has(m.id) || !s.projections.has(m.id))) ||
        !finite(m.dx) || !finite(m.dy) || m.dx !== 0 || m.dy !== 0 ||
        typeof m.selected !== 'boolean' || typeof m.dense !== 'boolean' || typeof m.showLabel !== 'boolean' ||
        !finite(m.size) || m.size < 7 || m.size > 48 || !Number.isSafeInteger(m.clusterCount) || m.clusterCount < 0 ||
        !/^#[0-9a-f]{6}$/i.test(m.fill)) return;
      if (m.label && (!finite(m.label.x) || !finite(m.label.y) || !finite(m.label.width) ||
        m.label.width < 0 || m.label.width > 112 || !finite(m.label.height) || m.label.height > 48 ||
        typeof m.label.name !== 'string' || m.label.name.length > 300 ||
        ![12, 13].includes(m.label.fontSize) || !/^#[0-9a-f]{6}$/i.test(m.label.color) ||
        (m.label.auxiliary != null && (typeof m.label.auxiliary !== 'string' || m.label.auxiliary.length > 80)))) return;
      const source = c || s.source.get(m.id);
      if ((source.precision !== 'point' || !['place', 'moment'].includes(source.kind)) && (m.dx || m.dy)) return;
      ids.add(m.id);
    }
    s.displayClusters = clusters;
    for (const [id, row] of s.nodes) if (!ids.has(id)) { removeNode(row); s.nodes.delete(id); }
    for (const m of v.markers) {
      const item = clusters.get(m.id) || s.source.get(m.id), p = s.projections.get(item.anchorID && clusters.has(m.id) ? item.anchorID : m.id);
      let row = s.nodes.get(m.id); if (!row) { row = node(s, item); s.nodes.set(m.id, row); }
      const pale = m.dense && !m.selected, size = m.size;
      row.micro = pale && item.kind === 'place';
      row.logicalSize = size; row.clusterCount = m.clusterCount; row.layoutLabel = m.label;
      row.button.style.left = `${p.x - 24}px`; row.button.style.top = `${p.y - 24}px`;
      row.button.disabled = s.reader; row.button.tabIndex = s.reader ? -1 : 0;
      row.button.style.zIndex = m.selected ? '3' : '2'; row.button.setAttribute('aria-pressed', String(m.selected));
      Object.assign(row.shape.style, {width: `${size}px`, height: `${size}px`,
        borderRadius: item.kind === 'moment' ? '3px' : '50%',
        background: item.kind === 'moment' ? '#87683C' : item.kind === 'cluster' ? '#64707A' : m.fill});
      const glyphs = {
        dining: 'M4 2h2v7h1V2h2v7h1V2h2v7c0 2-1 3-3 4v9H6v-9c-2-1-3-2-3-4V2zm13 0h3v20h-3v-9h-2V6c0-2 1-4 2-4z',
        cafe: 'M3 3h15v3h3v7h-3v2c0 3-3 5-7 5s-7-2-7-5V5H3zm15 5v3h1V8zM2 21h20v1H2z',
        nightlife: 'M3 2h18L13 12v8h5v2H6v-2h5v-8zm5 3 4 5 4-5z',
        culture: 'M2 8 12 2l10 6v2H2zm2 3h3v8H4zm6 0h4v8h-4zm7 0h3v8h-3zM2 20h20v2H2z',
        nature: 'm12 2 7 9h-3l5 7h-7v4h-4v-4H3l5-7H5z',
        sport: 'M4 4h4v5H4zm12 0h4v5h-4zM2 10h20v4H2zm2 5h4v5H4zm12 0h4v5h-4z',
        shopping: 'M5 7h3V5a4 4 0 0 1 8 0v2h3l2 15H3zm5 0h4V5a2 2 0 0 0-4 0z',
        other: 'M12 2a7 7 0 0 0-7 7c0 5 7 13 7 13s7-8 7-13a7 7 0 0 0-7-7zm0 10a3 3 0 1 1 0-6 3 3 0 0 1 0 6z',
      };
      row.icon.firstChild.setAttribute('d', item.kind === 'moment' ? 'M4 5h16v2H4zm0 6h16v2H4zm0 6h10v2H4z' : glyphs[m.category] ?? glyphs.other);
      row.icon.style.width = row.icon.style.height = `${pale ? 12 : 14}px`;
      row.text.style.display = m.showLabel && m.label ? '' : 'none';
      if (m.label) Object.assign(row.text.style, {left: `${24 + m.label.x}px`, top: `${24 + m.label.y}px`,
        width: `${m.label.width}px`, height: `${m.label.height}px`, fontSize: `${m.label.fontSize}px`,
        lineHeight: '1.25', color: m.label.color, fontWeight: '500',
        textShadow: '-1px 0 white, 1px 0 white, 0 -1px white, 0 1px white'});
      paintMedia(s, item, row, m.selected, pale);
    }
  };
  window.birdtieAMapProject = handle => { const s = maps.get(handle); if (s) queueFrame(s); };
  window.birdtieAMapReaderVisible = (handle, visible) => {
    const s = maps.get(handle); if (!s || !alive(s)) return;
    s.reader = visible === true;
    const element = document.getElementById(s.elementID); if (element) element.style.pointerEvents = s.reader ? 'none' : '';
    for (const row of s.nodes.values()) { row.button.disabled = s.reader; row.button.tabIndex = s.reader ? -1 : 0; }
  };
  window.birdtieAMapDestroy = handle => {
    const s = maps.get(handle); if (!s) return;
    s.retired = true; retireSource(s); s.focus = undefined;
    clearTimeout(s.viewportTimer); s.displayClusters.clear();
    for (const cancel of [...s.mountPending]) cancel(); s.mountPending.clear();
    if (s.map) {
      for (const event of ['mapmove', 'zoomchange', 'rotatechange', 'resize', 'moveend', 'zoomend', 'rotateend', 'complete']) s.map.off(event, s.move);
      for (const event of ['movestart', 'zoomstart']) s.map.off(event, s.motionStart);
      s.map.off('click', s.click);
      s.map.destroy();
    }
    const element = document.getElementById(s.elementID);
    if (element) {
      element.style.pointerEvents = '';
      for (const event of ['pointerdown', 'wheel', 'touchstart', 'keydown']) element.removeEventListener(event, s.gestureStart, true);
      for (const event of ['pointerup', 'pointercancel', 'touchend', 'keyup']) element.removeEventListener(event, s.gestureEnd, true);
    }
    s.overlay?.remove(); s.overlay = s.map = s.sdk = null; maps.delete(handle);
    if (maps.size === 0 && loaderCancel) loaderCancel();
  };
})();
