import 'dart:async';
import 'package:birdtie_client/src/workspace/now_context_selection_api.dart';
import 'package:birdtie_client/src/workspace/now_context_selection_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'model_egress_api_test.dart' show egressOwner;
import 'now_context_selection_api_test.dart';

void main() {
  test(
    'local choice preserves unrelated state and only explicit resolve POST reads current source',
    () async {
      final raw = selectionOptions();
      final calls = <String>[];
      final data = NowContextSelectionController(
        api: NowContextSelectionAPI(
          client: MockClient((r) async {
            calls.add(r.method);
            return selectionResponse(
              r.method == 'GET'
                  ? raw
                  : selectionReceipt(
                      raw,
                      (raw['items'] as List)[1] as Map<String, dynamic>,
                    ),
            );
          }),
        ),
        identity: () =>
            const NowSelectionIdentity('Bearer synthetic', egressOwner, null),
      );
      await data.load();
      data.select(data.options!.items[1]);
      expect(calls, ['GET']);
      final v = await data.resolve();
      expect(v?.contextType, 'ONLINE');
      expect(calls, ['GET', 'POST']);
      data.dispose();
    },
  );
  test(
    'pending selected read after workspace ABA never delivers late choice or writes',
    () async {
      final raw = selectionOptions();
      final pending = Completer<http.Response>();
      var workspace = '';
      var calls = 0;
      final data = NowContextSelectionController(
        api: NowContextSelectionAPI(
          client: MockClient((r) async {
            calls++;
            return r.method == 'GET' ? selectionResponse(raw) : pending.future;
          }),
        ),
        identity: () => NowSelectionIdentity(
          'Bearer synthetic',
          egressOwner,
          workspace.isEmpty ? null : workspace,
        ),
      );
      await data.load();
      data.select(data.options!.items.first);
      final result = data.resolve();
      workspace = 'org';
      data.sync();
      workspace = '';
      data.sync();
      pending.complete(
        selectionResponse(
          selectionReceipt(
            raw,
            (raw['items'] as List).first as Map<String, dynamic>,
          ),
        ),
      );
      expect(await result, isNull);
      expect(data.current, isFalse);
      expect(data.options, isNull);
      await data.load();
      expect(calls, 2);
      data.dispose();
    },
  );
  test(
    'native finite options expire and unknown resolve has read-only refresh path',
    () async {
      final raw = selectionOptions();
      var now = DateTime.now().toUtc();
      var posts = 0;
      final data = NowContextSelectionController(
        api: NowContextSelectionAPI(
          client: MockClient((r) async {
            if (r.method == 'POST') {
              posts++;
              throw StateError('unknown transport');
            }
            return selectionResponse(raw);
          }),
        ),
        identity: () =>
            const NowSelectionIdentity('Bearer synthetic', egressOwner, null),
        now: () => now,
      );
      await data.load();
      data.select(data.options!.items.first);
      expect(await data.resolve(), isNull);
      expect(data.message, contains('未修改情境声明'));
      expect(await data.resolve(), isNull);
      expect(posts, 1);
      await data.load();
      data.select(data.options!.items.first);
      now = now.add(const Duration(minutes: 2));
      data.expire();
      expect(data.options, isNull);
      expect(await data.resolve(), isNull);
      expect(posts, 1);
      data.dispose();
    },
  );
  testWidgets(
    'actual API timeout clears only selector and refuses a second readonly POST',
    (t) async {
      final raw = selectionOptions();
      final pending = Completer<http.Response>();
      var posts = 0;
      final data = NowContextSelectionController(
        api: NowContextSelectionAPI(
          client: MockClient((r) {
            if (r.method == 'POST') {
              posts++;
              return pending.future;
            }
            return Future.value(selectionResponse(raw));
          }),
        ),
        identity: () =>
            const NowSelectionIdentity('Bearer synthetic', egressOwner, null),
      );
      await data.load();
      data.select(data.options!.items.first);
      final result = data.resolve();
      await t.pump();
      await t.pump(const Duration(seconds: 13));
      expect(await result, isNull);
      expect(data.busy, isFalse);
      expect(data.options, isNull);
      expect(data.message, contains('未修改情境声明或发起查询'));
      expect(await data.resolve(), isNull);
      expect(posts, 1);
      pending.complete(
        selectionResponse(
          selectionReceipt(
            raw,
            (raw['items'] as List).first as Map<String, dynamic>,
          ),
        ),
      );
      await t.pump();
      expect(data.options, isNull);
      data.dispose();
    },
  );
  test(
    'same-token account ABA and different-token session ABA permanently retire old choices',
    () async {
      for (final account in [false, true]) {
        final raw = selectionOptions();
        final pending = Completer<http.Response>();
        var owner = egressOwner, auth = 'Bearer synthetic';
        var calls = 0;
        final data = NowContextSelectionController(
          api: NowContextSelectionAPI(
            client: MockClient((r) {
              calls++;
              return r.method == 'GET'
                  ? Future.value(selectionResponse(raw))
                  : pending.future;
            }),
          ),
          identity: () => NowSelectionIdentity(auth, owner, null),
        );
        await data.load();
        data.select(data.options!.items.first);
        final result = data.resolve();
        if (account) {
          owner = selectionContext;
        } else {
          auth = 'Bearer other-session';
        }
        data.sync();
        owner = egressOwner;
        auth = 'Bearer synthetic';
        data.sync();
        pending.complete(
          selectionResponse(
            selectionReceipt(
              raw,
              (raw['items'] as List).first as Map<String, dynamic>,
            ),
          ),
        );
        expect(await result, isNull);
        expect(data.current, isFalse);
        expect(data.selected, isNull);
        await data.load();
        expect(calls, 2);
        data.dispose();
      }
    },
  );
}
