import 'dart:async';
import 'package:birdtie_client/src/workspace/agent_profile_visibility_api.dart';
import 'package:birdtie_client/src/workspace/agent_profile_visibility_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'agent_profile_visibility_api_test.dart'
    show
        audienceOwner,
        audienceCommunity,
        audienceOtherCommunity,
        audienceWire,
        audienceResponse,
        audienceCommunityRow,
        AudienceWireClient;

AgentProfileVisibilityController makeController(
  AudienceWireClient client, {
  String? Function()? auth,
  bool Function()? current,
}) => AgentProfileVisibilityController(
  api: AgentProfileVisibilityAPI(
    client: client,
    apiBaseUrl: 'https://audience.test',
  ),
  authorizationHeader: auth ?? (() => 'Bearer synthetic'),
  ownerID: () => audienceOwner,
  current: current,
);

void main() {
  test('读取编辑具体检查显式保存，改变草稿使旧检查失效', () async {
    Map<String, Object>? saved;
    final c = AudienceWireClient(
      (r) => audienceResponse(
        audienceWire(
          version: r.method == 'PUT' ? 2 : 1,
          rules: r.method == 'PUT' ? saved : null,
          configured: r.method == 'PUT',
        ),
      ),
    );
    final d = makeController(c);
    await d.load();
    expect(c.sent.length, 1);
    expect(d.changed, false);
    d.change('bio', ProfileFieldAudience('CONNECTIONS'));
    final old = d.prepareReview()!;
    d.change('agentNotes', ProfileFieldAudience('AGENT_ONLY'));
    await d.save(old);
    expect(c.sent.length, 1);
    final current = d.prepareReview()!;
    saved = profileRulesWire(current.rules);
    await d.save(current);
    expect(c.sent.where((r) => r.method == 'PUT').length, 1);
    expect(d.record!.version, 2);
    expect(d.notice, contains('已收到'));
    expect(d.changed, false);
    expect(d.review, null);
    d.dispose();
  });
  test('已保存未列出社群不自动删，新选择仅真实active成员', () async {
    final initialRules = audienceWire()['rules'] as Map<String, dynamic>;
    initialRules['bio'] = {
      'visibility': 'COMMUNITY',
      'communityIds': [audienceOtherCommunity],
    };
    final c = AudienceWireClient(
      (r) => audienceResponse(
        r.url.path.endsWith('social-communities')
            ? [
                audienceCommunityRow(audienceCommunity, '本人有效社群', 'active'),
                audienceCommunityRow(
                  audienceOtherCommunity,
                  '邀请非成员',
                  'invited',
                ),
              ]
            : audienceWire(
                rules: initialRules.cast<String, Object>(),
                configured: true,
              ),
      ),
    );
    final d = makeController(c);
    await d.load();
    expect(d.draft['bio']!.communityIDs, [audienceOtherCommunity]);
    await d.loadCommunities();
    expect(d.draft['bio']!.communityIDs, [audienceOtherCommunity]);
    expect(d.unavailableIDs, [audienceOtherCommunity]);
    d.change('agentNotes', ProfileFieldAudience('AGENT_ONLY'));
    expect(d.prepareReview(), isNotNull);
    d.change(
      'bio',
      ProfileFieldAudience('COMMUNITY', [audienceOtherCommunity]),
    );
    expect(d.draft['bio']!.communityIDs, [audienceOtherCommunity]);
    d.change('bio', ProfileFieldAudience('COMMUNITY', [audienceCommunity]));
    expect(d.prepareReview(), isNotNull);
    expect(c.sent.every((r) => r.method == 'GET'), true);
    d.dispose();
  });
  test('mine上限未列出旧ID仍随原规则提交，不允许另字段借用未知ID', () async {
    final rules = audienceWire()['rules'] as Map<String, dynamic>;
    rules['bio'] = {
      'visibility': 'COMMUNITY',
      'communityIds': [audienceOtherCommunity],
    };
    Map<String, Object>? saved;
    final c = AudienceWireClient((r) {
      if (r.url.path.endsWith('social-communities')) {
        return audienceResponse([
          audienceCommunityRow(audienceCommunity, '当前返回社群', 'active'),
        ]);
      }
      return audienceResponse(
        audienceWire(
          version: r.method == 'PUT' ? 2 : 1,
          rules: r.method == 'PUT' ? saved : rules.cast<String, Object>(),
          configured: true,
        ),
      );
    });
    final d = makeController(c);
    await d.load();
    await d.loadCommunities();
    d.change('agentNotes', ProfileFieldAudience('AGENT_ONLY'));
    d.change(
      'socialPreferences',
      ProfileFieldAudience('COMMUNITY', [audienceOtherCommunity]),
    );
    expect(d.draft['socialPreferences']!.visibility, 'PRIVATE');
    final review = d.prepareReview()!;
    saved = profileRulesWire(review.rules);
    await d.save(review);
    expect(d.notice, contains('已收到'));
    expect((saved['bio'] as Map)['communityIds'], [audienceOtherCommunity]);
    expect(c.sent.where((r) => r.method == 'PUT').length, 1);
    d.dispose();
  });
  test('409必须新GET与新检查，原检查不能重发', () async {
    var gets = 0;
    final c = AudienceWireClient(
      (r) => r.method == 'PUT'
          ? audienceResponse({}, status: 409)
          : audienceResponse(audienceWire(version: ++gets)),
    );
    final d = makeController(c);
    await d.load();
    d.change('bio', ProfileFieldAudience('PRIVATE'));
    final old = d.prepareReview()!;
    await d.save(old);
    expect(d.record, null);
    expect(d.error, contains('版本已变化'));
    await d.save(old);
    expect(c.sent.where((r) => r.method == 'PUT').length, 1);
    await d.load();
    expect(d.record!.version, 2);
    expect(d.review, null);
    expect(d.changed, false);
    d.dispose();
  });
  test('响应丢失只能GET核当前状态，不自动写或宣称因果成功', () async {
    Map<String, Object>? desired;
    final c = AudienceWireClient((r) {
      if (r.method == 'PUT') {
        throw http.ClientException('synthetic response lost');
      }
      return audienceResponse(
        audienceWire(
          version: desired == null ? 1 : 2,
          rules: desired,
          configured: desired != null,
        ),
      );
    });
    final d = makeController(c);
    await d.load();
    d.change('availability', ProfileFieldAudience('PRIVATE'));
    d.change('bio', ProfileFieldAudience('PRIVATE'));
    final captured = d.prepareReview()!;
    desired = profileRulesWire(captured.rules);
    await d.save(captured);
    expect(d.resultUnknown, true);
    expect(d.canEdit, false);
    expect(d.prepareReview(), null);
    await d.save(captured);
    expect(c.sent.where((r) => r.method == 'PUT').length, 1);
    await d.load();
    expect(d.resultUnknown, false);
    expect(d.notice, contains('不能证明'));
    expect(d.notice, isNot(contains('已收到')));
    expect(d.review, null);
    expect(d.changed, false);
    d.dispose();
  });
  test('401与403明确恢复条件，不复用旧检查', () async {
    for (final status in [401, 403]) {
      final c = AudienceWireClient(
        (r) => audienceResponse(
          r.method == 'PUT' ? {} : audienceWire(),
          status: r.method == 'PUT' ? status : 200,
        ),
      );
      final d = makeController(c);
      await d.load();
      d.change('bio', ProfileFieldAudience('PRIVATE'));
      final old = d.prepareReview()!;
      await d.save(old);
      expect(d.record, null);
      expect(d.review, null);
      expect(d.error, contains(status == 401 ? '登录已失效' : '社群资格'));
      await d.save(old);
      expect(c.sent.where((r) => r.method == 'PUT').length, 1);
      d.dispose();
    }
  });
  test('同步busy监听器使入口退休时，真实send边界零GET或PUT', () async {
    var current = true;
    final c = AudienceWireClient((_) => audienceResponse(audienceWire()));
    final d = makeController(c, current: () => current);
    void retire() {
      if (d.busy) {
        current = false;
        d.synchronizeIdentity();
      }
    }

    d.addListener(retire);
    await d.load();
    expect(c.sent, isEmpty);
    expect(d.retired, true);
    d.removeListener(retire);
    d.dispose();
    current = true;
    final next = makeController(c, current: () => current);
    await next.load();
    next.change('bio', ProfileFieldAudience('PRIVATE'));
    final review = next.prepareReview()!;
    next.addListener(() {
      if (next.busy) {
        current = false;
        next.synchronizeIdentity();
      }
    });
    await next.save(review);
    expect(c.sent.where((r) => r.method == 'PUT'), isEmpty);
    next.dispose();
  });
  test('会话ABA与迟到旧GET永久退休，dispose后旧失败不能通知', () async {
    final delayed = Completer<http.StreamedResponse>();
    var token = 'Bearer A';
    final c = AudienceWireClient((_) => delayed.future);
    final d = makeController(c, auth: () => token);
    final loading = d.load();
    expect(c.sent.length, 1);
    token = 'Bearer B';
    d.synchronizeIdentity();
    token = 'Bearer A';
    d.synchronizeIdentity();
    delayed.complete(audienceResponse(audienceWire()));
    await loading;
    expect(d.retired, true);
    expect(d.record, null);
    expect(d.draft, isEmpty);
    await d.load();
    expect(c.sent.length, 1);
    d.dispose();
    final pending = Completer<http.StreamedResponse>();
    final close = makeController(AudienceWireClient((_) => pending.future));
    final waiting = close.load();
    close.dispose();
    pending.completeError(http.ClientException('late'));
    await waiting;
    expect(close.record, null);
  });
}
