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

  window.birdtieMapboxMount = async (id, json, onStatus) => {
    window.birdtieMapboxDestroy(id);
    const config = JSON.parse(json);
    if (typeof config.accessToken !== 'string' || !config.accessToken.startsWith('pk.') ||
        typeof config.style !== 'string' || !config.style.startsWith('mapbox://styles/')) {
      throw new Error('Map configuration unavailable');
    }
    const job = {cancelled: false, camera: camera(config.camera), map: null, observer: null, attachTimer: null};
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
      const suppressProviderPoi = () => {
        if (!current(id, job)) return;
        try {
          for (const layer of map.getStyle().layers || []) {
            if (layer.type !== 'symbol') continue;
            const labels = `${layer.id} ${layer['source-layer'] || ''}`.toLowerCase();
            if (/(^|[\s_:/.-])poi([\s_:/.-]|$)/.test(labels)) {
              map.setLayoutProperty(layer.id, 'visibility', 'none');
            }
          }
        } catch (_) { /* A style error does not remove Birdtie content. */ }
      };
      map.on('style.load', suppressProviderPoi);
      map.on('load', () => { if (current(id, job)) { suppressProviderPoi(); apply(job); status('ready'); } });
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
