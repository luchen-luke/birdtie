import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_memory_candidate_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _base = 'http://127.0.0.1:9670';
const _auth = 'Bearer synthetic-transport-test';
const _approve =
    '/v1/me/agent-enrichment-purpose/previews/test-preview/approve';

http.Response _ok() => http.Response(
  jsonEncode({
    'data': {'ack': true},
  }),
  200,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

void main() {
  for (final explicitNull in [false, true]) {
    test('POST approval uses truly empty body ($explicitNull)', () async {
      late http.Request observed;
      var calls = 0;
      final client = MockClient((request) async {
        calls++;
        observed = request;
        return _ok();
      });
      final api = AgentMemoryCandidateAPI(client: client, apiBaseUrl: _base);
      final value = explicitNull
          ? await api.request('POST', _approve, _auth, null)
          : await api.request('POST', _approve, _auth);
      expect(value, {'ack': true});
      expect(calls, 1);
      expect(observed.method, 'POST');
      expect(observed.bodyBytes, isEmpty);
      expect(observed.headers.containsKey('content-type'), isFalse);
      expect(observed.headers['authorization'], _auth);
      expect(observed.headers['accept'], 'application/json');
      expect(observed.url.toString(), '$_base$_approve');
      api.dispose();
      client.close();
    });
  }

  test(
    'DELETE sends original expectedRevision JSON with actual DELETE',
    () async {
      late http.Request observed;
      final client = MockClient((request) async {
        observed = request;
        return _ok();
      });
      final api = AgentMemoryCandidateAPI(client: client, apiBaseUrl: _base);
      expect(
        await api.request(
          'DELETE',
          '/v1/me/agent-enrichment-purpose/grants/test-grant',
          _auth,
          {'expectedRevision': 7},
        ),
        {'ack': true},
      );
      expect(observed.method, 'DELETE');
      expect(jsonDecode(observed.body), {'expectedRevision': 7});
      expect(observed.headers['content-type'], startsWith('application/json'));
      expect(observed.headers['authorization'], _auth);
      expect(observed.headers['accept'], 'application/json');
      api.dispose();
      client.close();
    },
  );

  test('DELETE without body does not invent JSON null', () async {
    late http.Request observed;
    final client = MockClient((request) async {
      observed = request;
      return _ok();
    });
    final api = AgentMemoryCandidateAPI(client: client, apiBaseUrl: _base);
    await api.request(
      'DELETE',
      '/v1/me/agent-enrichment-purpose/grants/test-grant',
      _auth,
    );
    expect(observed.method, 'DELETE');
    expect(observed.bodyBytes, isEmpty);
    expect(observed.headers.containsKey('content-type'), isFalse);
    api.dispose();
    client.close();
  });

  test('original GET remains bodyless and preserves configured base', () async {
    late http.Request observed;
    final client = MockClient((request) async {
      observed = request;
      return _ok();
    });
    final api = AgentMemoryCandidateAPI(client: client, apiBaseUrl: '$_base/');
    await api.request('GET', AgentMemoryCandidateAPI.path, _auth);
    expect(observed.method, 'GET');
    expect(observed.url.toString(), '$_base${AgentMemoryCandidateAPI.path}');
    expect(observed.bodyBytes, isEmpty);
    expect(observed.headers.containsKey('content-type'), isFalse);
    api.dispose();
    client.close();
  });

  test('original human POST retains exact structured JSON', () async {
    late http.Request observed;
    final client = MockClient((request) async {
      observed = request;
      return _ok();
    });
    final api = AgentMemoryCandidateAPI(client: client, apiBaseUrl: _base);
    final body = {
      'category': 'badminton',
      'sources': [
        {'type': 'MOMENT', 'id': 'synthetic-native-source'},
      ],
    };
    await api.request('POST', AgentMemoryCandidateAPI.path, _auth, body);
    expect(observed.method, 'POST');
    expect(jsonDecode(observed.body), body);
    expect(observed.headers['content-type'], startsWith('application/json'));
    api.dispose();
    client.close();
  });

  for (final method in ['PUT', 'PATCH', 'HEAD', 'OPTIONS', 'post', ' POST ']) {
    test('unsupported method $method makes zero requests', () async {
      var calls = 0;
      final client = MockClient((request) async {
        calls++;
        return _ok();
      });
      final api = AgentMemoryCandidateAPI(client: client, apiBaseUrl: _base);
      await expectLater(
        api.request(method, AgentMemoryCandidateAPI.path, _auth, {'value': 1}),
        throwsA(isA<FormatException>()),
      );
      expect(calls, 0);
      api.dispose();
      client.close();
    });
  }

  test(
    'GET with body is rejected rather than silently discarding consent input',
    () async {
      var calls = 0;
      final client = MockClient((request) async {
        calls++;
        return _ok();
      });
      final api = AgentMemoryCandidateAPI(client: client, apiBaseUrl: _base);
      await expectLater(
        api.request('GET', AgentMemoryCandidateAPI.path, _auth, {
          'expectedRevision': 1,
        }),
        throwsA(isA<FormatException>()),
      );
      expect(calls, 0);
      api.dispose();
      client.close();
    },
  );

  for (final status in [401, 409, 503]) {
    test(
      'DELETE $status keeps typed error and does not fallback or retry',
      () async {
        var calls = 0;
        final client = MockClient((request) async {
          calls++;
          return http.Response('unavailable', status);
        });
        final api = AgentMemoryCandidateAPI(client: client, apiBaseUrl: _base);
        await expectLater(
          api.request(
            'DELETE',
            '/v1/me/agent-enrichment-purpose/grants/test-grant',
            _auth,
            {'expectedRevision': 1},
          ),
          throwsA(
            isA<CandidateAPIError>().having((e) => e.status, 'status', status),
          ),
        );
        expect(calls, 1);
        api.dispose();
        client.close();
      },
    );
  }
  for (final prefix in [
    '/v1/me/agent-enrichment-purpose',
    '/v1/me/agent-multi-candidates',
  ]) {
    for (final pair in <(String, String)>[
      ('POST', '$prefix/previews'),
      ('GET', '$prefix/previews/test-preview'),
      ('POST', '$prefix/previews/test-preview/approve'),
      ('GET', '$prefix/previews/test-preview/receipt'),
      ('GET', '$prefix/grants/test-grant'),
      ('DELETE', '$prefix/grants/test-grant'),
    ]) {
      test('exact original purpose bare wire ${pair.$1} ${pair.$2}', () async {
        var calls = 0;
        final client = MockClient((r) async {
          calls++;
          return http.Response(
            '{"schemaVersion":"original-bare-dto","modelAccess":false}',
            200,
          );
        });
        final api = AgentMemoryCandidateAPI(client: client, apiBaseUrl: _base);
        expect(await api.request(pair.$1, pair.$2, _auth), {
          'schemaVersion': 'original-bare-dto',
          'modelAccess': false,
        });
        expect(calls, 1);
        api.dispose();
        client.close();
      });
    }
  }
  for (final pair in <(String, String)>[
    ('POST', '/v1/me/agent-multi-candidates/stage'),
    ('GET', '/v1/me/agent-multi-candidates/grants/test-grant/receipt'),
  ]) {
    test('exact multi route bare wire ${pair.$2}', () async {
      final client = MockClient(
        (r) async =>
            http.Response('{"schemaVersion":"original-bare-dto"}', 200),
      );
      final api = AgentMemoryCandidateAPI(client: client, apiBaseUrl: _base);
      expect(await api.request(pair.$1, pair.$2, _auth), {
        'schemaVersion': 'original-bare-dto',
      });
      api.dispose();
      client.close();
    });
  }
  for (final pair in <(String, String)>[
    ('GET', AgentMemoryCandidateAPI.path),
    ('GET', '/v1/me/agent-enrichment-purpose-evil/previews/x/receipt'),
    ('GET', '/v1/me/agent-enrichment-purpose/previews/x/receipt/'),
    (
      'GET',
      '/v1/me/agent-enrichment-purpose/previews/x/receipt?confirmed=true',
    ),
    ('GET', '/v1/me/agent-enrichment-purpose/previews/x/unknown'),
    ('POST', '/v1/me/agent-enrichment-purpose/grants/x'),
    ('GET', '/v1/me/agent-multi-candidates/stage'),
    ('DELETE', '/v1/me/agent-multi-candidates/previews/x/approve'),
  ]) {
    test(
      'bare fallback excluded outside exact method and route ${pair.$1} ${pair.$2}',
      () async {
        final client = MockClient(
          (r) async =>
              http.Response('{"schemaVersion":"unselected-bare-object"}', 200),
        );
        final api = AgentMemoryCandidateAPI(client: client, apiBaseUrl: _base);
        expect(await api.request(pair.$1, pair.$2, _auth), isNull);
        api.dispose();
        client.close();
      },
    );
  }
}
