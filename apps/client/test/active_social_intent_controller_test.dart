import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/active_social_intent_api.dart';
import 'package:birdtie_client/src/workspace/active_social_intent_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'active_social_intent_api_test.dart';

void main() {
  test(
    'local reset writes nothing and specific approval does not activate edit',
    () async {
      final methods = <String>[];
      Map<String, dynamic>? preview;
      final source = nowItem(status: 'ACTIVE');
      final c = ActiveSocialIntentController(
        api: ActiveSocialIntentAPI(
          client: MockClient((r) async {
            methods.add(r.method);
            if (r.url.path.endsWith('/preview')) {
              final b = jsonDecode(r.body);
              preview = nowPreview(source, b['operation'], edit: b['edit']);
              return nowResponse(preview!);
            }
            if (r.url.path.endsWith('/approve')) {
              return nowResponse(nowReceipt(preview!));
            }
            return nowResponse(
              r.url.path.endsWith('/options') ? nowOptions() : nowList(source),
            );
          }),
        ),
        identity: () => const ActiveIntentIdentity('Bearer A', nowOwner, null),
      );
      await c.load();
      c.select(nowIntent);
      c.reset();
      expect(c.selected, isNull);
      expect(methods.every((m) => m == 'GET'), isTrue);
      expect(c.message, contains('服务器意图未取消'));
      c.select(nowIntent);
      final edit = c.selected!.draft..['title'] = '修改本人计划';
      final p = (await c.prepare('EDIT', edit: edit))!;
      expect(p.after.status, 'DRAFT');
      await c.approve(p, c.generation);
      expect(c.selected!.status, 'DRAFT');
      expect(methods.where((m) => m == 'POST'), hasLength(2));
      expect(c.options, isNull);
      c.dispose();
    },
  );
  test('unknown approve only factual original GET no blind repeat', () async {
    var posts = 0;
    final paths = <String>[];
    final source = nowItem();
    final c = ActiveSocialIntentController(
      api: ActiveSocialIntentAPI(
        client: MockClient((r) async {
          paths.add(r.url.path);
          if (r.method == 'POST') {
            posts++;
            if (r.url.path.endsWith('/approve')) {
              throw Exception('lost response');
            }
            return nowResponse(nowPreview(source, 'CANCEL'));
          }
          if (r.url.path.endsWith('/options')) return nowResponse(nowOptions());
          if (r.url.path.endsWith(nowIntent)) {
            return nowResponse({...nowEnvelope(), 'item': source});
          }
          return nowResponse(nowList(source));
        }),
      ),
      identity: () => const ActiveIntentIdentity('Bearer A', nowOwner, null),
    );
    await c.load();
    c.select(nowIntent);
    final p = (await c.prepare('CANCEL'))!;
    await c.approve(p, c.generation);
    expect(c.unknown, isTrue);
    expect(await c.prepare('CANCEL'), isNull);
    await c.reconcile();
    expect(c.unknown, isFalse);
    expect(posts, 2);
    expect(paths.last, endsWith(nowIntent));
    expect(c.message, contains('不能据此证明'));
    c.dispose();
  });
  test(
    'identity ABA retires pending and original preview no permission',
    () async {
      var who = const ActiveIntentIdentity('Bearer A', nowOwner, null);
      final delayed = Completer<http.Response>();
      var requests = 0;
      final c = ActiveSocialIntentController(
        api: ActiveSocialIntentAPI(
          client: MockClient((r) {
            requests++;
            return delayed.future;
          }),
        ),
        identity: () => who,
      );
      final pending = c.load();
      who = const ActiveIntentIdentity('Bearer B', nowOwner, null);
      c.sync();
      who = const ActiveIntentIdentity('Bearer A', nowOwner, null);
      c.sync();
      delayed.complete(nowResponse(nowList()));
      await pending;
      expect(c.list, isNull);
      expect(c.current, isFalse);
      await c.load();
      expect(requests, 1);
      c.dispose();
    },
  );
  test('expiry refuses old approval', () async {
    final clock = DateTime.now();
    var now = clock, posts = 0;
    final c = ActiveSocialIntentController(
      api: ActiveSocialIntentAPI(
        client: MockClient((r) async {
          if (r.method == 'POST') {
            posts++;
            return nowResponse(nowPreview(nowItem(), 'CANCEL'));
          }
          return nowResponse(
            r.url.path.endsWith('/options') ? nowOptions() : nowList(),
          );
        }),
      ),
      identity: () => const ActiveIntentIdentity('Bearer A', nowOwner, null),
      now: () => now,
    );
    await c.load();
    c.select(nowIntent);
    final p = (await c.prepare('CANCEL'))!;
    now = p.expiresAt;
    c.expire();
    await c.approve(p, c.generation);
    expect(posts, 1);
    expect(c.review, isNull);
    c.dispose();
  });
}
