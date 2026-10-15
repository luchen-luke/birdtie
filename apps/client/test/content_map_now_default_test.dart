import 'dart:convert';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
void main() {
 testWidgets('Now startup and clearing search never request whole-city POIs; explicit query remains available', (t) async {
  final reads = <Uri>[];
  final client = MockClient((r) async {
   reads.add(r.url);
   return http.Response(jsonEncode({'data': r.url.path == '/v1/cities' ? [{
    'id':'aberdeen-gb','name':'合成城市','source':{'label':'fixture'},
    'map':{'provider':'mapbox','latitude':57.15,'longitude':-2.1,'defaultZoom':12},
   }] : []}), 200, headers:{'content-type':'application/json; charset=utf-8'});
  });
  final c = PublicCityController(client:client, apiBaseUrl:'https://fixture.invalid', loadUnrequestedPlaces:false);
  addTearDown(() {c.dispose();client.close();});
  await c.loadCities(); await t.pump();
  expect(c.selectedCity?.id,'aberdeen-gb');
  expect(reads.where((u)=>u.path.endsWith('/places')),isEmpty);
  expect(reads.any((u)=>u.path.endsWith('/activities')),isTrue);
  c.searchPlaces('无动态地点');await t.pump();
  final searches=reads.where((u)=>u.path.endsWith('/places')).toList();
  expect(searches.length,1);expect(searches.single.queryParameters['q'],'无动态地点');
  c.searchPlaces('');await t.pump();
  expect(c.places,isEmpty);expect(reads.where((u)=>u.path.endsWith('/places')).length,1);
 });
}
