import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/person_community_interest_api.dart';
import 'package:birdtie_client/src/workspace/person_community_interest_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'person_community_interest_api_test.dart';

http.Response interestReads(
  http.Request r, {
  String? state,
  bool available = true,
}) => interestResponse(
  interestView(
    options: r.url.path.endsWith('/options'),
    state: r.url.path.endsWith('/options') ? null : state,
    available: available,
  ),
);
void main() {
  test(
    'own receipt is only selector: hidden or expired native denial retires cache',
    () async {
      for (final status in [403, 409]) {
        var previews = 0, approves = 0;
        final c = PersonCommunityInterestController(
          api: PersonCommunityInterestAPI(
            client: MockClient((r) async {
              if (r.url.path.endsWith('/preview')) {
                previews++;
                if (jsonDecode(r.body)['operation'] == 'PUBLIC') {
                  return http.Response('{}', status);
                }
                return interestResponse(interestPreview());
              }
              if (r.url.path.endsWith('/approve')) {
                approves++;
                return interestResponse(interestView(state: 'PRIVATE'));
              }
              return interestReads(r);
            }),
            apiBaseUrl: 'http://fixture',
          ),
          identity: () =>
              const CommunityInterestIdentity('synthetic', interestOwner, null),
        );
        await c.load();
        expect(c.message, '已读取当前兴趣声明。');
        final p = (await c.prepare(interestCommunity, 'PRIVATE'))!;
        await c.approve(p, c.generation);
        expect(c.available, isNull);
        expect(c.own!.records.single.sourceAvailable, true);
        expect(await c.prepare(interestCommunity, 'PUBLIC'), isNull);
        expect(previews, 2);
        expect(c.own, isNull);
        expect(c.review, isNull);
        expect(c.message, contains('刷新'));
        expect(await c.prepare(interestCommunity, 'PUBLIC'), isNull);
        expect(previews, 2);
        expect(approves, 1);
        c.dispose();
      }
    },
  );
  test(
    'own PUBLIC selector late token ABA and unknown approval never reuse version',
    () async {
      final wait = Completer<http.Response>();
      var id = const CommunityInterestIdentity('a', interestOwner, null);
      var previews = 0, approves = 0;
      final c = PersonCommunityInterestController(
        api: PersonCommunityInterestAPI(
          client: MockClient((r) async {
            if (r.url.path.endsWith('/preview')) {
              previews++;
              return wait.future;
            }
            if (r.url.path.endsWith('/approve')) {
              approves++;
              throw Exception('unknown');
            }
            return interestReads(r, state: 'PRIVATE');
          }),
          apiBaseUrl: 'http://fixture',
        ),
        identity: () => id,
      );
      await c.load();
      // A current own source must work even with no option list cached.
      c.available = null;
      final future = c.prepare(interestCommunity, 'PUBLIC');
      id = const CommunityInterestIdentity('b', interestOwner, null);
      c.sync();
      id = const CommunityInterestIdentity('a', interestOwner, null);
      c.sync();
      final raw = interestPreview('PUBLIC')..['state'] = 'PRIVATE';
      raw['contextId'] = interestContext;
      wait.complete(interestResponse(raw));
      expect(await future, isNull);
      expect(c.review, isNull);
      expect(previews, 1);
      await c.load();
      c.available = null;
      final p = (await c.prepare(interestCommunity, 'PUBLIC'))!;
      await c.approve(p, c.generation);
      expect(c.unknown, true);
      expect(await c.prepare(interestCommunity, 'PUBLIC'), isNull);
      await c.approve(p, c.generation);
      expect(approves, 1);
      expect(previews, 2);
      await c.load();
      expect(c.message, contains('不能证明'));
      c.dispose();
    },
  );
  test(
    'PRIVATE default cancellation zero approve authoritative success once',
    () async {
      var approves = 0;
      final c = PersonCommunityInterestController(
        api: PersonCommunityInterestAPI(
          client: MockClient((r) async {
            if (r.url.path.endsWith('/preview')) {
              return interestResponse(interestPreview());
            }
            if (r.url.path.endsWith('/approve')) {
              approves++;
              return interestResponse(interestView(state: 'PRIVATE'));
            }
            return interestReads(r);
          }),
          apiBaseUrl: 'http://fixture',
        ),
        identity: () =>
            const CommunityInterestIdentity('synthetic', interestOwner, null),
      );
      await c.load();
      expect(c.visibility, 'PRIVATE');
      c.select(interestCommunity);
      await c.prepare(interestCommunity, 'PRIVATE');
      c.cancel();
      expect(approves, 0);
      final p = (await c.prepare(interestCommunity, 'PRIVATE'))!;
      final g = c.generation;
      await c.approve(p, g);
      await c.approve(p, g);
      expect(approves, 1);
      expect(c.own!.records.single.state, 'PRIVATE');
      c.dispose();
    },
  );
  test(
    'unknown only read no blind resend and state equality not receipt',
    () async {
      var approves = 0;
      final c = PersonCommunityInterestController(
        api: PersonCommunityInterestAPI(
          client: MockClient((r) async {
            if (r.url.path.endsWith('/preview')) {
              return interestResponse(interestPreview());
            }
            if (r.url.path.endsWith('/approve')) {
              approves++;
              throw Exception('transport unknown');
            }
            return interestReads(r, state: approves > 0 ? 'PRIVATE' : null);
          }),
          apiBaseUrl: 'http://fixture',
        ),
        identity: () =>
            const CommunityInterestIdentity('synthetic', interestOwner, null),
      );
      await c.load();
      final p = (await c.prepare(interestCommunity, 'PRIVATE'))!;
      await c.approve(p, c.generation);
      expect(c.unknown, isTrue);
      expect(await c.prepare(interestCommunity, 'PRIVATE'), isNull);
      await c.approve(p, c.generation);
      expect(approves, 1);
      await c.load();
      expect(c.unknown, isFalse);
      expect(c.message, contains('不能证明'));
      await c.approve(p, c.generation);
      expect(approves, 1);
      c.dispose();
    },
  );
  test('401 403 409 require current reload not cached preview', () async {
    for (final status in [401, 403, 409]) {
      var writes = 0;
      final c = PersonCommunityInterestController(
        api: PersonCommunityInterestAPI(
          client: MockClient((r) async {
            if (r.url.path.endsWith('/preview')) {
              return interestResponse(interestPreview());
            }
            if (r.url.path.endsWith('/approve')) {
              writes++;
              return http.Response('{}', status);
            }
            return interestReads(r);
          }),
          apiBaseUrl: 'http://fixture',
        ),
        identity: () =>
            const CommunityInterestIdentity('synthetic', interestOwner, null),
      );
      await c.load();
      final p = (await c.prepare(interestCommunity, 'PRIVATE'))!;
      await c.approve(p, c.generation);
      expect(c.own, isNull);
      expect(await c.prepare(interestCommunity, 'PRIVATE'), isNull);
      expect(writes, 1);
      c.dispose();
    }
  });
  test(
    'natural expiry identity token workspace ABA retires version zero write',
    () async {
      var now = DateTime.utc(2026, 10, 4);
      var id = const CommunityInterestIdentity('a', interestOwner, null);
      var writes = 0;
      final c = PersonCommunityInterestController(
        api: PersonCommunityInterestAPI(
          client: MockClient((r) async {
            if (r.url.path.endsWith('/preview')) {
              return interestResponse(interestPreview('PRIVATE', now));
            }
            if (r.url.path.endsWith('/approve')) writes++;
            return interestReads(r);
          }),
          apiBaseUrl: 'http://fixture',
        ),
        identity: () => id,
        now: () => now,
      );
      await c.load();
      var p = (await c.prepare(interestCommunity, 'PRIVATE'))!;
      var g = c.generation;
      now = now.add(const Duration(seconds: 90));
      c.expire();
      await c.approve(p, g);
      expect(writes, 0);
      p = (await c.prepare(interestCommunity, 'PRIVATE'))!;
      g = c.generation;
      id = const CommunityInterestIdentity('b', interestOwner, null);
      c.sync();
      id = const CommunityInterestIdentity('a', interestOwner, null);
      c.sync();
      await c.approve(p, g);
      expect(writes, 0);
      expect(c.own, isNull);
      await c.load();
      p = (await c.prepare(interestCommunity, 'PRIVATE'))!;
      g = c.generation;
      id = const CommunityInterestIdentity(
        'a',
        interestOwner,
        interestCommunity,
      );
      c.sync();
      id = const CommunityInterestIdentity('a', interestOwner, null);
      c.sync();
      await c.approve(p, g);
      expect(writes, 0);
      c.dispose();
    },
  );
  test(
    'late reads after actor switch stop before second GET and dispose responses ignored',
    () async {
      final wait = Completer<http.Response>();
      var calls = 0;
      var id = const CommunityInterestIdentity('a', interestOwner, null);
      final c = PersonCommunityInterestController(
        api: PersonCommunityInterestAPI(
          client: MockClient((_) async {
            calls++;
            return wait.future;
          }),
          apiBaseUrl: 'http://fixture',
        ),
        identity: () => id,
      );
      final f = c.load();
      id = const CommunityInterestIdentity('b', interestOwner, null);
      c.sync();
      wait.complete(interestResponse(interestView()));
      await f;
      expect(calls, 1);
      expect(c.own, isNull);
      c.dispose();
    },
  );
  test(
    'native Agent mismatch or hidden source public never gains preview',
    () async {
      var posts = 0;
      final c = PersonCommunityInterestController(
        api: PersonCommunityInterestAPI(
          client: MockClient((r) async {
            if (r.method == 'POST') posts++;
            final v = interestView(options: r.url.path.endsWith('/options'));
            if (r.url.path.endsWith('/options')) v['agentId'] = interestOwner;
            return interestResponse(v);
          }),
          apiBaseUrl: 'http://fixture',
        ),
        identity: () =>
            const CommunityInterestIdentity('synthetic', interestOwner, null),
      );
      await c.load();
      expect(c.own, isNull);
      expect(await c.prepare(interestCommunity, 'PUBLIC'), isNull);
      expect(posts, 0);
      c.dispose();
    },
  );
  test(
    'only current own hidden record can PRIVATE or DELETE not new hidden UUID',
    () async {
      final operations = <String>[];
      final c = PersonCommunityInterestController(
        api: PersonCommunityInterestAPI(
          client: MockClient((r) async {
            if (r.url.path.endsWith('/preview')) {
              final op = jsonDecode(r.body)['operation'] as String;
              operations.add(op);
              return interestResponse(interestPreview(op));
            }
            return interestResponse(
              interestView(
                state: r.url.path.endsWith('/options') ? null : 'PUBLIC',
                available: false,
              ),
            );
          }),
          apiBaseUrl: 'http://fixture',
        ),
        identity: () =>
            const CommunityInterestIdentity('synthetic', interestOwner, null),
      );
      await c.load();
      expect(await c.prepare(interestCommunity, 'PUBLIC'), isNull);
      await c.prepare(interestCommunity, 'DELETE');
      expect(operations, ['DELETE']);
      c.dispose();
    },
  );
}
