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

/// Explicit secondary-label names only. A generic `road-label` or `place_label`
/// source may contain essential roads/towns and cannot establish this category.
/// This closed policy is also passed to GL JS; it contains no map/user data.
const Map<String, Object> mapboxLabelWeightPolicy = {
  'version': 1,
  'roadFactor': 0.62,
  'localityFactor': 0.56,
  'roadIDs': [
    'road-street-label',
    'road-minor-label',
    'road-secondary-tertiary-label',
    'road-label-small',
    'road-label-secondary',
    'road-label-tertiary',
    'road-label-minor',
    'road-label-local',
  ],
  'localityIDs': [
    'settlement-subdivision-label',
    'settlement-neighborhood-label',
    'settlement-neighbourhood-label',
    'neighborhood-label',
    'neighbourhood-label',
    'district-label',
  ],
  'localitySourceLayers': [
    'settlement_subdivision',
    'neighborhood_label',
    'neighbourhood_label',
    'district_label',
  ],
  'protectedTokens': [
    'shield',
    'motorway',
    'trunk',
    'primary',
    'major',
    'country',
    'state',
    'airport',
    'transit',
    'rail',
    'water',
    'terrain',
    'annotation',
    'annotations',
    'birdtie',
  ],
};

/// A null result leaves the layer untouched. Icons (including road shields),
/// important place names and unknown layer names take precedence over matches.
double? mapboxSecondaryLabelFactor(
  String id,
  String type, {
  String sourceLayer = '',
  bool hasIconImage = false,
}) {
  if (type != 'symbol' ||
      hasIconImage ||
      mapboxProviderPoiLayer(id, type, sourceLayer: sourceLayer)) {
    return null;
  }
  final name = id.toLowerCase();
  final source = sourceLayer.toLowerCase();
  final tokens = '$name $source'.split(RegExp(r'[\s_:/.-]+'));
  final protected = mapboxLabelWeightPolicy['protectedTokens']! as List;
  if (tokens.any(protected.contains)) return null;
  if ((mapboxLabelWeightPolicy['roadIDs']! as List).contains(name)) {
    return mapboxLabelWeightPolicy['roadFactor']! as double;
  }
  if ((mapboxLabelWeightPolicy['localityIDs']! as List).contains(name) ||
      (mapboxLabelWeightPolicy['localitySourceLayers']! as List).contains(
        source,
      )) {
    return mapboxLabelWeightPolicy['localityFactor']! as double;
  }
  return null;
}

/// Scales the original opacity, never an opacity previously changed by us.
///
/// Null is the style-spec default (1). Only numeric values and explicit
/// top-level zoom step/interpolate expressions with numeric outputs are handled.
/// Keeping zoom at the top level preserves the SDK's camera-expression rules.
/// Unsupported/malformed values return null, meaning "do not write".
Object? mapboxWeightedTextOpacity(Object? original, double factor) {
  if (!factor.isFinite || factor <= 0 || factor > 1) return null;
  if (original == null) return factor;
  bool opacity(Object? value) =>
      value is num && value.isFinite && value >= 0 && value <= 1;
  if (opacity(original)) return (original as num) * factor;
  if (original is! List || original.isEmpty) return null;
  bool zoom(Object? value) =>
      value is List && value.length == 1 && value.first == 'zoom';
  final result = <Object?>[...original];
  int firstStop;
  if (original.first == 'step' &&
      original.length >= 5 &&
      (original.length - 3).isEven &&
      zoom(original[1]) &&
      opacity(original[2])) {
    result[2] = (original[2] as num) * factor;
    firstStop = 3;
  } else if (original.first == 'interpolate' &&
      original.length >= 7 &&
      (original.length - 3).isEven &&
      zoom(original[2])) {
    final mode = original[1];
    if (mode is! List ||
        !((mode.length == 1 && mode.first == 'linear') ||
            (mode.length == 2 &&
                mode.first == 'exponential' &&
                mode[1] is num &&
                (mode[1] as num).isFinite &&
                (mode[1] as num) > 0))) {
      return null;
    }
    firstStop = 3;
  } else {
    return null;
  }
  num? previous;
  for (var i = firstStop; i < original.length; i += 2) {
    final stop = original[i];
    final output = original[i + 1];
    if (stop is! num ||
        !stop.isFinite ||
        (previous != null && stop <= previous) ||
        !opacity(output)) {
      return null;
    }
    previous = stop;
    result[i + 1] = (output as num) * factor;
  }
  return result;
}
