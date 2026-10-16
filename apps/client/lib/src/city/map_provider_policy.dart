/// Provider selection follows an explicit city country, never camera movement.
/// Unknown legacy country keeps its reviewed provider; no coordinate inference.
String mapProviderForCountry({
  required String countryCode,
  required String configuredProvider,
}) {
  final country = countryCode.trim().toUpperCase();
  if (country == 'CN' || country == 'CHN') return 'amap';
  if (RegExp(r'^[A-Z]{2,3}$').hasMatch(country)) return 'mapbox';
  return configuredProvider;
}
