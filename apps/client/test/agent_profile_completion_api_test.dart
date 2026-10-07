import 'dart:convert';
import 'package:birdtie_client/src/content/agent_profile_completion_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const completionOwner = '81000000-0000-4000-8000-000000000001';
const completionAgent = '81000000-0000-4000-8000-000000000002';
const completionMemory = '81000000-0000-4000-8000-000000000003';
const completionPreviewID = '81000000-0000-4000-8000-000000000004';
final completionAt = DateTime.utc(2030, 1, 1);
String get completionPlan => 'a' * 64;
Map<String, dynamic> completionSource() => {
  'memoryId': completionMemory,
  'memoryVersion': 1,
  'category': 'hiking',
  'value': '我偏好徒步活动',
  'memoryValidUntil': completionAt
      .add(const Duration(hours: 1))
      .toIso8601String(),
};
Map<String, dynamic> completionBase() => {
  'schemaVersion': profileCompletionSchema,
  'owner': {'type': 'PERSON', 'id': completionOwner},
  'agentId': completionAgent,
  'modelAccess': false,
};
Map<String, dynamic> completionSuggestions() => {
  ...completionBase(),
  'profileVersion': 2,
  'targetField': profileCompletionField,
  'state': 'FIELD_EMPTY',
  'sources': [completionSource()],
  'observedAt': completionAt.toIso8601String(),
};
Map<String, dynamic> completionPreview({String id = completionPreviewID}) => {
  ...completionBase(),
  'id': id,
  'purpose': profileCompletionPurpose,
  'source': completionSource(),
  'expectedProfileVersion': 2,
  'targetField': profileCompletionField,
  'before': [],
  'after': ['我偏好徒步活动'],
  'planDigest': completionPlan,
  'observedAt': completionAt.toIso8601String(),
  'expiresAt': completionAt.add(const Duration(minutes: 5)).toIso8601String(),
  'explanation': '仅补这一项。来源到期或撤回不会自动删除独立人工声明。',
};
Map<String, dynamic> completionReceipt({
  String id = completionPreviewID,
  String state = 'COMMITTED',
  bool current = true,
}) => {
  ...completionBase(),
  'id': id,
  'purpose': profileCompletionPurpose,
  'memoryId': completionMemory,
  'memoryVersion': 1,
  'expectedProfileVersion': 2,
  'planDigest': completionPlan,
  'state': state,
  'currentProfileMatches': state == 'COMMITTED' && current,
  'observedAt': completionAt
      .add(Duration(minutes: state == 'EXPIRED' ? 6 : 0, seconds: 2))
      .toIso8601String(),
  'expiresAt': completionAt.add(const Duration(minutes: 5)).toIso8601String(),
  if (state == 'COMMITTED') 'resultProfileVersion': 3,
  if (state == 'COMMITTED')
    'committedAt': completionAt
        .add(const Duration(seconds: 1))
        .toIso8601String(),
};
http.Response completionResponse(dynamic m, {int status = 200}) =>
    http.Response(
      jsonEncode(m),
      status,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );
void main() {
  test('GET 真空 body 与 POST 唯一 JSON 契约', () async {
    final seen = <http.Request>[];
    final api = AgentProfileCompletionAPI(
      apiBaseUrl: 'http://local',
      client: MockClient((r) async {
        seen.add(r);
        return completionResponse(completionSuggestions());
      }),
    );
    await api.request('GET', 'suggestions', 'Bearer synthetic');
    await api.request(
      'POST',
      'previews',
      'Bearer synthetic',
      body: {'previewId': completionPreviewID},
    );
    expect(seen.first.bodyBytes, isEmpty);
    expect(seen.first.headers.containsKey('content-type'), false);
    expect(seen.first.url.query, isEmpty);
    expect(seen.last.body, jsonEncode({'previewId': completionPreviewID}));
    expect(
      () => api.request('GET', 'suggestions', 'Bearer synthetic', body: {}),
      throwsFormatException,
    );
  });
  test('有限时间及完整闭集 DTO', () {
    expect(
      ProfileCompletionSuggestions.read(
        completionSuggestions(),
        completionOwner,
      ).sources.single.category,
      'hiking',
    );
    expect(
      ProfileCompletionPreview.read(
        completionPreview(),
        completionOwner,
      ).source.value,
      '我偏好徒步活动',
    );
    expect(
      ProfileCompletionReceipt.read(
        completionReceipt(current: false),
        completionOwner,
      ).currentMatches,
      false,
    );
    for (final stamp in [
      'infinity',
      '2030-02-30T00:00:00Z',
      '2030-01-01T00:00:00+99:00',
    ]) {
      expect(() => completionTime(stamp), throwsFormatException);
    }
    for (final mutate in <void Function(Map<String, dynamic>)>[
      (m) => m['modelAccess'] = true,
      (m) => m['sources'] = [completionSource(), completionSource()],
      (m) => m['body'] = 'private',
      (m) => m['profileVersion'] = 9007199254740991,
      (m) => m['owner'] = {'type': 'PERSON', 'id': completionMemory},
    ]) {
      final m = completionSuggestions();
      mutate(m);
      expect(
        () => ProfileCompletionSuggestions.read(m, completionOwner),
        throwsFormatException,
      );
    }
    for (final mutate in <void Function(Map<String, dynamic>)>[
      (m) => m['after'] = ['其它内容'],
      (m) => m['expectedProfileVersion'] = 9007199254740991,
      (m) => m['expiresAt'] = 'infinity',
      (m) => m['source'] = {...completionSource(), 'category': 'unknown'},
    ]) {
      final m = completionPreview();
      mutate(m);
      expect(
        () => ProfileCompletionPreview.read(m, completionOwner),
        throwsFormatException,
      );
    }
    for (final mutate in <void Function(Map<String, dynamic>)>[
      (m) => m['resultProfileVersion'] = 4,
      (m) => m['body'] = 'private',
      (m) => m['currentProfileMatches'] = 'true',
    ]) {
      final m = completionReceipt();
      mutate(m);
      expect(
        () => ProfileCompletionReceipt.read(m, completionOwner),
        throwsFormatException,
      );
    }
  });
}
