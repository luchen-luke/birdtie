import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/activity_participation_disclosure_api.dart';
import 'package:birdtie_client/src/workspace/activity_participation_disclosure_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'activity_participation_disclosure_api_test.dart';

void main() {
  test(
    'private default no implicit expiry; PUBLIC concrete selector uses own record after result',
    () async {
      final ops = <String>[];
      DateTime? expiry;
      final c = ActivityParticipationDisclosureController(
        api: ActivityParticipationDisclosureAPI(
          client: MockClient((r) async {
            if (r.url.path.endsWith('/preview')) {
              final b = jsonDecode(r.body);
              ops.add(b['operation']);
              expiry = b['disclosureExpiresAt'] == null
                  ? null
                  : DateTime.parse(b['disclosureExpiresAt']);
              return disclosureResponse(
                disclosurePreview(b['operation'], expiry: expiry),
              );
            }
            if (r.url.path.endsWith('/approve')) {
              return disclosureResponse(disclosureView());
            }
            return disclosureResponse(disclosureView());
          }),
        ),
        identity: () => const ParticipationDisclosureIdentity(
          'Bearer A',
          disclosureOwner,
          null,
        ),
      );
      await c.load();
      expect(c.visibility, 'PRIVATE');
      expect(c.chosenExpiry, isNull);
      expect(await c.prepare(disclosureP, 'PUBLIC'), isNull);
      expect(ops, isEmpty);
      expect(c.message, contains('具体期限'));
      final p = (await c.prepare(disclosureP, 'PRIVATE'))!;
      await c.approve(p, c.generation);
      expect(c.available, isNull);
      c.chooseExpiry(DateTime.now().toUtc().add(const Duration(hours: 1)));
      expect(await c.prepare(disclosureP, 'PUBLIC'), isNotNull);
      expect(ops, ['PRIVATE', 'PUBLIC']);
      c.dispose();
    },
  );
  test(
    'identity A B A permanently retires controller; late read ignored',
    () async {
      var identity = const ParticipationDisclosureIdentity(
        'Bearer A',
        disclosureOwner,
        null,
      );
      final delayed = Completer<http.Response>();
      var count = 0;
      final c = ActivityParticipationDisclosureController(
        api: ActivityParticipationDisclosureAPI(
          client: MockClient((r) {
            count++;
            return delayed.future;
          }),
        ),
        identity: () => identity,
      );
      final pending = c.load();
      identity = const ParticipationDisclosureIdentity(
        'Bearer B',
        disclosureOwner,
        null,
      );
      c.sync();
      identity = const ParticipationDisclosureIdentity(
        'Bearer A',
        disclosureOwner,
        null,
      );
      c.sync();
      delayed.complete(disclosureResponse(disclosureView()));
      await pending;
      expect(c.own, isNull);
      expect(c.current, false);
      await c.load();
      expect(count, 1);
      c.dispose();
    },
  );
  test(
    'unknown approve only read reconciles without retry or claiming prior operation',
    () async {
      var posts = 0;
      final c = ActivityParticipationDisclosureController(
        api: ActivityParticipationDisclosureAPI(
          client: MockClient((r) async {
            if (r.url.path.endsWith('/preview')) {
              return disclosureResponse(disclosurePreview('PRIVATE'));
            }
            if (r.url.path.endsWith('/approve')) {
              posts++;
              throw StateError('network unknown');
            }
            return disclosureResponse(disclosureView());
          }),
        ),
        identity: () => const ParticipationDisclosureIdentity(
          'Bearer A',
          disclosureOwner,
          null,
        ),
      );
      await c.load();
      expect(c.message, contains('已读取当前报名'));
      expect(c.message, isNot(contains('未知')));
      final p = (await c.prepare(disclosureP, 'PRIVATE'))!;
      await c.approve(p, c.generation);
      expect(c.unknown, true);
      expect(c.canPrepare, false);
      await c.approve(p, c.generation);
      expect(posts, 1);
      await c.load();
      expect(c.unknown, false);
      expect(c.message, contains('不能证明'));
      expect(posts, 1);
      c.dispose();
    },
  );
  test(
    'hidden source PUBLIC denied locally PRIVATE may obtain new native preview',
    () async {
      var posts = 0;
      final c = ActivityParticipationDisclosureController(
        api: ActivityParticipationDisclosureAPI(
          client: MockClient((r) async {
            if (r.url.path.endsWith('/preview')) {
              posts++;
              return disclosureResponse(
                disclosurePreview('PRIVATE', available: false),
              );
            }
            return disclosureResponse(
              disclosureView(state: 'PUBLIC', available: false),
            );
          }),
        ),
        identity: () => const ParticipationDisclosureIdentity(
          'Bearer A',
          disclosureOwner,
          null,
        ),
      );
      await c.load();
      c.chooseExpiry(DateTime.now().add(const Duration(hours: 1)));
      expect(await c.prepare(disclosureP, 'PUBLIC'), isNull);
      expect(posts, 0);
      expect(await c.prepare(disclosureP, 'PRIVATE'), isNotNull);
      expect(posts, 1);
      c.dispose();
    },
  );
  test(
    'native409 clears stale source and expired preview never approves',
    () async {
      var futureNow = DateTime.now();
      var approve = 0, fail = false;
      final c = ActivityParticipationDisclosureController(
        api: ActivityParticipationDisclosureAPI(
          client: MockClient((r) async {
            if (r.url.path.endsWith('/preview')) {
              return fail
                  ? http.Response('{}', 409)
                  : disclosureResponse(disclosurePreview('PRIVATE'));
            }
            if (r.url.path.endsWith('/approve')) {
              approve++;
              return disclosureResponse(disclosureView());
            }
            return disclosureResponse(disclosureView());
          }),
        ),
        identity: () => const ParticipationDisclosureIdentity(
          'Bearer A',
          disclosureOwner,
          null,
        ),
        now: () => futureNow,
      );
      await c.load();
      final p = (await c.prepare(disclosureP, 'PRIVATE'))!;
      futureNow = p.expiresAt;
      c.expire();
      await c.approve(p, c.generation);
      expect(approve, 0);
      expect(c.review, isNull);
      futureNow = DateTime.now();
      fail = true;
      expect(await c.prepare(disclosureP, 'PRIVATE'), isNull);
      expect(c.own, isNull);
      expect(c.message, contains('刷新'));
      c.dispose();
    },
  );
}
