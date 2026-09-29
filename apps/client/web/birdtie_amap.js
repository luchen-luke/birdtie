// Birdtie Web-only AMap bridge. Security key belongs in the serviceHost proxy.
(() => {
  let loaderPromise;
  let nextHandle = 1;
  const maps = new Map();

  function loader() {
    if (!loaderPromise) {
      loaderPromise = new Promise((resolve, reject) => {
        const script = document.createElement('script');
        script.src = 'https://webapi.amap.com/loader.js';
        script.onload = () => resolve(window.AMapLoader);
        script.onerror = () => reject(new Error('AMap loader unavailable'));
        document.head.appendChild(script);
      });
    }
    return loaderPromise;
  }

  function attached(id) {
    return new Promise((resolve, reject) => {
      let attempts = 0;
      const check = () => {
        const element = document.getElementById(id);
        if (element && element.isConnected && element.clientWidth > 0) {
          resolve(element);
        } else if (++attempts < 120) {
          requestAnimationFrame(check);
        } else {
          reject(new Error('AMap container unavailable'));
        }
      };
      check();
    });
  }

  function convert(AMap, longitude, latitude) {
    return new Promise((resolve, reject) => {
      AMap.convertFrom([longitude, latitude], 'gps', (status, result) => {
        if (status === 'complete' && result.locations?.length) {
          resolve(result.locations[0]);
        } else {
          reject(new Error('AMap coordinate conversion failed'));
        }
      });
    });
  }

  window.birdtieAMapMount = async (id, json, onSelect) => {
    const config = JSON.parse(json);
    if (!config.key || !config.serviceHost ||
        !new URL(config.serviceHost).pathname.endsWith('/_AMapService')) {
      throw new Error('AMap Web configuration unavailable');
    }
    window._AMapSecurityConfig = { serviceHost: config.serviceHost };
    const AMapLoader = await loader();
    const AMap = await AMapLoader.load({ key: config.key, version: '2.0' });
    const element = await attached(id);
    const centre = await convert(AMap, config.longitude, config.latitude);
    const map = new AMap.Map(element, {
      center: centre,
      zoom: config.zoom,
      resizeEnable: true,
    });
    const handle = nextHandle++;
    maps.set(handle, map);
    try {
      for (const item of config.markers) {
        const position = await convert(AMap, item.longitude, item.latitude);
        const marker = new AMap.Marker({ position, title: item.title });
        marker.on('click', () => onSelect(item.id));
        map.add(marker);
      }
      if (config.markers.length) {
        map.setFitView(null, false, [90, 50, 90, 50]);
      }
    } catch (error) {
      map.destroy();
      maps.delete(handle);
      throw error;
    }
    return handle;
  };

  window.birdtieAMapDestroy = (handle) => {
    const map = maps.get(handle);
    if (map) map.destroy();
    maps.delete(handle);
  };
})();
