import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_profile_api_test.dart';
import 'agent_profile_page_test.dart' show profileHarness;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'agent_profile_memory_detail_metadata_test.dart'
    show profileMetadataDetail;

Future<void> openCurrentProfileMemory(WidgetTester t) async {
  final tile = find.byKey(const ValueKey('profile-memory-$profileMemory'));
  await t.scrollUntilVisible(
    tile,
    200,
    maxScrolls: 40,
    scrollable: find.byType(Scrollable).first,
  );
  await t.ensureVisible(tile);
  await t.pump();
  expect(tile.hitTestable(), findsOneWidget);
  await t.tap(tile);
  await t.pumpAndSettle();
}

void main() {
  for (final metadata in [true, false]) {
    testWidgets('当前详情元数据兼容：原Page实际卡片读取${metadata ? '新增元数据' : '旧详情'}', (
      t,
    ) async {
      final auth = SeedTestAuth()..owner = profileOwner;
      addTearDown(auth.dispose);
      final requests = <String>[];
      final client = MockClient((r) async {
        requests.add('${r.method} ${r.url.path}');
        final detail = r.url.path == '/v1/me/agent-memories/$profileMemory';
        return profileResponse(
          detail && metadata
              ? profileMetadataDetail()
              : profileData(r.url.path),
        );
      });
      addTearDown(client.close);
      await t.pumpWidget(profileHarness(auth, client));
      await t.pumpAndSettle();
      await openCurrentProfileMemory(t);
      expect(find.text('当前记忆详情'), findsOneWidget);
      expect(find.widgetWithText(SelectableText, '我偏好徒步活动'), findsOneWidget);
      expect(find.text('本人明确确认的声明'), findsOneWidget);
      expect(find.text('此详情仅供本人查看，不是模型读取许可。'), findsOneWidget);
      expect(find.text('查看来源与时间'), findsNothing);
      expect(find.text('暂时无法读取，请刷新重试。'), findsNothing);
      expect(requests.where((s) => s.endsWith('/$profileMemory')).toList(), [
        'GET /v1/me/agent-memories/$profileMemory',
      ]);
      expect(requests.every((s) => s.startsWith('GET ')), isTrue);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox.shrink());
    });
  }

  testWidgets('当前详情元数据兼容：顶层模型许可仍拒绝且元数据不替代错误', (t) async {
    final auth = SeedTestAuth()..owner = profileOwner;
    addTearDown(auth.dispose);
    final client = MockClient(
      (r) async => profileResponse(
        r.url.path == '/v1/me/agent-memories/$profileMemory'
            ? (profileMetadataDetail()..['modelAccess'] = true)
            : profileData(r.url.path),
      ),
    );
    addTearDown(client.close);
    await t.pumpWidget(profileHarness(auth, client));
    await t.pumpAndSettle();
    await openCurrentProfileMemory(t);
    expect(find.text('暂时无法读取，请刷新重试。'), findsOneWidget);
    expect(find.widgetWithText(SelectableText, '我偏好徒步活动'), findsNothing);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('当前详情元数据兼容：已发GET迟到详情不跨账号ABA显示', (t) async {
    final auth = SeedTestAuth()..owner = profileOwner;
    addTearDown(auth.dispose);
    final response = Completer<http.Response>();
    var reads = 0;
    final client = MockClient((r) async {
      if (r.url.path == '/v1/me/agent-memories/$profileMemory') {
        reads++;
        return response.future;
      }
      return profileResponse(profileData(r.url.path));
    });
    addTearDown(client.close);
    await t.pumpWidget(profileHarness(auth, client));
    await t.pumpAndSettle();
    final tile = find.byKey(const ValueKey('profile-memory-$profileMemory'));
    await t.scrollUntilVisible(
      tile,
      200,
      maxScrolls: 40,
      scrollable: find.byType(Scrollable).first,
    );
    await t.ensureVisible(tile);
    await t.tap(tile);
    await t.pump();
    expect(reads, 1);
    auth.changeIdentity(
      'Bearer B',
      nextOwner: '82000000-0000-4000-8000-000000000099',
    );
    auth.changeIdentity('Bearer owner', nextOwner: profileOwner);
    response.complete(profileResponse(profileMetadataDetail()));
    await t.pumpAndSettle();
    expect(find.text('身份或连接已变化，请返回设置重新打开。'), findsOneWidget);
    expect(find.widgetWithText(SelectableText, '我偏好徒步活动'), findsNothing);
    expect(reads, 1);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox.shrink());
  });
}
