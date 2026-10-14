import 'dart:async' show Completer;
import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/workspace/activity_detail_sheet.dart';
import 'package:birdtie_client/src/workspace/activity_participation_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'activity_participation_operation_contract_test.dart';
import 'activity_participation_operation_controller_test.dart'
    show receiptResponse;
import 'entity_action_contract_test.dart' show actionWire;
import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'package:birdtie_client/src/workspace/activity_participation_operation.dart';

Map<String, dynamic> recoveryActivity() => {
  'id': participationActivity,
  'title': '周末城市漫步与中文交流活动 Aberdeen Community Walk：长标题自然换行',
  'hostLabel': '本地合成社区',
  'summary': '这是用于恢复与文字布局的合成活动，不是实际主办方发布。',
  'placeName': 'Aberdeen 市中心',
  'startsAt': '2027-10-20T10:00:00Z',
  'endsAt': '2027-10-20T12:00:00Z',
  'timeZone': 'Europe/London',
  'status': 'upcoming',
  'schedule': '周六 11:00',
  'priceMinor': 0,
  'currency': 'GBP',
  'source': {'label': '本地合成资料'},
};
final recoveryBoundary = GlobalKey();
Future<void> captureRecovery(WidgetTester tester, String name) async {
  final directory = Platform.environment['BIRDTIE_RSVP_WIDGET_DIR'];
  if (directory == null) return;
  await tester.runAsync(() async {
    final render =
        recoveryBoundary.currentContext!.findRenderObject()!
            as RenderRepaintBoundary;
    final image = await render.toImage(pixelRatio: 1);
    final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
    image.dispose();
    final out = Directory(directory);
    await out.create(recursive: true);
    await File(
      '${out.path}/$name.png',
    ).writeAsBytes(bytes!.buffer.asUint8List());
  });
}

Future<void> mountRecovery(
  WidgetTester tester,
  MemoryParticipationPendingStore store,
  http.Client client, {
  double scale = 1,
  Brightness brightness = Brightness.light,
}) async {
  await tester.pumpWidget(
    RepaintBoundary(
      key: recoveryBoundary,
      child: MaterialApp(
        debugShowCheckedModeBanner: false,
        theme: birdtieTheme(brightness),
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: TextScaler.linear(scale)),
          child: child!,
        ),
        home: Scaffold(
          body: ActivityDetailSheet(
            activity: PublicActivity.fromJson(recoveryActivity()),
            authorizationHeader: () => 'Bearer original',
            accountID: () => participationOwner,
            apiBaseUrl: 'http://api.test',
            client: client,
            participationPendingStore: store,
          ),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  setUpAll(() async {
    if (Platform.environment['BIRDTIE_RSVP_WIDGET_DIR'] == null) return;
    final font = FontLoader('Roboto')
      ..addFont(
        Future.value(
          ByteData.sublistView(
            File(r'C:\Windows\Fonts\msyh.ttc').readAsBytesSync(),
          ),
        ),
      );
    await font.load();
    final icons = FontLoader('MaterialIcons')
      ..addFont(
        Future.value(
          ByteData.sublistView(
            File(
              r'D:\DevTools\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf',
            ).readAsBytesSync(),
          ),
        ),
      );
    await icons.load();
  });
  testWidgets(
    'transport replaced during unknown submit retires old response and restores current controls',
    (tester) async {
      final store = MemoryParticipationPendingStore();
      final swap = ValueNotifier(false), lost = Completer<http.Response>();
      var writes = 0;
      http.Request? original;
      http.Response ordinary(http.Request r) {
        if (r.url.path.contains('/entity-actions/')) {
          return http.Response(
            jsonEncode({
              'data': actionWire(
                ref: const EntityActionRef('activity', participationActivity),
              ),
            }),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }
        if (r.url.path.endsWith('/participations/me')) {
          return http.Response(
            jsonEncode({
              'data': original == null ? null : {'status': 'going'},
            }),
            200,
          );
        }
        return http.Response(
          jsonEncode({'data': recoveryActivity()}),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }

      http.Response committed() {
        final body = jsonDecode(original!.body) as Map<String, dynamic>;
        final p = store.values.values.single;
        final raw = participationWire();
        raw['operationId'] = body['operationId'];
        raw['requestDigest'] = p.requestDigest;
        raw['effectKey'] = participationEffectKey(
          participationOwner,
          p.operationID,
        );
        return http.Response(jsonEncode({'data': raw}), 200);
      }

      final first = MockClient((r) async {
        if (r.method == 'POST') {
          writes++;
          original = r;
          return lost.future;
        }
        return ordinary(r);
      });
      final second = MockClient((r) async {
        expect(r.method, 'GET');
        if (r.url.path.contains('/participation-operations/')) {
          return committed();
        }
        return ordinary(r);
      });
      String? token() => 'Bearer original';
      String? owner() => participationOwner;
      await tester.pumpWidget(
        MaterialApp(
          home: ValueListenableBuilder<bool>(
            valueListenable: swap,
            builder: (context, changed, _) => Scaffold(
              body: ActivityDetailSheet(
                key: const ValueKey('same'),
                activity: PublicActivity.fromJson(recoveryActivity()),
                authorizationHeader: token,
                accountID: owner,
                apiBaseUrl: 'http://api.test',
                client: changed ? second : first,
                participationPendingStore: store,
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('报名参加'));
      await tester.tap(find.text('报名参加'));
      await tester.pumpAndSettle();
      expect(
        find.text('确认报名'),
        findsOneWidget,
        reason: find
            .byType(Text)
            .evaluate()
            .map((e) => (e.widget as Text).data)
            .join('|'),
      );
      await tester.tap(find.text('确认报名'));
      await tester.pumpAndSettle();
      expect(writes, 1);
      expect(store.values, hasLength(1));
      final late = committed();
      swap.value = true;
      await tester.pumpAndSettle();
      expect(store.values, isEmpty);
      expect(
        tester
            .widget<FilledButton>(find.widgetWithText(FilledButton, '取消报名'))
            .onPressed,
        isNotNull,
      );
      lost.complete(late);
      await tester.pumpAndSettle();
      expect(writes, 1);
      expect(find.text('正在更新…'), findsNothing);
      await tester.pumpWidget(const SizedBox());
      swap.dispose();
      first.close();
      second.close();
    },
  );
  testWidgets(
    'unknown receipt blocks submit, stop requires a separate fresh human click',
    (tester) async {
      final store = MemoryParticipationPendingStore();
      await store.write(
        'http://api.test',
        participationOwner,
        testParticipationPending,
      );
      final writes = <http.Request>[];
      final client = MockClient((r) async {
        if (r.method == 'POST') {
          writes.add(r);
          return receiptResponse(closure: true);
        }
        if (r.url.path.endsWith('/participations/me')) {
          return http.Response('{"data":null}', 200);
        }
        if (r.url.path.contains('/participation-operations/')) {
          return http.Response('{}', 404);
        }
        return http.Response(
          jsonEncode({'data': recoveryActivity()}),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      });
      await mountRecovery(tester, store, client);
      expect(
        tester
            .widget<FilledButton>(find.widgetWithText(FilledButton, '报名参加'))
            .onPressed,
        isNull,
      );
      await tester.ensureVisible(find.text('停止这次提交'));
      await tester.tap(find.text('停止这次提交'));
      await tester.pumpAndSettle();
      expect(writes, isEmpty);
      expect(find.textContaining('不会取消已有报名'), findsOneWidget);
      await tester.tap(find.text('返回'));
      await tester.pumpAndSettle();
      expect(writes, isEmpty);
      await tester.tap(find.text('停止这次提交'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('确认停止提交'));
      await tester.pumpAndSettle();
      expect(writes, hasLength(1));
      expect(writes.single.url.path, endsWith('/close'));
      expect(find.textContaining('原报名结果未核实'), findsOneWidget);
      expect(await store.read('http://api.test', participationOwner), isEmpty);
    },
  );
  for (final reclose in [(1.0, Brightness.light), (2.0, Brightness.dark)]) {
    testWidgets(
      'unconfirmed stop restore allows only a new independent same-key close confirmation $reclose',
      (tester) async {
        tester.view.physicalSize = const Size(320, 720);
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        final store = MemoryParticipationPendingStore();
        await store.write(
          'http://api.test',
          participationOwner,
          testParticipationPending.stopping(),
        );
        final writes = <http.Request>[];
        final client = MockClient((r) async {
          if (r.method == 'POST') {
            writes.add(r);
            return receiptResponse(closure: true);
          }
          if (r.url.path.endsWith('/participations/me')) {
            return http.Response('{"data":null}', 200);
          }
          if (r.url.path.contains('/participation-operations/')) {
            return http.Response('{}', 404);
          }
          return http.Response(
            jsonEncode({'data': recoveryActivity()}),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        });
        addTearDown(client.close);
        await mountRecovery(
          tester,
          store,
          client,
          scale: reclose.$1,
          brightness: reclose.$2,
        );
        expect(writes, isEmpty);
        await tester.ensureVisible(find.text('再次确认停止提交'));
        await tester.pumpAndSettle();
        final suffix =
            '${reclose.$2 == Brightness.dark ? 'dark' : 'light'}-${(100 * reclose.$1).round()}';
        await captureRecovery(tester, 'rsvp-reclose-unknown-$suffix');
        await tester.tap(find.text('再次确认停止提交'));
        await tester.pumpAndSettle();
        await captureRecovery(tester, 'rsvp-reclose-confirm-$suffix');
        await tester.tap(find.text('返回'));
        await tester.pumpAndSettle();
        expect(writes, isEmpty);
        expect(
          await store.read('http://api.test', participationOwner),
          hasLength(1),
        );
        await tester.tap(find.text('再次确认停止提交'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('确认停止提交'));
        await tester.pumpAndSettle();
        expect(writes, hasLength(1));
        expect(writes.single.url.path, endsWith('/$participationOp/close'));
        expect(jsonDecode(writes.single.body), {
          'operation': 'JOIN',
          'requestDigest': participationDigest,
        });
        expect(
          await store.read('http://api.test', participationOwner),
          isEmpty,
        );
      },
    );
  }
  testWidgets(
    'recovered old JOIN never overwrites a newer cancelled current participation',
    (tester) async {
      final store = MemoryParticipationPendingStore();
      await store.write(
        'http://api.test',
        participationOwner,
        testParticipationPending,
      );
      var resolved = false, reads = 0;
      final client = MockClient((r) async {
        expect(r.method, 'GET');
        if (r.url.path.endsWith('/participations/me')) {
          reads++;
          return http.Response('{"data":{"status":"cancelled"}}', 200);
        }
        if (r.url.path.contains('/participation-operations/')) {
          return resolved ? receiptResponse() : http.Response('{}', 404);
        }
        return http.Response(
          jsonEncode({'data': recoveryActivity()}),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      });
      await mountRecovery(tester, store, client);
      resolved = true;
      await tester.ensureVisible(find.text('核对原操作'));
      await tester.tap(find.text('核对原操作'));
      await tester.pumpAndSettle();
      expect(reads, greaterThanOrEqualTo(2));
      expect(find.text('你已报名'), findsNothing);
      expect(find.text('取消报名'), findsNothing);
      expect(find.text('报名参加'), findsOneWidget);
    },
  );
  for (final brightness in [Brightness.light, Brightness.dark]) {
    for (final scale in [1.0, 1.3, 2.0]) {
      testWidgets(
        'narrow long Chinese and mixed text recovery ${brightness.name} $scale',
        (tester) async {
          tester.view.physicalSize = const Size(320, 720);
          tester.view.devicePixelRatio = 1;
          addTearDown(tester.view.resetPhysicalSize);
          addTearDown(tester.view.resetDevicePixelRatio);
          final store = MemoryParticipationPendingStore();
          await store.write(
            'http://api.test',
            participationOwner,
            testParticipationPending,
          );
          final client = MockClient((r) async {
            if (r.url.path.endsWith('/participations/me')) {
              return http.Response('{"data":null}', 200);
            }
            if (r.url.path.contains('/participation-operations/')) {
              return http.Response('{}', 404);
            }
            return http.Response(
              jsonEncode({'data': recoveryActivity()}),
              200,
              headers: {'content-type': 'application/json; charset=utf-8'},
            );
          });
          await mountRecovery(
            tester,
            store,
            client,
            scale: scale,
            brightness: brightness,
          );
          await tester.ensureVisible(find.text('核对原操作'));
          await tester.pumpAndSettle();
          expect(
            tester.getSize(find.widgetWithText(OutlinedButton, '核对原操作')).height,
            greaterThanOrEqualTo(48),
          );
          await captureRecovery(
            tester,
            'rsvp-unknown-${brightness.name}-${(scale * 100).round()}',
          );
          await tester.tap(find.text('停止这次提交'));
          await tester.pumpAndSettle();
          await tester.ensureVisible(find.text('确认停止提交'));
          await tester.pumpAndSettle();
          expect(
            tester.getSize(find.widgetWithText(FilledButton, '确认停止提交')).height,
            greaterThanOrEqualTo(48),
          );
          expect(tester.takeException(), isNull);
          await captureRecovery(
            tester,
            'rsvp-stop-confirm-${brightness.name}-${(scale * 100).round()}',
          );
          await tester.tap(find.text('返回'));
          await tester.pumpAndSettle();
        },
      );
    }
  }
}
