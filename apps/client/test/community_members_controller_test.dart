import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/workspace/community_api.dart';
import 'package:birdtie_client/src/workspace/community_members_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const commGroup = 'a0000000-0000-4000-8000-000000000001';
const commOwner = 'a0000000-0000-4000-8000-000000000002';
const commPeer = 'a0000000-0000-4000-8000-000000000003';
const commMember = 'a0000000-0000-4000-8000-000000000004';

class CommunityHarness extends ChangeNotifier {
  CommunityHarness() {
    api = CommunityApi(
      authorizationHeader: () => token,
      apiBaseUrl: 'https://local.synthetic.invalid',
      client: MockClient(handle),
    );
    controller = CommunityMembersController(
      api: api,
      communityId: commGroup,
      actorId: () => actor,
      authority: this,
    );
  }
  String? token = 'Bearer SYNTHETIC_A', actor = commOwner;
  String role = 'owner';
  int writes = 0, previews = 0;
  bool failWrite = false, failFriends = false;
  Completer<void>? delay, writeDelay;
  String? wrongActor, wrongTarget;
  late CommunityApi api;
  late CommunityMembersController controller;
  final seen = <http.Request>[];
  Map<String, dynamic> get group => {
    'id': commGroup,
    'name': '本地合成中文社群',
    'description': '仅合成UI测试',
    'visibility': 'private',
    'joinPolicy': 'request',
    'status': 'active',
    'memberCount': 2,
    'myRole': role,
    'myStatus': 'active',
  };
  Map<String, dynamic> get member => {
    'id': commMember,
    'communityId': commGroup,
    'userAccountId': commPeer,
    'displayName': '测试成员名字很长但应完整可阅读',
    'role': 'member',
    'status': 'active',
  };
  http.Response json(dynamic data, [int status = 200]) => http.Response.bytes(
    utf8.encode(jsonEncode({'data': data})),
    status,
    headers: {'content-type': 'application/json; charset=utf-8'},
  );
  Future<http.Response> handle(http.Request r) async {
    seen.add(r);
    if (r.method == 'GET') {
      if (r.url.path.endsWith('/members')) {
        return json([member]);
      }
      if (r.url.path.endsWith('/requests')) {
        return json([
          {...member, 'status': 'pending'},
        ]);
      }
      if (r.url.path == '/v1/me/ties') {
        return failFriends
            ? http.Response('', 503)
            : json([
                {
                  'id': commMember,
                  'otherAccountId': commPeer,
                  'otherName': '合成好友',
                },
              ]);
      }
      return json(group);
    }
    if (r.headers['X-Birdtie-Community-Preview'] == '1') {
      previews++;
      if (delay != null) await delay!.future;
      final path = r.url.path;
      var op = path.split('/').last, target = '', desiredRole = '';
      if (op == 'invitations' || op == 'transfer-owner') {
        op = op == 'invitations' ? 'invite' : 'transfer';
        target = jsonDecode(r.body)['userAccountId'] as String;
      }
      if (op == 'role') {
        target = commMember;
        desiredRole = jsonDecode(r.body)['role'] as String;
      }
      if (r.method == 'DELETE') {
        op = 'remove';
        target = commMember;
      }
      if (op == 'approve' || op == 'reject') target = commMember;
      return json({
        'snapshot': 'SYNTHETIC_CONTRACT_ONLY',
        'actorId': wrongActor ?? commOwner,
        'communityId': commGroup,
        'targetId': wrongTarget ?? target,
        'operation': op,
        'role': desiredRole,
        'expiresAt': DateTime.now()
            .toUtc()
            .add(const Duration(seconds: 25))
            .toIso8601String(),
      });
    }
    writes++;
    final writeGate = writeDelay;
    if (writeGate != null) await writeGate.future;
    if (failWrite) return http.Response('', 503);
    if (r.url.path.endsWith('/archive') ||
        r.method == 'DELETE' ||
        r.url.path.endsWith('/transfer-owner')) {
      return http.Response('', 204);
    }
    return json({
      ...member,
      'status': r.url.path.endsWith('/invitations')
          ? 'invited'
          : (r.url.path.endsWith('/reject') ? 'rejected' : 'active'),
      'role': r.url.path.endsWith('/role')
          ? jsonDecode(r.body)['role']
          : 'member',
    }, r.url.path.endsWith('/invitations') ? 201 : 200);
  }

  void switchActor(String? who, String? auth) {
    actor = who;
    token = auth;
    notifyListeners();
  }

  @override
  void dispose() {
    controller.dispose();
    api.dispose();
    super.dispose();
  }
}

void main() {
  test('迟到确认清理不能取消A到B到A后的新处理中状态', () async {
    final h = CommunityHarness();
    addTearDown(h.dispose);
    final oldGate = Completer<void>(), newGate = Completer<void>();
    h.writeDelay = oldGate;
    await h.controller.prepare(
      CommunityAction.remove(commGroup, commMember),
      target: '旧操作对象',
      consequence: '移除成员',
    );
    final oldConfirm = h.controller.confirm();
    await Future<void>.delayed(Duration.zero);
    h.switchActor(commPeer, 'Bearer B');
    h.switchActor(commOwner, 'Bearer SYNTHETIC_A');
    h.writeDelay = newGate;
    await h.controller.prepare(
      CommunityAction.remove(commGroup, commMember),
      target: '新的明确操作对象',
      consequence: '新一轮明确确认',
    );
    final newConfirm = h.controller.confirm();
    await Future<void>.delayed(Duration.zero);
    try {
      expect(h.controller.busy, isTrue);
      oldGate.complete();
      expect(await oldConfirm, isFalse);
      expect(h.controller.busy, isTrue);
      expect(h.writes, 2);
    } finally {
      if (!oldGate.isCompleted) oldGate.complete();
      if (!newGate.isCompleted) newGate.complete();
      await oldConfirm;
      await newConfirm;
    }
  });
  test('同一预览重复确认只提交一次', () async {
    final h = CommunityHarness();
    addTearDown(h.dispose);
    await h.controller.prepare(
      CommunityAction.remove(commGroup, commMember),
      target: '合成成员',
      consequence: '失去访问',
    );
    final outcomes = await Future.wait([
      h.controller.confirm(),
      h.controller.confirm(),
    ]);
    expect(outcomes, [true, false]);
    expect(h.writes, 1);
  });
  test('审批分为读取预览、明确确认和当前回执', () async {
    final h = CommunityHarness();
    addTearDown(h.dispose);
    await h.controller.reload();
    await h.controller.prepare(
      CommunityAction.decide(commGroup, commMember, approve: true),
      target: '合成成员',
      consequence: '会获得成员访问',
    );
    expect(h.previews, 1);
    expect(h.writes, 0);
    expect(h.controller.proposal, isNotNull);
    expect(await h.controller.confirm(), true);
    expect(h.writes, 1);
    expect(h.seen.last.method, 'GET');
    expect(h.controller.proposal, isNull);
  });
  test('切换主体清空名单与批准，A到B再回A仍不能复用', () async {
    final h = CommunityHarness();
    addTearDown(h.dispose);
    await h.controller.reload();
    await h.controller.prepare(
      CommunityAction.remove(commGroup, commMember),
      target: '合成成员',
      consequence: '失去访问',
    );
    h.switchActor(commPeer, 'Bearer B');
    h.switchActor(commOwner, 'Bearer SYNTHETIC_A');
    expect(h.controller.item, isNull);
    expect(h.controller.members, isEmpty);
    expect(h.controller.proposal, isNull);
    expect(await h.controller.confirm(), false);
    expect(h.writes, 0);
  });
  test('迟到预览与重复确认不产生额外写入', () async {
    final h = CommunityHarness();
    addTearDown(h.dispose);
    h.delay = Completer<void>();
    final pending = h.controller.prepare(
      CommunityAction.remove(commGroup, commMember),
      target: '成员',
      consequence: '移除',
    );
    h.switchActor(commPeer, 'Bearer B');
    h.delay!.complete();
    await pending;
    expect(h.controller.proposal, isNull);
    expect(h.writes, 0);
  });
  test('错误回复主体或目标拒绝确认', () async {
    for (final mode in ['actor', 'target']) {
      final h = CommunityHarness();
      if (mode == 'actor') {
        h.wrongActor = commPeer;
      } else {
        h.wrongTarget = commOwner;
      }
      await h.controller.prepare(
        CommunityAction.remove(commGroup, commMember),
        target: '成员',
        consequence: '移除',
      );
      expect(h.controller.proposal, isNull);
      expect(h.writes, 0);
      h.dispose();
    }
  });
  test('结果未知先读取权威状态，不盲重试', () async {
    final h = CommunityHarness();
    addTearDown(h.dispose);
    h.failWrite = true;
    await h.controller.prepare(
      CommunityAction.remove(commGroup, commMember),
      target: '成员',
      consequence: '移除',
    );
    expect(await h.controller.confirm(), false);
    expect(h.writes, 1);
    expect(h.controller.unknownResult, true);
    expect(h.controller.message, contains('待核实'));
    expect(h.controller.item, isNotNull);
    expect(await h.controller.confirm(), false);
    expect(h.writes, 1);
  });
  test('好友读取失败不阻断已有成员管理', () async {
    final h = CommunityHarness();
    addTearDown(h.dispose);
    h.failFriends = true;
    await h.controller.reload();
    expect(h.controller.item, isNotNull);
    expect(h.controller.members, hasLength(1));
    expect(h.controller.friendMessage, contains('仍可使用'));
  });
  test('刷新与取消都丢弃已有批准', () async {
    final h = CommunityHarness();
    addTearDown(h.dispose);
    await h.controller.prepare(
      CommunityAction.remove(commGroup, commMember),
      target: '成员',
      consequence: '移除',
    );
    h.controller.cancelProposal();
    expect(await h.controller.confirm(), false);
    await h.controller.prepare(
      CommunityAction.remove(commGroup, commMember),
      target: '成员',
      consequence: '移除',
    );
    await h.controller.reload();
    expect(await h.controller.confirm(), false);
    expect(h.writes, 0);
  });
}
