import 'dart:convert';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/agent_composer.dart';
import 'package:birdtie_client/src/workspace/map_canvas.dart';
import 'package:birdtie_client/src/workspace/map_entities.dart';
import 'package:birdtie_client/src/workspace/map_workspace.dart';
import 'package:birdtie_client/src/workspace/now_map_content_panel.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'content_map_controller_test.dart' show Fixture, id;
import 'map_layers_api_test.dart' show mapTestBounds;
import 'now_scope_recovery_test.dart' show NowFixtureCity, NowFixtureSource, nowField;

// Two location projections of one original Moment, never production data.
class _Source extends Fixture {
  _Source({
    this.readerStatus = 'published',
    this.readerOrigin = 'UNKNOWN',
    this.readerBody = '合成正文',
  }) { count = 2; }
  final String readerStatus, readerOrigin, readerBody;
  @override
  Map<String, Object?> detail(int n) => {
    ...super.detail(n),
    'status': readerStatus,
    'createdAtSource': readerOrigin,
    'title': '',
    'body': readerBody,
  };
  @override
  Map<String, Object?> pin(int n, MapBounds b) => {
    ...super.pin(n,b),
    'entityId':id(1), 'momentId':id(1), 'entryId':id(1), 'threadId':'',
    'name':'测试正文', 'moments':[selector(1)],
  };
  @override
  Map<String,Object?> page(Uri uri) => {...super.page(uri),'totalContentCount':1};
}

void expectContinuousReaderSurface(WidgetTester t, {double keyboard = 0}) {
  final surfaceFinder = find.byKey(const Key('now-expanded-reader-surface'));
  expect(surfaceFinder, findsOneWidget);
  final surface = t.widget<Material>(surfaceFinder);
  expect(surface.type, isNot(MaterialType.transparency));
  expect(surface.color, isNotNull);
  expect(surface.color!.a, 1, reason: 'reader and composer cannot reveal the map');
  final backing = t.getRect(surfaceFinder);
  final reader = t.getRect(find.byType(NowMapContentPanel));
  final composer = t.getRect(find.byType(AgentComposer));
  final viewport = find.descendant(
    of: find.byType(NowMapContentPanel),
    matching: find.byWidgetPredicate((w) => w is ListView || w is CustomScrollView),
  ).first;
  expect(backing.left, 0);
  expect(backing.right, 390);
  expect(backing.top, closeTo(reader.top, 1));
  expect(backing.bottom, 844, reason: 'opaque surface includes bottom safe area');
  expect(t.getRect(viewport).bottom, lessThanOrEqualTo(composer.top + 1),
      reason: 'scrolling body must not hide behind the sole input');
  expect(backing.contains(composer.center), isTrue);
  expect(composer.bottom, lessThanOrEqualTo(844 - keyboard + 1));
  expect(nowField().hitTestable(), findsOneWidget,
      reason: 'opaque backing must be below the actual input');
  expect(find.byTooltip('返回地图').hitTestable(), findsOneWidget,
      reason: 'reader close remains reachable over the backing');
}

void main() {
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  final scenarios = [
    (label: 'explicit Moment', overlap: false, origin: 'UNKNOWN',
      status: 'published', body: '合成正文', reason: '非公开内容暂不支持评论', keyboard: false),
    (label: 'deduplicated same Moment', overlap: true, origin: 'UNKNOWN',
      status: 'published', body: '合成正文', reason: '非公开内容暂不支持评论', keyboard: false),
    (label: 'legacy original with no title or body', overlap: false,
      origin: 'CIVU_ORIGINAL', status: 'draft', body: '', reason: '当前无法评论', keyboard: true),
    (label: 'native unpublished long body', overlap: false, origin: 'NATIVE',
      status: 'draft', body: List.generate(40, (i) => '原生未发布正文第$i行，保留原本内容。').join('\n'),
      reason: '未发布的内容暂不支持评论', keyboard: false),
  ];
  for (final scenario in scenarios) {
    testWidgets('${scenario.label} opens complete reader and keeps disabled comment target', (t) async {
      final f=_Source(readerStatus: scenario.status,
          readerOrigin: scenario.origin, readerBody: scenario.body);
      final overlap = scenario.overlap;
      final auth=SeedTestAuth()..owner=f.owner;
      final city=NowFixtureCity();
      final source=NowFixtureSource(city);
      final requests=<http.Request>[];
      final client=MockClient((r) async {
        requests.add(r); f.now=DateTime.now().toUtc();
        http.Response json(Object value)=>http.Response.bytes(utf8.encode(jsonEncode({'data':value})),200,headers:{'content-type':'application/json; charset=utf-8','cache-control':'no-store'});
        if(r.method!='GET') throw StateError('No writes permitted');
        if(r.url.path=='/v1/me/map-content')return json(f.page(r.url));
        if(r.url.path=='/v1/me/map-content/moments/${id(1)}')return json(f.detail(1));
        if(r.url.path.endsWith('/thread'))return http.Response('404 page not found\n',404,headers:{'content-type':'text/plain; charset=utf-8'});
        if(r.url.path=='/v1/cities/alpha/map-layers')return json({'schemaVersion':'typed-map-layers-v1','scope':'PUBLIC','cityId':'alpha','items':[],'truncated':false,...f.lease()});
        if(r.url.path.endsWith('/activities')||r.url.path=='/v1/me/moments')return json([]);
        return http.Response('{}',404);
      });
      final moments=PrivateMomentController(client:client,apiBaseUrl:'https://direct-open.test',authorizationHeader:()=>auth.authorizationHeader);
      t.view.physicalSize=const Size(390,844); t.view.devicePixelRatio=1;
      t.view.padding = const FakeViewPadding(top: 24, bottom: 24);
      t.view.viewPadding = t.view.padding;
      addTearDown(t.view.resetPhysicalSize);addTearDown(t.view.resetDevicePixelRatio);
      addTearDown(t.view.resetPadding);addTearDown(t.view.resetViewPadding);
      addTearDown(t.view.resetViewInsets);
      t.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      Future<void> settle() async {for(var n=0;n<15;n++){await t.runAsync(()=>Future<void>.delayed(const Duration(milliseconds:10)));await t.pump(const Duration(milliseconds:10));}await t.pump(const Duration(milliseconds:300));}
      try {
        await t.pumpWidget(MaterialApp(home:MapWorkspace(city:city,auth:auth,moments:moments,agentTaskSource:source,seedClient:client,seedApiBaseUrl:'https://direct-open.test')));
        await settle();
        MapCanvas canvas()=>t.widget<MapCanvas>(find.byType(MapCanvas));
        await canvas().layers!.load('alpha',mapTestBounds);
        canvas().mapState.initializeViewport(mapTestBounds);
        canvas().mapContent!.sync(cityID:'alpha',bounds:mapTestBounds);
        await settle();
        final owned=canvas().mapContent!;
        expect(owned.current,isTrue,reason:owned.error);
        expect(owned.entities,hasLength(2));
        await t.enterText(nowField(),'未发送的Now草稿');
        FocusManager.instance.primaryFocus?.unfocus();t.testTextInput.hide();await settle();
        final map=t.element(find.byType(PublicCityMapView));
        final camera=canvas().mapState.viewportBounds;
        if(overlap){canvas().onContentOverlap!(owned.entities.toList());}
        else{t.widget<PublicCityMapView>(find.byType(PublicCityMapView)).onEntitySelected!(owned.entities.first);}
        await settle();
        expect(find.byType(NowMapContentGroupPanel),findsNothing);
        expect(find.text('选择查看'),findsNothing);
        if (scenario.body.isNotEmpty) expect(find.text(scenario.body),findsOneWidget);
        final panel=t.widget<NowMapContentPanel>(find.byType(NowMapContentPanel));
        expect(panel.initialExpanded,isTrue);
        expect(find.text('查看动态'),findsNothing);
        expect(find.byType(TextField),findsOneWidget);
        final composer=t.widget<AgentComposer>(find.byType(AgentComposer));
        expect(composer.contentLabel,contains(scenario.reason));
        expect(composer.contentLabel, isNot(contains('《》')));
        if (scenario.origin == 'CIVU_ORIGINAL') {
          expect(composer.contentLabel, isNot(contains('未发布')));
          expect(composer.contentLabel, isNot(contains('非公开')));
          expect(composer.contentLabel, isNot(contains('《')),
              reason: 'an untitled media item must not receive an invented title');
        }
        expectContinuousReaderSurface(t);
        if (scenario.origin == 'NATIVE') {
          final viewport = find.descendant(of: find.byType(NowMapContentPanel),
              matching: find.byType(ListView));
          final scroll = t.widget<ListView>(viewport).controller!;
          expect(scroll.position.maxScrollExtent, greaterThan(0));
          await t.drag(viewport, const Offset(0, -240));
          await t.pumpAndSettle();
          expect(scroll.offset, greaterThan(0));
          expectContinuousReaderSurface(t);
        }
        expect(composer.inputReadOnly,isTrue);
        expect(source.queries,isEmpty);
        expect(t.element(find.byType(PublicCityMapView)),same(map));
        expect(canvas().mapState.viewportBounds,camera);
        // The unavailable target stays explicit until the user chooses Now.
        expect(find.byTooltip('回到 Now 对话').hitTestable(), findsOneWidget);
        await t.tap(find.byTooltip('回到 Now 对话'));await settle();
        expect(t.widget<AgentComposer>(find.byType(AgentComposer)).contentLabel,isNull);
        expect(t.widget<TextField>(nowField()).controller!.text,'未发送的Now草稿');
        expect(source.queries,isEmpty);
        if (scenario.keyboard) {
          await t.tap(nowField());
          t.view.viewInsets = const FakeViewPadding(bottom: 280);
          await settle();
          expectContinuousReaderSurface(t, keyboard: 280);
          await t.enterText(nowField(), '未发送的Now草稿继续输入');
          expect(t.widget<TextField>(nowField()).controller!.text,
              '未发送的Now草稿继续输入');
          t.view.viewInsets = const FakeViewPadding();
          FocusManager.instance.primaryFocus?.unfocus();t.testTextInput.hide();
          await settle();
          expectContinuousReaderSurface(t);
        }
        expect(t.element(find.byType(PublicCityMapView)),same(map));
        expect(canvas().mapState.viewportBounds,camera);
        await t.tap(find.byTooltip('返回地图'));await settle();
        expect(find.byType(NowMapContentPanel), findsNothing);
        expect(find.byKey(const Key('now-expanded-reader-surface')), findsNothing);
        expect(t.element(find.byType(PublicCityMapView)),same(map));
        expect(canvas().mapState.viewportBounds,camera);
        expect(requests.where((r)=>r.method!='GET'),isEmpty);
        expect(requests.where((r)=>r.url.path=='/v1/me/map-content/moments/${id(1)}'),hasLength(1));
        expect(t.takeException(),isNull);
      } finally {
        await t.pumpWidget(const SizedBox());await t.pump();
        moments.dispose();city.dispose();auth.dispose();client.close();f.close();
      }
    });
  }
}
