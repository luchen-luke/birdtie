import 'dart:convert';
import 'package:http/http.dart' as http;
import 'human_causal_control.dart';

class LocationReferenceChoice {
  const LocationReferenceChoice(
    this.id,
    this.name,
    this.address,
    this.attribution,
    this.sourceURL,
    this.validUntil,
  );
  final String id, name, address, attribution, sourceURL;
  final DateTime validUntil;
  bool get current => DateTime.now().toUtc().isBefore(validUntil);
  factory LocationReferenceChoice.decode(Object? raw, DateTime responseUntil) {
    final v = humanControlObject(raw, {
      'referenceId',
      'name',
      'address',
      'region',
      'countryCode',
      'sourceUrl',
      'attribution',
      'coordinateSystem',
      'latitude',
      'longitude',
      'sourceVersion',
      'observedAt',
      'validUntil',
    });
    final at = humanControlTime(v['observedAt']),
        until = humanControlTime(v['validUntil']);
    for (final key in [
      'name',
      'address',
      'region',
      'countryCode',
      'sourceUrl',
      'attribution',
    ]) {
      if (v[key] is! String ||
          utf8.encode(v[key]).length > 640 ||
          v[key] != v[key].trim() ||
          v[key].contains('\u0000'))
        throw const FormatException('位置资料不符');
    }
    if (!humanControlID(v['referenceId']) ||
        !humanControlHash(v['sourceVersion']) ||
        v['name'] == '' ||
        v['attribution'] != '© OpenStreetMap contributors · ODbL 1.0' ||
        !RegExp(
          r'^https://www\.openstreetmap\.org/(node|way|relation)/[1-9][0-9]*$',
        ).hasMatch(v['sourceUrl']) ||
        v['coordinateSystem'] != 'wgs84' ||
        v['latitude'] is! num ||
        v['longitude'] is! num ||
        !(v['latitude'] as num).isFinite ||
        !(v['longitude'] as num).isFinite ||
        (v['latitude'] as num).abs() > 90 ||
        (v['longitude'] as num).abs() > 180 ||
        !until.isAfter(at) ||
        until.difference(at) > const Duration(hours: 24))
      throw const FormatException('位置来源不符');
    return LocationReferenceChoice(
      v['referenceId'],
      v['name'],
      v['address'],
      v['attribution'],
      v['sourceUrl'],
      until.isBefore(responseUntil) ? until : responseUntil,
    );
  }
}

Future<List<LocationReferenceChoice>> searchLocationReferences(
  http.Client client,
  String base,
  String query,
) async {
  final uri = Uri.parse(
    '$base/v1/location-search',
  ).replace(queryParameters: {'q': query, 'limit': '10'});
  final res = await client.get(uri).timeout(const Duration(seconds: 12));
  if (res.statusCode != 200 || res.bodyBytes.length > 128 * 1024)
    throw const FormatException('外部地点暂不可用');
  final body = humanControlObject(jsonDecode(utf8.decode(res.bodyBytes)), {
    'data',
  });
  final v = humanControlObject(body['data'], {
    'schemaVersion',
    'status',
    'items',
    'observedAt',
    'validUntil',
  });
  final at = humanControlTime(v['observedAt']),
      until = humanControlTime(v['validUntil']);
  if (v['schemaVersion'] != 'location-search-v1' ||
      !{'SUCCESS', 'EMPTY', 'UNAVAILABLE'}.contains(v['status']) ||
      v['items'] is! List ||
      (v['items'] as List).length > 10 ||
      !until.isAfter(at) ||
      until.difference(at) > const Duration(seconds: 60) ||
      !DateTime.now().toUtc().isBefore(until))
    throw const FormatException('位置来源已变化');
  if (v['status'] == 'UNAVAILABLE') throw const FormatException('外部地点暂不可用');
  final items = (v['items'] as List)
      .map((v) => LocationReferenceChoice.decode(v, until))
      .toList(growable: false);
  if (items.map((v) => v.id).toSet().length != items.length ||
      v['status'] == 'EMPTY' && items.isNotEmpty)
    throw const FormatException('位置集合不符');
  return items;
}
