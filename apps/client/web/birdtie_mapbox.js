// Public Mapbox GL JS basemap only. FlutterMap owns gestures, entity data,
// reader permissions and selection; no account/session data enters this bridge.
(() => {
  const sdkRoot = 'https://api.mapbox.com/mapbox-gl-js/v3.32.0/';
  const jobs = new Map();
  let loaderPromise;

  function loadAsset(tag, url) {
    return new Promise((resolve, reject) => {
      const element = document.createElement(tag);
      if (tag === 'link') {
        element.rel = 'stylesheet';
        element.href = url;
      } else {
        element.src = url;
        element.async = true;
      }
      const timer = setTimeout(() => failed(), 15000);
      const failed = () => {
        clearTimeout(timer);
        element.remove();
        reject(new Error('Map renderer unavailable'));
      };
      element.onload = () => { clearTimeout(timer); resolve(); };
      element.onerror = failed;
      document.head.appendChild(element);
    });
  }

  function loader() {
    if (!loaderPromise) {
      loaderPromise = Promise.all([
        loadAsset('link', sdkRoot + 'mapbox-gl.css'),
        window.mapboxgl ? Promise.resolve() : loadAsset('script', sdkRoot + 'mapbox-gl.js'),
      ]).then(() => {
        if (!window.mapboxgl || !window.mapboxgl.supported()) {
          throw new Error('Map renderer unavailable');
        }
        return window.mapboxgl;
      }).catch(() => {
        loaderPromise = undefined; // A later explicit retry can load again.
        throw new Error('Map renderer unavailable');
      });
    }
    return loaderPromise;
  }

  function current(id, job) { return jobs.get(id) === job && !job.cancelled; }

  function attached(id, job) {
    return new Promise((resolve, reject) => {
      const end = performance.now() + 10000;
      job.finishAttach = (element) => {
        clearTimeout(job.attachTimer);
        job.finishAttach = null;
        resolve(element);
      };
      function check() {
        if (!current(id, job)) { job.finishAttach?.(null); return; }
        const element = document.getElementById(id);
        if (element && element.isConnected && element.clientWidth > 0 && element.clientHeight > 0) {
          job.finishAttach(element);
        } else if (performance.now() < end) {
          job.attachTimer = setTimeout(check, 30);
        } else {
          job.finishAttach = null;
          reject(new Error('Map container unavailable'));
        }
      }
      check();
    });
  }

  function camera(raw) {
    if (!raw || !Array.isArray(raw.center) || raw.center.length !== 2 ||
        !raw.center.every(Number.isFinite) || Math.abs(raw.center[1]) > 85.0511287798066 ||
        !Number.isFinite(raw.zoom) || raw.zoom < 0 || raw.zoom > 23 ||
        !Number.isFinite(raw.bearing) || raw.pitch !== 0) {
      throw new Error('Map camera unavailable');
    }
    return {center: raw.center, zoom: raw.zoom, bearing: raw.bearing, pitch: 0};
  }

  function apply(job) {
    if (job.map) job.map.jumpTo(job.camera);
  }

  // The Dart helper supplies this closed public policy. Missing/unknown policy
  // leaves text untouched; it does not select a fallback style or hide labels.
  function labelPolicy(raw) {
    if (!raw || raw.version !== 1 ||
        ![raw.roadFactor, raw.localityFactor].every(v => Number.isFinite(v) && v > 0 && v <= 1)) return null;
    const lists = ['roadIDs', 'localityIDs', 'localitySourceLayers', 'protectedTokens'];
    if (!lists.every(k => Array.isArray(raw[k]) && raw[k].length <= 32 &&
      raw[k].every(v => typeof v === 'string' && v.length > 0 && v.length <= 80))) return null;
    return raw;
  }

  function secondaryLabelFactor(layer, policy) {
    if (!policy || layer.type !== 'symbol' ||
        (layer.layout?.['icon-image'] != null && layer.layout['icon-image'] !== '')) return null;
    const name = layer.id.toLowerCase(), source = (layer['source-layer'] || '').toLowerCase();
    const all = `${name} ${source}`;
    if (/(^|[\s_:/.-])poi([\s_:/.-]|$)/.test(all)) return null;
    if (all.split(/[\s_:/.-]+/).some(t => policy.protectedTokens.includes(t))) return null;
    if (policy.roadIDs.includes(name)) return policy.roadFactor;
    if (policy.localityIDs.includes(name) || policy.localitySourceLayers.includes(source)) return policy.localityFactor;
    return null;
  }

  // Mirrors mapboxWeightedTextOpacity. In particular, do not wrap a zoom
  // expression in multiplication: zoom must remain at outer step/interpolate.
  function weightedTextOpacity(original, factor) {
    if (!Number.isFinite(factor) || factor <= 0 || factor > 1) return null;
    if (original == null) return factor;
    const opacity = v => Number.isFinite(v) && v >= 0 && v <= 1;
    if (opacity(original)) return original * factor;
    if (!Array.isArray(original) || original.length === 0) return null;
    const zoom = v => Array.isArray(v) && v.length === 1 && v[0] === 'zoom';
    const result = original.slice();
    if (original[0] === 'step' && original.length >= 5 &&
        (original.length - 3) % 2 === 0 && zoom(original[1]) && opacity(original[2])) {
      result[2] = original[2] * factor;
    } else if (original[0] === 'interpolate' && original.length >= 7 &&
        (original.length - 3) % 2 === 0 && zoom(original[2])) {
      const mode = original[1];
      if (!Array.isArray(mode) || !((mode.length === 1 && mode[0] === 'linear') ||
          (mode.length === 2 && mode[0] === 'exponential' && Number.isFinite(mode[1]) && mode[1] > 0))) return null;
    } else return null;
    let previous;
    for (let i = 3; i < original.length; i += 2) {
      const stop = original[i], output = original[i + 1];
      if (!Number.isFinite(stop) || (previous != null && stop <= previous) || !opacity(output)) return null;
      previous = stop;
      result[i + 1] = output * factor;
    }
    return result;
  }

  window.birdtieMapboxMount = async (id, json, onStatus) => {
    window.birdtieMapboxDestroy(id);
    const config = JSON.parse(json);
    if (typeof config.accessToken !== 'string' || !config.accessToken.startsWith('pk.') ||
        typeof config.style !== 'string' || !config.style.startsWith('mapbox://styles/')) {
      throw new Error('Map configuration unavailable');
    }
    const job = {cancelled: false, camera: camera(config.camera), map: null, observer: null, attachTimer: null,
      labelPolicy: labelPolicy(config.labelWeightPolicy), originalOpacity: new Map(), styleGeneration: 0,
      styleMetrics: null};
    jobs.set(id, job);
    try {
      const gl = await loader();
      if (!current(id, job)) return false;
      const element = await attached(id, job);
      if (!element || !current(id, job)) return false;
      element.style.pointerEvents = 'none';
      const map = new gl.Map({
        container: element,
        accessToken: config.accessToken,
        style: config.style,
        ...job.camera,
        interactive: false,
        attributionControl: false, // The existing Flutter Mapbox attribution stays visible.
        trackResize: false,
        renderWorldCopies: true,
        projection: 'mercator',
        minZoom: 0,
        maxZoom: 23,
      });
      job.map = map;
      let lastStatus;
      const status = (value) => {
        if (current(id, job) && lastStatus !== value) {
          lastStatus = value;
          onStatus(value);
        }
      };
      const applyLabelWeights = (newStyle = false) => {
        if (!current(id, job)) return;
        if (newStyle) {
          job.originalOpacity.clear();
          ++job.styleGeneration;
        }
        const metrics = {version: 1, styleGeneration: job.styleGeneration,
          poiHidden: 0, matched: 0, applied: 0, skipped: 0};
        try {
          for (const layer of map.getStyle().layers || []) {
            if (layer.type !== 'symbol') continue;
            const labels = `${layer.id} ${layer['source-layer'] || ''}`.toLowerCase();
            if (/(^|[\s_:/.-])poi([\s_:/.-]|$)/.test(labels)) {
              // Keep ordinary provider POIs; Now overlays original Moments.
              continue;
            }
            const factor = secondaryLabelFactor(layer, job.labelPolicy);
            if (factor == null) continue;
            ++metrics.matched;
            if (!job.originalOpacity.has(layer.id)) {
              // Clone before mutation. A second load callback must not scale
              // our already-applied opacity; only a real style.load resets it.
              const value = layer.paint?.['text-opacity'];
              job.originalOpacity.set(layer.id, value == null ? null :
                (typeof value === 'number' ? value : JSON.parse(JSON.stringify(value))));
            }
            const opacity = weightedTextOpacity(job.originalOpacity.get(layer.id), factor);
            if (opacity == null) { ++metrics.skipped; continue; }
            try {
              map.setPaintProperty(layer.id, 'text-opacity', opacity);
              ++metrics.applied;
            } catch (_) { ++metrics.skipped; }
          }
        } catch (_) { /* A style error does not remove Birdtie content. */ }
        job.styleMetrics = metrics;
      };
      map.on('style.load', () => applyLabelWeights(true));
      map.on('load', () => { if (current(id, job)) { applyLabelWeights(); apply(job); status('ready'); } });
      // Handle errors without logging provider URLs or public token contents.
      map.on('error', () => status('failed'));
      job.observer = new ResizeObserver(() => {
        if (current(id, job)) { map.resize(); apply(job); }
      });
      job.observer.observe(element);
      apply(job);
      return true;
    } catch (_) {
      if (current(id, job)) {
        window.birdtieMapboxDestroy(id);
        onStatus('failed');
      }
      return false;
    }
  };

  window.birdtieMapboxUpdate = (id, json) => {
    const job = jobs.get(id);
    if (!job || !current(id, job)) return;
    // This also replaces the initial camera while SDK/container loading waits.
    job.camera = camera(JSON.parse(json));
    apply(job);
  };

  window.birdtieMapboxDestroy = (id) => {
    const job = jobs.get(id);
    if (!job) return;
    job.cancelled = true;
    clearTimeout(job.attachTimer);
    job.finishAttach?.(null);
    job.observer?.disconnect();
    job.map?.remove();
    jobs.delete(id);
  };

  // Numeric-only read-only diagnostics. Map readiness is separate from label
  // policy coverage; an unknown layer/property is never reported as applied.
  window.birdtieMapboxStyleMetrics = (id) => {
    const job = jobs.get(id);
    return job && current(id, job) && job.styleMetrics ? JSON.stringify(job.styleMetrics) : null;
  };

  // Read-only CSS-pixel projection for checking alignment with FlutterMap.
  // Callers supply public coordinates; no entities or private state are held.
  window.birdtieMapboxProjection = (id, json) => {
    const job = jobs.get(id);
    if (!job?.map || !current(id, job)) return null;
    const coordinates = JSON.parse(json);
    if (!Array.isArray(coordinates) || coordinates.length > 20) return null;
    const points = [];
    for (const point of coordinates) {
      if (!Array.isArray(point) || point.length !== 2 || !point.every(Number.isFinite)) return null;
      const projected = job.map.project(point);
      points.push({x: projected.x, y: projected.y});
    }
    return JSON.stringify(points);
  };
})();
