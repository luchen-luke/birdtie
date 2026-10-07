import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/entity_action_api.dart';
import 'package:birdtie_client/src/workspace/entity_action_contract.dart';
import 'package:birdtie_client/src/workspace/entity_action_controller.dart';
import 'package:birdtie_client/src/workspace/entity_action_dispatcher.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'entity_action_contract_test.dart' show actionWire, actionRef;

void main() {
  for (final outcome in [
    'button',
    'back',
    'barrier',
    'denied',
    'unavailable',
    'changed',
    'unknown',
  ]) {
    testWidgets('原详情共享合同 $outcome 提示准确且不自动写或重试', (tester) async {
      var reads = 0, effects = 0, completed = 0;
      bool? result;
      final client = MockClient((r) async {
        reads++;
        expect(r.method, 'GET');
        if (outcome == 'denied') {
          return http.Response('', 403);
        }
        final wire = actionWire(
          version: outcome == 'changed' && reads > 1 ? 'b' * 64 : 'a' * 64,
        );
        if (outcome == 'unavailable') {
          wire['actions'][4]['state'] = 'UNAVAILABLE';
        }
        return http.Response.bytes(
          utf8.encode(jsonEncode({'data': wire})),
          200,
        );
      });
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (context) => TextButton(
                onPressed: () async {
                  result = await runEntityAction(
                    context,
                    ref: actionRef,
                    kind: EntityActionKind.save,
                    authorizationHeader: () => 'Bearer original',
                    client: client,
                    apiBaseUrl: 'https://local.synthetic.invalid',
                    handler: (_) async {
                      effects++;
                      if (outcome == 'unknown') {
                        throw TimeoutException(
                          'local synthetic unknown receipt',
                        );
                      }
                    },
                  );
                  completed++;
                },
                child: const Text('检查当前操作'),
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('检查当前操作'));
      await tester.pumpAndSettle();
      if (outcome != 'denied' && outcome != 'unavailable') {
        expect(find.text('确认收藏'), findsOneWidget);
        expect(effects, 0);
        if (outcome == 'button') {
          await tester.tap(find.text('取消'));
        } else if (outcome == 'back') {
          await tester.binding.handlePopRoute();
        } else if (outcome == 'barrier') {
          await tester.tapAt(const Offset(4, 4));
        } else {
          await tester.tap(find.text('确认收藏'));
        }
      }
      await tester.pumpAndSettle();
      expect(completed, 1);
      expect(result, false);
      expect(effects, outcome == 'unknown' ? 1 : 0);
      expect(reads, outcome == 'changed' || outcome == 'unknown' ? 2 : 1);
      switch (outcome) {
        case 'button' || 'back' || 'barrier':
          expect(find.byType(SnackBar), findsNothing);
          expect(find.text('当前来源不支持此操作，请检查最新内容。'), findsNothing);
        case 'denied':
          expect(find.text('请切回个人身份检查可用操作。'), findsOneWidget);
        case 'unavailable':
          expect(find.text('当前来源不支持此操作，请检查最新内容。'), findsOneWidget);
        case 'changed':
          expect(find.text('具体来源已变化，请检查当前内容后重新选择。'), findsOneWidget);
        case 'unknown':
          expect(find.text('结果尚未确认，请核实原操作，不要重复提交。'), findsOneWidget);
      }
      expect(tester.takeException(), isNull);
      await tester.pump(const Duration(seconds: 1));
      expect(effects, outcome == 'unknown' ? 1 : 0);
      await tester.pumpWidget(const SizedBox());
      client.close();
    });
  }
  testWidgets('同身份普通重建和新callback map不能重开正在提交的批准', (tester) async {
    int reads = 0, writes = 0;
    final finish = Completer<void>();
    final redraw = ValueNotifier(0);
    final client = MockClient((_) async {
      reads++;
      return http.Response(
        jsonEncode({'data': actionWire()}),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
    String? token() => 'Bearer a';
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ValueListenableBuilder<int>(
            valueListenable: redraw,
            builder: (_, n, _) => EntityActionControls(
              ref: actionRef,
              authorizationHeader: token,
              client: client,
              apiBaseUrl: 'http://127.0.0.1:3707',
              handlers: {
                EntityActionKind.save: (_) async {
                  writes++;
                  await finish.future;
                },
              },
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('entity-action-save')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('确认收藏'));
    await tester.pumpAndSettle();
    expect(writes, 1);
    expect(reads, 3);
    redraw.value++;
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('entity-action-save')));
    await tester.pumpAndSettle();
    expect(reads, 3);
    expect(writes, 1);
    finish.complete();
    await tester.pumpAndSettle();
    await tester.pumpWidget(const SizedBox());
    redraw.dispose();
  });
  test('取消零写；具体人审后再GET原版本；未知提交仅一次不盲重试', () async {
    int reads = 0, writes = 0;
    String version = 'a' * 64;
    final api = EntityActionApi(
      client: MockClient((_) async {
        reads++;
        return http.Response(
          jsonEncode({'data': actionWire(version: version)}),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        );
      }),
      apiBaseUrl: 'http://127.0.0.1:3707',
    );
    final c = EntityActionController(
      api: api,
      ref: actionRef,
      identity: () => ('Bearer a', 'owner', null),
    );
    final d = EntityActionDispatcher(
      controller: c,
      handlers: {
        EntityActionKind.save: (a) async {
          expect(a.target, actionRef);
          writes++;
          throw TimeoutException('unknown');
        },
      },
    );
    expect(
      await d.propose(EntityActionKind.save, (_, _) async => false),
      false,
    );
    expect(writes, 0);
    expect(reads, 1);
    expect(
      await d.propose(EntityActionKind.save, (_, _) async {
        version = 'b' * 64;
        return true;
      }),
      false,
    );
    expect(writes, 0);
    await expectLater(
      d.propose(EntityActionKind.save, (_, _) async => true),
      throwsA(isA<TimeoutException>()),
    );
    expect(writes, 1);
    await Future<void>.delayed(Duration.zero);
    expect(writes, 1);
    c.dispose();
    api.dispose();
  });
  test('批准期间token账号工作区变化和A-B-A不继承', () async {
    EntityActionIdentity id = ('Bearer a', 'a', null);
    int writes = 0;
    final api = EntityActionApi(
      client: MockClient(
        (_) async => http.Response(
          jsonEncode({'data': actionWire()}),
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        ),
      ),
      apiBaseUrl: 'http://127.0.0.1:3707',
    );
    final c = EntityActionController(
      api: api,
      ref: actionRef,
      identity: () => id,
    );
    final d = EntityActionDispatcher(
      controller: c,
      handlers: {
        EntityActionKind.save: (_) async {
          writes++;
        },
      },
    );
    expect(
      await d.propose(EntityActionKind.save, (_, _) async {
        id = ('Bearer b', 'b', null);
        c.sync();
        id = ('Bearer a', 'a', null);
        c.sync();
        return true;
      }),
      false,
    );
    expect(writes, 0);
    c.dispose();
    api.dispose();
  });
  testWidgets('320大字IME真实中文检查取消48dp，不自动写', (tester) async {
    tester.view.physicalSize = const Size(320, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    int writes = 0;
    final client = MockClient(
      (_) async => http.Response(
        jsonEncode({'data': actionWire()}),
        200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      ),
    );
    await tester.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(
            size: Size(320, 800),
            textScaler: TextScaler.linear(3),
            viewInsets: EdgeInsets.only(bottom: 260),
          ),
          child: Scaffold(
            body: SingleChildScrollView(
              child: EntityActionControls(
                ref: actionRef,
                authorizationHeader: () => 'Bearer a',
                apiBaseUrl: 'http://127.0.0.1:3707',
                client: client,
                handlers: {
                  EntityActionKind.save: (_) async {
                    writes++;
                  },
                },
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    final button = find.byKey(const Key('entity-action-save'));
    expect(tester.getSize(button).height, greaterThanOrEqualTo(48));
    await tester.tap(button);
    await tester.pumpAndSettle();
    expect(find.textContaining('真实原活动名称'), findsOneWidget);
    expect(find.text('取消'), findsOneWidget);
    await tester.tap(find.text('取消'));
    await tester.pumpAndSettle();
    expect(writes, 0);
    expect(find.byType(SnackBar), findsNothing);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
}
