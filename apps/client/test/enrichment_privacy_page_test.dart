import 'dart:convert';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/workspace/agent_multi_candidate_pending_store.dart';
import 'package:birdtie_client/src/workspace/enrichment_privacy_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'enrichment_privacy_controller_test.dart'
    show PrivacyWire, privacyNow, privacyOwner;

class _Auth extends BirdtieAuthController {
  @override
  String? get authorizationHeader => 'Bearer privacy-fixture';
  @override
  String? get accountID => privacyOwner;
  @override
  bool get signedIn => true;
}

Future<void> _visible(WidgetTester t, String label) async {
  await t.scrollUntilVisible(find.text(label), 150);
  await t.ensureVisible(find.text(label));
  await t.pump();
  expect(find.text(label).hitTestable(), findsOneWidget);
}

void main() {
  testWidgets('中文许可页真实检查确认，原ID版本撤回后刷新而非开启分析', (t) async {
    final a = _Auth(), w = PrivacyWire();
    await t.pumpWidget(
      MaterialApp(
        home: EnrichmentPrivacyPage(
          auth: a,
          client: w,
          apiBaseUrl: 'https://privacy.test',
          pendingStore: MemoryAgentMultiCandidatePendingStore(),
          now: privacyNow,
        ),
      ),
    );
    await t.pumpAndSettle();
    expect(w.sent.length, 1);
    await _visible(t, '检查这项撤回');
    await t.tap(find.text('检查这项撤回'));
    await t.pump();
    expect(w.sent.where((r) => r.method == 'DELETE'), isEmpty);
    await _visible(t, '确认撤回这项许可');
    await t.tap(find.text('确认撤回这项许可'));
    await t.pumpAndSettle();
    expect(w.sent.where((r) => r.method == 'DELETE').length, 1);
    expect(jsonDecode(w.sent.firstWhere((r) => r.method == 'DELETE').body), {
      'expectedRevision': 1,
    });
    expect(find.text('已撤回'), findsOneWidget);
    expect(find.text('确认撤回这项许可'), findsNothing);
    await t.pumpWidget(const SizedBox());
    a.dispose();
    w.close();
  });
  testWidgets('许可页大字号确认可达，短读取到期关闭批准但保留历史解释', (t) async {
    final a = _Auth(), w = PrivacyWire();
    var now = privacyNow();
    DateTime clock() => now;
    await t.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: const TextScaler.linear(2)),
          child: child!,
        ),
        home: EnrichmentPrivacyPage(
          auth: a,
          client: w,
          apiBaseUrl: 'https://privacy.test',
          pendingStore: MemoryAgentMultiCandidatePendingStore(),
          now: clock,
        ),
      ),
    );
    await t.pumpAndSettle();
    await _visible(t, '检查这项撤回');
    await t.tap(find.text('检查这项撤回'));
    await t.pump();
    await _visible(t, '确认撤回这项许可');
    expect(t.takeException(), isNull);
    now = now.add(const Duration(seconds: 31));
    await t.pump(const Duration(seconds: 31));
    await t.pump();
    expect(find.text('确认撤回这项许可'), findsNothing);
    expect(w.sent.where((r) => r.method == 'DELETE'), isEmpty);
    expect(find.textContaining('这不是原许可的到期时间'), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    a.dispose();
    w.close();
  });
  testWidgets('直接Page同key来源替换与ABA只退役，不拿新凭据到旧transport', (t) async {
    final a = _Auth(), w = PrivacyWire(), other = PrivacyWire();
    final source = ValueNotifier(w);
    final store = MemoryAgentMultiCandidatePendingStore();
    await t.pumpWidget(
      MaterialApp(
        home: ValueListenableBuilder<PrivacyWire>(
          valueListenable: source,
          builder: (_, client, _) => EnrichmentPrivacyPage(
            key: const ValueKey('same'),
            auth: a,
            client: client,
            apiBaseUrl: 'https://privacy.test',
            pendingStore: store,
            now: privacyNow,
          ),
        ),
      ),
    );
    await t.pumpAndSettle();
    await _visible(t, '检查这项撤回');
    await t.tap(find.text('检查这项撤回'));
    await t.pump();
    source.value = other;
    await t.pump();
    source.value = w;
    await t.pump();
    expect(find.text('确认撤回这项许可'), findsNothing);
    expect(w.sent.where((r) => r.method == 'DELETE'), isEmpty);
    expect(other.sent, isEmpty);
    expect(find.text('身份或入口来源已变化，请返回设置重新打开。'), findsOneWidget);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
    source.dispose();
    a.dispose();
    w.close();
    other.close();
  });
}
