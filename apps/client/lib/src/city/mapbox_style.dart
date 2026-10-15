// Style supplied by the product owner from the Civu Mapbox account.
const mapboxStylePath = 'lookluo/cmth7kwad001p01ssc9kw7kco';
const mapboxStyleUri = 'mapbox://styles/$mapboxStylePath';

/// Only provider POI symbols are suppressed. Road shields, settlement names,
/// terrain labels and the provider attribution are independent of this layer.
bool mapboxProviderPoiLayer(String id, String type, {String sourceLayer = ''}) {
  if (type != 'symbol') return false;
  final parts = '${id.toLowerCase()} ${sourceLayer.toLowerCase()}';
  return RegExp(r'(^|[\s_:/.-])poi([\s_:/.-]|$)').hasMatch(parts);
}
