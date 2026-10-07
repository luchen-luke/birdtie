import 'dart:convert';
import 'package:birdtie_client/src/content/agent_seed_sheet.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'agent_seed_controller_test.dart' show seedJson, seedResponse;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'agent_profile_completion_api_test.dart';
import 'agent_profile_completion_sheet_test.dart' show completionSheetTap;

void main() {
  testWidgets('真实渐进入口完成单项后 reload parent 当前snapshot再保存', (t) async {
    FlutterSecureStorage.setMockInitialValues({});
    final auth = SeedTestAuth()..owner = completionOwner;
    addTearDown(auth.dispose);
    var id = '', saved = false, reads = 0;
    Map<String, dynamic>? input;
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/agent-seed')) {
        if (r.method == 'GET') reads++;
        if (r.method == 'PUT') {
          input = jsonDecode(r.body);
          return seedResponse(
            seedJson(
              owner: completionOwner,
              snapshot: 'b',
              city: 'aberdeen-gb',
              languages: ['zh-CN'],
              intent: 'JUST_EXPLORE',
              progress: 'COMPLETED',
            ),
          );
        }
        return seedResponse(
          seedJson(
            owner: completionOwner,
            snapshot: saved ? 'b' : 'a',
            city: 'aberdeen-gb',
            languages: ['zh-CN'],
            intent: 'JUST_EXPLORE',
            progress: 'COMPLETED',
          ),
        );
      }
      if (r.method == 'POST') {
        if (r.url.path.endsWith('/accept')) {
          saved = true;
          return completionResponse(completionReceipt(id: id));
        }
        id = jsonDecode(r.body)['previewId'];
        return completionResponse(completionPreview(id: id));
      }
      return completionResponse(completionSuggestions());
    });
    await t.pumpWidget(
      MaterialApp(
        home: AgentSeedSheet(
          auth: auth,
          client: client,
          apiBaseUrl: 'http://local',
          progressive: true,
        ),
      ),
    );
    await t.pumpAndSettle();
    await completionSheetTap(t, '用已确认记忆补齐活动偏好');
    await completionSheetTap(t, '检查：我偏好徒步活动');
    await completionSheetTap(t, '确认补齐这一项');
    await completionSheetTap(t, '关闭，保留当前资料');
    expect(reads, 2);
    await completionSheetTap(t, '私密兴趣');
    await completionSheetTap(t, '下一步');
    await completionSheetTap(t, '保存这些设置');
    expect(input?['expectedSnapshot'], 'b' * 64);
    expect(t.takeException(), isNull);
    await t.pumpWidget(const SizedBox());
  });
}
