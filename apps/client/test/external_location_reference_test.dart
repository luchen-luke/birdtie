import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/content/moment_control.dart';
import 'package:birdtie_client/src/content/moment_locations.dart';
import 'package:birdtie_client/src/workspace/moment_location_picker.dart';
import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'map_layers_api_test.dart' show mapTestBounds,mapTestItem,mapTestView;

const relation='00000000-0000-4000-8000-000000000001';
const reference='d9057584-cb0a-5c0f-8b4d-3d90026f6673';
const attribution='© OpenStreetMap contributors · ODbL 1.0';
const select=MomentLocationInput(id:relation,kind:'REFERENCE',referenceID:reference,precision:'place');
void main(){
 test('external selection uses the server digest and a real typed reference without a city',(){
  final v=MomentControlInput(operationID:relation,action:'CREATE',expectedRevision:0,input:{'body':'正文','title':'','cityId':'','placeId':'','timePrecision':'unknown','locationPrecision':'none','locations':[select.toJson()]});
  expect(v.inputHash,'7dc883f75781302a8654240fb0a30dfabb4aafb21f97f687a3c534efa963693e');
  expect(MomentLocationInput.decode(select.toJson()).referenceID,reference);
  expect(select.withPrecision('city').valid,false);
  final different=MomentLocationInput(id:'00000000-0000-4000-8000-000000000002',kind:'REFERENCE',referenceID:'00000000-0000-4000-8000-000000000003',precision:'none');
  expect(decodeMomentLocationInputs([select.toJson(),different.toJson()]),hasLength(2));
 });
 test('v6 retains Moment detail and OSM attribution; old maps cannot reinterpret it as a Place',(){
  final at=DateTime.now().toUtc();final item=mapTestItem('MOMENT');
  item['pinIdentity']=relation;final anchor=Map<String,dynamic>.from(item['anchor']);
  anchor.addAll({'placeId':'','referenceId':reference,'targetKind':'REFERENCE','attribution':attribution});item['anchor']=anchor;
  final view=mapTestView(observedAt:at.toIso8601String());view['schemaVersion']='typed-map-layers-v6';view['items']=[item];
  final parsed=TypedMapView.decode(view,'owned-city',mapTestBounds,false,now:at);
  expect(parsed.items.single.referenceID,reference);expect(parsed.items.single.placeID,isEmpty);expect(parsed.items.single.detailID,parsed.items.single.id);
  expect(parsed.items.single.explanation,contains(attribution));
  view['schemaVersion']='typed-map-layers-v5';expect(()=>TypedMapView.decode(view,'owned-city',mapTestBounds,false,now:at),throwsFormatException);
 });
 testWidgets('location selection still works when optional City lookup fails',(tester)async{
  final at=DateTime.now().toUtc(),until=DateTime.now().toUtc().add(const Duration(seconds:45));
  final client=MockClient((r)async{
   if(r.url.path=='/v1/cities')return http.Response('',503);
   if(r.url.path=='/v1/places')return http.Response('{"data":[]}',200);
   return http.Response.bytes(utf8.encode(jsonEncode({'data':{'schemaVersion':'location-search-v1','status':'SUCCESS','items':[{'referenceId':reference,'name':'合成外部地点','address':'合成地址','region':'','countryCode':'GB','sourceUrl':'https://www.openstreetmap.org/node/12345','attribution':attribution,'coordinateSystem':'wgs84','latitude':51.5,'longitude':-.1,'sourceVersion':List.filled(64,'a').join(),'observedAt':at.toIso8601String(),'validUntil':at.add(const Duration(hours:1)).toIso8601String()}],'observedAt':at.toIso8601String(),'validUntil':until.toIso8601String()}})),200);
  });
  MomentLocationChoice? chosen;
  await tester.pumpWidget(MaterialApp(home:Builder(builder:(context)=>Scaffold(body:TextButton(onPressed:()async{chosen=await showModalBottomSheet<MomentLocationChoice>(context:context,isScrollControlled:true,builder:(_)=>MomentLocationPicker(client:client,baseUrl:'https://synthetic.invalid',current:()=>true));},child:const Text('选择'))))));
  await tester.tap(find.text('选择'));await tester.pumpAndSettle();await tester.enterText(find.byType(TextField),'合成');await tester.pump(const Duration(milliseconds:350));await tester.pumpAndSettle();
  expect(find.text('合成外部地点'),findsOneWidget);await tester.tap(find.text('合成外部地点'));await tester.pumpAndSettle();
  expect(chosen!.toLocation()!.referenceID,reference);expect(chosen!.toLocation()!.cityID,isEmpty);client.close();
 });
}
