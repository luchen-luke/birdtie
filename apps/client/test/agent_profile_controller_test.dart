import 'dart:async';
import 'package:birdtie_client/src/workspace/agent_profile_api.dart';
import 'package:birdtie_client/src/workspace/agent_profile_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_profile_api_test.dart';

void main() {
  test('五源独立读取与过期清私密显示', () async {
    final client = ProfileTrackedClient(
      (r) async => profileResponse(profileData(r.url.path)),
    );
    final c = AgentProfileController(
      api: AgentProfileAPI(client: client, apiBaseUrl: 'http://local'),
      authorizationHeader: () => 'Bearer owner',
      accountID: () => profileOwner,
    );
    await c.load();
    expect(c.values.length, 5);
    expect(c.errors, isEmpty);
    await c.detail(profileMemory);
    expect(c.selectedDetail!.memory, isNotNull);
    c.dispose();
    c.dispose();
    expect(client.closeCount, 0);
    client.close();
  });
  test('来源失败不把其它记录猜成空或全知', () async {
    final client = MockClient(
      (r) async => r.url.path.endsWith('community-interests')
          ? http.Response('{}', 503)
          : profileResponse(profileData(r.url.path)),
    );
    final c = AgentProfileController(
      api: AgentProfileAPI(client: client, apiBaseUrl: 'http://local'),
      authorizationHeader: () => 'Bearer owner',
      accountID: () => profileOwner,
    );
    await c.load();
    expect(c.interests, isNull);
    expect(c.errors['interests'], isNotNull);
    expect(c.profile, isNotNull);
    c.dispose();
    client.close();
  });
  for (final change in ['owner', 'token', 'workspace']) {
    test('$change A-B-A永久退役且迟到不恢复旧资料', () async {
      String? token = 'Bearer a', owner = profileOwner, workspace;
      final late = Completer<http.Response>();
      final client = MockClient(
        (r) async => r.url.path.endsWith('agent-private-profile')
            ? late.future
            : profileResponse(profileData(r.url.path)),
      );
      final c = AgentProfileController(
        api: AgentProfileAPI(client: client, apiBaseUrl: 'http://local'),
        authorizationHeader: () => token,
        accountID: () => owner,
        organizationWorkspaceID: () => workspace,
      );
      final load = c.load();
      await Future<void>.delayed(Duration.zero);
      if (change == 'owner') owner = '82000000-0000-4000-8000-000000000099';
      if (change == 'token') token = 'Bearer b';
      if (change == 'workspace') {
        workspace = '82000000-0000-4000-8000-000000000099';
      }
      c.identityChanged();
      owner = profileOwner;
      token = 'Bearer a';
      workspace = null;
      c.identityChanged();
      late.complete(profileResponse(profileRaw()));
      await load;
      expect(c.retired, true);
      expect(c.values, isEmpty);
      expect(c.current, false);
      c.dispose();
      client.close();
    });
  }
  test('各来源Agent不同必须清全部资料', () async {
    final client = MockClient((r) async {
      final v = profileData(r.url.path);
      if (r.url.path.endsWith('agent-policies')) {
        v['agentId'] = '82000000-0000-4000-8000-000000000099';
      }
      return profileResponse(v);
    });
    final c = AgentProfileController(
      api: AgentProfileAPI(client: client, apiBaseUrl: 'http://local'),
      authorizationHeader: () => 'Bearer owner',
      accountID: () => profileOwner,
    );
    await c.load();
    expect(c.retired, true);
    expect(c.values, isEmpty);
    c.dispose();
    client.close();
  });
  test('401或403后不释放兄弟source私密值', () async {
    final client = MockClient(
      (r) async => r.url.path.endsWith('agent-policies')
          ? http.Response('{}', 403)
          : profileResponse(profileData(r.url.path)),
    );
    final c = AgentProfileController(
      api: AgentProfileAPI(client: client, apiBaseUrl: 'http://local'),
      authorizationHeader: () => 'Bearer owner',
      accountID: () => profileOwner,
    );
    await c.load();
    expect(c.retired, true);
    expect(c.values, isEmpty);
    c.dispose();
    client.close();
  });
  for (final delayed in [false, true]) {
    test('server比client未来短lease ${delayed ? '等待越界不首次释放' : '末端清正文'}', () async {
      final entered = Completer<void>(), response = Completer<http.Response>();
      final server = DateTime.now().toUtc().add(const Duration(hours: 1));
      final client = MockClient((r) async {
        if (r.url.path.endsWith('/$profileMemory')) {
          entered.complete();
          return response.future;
        }
        return profileResponse(profileData(r.url.path));
      });
      final c = AgentProfileController(
        api: AgentProfileAPI(client: client, apiBaseUrl: 'http://local'),
        authorizationHeader: () => 'Bearer owner',
        accountID: () => profileOwner,
      );
      await c.load();
      final read = c.detail(profileMemory);
      await entered.future;
      if (delayed) {
        await Future<void>.delayed(const Duration(milliseconds: 150));
      }
      response.complete(
        profileResponse(
          detailRaw(at: server, lease: const Duration(milliseconds: 100)),
        ),
      );
      await read;
      if (!delayed) {
        expect(c.selectedDetail, isNotNull);
        await Future<void>.delayed(const Duration(milliseconds: 150));
      }
      expect(c.selectedDetail, isNull);
      expect(c.errors['detail'], contains('到期'));
      c.dispose();
      client.close();
    });
  }
  test('未登录或组织身份零GET零POST', () async {
    var calls = 0;
    final client = MockClient((r) async {
      calls++;
      return profileResponse(profileRaw());
    });
    for (final mode in ['anonymous', 'organization']) {
      final c = AgentProfileController(
        api: AgentProfileAPI(client: client, apiBaseUrl: 'http://local'),
        authorizationHeader: () => mode == 'anonymous' ? null : 'Bearer owner',
        accountID: () => profileOwner,
        organizationWorkspaceID: () =>
            mode == 'organization' ? profileAgent : null,
      );
      await c.load();
      expect(c.values, isEmpty);
      c.dispose();
    }
    expect(calls, 0);
    client.close();
  });
}
