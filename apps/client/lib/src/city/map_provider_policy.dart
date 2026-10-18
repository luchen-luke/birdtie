import 'map_navigation_target.dart';
import 'map_route_region.dart';

const mapRoutePolicyVersion = 'birdtie-map-route-v1';

// ISO region identifiers, not language/name heuristics. Unknown identifiers do
// not become positive evidence merely because they contain two/three letters.
final _knownIso2 =
    ('AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ '
            'BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO '
            'CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM '
            'FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN '
            'HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP '
            'KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML '
            'MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR '
            'NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA '
            'SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG '
            'TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN '
            'VU WF WS YE YT ZA ZM ZW')
        .split(' ')
        .toSet();

// Existing APIs occasionally use alpha-3. This reviewed compatibility set is
// derived from Natural Earth's ISO_A3 identifiers, with the corresponding
// standard identifiers for map units whose source ISO_A3 is intentionally -99.
final _knownIso3 =
    ('ABW AFG AGO AIA ALA ALB AND ARE ARG ARM ASM ATA ATF ATG AUS AUT AZE BDI BEL '
            'BEN BES BFA BGD BGR BHR BHS BIH BLM BLR BLZ BMU BOL BRA BRB BRN BTN BVT '
            'BWA CAF CAN CCK CHE CHL CHN CIV CMR COD COG COK COL COM CPV CRI CUB CUW '
            'CXR CYM CYP CZE DEU DJI DMA DNK DOM DZA ECU EGY ERI ESH ESP EST ETH FIN '
            'FJI FLK FRA FRO FSM GAB GBR GEO GGY GHA GIB GIN GLP GMB GNB GNQ GRC GRD '
            'GRL GTM GUF GUM GUY HKG HMD HND HRV HTI HUN IDN IMN IND IOT IRL IRN IRQ '
            'ISL ISR ITA JAM JEY JOR JPN KAZ KEN KGZ KHM KIR KNA KOR KWT LAO LBN LBR '
            'LBY LCA LIE LKA LSO LTU LUX LVA MAC MAF MAR MCO MDA MDG MDV MEX MHL MKD '
            'MLI MLT MMR MNE MNG MNP MOZ MRT MSR MTQ MUS MWI MYS MYT NAM NCL NER NFK '
            'NGA NIC NIU NLD NOR NPL NRU NZL OMN PAK PAN PCN PER PHL PLW PNG POL PRI '
            'PRK PRT PRY PSE PYF QAT REU ROU RUS RWA SAU SDN SEN SGP SGS SHN SJM SLB '
            'SLE SLV SMR SOM SPM SRB SSD STP SUR SVK SVN SWE SWZ SXM SYC SYR TCA TCD '
            'TGO THA TJK TKL TKM TLS TON TTO TUN TUR TUV TWN TZA UGA UKR UMI URY USA '
            'UZB VAT VCT VEN VGB VIR VNM VUT WLF WSM YEM ZAF ZMB ZWE')
        .split(' ')
        .toSet();

MapRouteRegion? _structuredRegion(String countryCode, String regionCode) {
  final country = countryCode.trim().toUpperCase();
  final region = regionCode.trim().toUpperCase();
  if (const {'HK', 'HKG', 'CN-HK', '810000'}.contains(region) ||
      const {'HK', 'HKG', 'CN-HK'}.contains(country)) {
    return MapRouteRegion.hongKong;
  }
  if (const {'MO', 'MAC', 'CN-MO', '820000'}.contains(region) ||
      const {'MO', 'MAC', 'CN-MO'}.contains(country)) {
    return MapRouteRegion.macao;
  }
  if (const {'TW', 'TWN', 'CN-TW', '710000'}.contains(region) ||
      const {'TW', 'TWN', 'CN-TW'}.contains(country)) {
    return MapRouteRegion.taiwan;
  }
  if (country == 'CN' || country == 'CHN') return MapRouteRegion.mainland;
  if (_knownIso2.contains(country) || _knownIso3.contains(country)) {
    return MapRouteRegion.overseas;
  }
  return null;
}

/// Compatibility for existing explicit City callers. Unknown legacy country
/// keeps its reviewed provider; camera movement uses [resolveMapRoute].
String mapProviderForCountry({
  required String countryCode,
  required String configuredProvider,
}) {
  final region = _structuredRegion(countryCode, '');
  return region == null
      ? configuredProvider
      : region == MapRouteRegion.mainland
      ? 'amap'
      : 'mapbox';
}

class MapRouteDecision {
  const MapRouteDecision({
    required this.provider,
    required this.region,
    required this.isDefinite,
    required this.reason,
    required this.regionVersion,
    required this.targetGeneration,
  });
  final String provider, reason, regionVersion;
  final MapRouteRegion region;
  final bool isDefinite;
  final int targetGeneration;
}

/// Explicit current structured targets take effect immediately. Stable viewport
/// targets use positive local land geometry; world/sea/border/unknown retain the
/// last provider without manufacturing a country or changing a business City.
MapRouteDecision resolveMapRoute(
  MapNavigationTarget target, {
  MapRouteRegionIndex? regions,
  String previousProvider = 'mapbox',
}) {
  final retained = const {'amap', 'mapbox'}.contains(previousProvider)
      ? previousProvider
      : 'mapbox';
  MapRouteDecision unknown(String reason) => MapRouteDecision(
    provider: retained,
    region: MapRouteRegion.unknown,
    isDefinite: false,
    reason: reason,
    regionVersion: regions?.version ?? mapRoutePolicyVersion,
    targetGeneration: target.generation,
  );
  if (!target.isValid) return unknown('invalid-target');
  if (target.isExplicit) {
    final region = _structuredRegion(target.countryCode, target.regionCode);
    if (region != null) {
      return MapRouteDecision(
        provider: region == MapRouteRegion.mainland ? 'amap' : 'mapbox',
        region: region,
        isDefinite: true,
        reason: 'explicit-region',
        regionVersion: mapRoutePolicyVersion,
        targetGeneration: target.generation,
      );
    }
  } else {
    // SDK zoom values are not equivalent measures of geographic coverage.
    // Only an actual settled viewport can classify a free camera movement.
    final longitudeSpan = target.viewportLongitudeSpan;
    final latitudeSpan = target.viewportLatitudeSpan;
    if (longitudeSpan == null || latitudeSpan == null) {
      return unknown('viewport-coverage-missing');
    }
    if (longitudeSpan >= 120 || latitudeSpan >= 80) {
      return unknown('world-viewport');
    }
  }
  if (regions == null) return unknown('regions-not-ready');
  final area = regions.classify(
    target.camera.latitude,
    target.camera.longitude,
  );
  if (!area.isDefinite) return unknown('unknown-region');
  return MapRouteDecision(
    provider: area.provider!,
    region: area.region,
    isDefinite: true,
    reason: 'positive-land',
    regionVersion: area.version,
    targetGeneration: target.generation,
  );
}
