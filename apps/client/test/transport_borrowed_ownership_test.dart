import 'dart:convert';

import 'package:birdtie_client/src/auth/authenticated_client.dart';
import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/workspace/community_api.dart';
import 'package:birdtie_client/src/workspace/now_discovery_controller.dart';
import 'package:birdtie_client/src/workspace/organization_membership_api.dart';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

class _Auth extends BirdtieAuthController {
  @override
  bool get signedIn => true;
  @override
  String get authorizationHeader => 'Bearer synthetic-owner';
  @override
  String get accountID => 'synthetic-owner';
}

void main() {
  test(
    'leaving borrowed API routes keeps the shared private connection usable',
    () async {
      final auth = _Auth();
      addTearDown(auth.dispose);
      var recoveries = 0;
      final paths = <String>[];
      final transport = MockClient((r) async {
        expect(r.headers['Authorization'], auth.authorizationHeader);
        paths.add(r.url.path);
        return http.Response.bytes(utf8.encode('{"data":[]}'), 200);
      });
      addTearDown(transport.close);
      final client = AuthenticatedClient(
        auth: auth,
        client: transport,
        onUnauthorized: (_) async {
          recoveries++;
        },
      );
      addTearDown(client.close);
      const base = 'https://unit.invalid';
      final community = CommunityApi(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: base,
      );
      expect(await community.mine(), isEmpty);
      community.dispose();

      final membership = OrganizationMembershipApi(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: base,
      );
      expect(await membership.invitations(), isEmpty);
      membership.dispose();

      final discovery = NowDiscoveryController(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: base,
      );
      await discovery.loadCityActivities('aberdeen-gb');
      expect(discovery.cityError, isNull);
      discovery.dispose();

      final inbox = RemoteInboxSource(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: base,
      );
      expect(await inbox.load(), isEmpty);
      inbox.dispose();
      final secondInbox = RemoteInboxSource(
        authorizationHeader: () => auth.authorizationHeader,
        client: client,
        apiBaseUrl: base,
      );
      expect(await secondInbox.load(), isEmpty);
      secondInbox.dispose();
      expect(paths, hasLength(5));
      expect(recoveries, 0);

      client.close();
      await expectLater(
        client.get(
          Uri.parse('$base/v1/me/inbox'),
          headers: {'Authorization': auth.authorizationHeader},
        ),
        throwsA(isA<http.ClientException>()),
      );
      expect(
        paths,
        hasLength(5),
        reason: 'only the connection owner may retire it',
      );
    },
  );
}
