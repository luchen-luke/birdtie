import 'map_link_stub.dart'
    if (dart.library.js_interop) 'map_link_web.dart'
    as platform;

void openMapAttribution(String address) => platform.openMapAttribution(address);
