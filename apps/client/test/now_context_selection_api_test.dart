import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:birdtie_client/src/workspace/now_context_selection_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'model_egress_api_test.dart' show egressOwner, egressAgent;

const selectionContext = '00000000-0000-4000-8000-000000000006';
Map<String, dynamic> selectionOption({
  String mode = 'CURRENT',
  bool declared = true,
  String kind = 'CITY',
  String relation = 'current',
}) => {
  'optionId': List.filled(
    64,
    mode == 'ONLINE'
        ? 'b'
        : mode == 'PAST'
        ? 'c'
        : 'a',
  ).join(),
  'contextId': selectionContext,
  'contextType': kind,
  'cityId': kind == 'CITY' ? 'synthetic-now006-city' : '',
  'label': kind == 'ONLINE' ? '本人合成线上情境' : '本人合成城市',
  'relation': declared ? relation : '',
  'viewMode': mode,
  'declared': declared,
  'queryRoute': kind == 'CITY'
      ? 'CITY'
      : kind == 'ONLINE'
      ? 'ONLINE'
      : 'UNAVAILABLE',
};
Map<String, dynamic> selectionOptions({DateTime? observed}) {
  final at = observed ?? DateTime.now().toUtc();
  return {
    'schemaVersion': 'now-context-selection-v1',
    'owner': {'type': 'PERSON', 'id': egressOwner},
    'agentId': egressAgent,
    'observedAt': at.toIso8601String(),
    'expiresAt': at.add(const Duration(seconds: 90)).toIso8601String(),
    'viewOnly': true,
    'modelAccess': false,
    'sendAllowed': false,
    'optionsToken': List.filled(90, 'a').join(),
    'items': [
      selectionOption(),
      selectionOption(mode: 'ONLINE', kind: 'ONLINE', relation: 'interest'),
      selectionOption(mode: 'PAST', relation: 'past'),
    ],
    'limit': 100,
    'truncated': false,
  };
}

Map<String, dynamic> selectionReceipt(
  Map<String, dynamic> options,
  Map<String, dynamic> item,
) => {
  for (final entry in options.entries.where(
    (e) => !{'items', 'limit', 'truncated', 'optionsToken'}.contains(e.key),
  ))
    entry.key: entry.value,
  'choice': item,
};
http.Response selectionResponse(Map<String, dynamic> data) => http.Response(
  jsonEncode({'data': data}),
  200,
  headers: {'content-type': 'application/json'},
);
void main() {
  test(
    'strict own typed options accepts native RFC3339 offsets and refuses implicit authority',
    () {
      final raw = selectionOptions();
      final o = NowContextOptions(raw, egressOwner);
      expect(o.items.length, 3);
      for (final key in ['observedAt', 'expiresAt']) {
        raw[key] = (raw[key] as String).replaceFirst('Z', '+00:00');
      }
      expect(
        NowContextOptions(raw, egressOwner).items.first.cityID,
        'synthetic-now006-city',
      );
      for (final mutate in <void Function(Map<String, dynamic>)>[
        (m) => m['modelAccess'] = true,
        (m) => m['sendAllowed'] = true,
        (m) => m['viewOnly'] = false,
        (m) => m['latitude'] = 57,
        (m) => m['owner'] = {'type': 'PERSON', 'id': selectionContext},
        (m) => m['expiresAt'] = '2030-02-30T10:00:00+08:00',
        (m) => (m['items'] as List).first['contextType'] = 'INSTITUTION',
        (m) => m['limit'] = 101,
      ]) {
        final bad =
            jsonDecode(jsonEncode(selectionOptions())) as Map<String, dynamic>;
        mutate(bad);
        expect(() => NowContextOptions(bad, egressOwner), throwsA(anything));
      }
    },
  );
  test(
    'API resolves original option token only and keeps owner source envelope exact',
    () async {
      final raw = selectionOptions();
      final calls = <http.Request>[];
      final client = MockClient((r) async {
        calls.add(r);
        return selectionResponse(
          r.method == 'GET'
              ? raw
              : selectionReceipt(
                  raw,
                  (raw['items'] as List).first as Map<String, dynamic>,
                ),
        );
      });
      final api = NowContextSelectionAPI(
        client: client,
        apiBaseUrl: 'http://fixture-a',
      );
      final o = await api.options('Bearer synthetic', egressOwner);
      final v = await api.resolve(
        'Bearer synthetic',
        egressOwner,
        o,
        o.items.first,
      );
      expect(v.contextType, 'CITY');
      expect(v.viewMode, 'CURRENT');
      expect(calls.map((r) => r.url.path), [
        '/v1/me/now/context-selection/options',
        '/v1/me/now/context-selection/resolve',
      ]);
      expect(jsonDecode(calls.last.body), {
        'optionsToken': o.optionsToken,
        'optionId': o.items.first.optionID,
      });
      expect(calls.every((r) => !r.url.path.contains('/tasks')), isTrue);
      api.dispose();
      expect(
        () => api.options('Bearer synthetic', egressOwner),
        throwsStateError,
      );
    },
  );
  test(
    'cross-agent and mismatched original choice receipts refuse callback values',
    () async {
      final raw = selectionOptions();
      for (final alter in [false, true]) {
        final api = NowContextSelectionAPI(
          client: MockClient((r) async {
            final v = selectionReceipt(
              raw,
              (raw['items'] as List).first as Map<String, dynamic>,
            );
            if (alter) {
              v['agentId'] = selectionContext;
            } else {
              v['choice'] = selectionOption(
                mode: 'ONLINE',
                kind: 'ONLINE',
                relation: 'interest',
              );
            }
            return selectionResponse(v);
          }),
        );
        final o = NowContextOptions(raw, egressOwner);
        await expectLater(
          api.resolve('Bearer synthetic', egressOwner, o, o.items.first),
          throwsFormatException,
        );
        api.dispose();
      }
    },
  );
  test('actual registered native options and selection JSON parse unchanged', () {
    final f = File(
      '../../docs/testing/evidence/now-context-selection-2026-10-04/native-wire1.json',
    );
    final wire = jsonDecode(f.readAsStringSync()) as Map<String, dynamic>;
    final odata =
        (wire['OPTIONS'] as Map<String, dynamic>)['data']
            as Map<String, dynamic>;
    final owner = (odata['owner'] as Map<String, dynamic>)['id'] as String;
    final o = NowContextOptions(odata, owner);
    final c = NowContextChoice(
      (wire['SELECTION'] as Map<String, dynamic>)['data']
          as Map<String, dynamic>,
      owner,
    );
    expect(o.items.any((x) => x.same(c.option)), isTrue);
    expect(c.contextType, 'ONLINE');
    expect(c.expiresAt, o.expiresAt);
  });
  testWidgets(
    'GET and readonly POST stop waiting at twelve seconds without retries',
    (tester) async {
      final raw = selectionOptions();
      for (final post in [false, true]) {
        final pending = Completer<http.Response>();
        var requests = 0;
        var finished = false;
        Object? failure;
        final api = NowContextSelectionAPI(
          client: MockClient((r) {
            requests++;
            return pending.future;
          }),
        );
        final options = NowContextOptions(raw, egressOwner);
        final future =
            (post
                    ? api.resolve(
                        'Bearer synthetic',
                        egressOwner,
                        options,
                        options.items.first,
                      )
                    : api.options('Bearer synthetic', egressOwner))
                .then<void>(
                  (_) {
                    finished = true;
                  },
                  onError: (Object e) {
                    failure = e;
                    finished = true;
                  },
                );
        await tester.pump();
        await tester.pump(const Duration(seconds: 13));
        try {
          expect(
            finished,
            isTrue,
            reason: post
                ? 'readonly POST stayed pending'
                : 'GET stayed pending',
          );
          expect(failure, isA<TimeoutException>());
          expect(requests, 1);
        } finally {
          pending.complete(
            selectionResponse(
              post
                  ? selectionReceipt(
                      raw,
                      (raw['items'] as List).first as Map<String, dynamic>,
                    )
                  : raw,
            ),
          );
          await tester.pump();
          await future;
          api.dispose();
        }
      }
    },
  );
}
