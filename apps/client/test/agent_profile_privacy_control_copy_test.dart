import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import 'agent_profile_api_test.dart'
    show profileData, profileOwner, profileResponse;
import 'agent_profile_page_test.dart' show profileHarness;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;

const oldPrivacyCopy = '这些设置不授予模型读取、长期保留或自动执行权限。个性化和学习的全局开关尚未提供。';
const currentPrivacyCopy =
    '资料可见范围、记忆、本地分析和本次会话任务资料许可可在设置中分别管理。修改偏好不授予模型读取、长期保留或自动执行权限；资料使用仍需当前用途的具体批准。';

class PrivacyCopyClient extends http.BaseClient {
  final List<http.BaseRequest> requests = [];

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) {
    requests.add(request);
    final response = profileResponse(profileData(request.url.path));
    return Future.value(
      http.StreamedResponse(
        Stream.value(response.bodyBytes),
        response.statusCode,
        headers: response.headers,
      ),
    );
  }
}

void main() {
  testWidgets('智能体资料准确说明已有分项控制且不授予用途权限', (t) async {
    t.view.devicePixelRatio = 1;
    t.view.physicalSize = const Size(320, 640);
    addTearDown(t.view.resetDevicePixelRatio);
    addTearDown(t.view.resetPhysicalSize);
    final auth = SeedTestAuth()..owner = profileOwner;
    final client = PrivacyCopyClient();
    addTearDown(auth.dispose);
    addTearDown(client.close);
    await t.pumpWidget(
      profileHarness(auth, client, base: 'https://privacy-copy.test'),
    );
    await t.pumpAndSettle();
    final action = find.widgetWithText(OutlinedButton, '检查社交建议设置');
    await t.scrollUntilVisible(
      action,
      180,
      maxScrolls: 40,
      scrollable: find.byType(Scrollable).first,
    );
    await t.ensureVisible(action);
    await t.pumpAndSettle();
    expect(action.hitTestable(), findsOneWidget);
    expect(t.getSize(action).height, greaterThanOrEqualTo(48));
    final paragraph = find.byWidgetPredicate(
      (w) =>
          w is Text &&
          (w.data == oldPrivacyCopy || w.data == currentPrivacyCopy),
    );
    expect(paragraph, findsOneWidget);
    final rect = t.getRect(paragraph);
    expect(rect.overlaps(const Rect.fromLTWH(0, 0, 320, 640)), isTrue);
    expect(client.requests, isNotEmpty);
    expect(client.requests.every((r) => r.method == 'GET'), isTrue);
    debugPrint(
      'PRIVACY_COPY_MEASURE actual=${t.widget<Text>(paragraph).data} '
      'paragraph=$rect action=${t.getRect(action)} '
      'requests=${client.requests.length} writes=0',
    );
    expect(find.text(currentPrivacyCopy), findsOneWidget);
    expect(find.text(oldPrivacyCopy), findsNothing);
    expect(t.takeException(), isNull);
  });
}
