import 'dart:async';
import 'package:birdtie_client/src/workspace/agent_memory_correction_pending_store.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'package:http/http.dart' as http;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'agent_memory_correction_api_test.dart';
import 'agent_memory_correction_page_test.dart'
    show correctionPageHarness, correctionSettle, correctionTap;
import 'agent_memory_field_evidence_test.dart' show memoryDetailWire;

void main() {
  testWidgets('来源详情入口：本人当前记忆可实际打开来源与时间', (t) async {
    final auth = SeedTestAuth()..owner = correctionOwner;
    addTearDown(auth.dispose);
    final at = DateTime.now().toUtc();
    final client = MockClient(
      (r) async => correctionResponse({
        'data': r.url.path.endsWith('agent-memories')
            ? [correctionMemoryRaw(at: at)]
            : [],
      }),
    );
    await t.pumpWidget(
      correctionPageHarness(
        auth,
        client,
        MemoryAgentMemoryCorrectionPendingStore(),
      ),
    );
    await correctionSettle(t);
    expect(find.text('我偏好徒步活动'), findsOneWidget);
    final entry = find.widgetWithText(TextButton, '查看来源与时间');
    expect(entry, findsOneWidget);
    expect(t.widget<TextButton>(entry).onPressed, isNotNull);
    await t.pumpWidget(const SizedBox());
  });
  for (final inferred in [false, true]) {
    testWidgets('来源详情页面：实际点击展示${inferred ? '待审推断' : '明确声明'}中文时间，无自动POST', (
      t,
    ) async {
      final auth = SeedTestAuth()..owner = correctionOwner;
      addTearDown(auth.dispose);
      final at = DateTime.now().toUtc(),
          m = correctionMemoryRaw(at: DateTime.now().toUtc());
      // Use one actual record timestamp for both the list and detail fixture.
      m['createdAt'] = m['updatedAt'] = m['validFrom'] = at.toIso8601String();
      if (inferred) {
        m['sourceType'] = 'INFERRED';
        m['status'] = 'PENDING_REVIEW';
        m['confidence'] = .4;
      }
      var gets = 0, posts = 0;
      final client = MockClient((r) async {
        if (r.method == 'POST') posts++;
        if (r.url.path.endsWith(correctionMemory)) {
          gets++;
          return correctionResponse({
            'data': memoryDetailWire(at: at, memory: m, status: m['status']),
          });
        }
        return correctionResponse({
          'data': r.url.path.endsWith('agent-memories') ? [m] : [],
        });
      });
      await t.pumpWidget(
        correctionPageHarness(
          auth,
          client,
          MemoryAgentMemoryCorrectionPendingStore(),
        ),
      );
      await correctionSettle(t);
      await correctionTap(t, '查看来源与时间');
      expect(gets, 1);
      expect(posts, 0);
      expect(
        find.text(inferred ? '性质：未经本人确认的推断，仅供检查。' : '性质：本人明确声明，不等于已核验事实。'),
        findsOneWidget,
      );
      expect(find.text('拍摄时间未采集。记录与读取时间不证明事情发生、到场或到访。'), findsOneWidget);
      expect(find.text('这份单条来源说明不包含跨来源比较。'), findsOneWidget);
      expect(find.textContaining('记录创建时间：'), findsOneWidget);
      if (inferred) expect(find.text('声明时间：未提供本人声明时间'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
    });
  }
  testWidgets('来源详情页面：缺metadata不补声明时间，原更正入口仍可检查', (t) async {
    final auth = SeedTestAuth()..owner = correctionOwner;
    addTearDown(auth.dispose);
    final at = DateTime.now().toUtc();
    var posts = 0;
    final client = MockClient((r) async {
      if (r.method == 'POST') posts++;
      return correctionResponse({
        'data': r.url.path.endsWith(correctionMemory)
            ? memoryDetailWire(at: at, metadata: false)
            : r.url.path.endsWith('agent-memories')
            ? [correctionMemoryRaw(at: at)]
            : [],
      });
    });
    await t.pumpWidget(
      correctionPageHarness(
        auth,
        client,
        MemoryAgentMemoryCorrectionPendingStore(),
      ),
    );
    await correctionSettle(t);
    await correctionTap(t, '查看来源与时间');
    expect(find.text('当前未提供这条记忆的来源说明；不会猜测或补全。'), findsOneWidget);
    expect(find.textContaining('声明时间：'), findsNothing);
    expect(find.textContaining('记录创建时间：'), findsOneWidget);
    expect(find.textContaining('记录更新时间：'), findsOneWidget);
    await correctionTap(t, '修改这条记忆');
    expect(find.byType(TextField), findsOneWidget);
    expect(posts, 0);
    await t.pumpWidget(const SizedBox());
  });
  for (final status in [401, 403, 503]) {
    testWidgets('来源详情页面：HTTP$status真实错误不变空，保留原记忆', (t) async {
      final auth = SeedTestAuth()..owner = correctionOwner;
      addTearDown(auth.dispose);
      final at = DateTime.now().toUtc();
      var fail = true, gets = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith(correctionMemory)) {
          gets++;
          return fail
              ? http.Response('', status)
              : correctionResponse({'data': memoryDetailWire(at: at)});
        }
        return correctionResponse({
          'data': r.url.path.endsWith('agent-memories')
              ? [correctionMemoryRaw(at: at)]
              : [],
        });
      });
      await t.pumpWidget(
        correctionPageHarness(
          auth,
          client,
          MemoryAgentMemoryCorrectionPendingStore(),
        ),
      );
      await correctionSettle(t);
      await correctionTap(t, '查看来源与时间');
      expect(find.text('我偏好徒步活动'), findsOneWidget);
      expect(find.text('这条记忆的来源与时间'), findsNothing);
      expect(
        find.textContaining(
          status == 401
              ? '恢复本人登录'
              : status == 403
              ? '没有查看权限'
              : '暂时无法读取来源与时间',
        ),
        findsOneWidget,
      );
      if (status == 503) {
        fail = false;
        await correctionTap(t, '查看来源与时间');
        expect(gets, 2);
        expect(find.text('这条记忆的来源与时间'), findsOneWidget);
      }
      await t.pumpWidget(const SizedBox());
    });
  }
  testWidgets('来源详情页面：320大字可滚动到说明和原操作，不溢出', (t) async {
    t.view.physicalSize = const Size(320, 700);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final auth = SeedTestAuth()..owner = correctionOwner;
    addTearDown(auth.dispose);
    final at = DateTime.now().toUtc();
    final client = MockClient(
      (r) async => correctionResponse({
        'data': r.url.path.endsWith(correctionMemory)
            ? memoryDetailWire(at: at)
            : r.url.path.endsWith('agent-memories')
            ? [correctionMemoryRaw(at: at)]
            : [],
      }),
    );
    await t.pumpWidget(
      correctionPageHarness(
        auth,
        client,
        MemoryAgentMemoryCorrectionPendingStore(),
        scale: 2,
      ),
    );
    await correctionSettle(t);
    await correctionTap(t, '查看来源与时间');
    final note = find.text('查看来源不会批准更正、启用模型或自动写入。');
    await t.ensureVisible(note);
    await correctionSettle(t);
    expect(note.hitTestable(), findsOneWidget);
    await correctionTap(t, '拒绝保留');
    expect(find.widgetWithText(FilledButton, '检查具体纠正版本'), findsOneWidget);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
  });
  for (final change in ['ABA', 'client', 'base', 'close']) {
    testWidgets('来源详情页面：$change后真实待回复旧详情不展示或复活', (t) async {
      final auth = SeedTestAuth()..owner = correctionOwner;
      addTearDown(auth.dispose);
      final at = DateTime.now().toUtc(), pending = Completer<http.Response>();
      var detailGets = 0;
      final store = MemoryAgentMemoryCorrectionPendingStore();
      final a = MockClient((r) async {
        if (r.url.path.endsWith(correctionMemory)) {
          detailGets++;
          return pending.future;
        }
        return correctionResponse({
          'data': r.url.path.endsWith('agent-memories')
              ? [correctionMemoryRaw(at: at)]
              : [],
        });
      });
      final b = MockClient(
        (r) async => throw StateError('replacement must not GET'),
      );
      await t.pumpWidget(correctionPageHarness(auth, a, store));
      await correctionSettle(t);
      final button = find.widgetWithText(TextButton, '查看来源与时间');
      await t.ensureVisible(button);
      await t.pump();
      await t.tap(button);
      await t.pump();
      await t.runAsync(() async {
        await Future<void>.delayed(const Duration(milliseconds: 20));
      });
      expect(detailGets, 1);
      if (change == 'ABA') {
        auth.changeIdentity('Bearer B', nextOwner: correctionID);
        auth.changeIdentity('Bearer owner', nextOwner: correctionOwner);
        await t.pump();
      } else if (change == 'close') {
        await t.pumpWidget(const SizedBox());
      } else {
        await t.pumpWidget(
          correctionPageHarness(
            auth,
            change == 'client' ? b : a,
            store,
            base: change == 'base' ? 'http://peer' : 'http://local',
          ),
        );
      }
      pending.complete(correctionResponse({'data': memoryDetailWire(at: at)}));
      await correctionSettle(t);
      expect(find.text('这条记忆的来源与时间'), findsNothing);
      if (change != 'close') {
        expect(find.text('身份或连接已变化，请关闭后以当前身份重新打开。'), findsOneWidget);
      }
      expect(detailGets, 1);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    });
  }
}
