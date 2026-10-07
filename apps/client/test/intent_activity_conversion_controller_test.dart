import 'dart:async';
import 'package:birdtie_client/src/workspace/active_social_intent_controller.dart';
import 'package:birdtie_client/src/workspace/intent_activity_conversion_api.dart';
import 'package:birdtie_client/src/workspace/intent_activity_conversion_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'active_social_intent_api_test.dart' show nowOwner, nowIntent;
import 'intent_activity_conversion_api_test.dart';

void main() {
  test(
    'post-commit source 409 remains unknown and only original GET reconciles',
    () async {
      var posts = 0;
      final c = IntentActivityConversionController(
        api: IntentActivityConversionAPI(
          client: MockClient((r) async {
            if (r.method == 'POST') {
              posts++;
              if (r.url.path.endsWith('/approve')) {
                return http.Response(
                  '{"error":{"code":"intent_conversion_source_changed"}}',
                  409,
                );
              }
              return conversionResponse(conversionPreview());
            }
            return conversionResponse(conversionList());
          }),
        ),
        identity: () => const ActiveIntentIdentity('Bearer A', nowOwner, null),
        intentID: nowIntent,
      );
      await c.load();
      final p = (await c.prepare(c.options!.choices.single))!;
      await c.approve(p, c.generation);
      expect(c.unknown, true);
      await c.load();
      expect(posts, 2);
      expect(c.message, contains('不能证明'));
      c.dispose();
    },
  );

  test(
    'actual selected preview cancel is zero approval and late owner ABA retires permanently',
    () async {
      var identity = const ActiveIntentIdentity('Bearer A', nowOwner, null),
          approvals = 0;
      final pending = Completer<http.Response>();
      final c = IntentActivityConversionController(
        api: IntentActivityConversionAPI(
          client: MockClient((r) async {
            if (r.url.path.endsWith('/approve')) approvals++;
            if (r.url.path.endsWith('/preview')) return pending.future;
            return conversionResponse(conversionList());
          }),
        ),
        identity: () => identity,
        intentID: nowIntent,
      );
      await c.load();
      final request = c.prepare(c.options!.choices.single);
      identity = const ActiveIntentIdentity('Bearer B', nowOwner, null);
      c.sync();
      identity = const ActiveIntentIdentity('Bearer A', nowOwner, null);
      c.sync();
      pending.complete(conversionResponse(conversionPreview()));
      expect(await request, isNull);
      expect(c.current, false);
      expect(c.options, isNull);
      expect(approvals, 0);
      c.dispose();
    },
  );
  test(
    'unknown approve authoritative original GET only does not retry or claim saved from equal state',
    () async {
      var posts = 0;
      final paths = <String>[];
      final c = IntentActivityConversionController(
        api: IntentActivityConversionAPI(
          client: MockClient((r) async {
            paths.add(r.url.path);
            if (r.method == 'POST') {
              posts++;
              if (r.url.path.endsWith('/approve')) {
                throw Exception('unknown transport');
              }
              return conversionResponse(conversionPreview());
            }
            return conversionResponse(conversionList());
          }),
        ),
        identity: () => const ActiveIntentIdentity('Bearer A', nowOwner, null),
        intentID: nowIntent,
      );
      await c.load();
      final p = (await c.prepare(c.options!.choices.single))!;
      await c.approve(p, c.generation);
      expect(c.unknown, true);
      expect(c.receipt, isNull);
      await c.load();
      expect(posts, 2);
      expect(paths.last, endsWith('/$nowIntent/activity-conversion'));
      expect(c.message, contains('不能证明'));
      c.dispose();
    },
  );
  test(
    'expired preview, absent selection and dispose late response never write',
    () async {
      var now = DateTime.now(), posts = 0;
      final c = IntentActivityConversionController(
        api: IntentActivityConversionAPI(
          client: MockClient((r) async {
            if (r.method == 'POST') posts++;
            return conversionResponse(
              r.url.path.endsWith('/preview')
                  ? conversionPreview()
                  : conversionList(),
            );
          }),
        ),
        identity: () => const ActiveIntentIdentity('Bearer A', nowOwner, null),
        intentID: nowIntent,
        now: () => now,
      );
      await c.load();
      final p = (await c.prepare(c.options!.choices.single))!;
      now = p.expiresAt;
      c.expire();
      await c.approve(p, c.generation);
      expect(posts, 1);
      expect(c.review, isNull);
      c.dispose();
      final late = Completer<http.Response>();
      final d = IntentActivityConversionController(
        api: IntentActivityConversionAPI(
          client: MockClient((_) => late.future),
        ),
        identity: () => const ActiveIntentIdentity('Bearer A', nowOwner, null),
        intentID: nowIntent,
      );
      final load = d.load();
      d.dispose();
      late.complete(conversionResponse(conversionList()));
      await load;
      expect(d.options, isNull);
    },
  );
  test(
    'normal same current owner successful receipt preserves original association and does not second RSVP',
    () async {
      final p = conversionPreview(), paths = <String>[];
      final c = IntentActivityConversionController(
        api: IntentActivityConversionAPI(
          client: MockClient((r) async {
            paths.add(r.url.path);
            return conversionResponse(
              r.url.path.endsWith('/approve')
                  ? conversionReceipt(p)
                  : r.url.path.endsWith('/preview')
                  ? p
                  : conversionList(),
            );
          }),
        ),
        identity: () => const ActiveIntentIdentity('Bearer A', nowOwner, null),
        intentID: nowIntent,
      );
      await c.load();
      final preview = (await c.prepare(c.options!.choices.single))!;
      await c.approve(preview, c.generation);
      expect(c.receipt!.participationID, conversionParticipation);
      expect(c.options, isNull);
      expect(paths.every((p) => p.contains('activity-conversion')), true);
      c.dispose();
    },
  );
}
