import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_introduction_api.dart';
import 'package:birdtie_client/src/workspace/agent_introduction_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_introduction_api_test.dart';

AgentIntroductionController introController(
  MockClient client, {
  String? Function()? token,
  String? Function()? owner,
  String? Function()? workspace,
  String? initial,
}) => AgentIntroductionController(
  api: AgentIntroductionAPI(client: client, apiBaseUrl: 'http://fixture'),
  authorizationHeader: token ?? () => 'Bearer own',
  accountID: owner ?? () => introOwner,
  organizationWorkspaceID: workspace ?? () => null,
  initialSourceIntentID: initial,
);
http.Response introReads(
  http.Request r, {
  bool consent = true,
  bool enabled = true,
  List<dynamic>? intents,
}) => introResponse(switch (r.url.path) {
  '/v1/me/new-people/consent' => {'enabled': consent},
  '/v1/me/new-people/intents' => intents ?? [introIntent()],
  '/v1/me/agent-policies' => introPolicy(enabled: enabled),
  _ => introResult(),
});
void main() {
  test(
    'shared-registration policy review retains other four native preferences and cancellation has no PUT',
    () async {
      var writes = 0;
      final c = introController(
        MockClient((r) async {
          if (r.method != 'GET') {
            writes++;
          }
          return introReads(r);
        }),
      );
      await c.load();
      final original = c.policy!;
      c.preparePolicy(
        unknownPerson: true,
        community: true,
        sharedActivity: true,
        days: 1,
      );
      final p = c.review!;
      expect(p.original, same(original));
      expect(p.rules['SHARED_ACTIVITY'], 'REVIEW_REQUIRED');
      for (final k in [
        'SAME_UNIVERSITY',
        'EXISTING_CONNECTION',
        'BUSINESS',
        'ORGANIZATION',
      ]) {
        expect(p.rules[k], original.rules[k]);
      }
      c.cancelPolicy();
      await c.savePolicy(p);
      expect(writes, 0);
      c.dispose();
    },
  );

  test(
    'pending concrete policy save rejects repeated clicks while original response is pending',
    () async {
      final held = Completer<http.Response>(), entered = Completer<void>();
      var puts = 0;
      final c = introController(
        MockClient((r) async {
          if (r.method == 'PUT') {
            puts++;
            entered.complete();
            return held.future;
          }
          return introReads(r);
        }),
      );
      await c.load();
      c.preparePolicy(unknownPerson: true, community: false, days: 7);
      final d = c.review!;
      final first = c.savePolicy(d);
      await entered.future;
      await c.savePolicy(d);
      expect(puts, 1);
      held.complete(
        introResponse(
          introPolicy(
            revision: 2,
            values: d.rules,
            expiry: d.expiry.toIso8601String(),
          ),
        ),
      );
      await first;
      expect(c.policy!.revision, 2);
      expect(c.review, isNull);
      expect(puts, 1);
      c.dispose();
    },
  );
  test(
    'navigation checks current token even before identity notification and rejects policy expiry',
    () async {
      var token = 'Bearer A';
      final deadline = introStamp(const Duration(milliseconds: 180));
      final c = introController(
        MockClient(
          (r) async => r.url.path.endsWith('agent-policies')
              ? introResponse(introPolicy(expiry: deadline))
              : introReads(r),
        ),
        token: () => token,
      );
      await c.load();
      c.choose(introSource);
      await c.search();
      final candidate = c.result!.candidates.single;
      token = 'Bearer B';
      expect(c.candidateCurrent(candidate), false);
      expect(c.currentSourceID, isNull);
      token = 'Bearer A';
      expect(c.candidateCurrent(candidate), true);
      await Future<void>.delayed(const Duration(milliseconds: 200));
      expect(c.candidateCurrent(candidate), false);
      c.expire();
      expect(c.result, isNull);
      c.dispose();
    },
  );
  test(
    'late suggestions cannot overwrite a restored A identity after B workspace',
    () async {
      String? workspace;
      final held = Completer<http.Response>(), entered = Completer<void>();
      final c = introController(
        MockClient((r) async {
          if (r.url.path.endsWith('agent-introductions')) {
            entered.complete();
            return held.future;
          }
          return introReads(r);
        }),
        workspace: () => workspace,
      );
      await c.load();
      c.choose(introSource);
      final loading = c.search();
      await entered.future;
      workspace = introPeer;
      c.synchronizeIdentity();
      workspace = null;
      c.synchronizeIdentity();
      held.complete(introResponse(introResult()));
      await loading;
      expect(c.result, isNull);
      expect(c.selectedID, isNull);
      expect(c.policy, isNull);
      c.dispose();
    },
  );
  test(
    'only current own PUBLIC intent can query human suggestions, no automatic effects',
    () async {
      final calls = <http.Request>[];
      final rows = [
        introIntent(audience: 'PRIVATE'),
        introIntent()..['id'] = introPeerIntent,
      ];
      final c = introController(
        MockClient((r) async {
          calls.add(r);
          return introReads(r, intents: rows);
        }),
        initial: introSource,
      );
      await c.load();
      expect(c.available.single.id, introPeerIntent);
      expect(c.selectedID, isNull);
      c.choose(introSource);
      await c.search();
      expect(
        calls.where((r) => r.url.path.endsWith('agent-introductions')),
        isEmpty,
      );
      expect(calls.every((r) => r.method == 'GET'), true);
      c.dispose();
    },
  );
  test(
    'disabled matching consent and conservative policy never auto enable or query',
    () async {
      for (final enabled in [true, false]) {
        final calls = <http.Request>[];
        final c = introController(
          MockClient((r) async {
            calls.add(r);
            return introReads(r, consent: false, enabled: enabled);
          }),
        );
        await c.load();
        c.choose(introSource);
        await c.search();
        expect(c.canSearch, false);
        expect(calls.length, 3);
        expect(calls.every((r) => r.method == 'GET'), true);
        c.dispose();
      }
    },
  );
  test(
    'current query uses original selector, candidate navigation is blocked at natural expiry',
    () async {
      final end = introStamp(const Duration(milliseconds: 120));
      final c = introController(
        MockClient(
          (r) async => r.url.path.endsWith('agent-introductions')
              ? introResponse(introResult(end: end))
              : introReads(r),
        ),
      );
      await c.load();
      c.choose(introSource);
      await c.search();
      final candidate = c.result!.candidates.single;
      expect(c.candidateCurrent(candidate), true);
      await Future<void>.delayed(const Duration(milliseconds: 150));
      expect(c.candidateCurrent(candidate), false);
      c.expire();
      expect(c.result, isNull);
      c.dispose();
    },
  );
  test(
    'concrete social save failure stays unknown then authoritative GET never resends',
    () async {
      var puts = 0, reads = 0;
      final c = introController(
        MockClient((r) async {
          if (r.method == 'PUT') {
            puts++;
            throw http.ClientException('lost response');
          }
          if (r.url.path.endsWith('agent-policies')) reads++;
          return introReads(r);
        }),
      );
      await c.load();
      c.preparePolicy(unknownPerson: true, community: false, days: 7);
      final draft = c.review!;
      await c.savePolicy(draft);
      expect(c.unknownSave, true);
      expect(puts, 1);
      await c.savePolicy(draft);
      expect(puts, 1);
      await c.load();
      expect(reads, 2);
      expect(c.unknownSave, false);
      expect(puts, 1);
      expect(c.review, isNull);
      c.dispose();
    },
  );
  test(
    '409 CAS rejects original preview and only explicit re-read creates a new version review',
    () async {
      var puts = 0;
      final c = introController(
        MockClient((r) async {
          if (r.method == 'PUT') {
            puts++;
            return introResponse({}, status: 409);
          }
          return introReads(r);
        }),
      );
      await c.load();
      c.preparePolicy(unknownPerson: true, community: false, days: 1);
      final d = c.review!;
      await c.savePolicy(d);
      expect(c.policy, isNull);
      expect(c.review, isNull);
      await c.savePolicy(d);
      expect(puts, 1);
      await c.load();
      expect(c.policy, isNotNull);
      expect(c.review, isNull);
      expect(puts, 1);
      c.dispose();
    },
  );
  test(
    'save preserves all other current preferences and sends one original revision',
    () async {
      final calls = <http.Request>[];
      final c = introController(
        MockClient((r) async {
          calls.add(r);
          if (r.method == 'PUT') {
            final body = jsonDecode(r.body);
            return introResponse(
              introPolicy(
                revision: 2,
                values: {
                  for (final v in body['settings']['rules'])
                    v['category'] as String: v['preference'] as String,
                },
                expiry: body['expiresAt'],
              ),
            );
          }
          return introReads(r);
        }),
      );
      await c.load();
      c.preparePolicy(unknownPerson: true, community: true, days: 7);
      final d = c.review!;
      await c.savePolicy(d);
      expect(c.policy!.revision, 2);
      expect(c.policy!.rules['SHARED_COMMUNITY'], 'REVIEW_REQUIRED');
      expect(c.policy!.rules['BUSINESS'], 'DISABLED');
      expect(calls.where((r) => r.method == 'PUT').length, 1);
      c.dispose();
    },
  );
  test(
    'A-B-A token and workspace changes retire late approved settings and original review',
    () async {
      var token = 'Bearer A';
      String? workspace;
      final held = Completer<http.Response>();
      final entered = Completer<void>();
      var puts = 0;
      final c = introController(
        MockClient((r) async {
          if (r.method == 'PUT') {
            puts++;
            entered.complete();
            return held.future;
          }
          return introReads(r);
        }),
        token: () => token,
        workspace: () => workspace,
      );
      await c.load();
      c.preparePolicy(unknownPerson: true, community: false, days: 7);
      final d = c.review!;
      final waiting = c.savePolicy(d);
      await entered.future;
      token = 'Bearer B';
      workspace = introPeer;
      c.synchronizeIdentity();
      token = 'Bearer A';
      workspace = null;
      c.synchronizeIdentity();
      held.complete(
        introResponse(
          introPolicy(
            revision: 2,
            values: d.rules,
            expiry: d.expiry.toIso8601String(),
          ),
        ),
      );
      await waiting;
      expect(c.policy, isNull);
      expect(c.review, isNull);
      expect(c.message, isNull);
      expect(puts, 1);
      c.dispose();
    },
  );
  test(
    'dispose and cancel create no policy write or replacement request',
    () async {
      var writes = 0;
      final c = introController(
        MockClient((r) async {
          if (r.method != 'GET') writes++;
          return introReads(r);
        }),
      );
      await c.load();
      c.preparePolicy(unknownPerson: true, community: false, days: 7);
      final d = c.review!;
      c.cancelPolicy();
      await c.savePolicy(d);
      expect(writes, 0);
      c.dispose();
    },
  );
}
