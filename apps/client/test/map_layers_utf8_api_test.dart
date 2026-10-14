import 'dart:convert';

import 'package:birdtie_client/src/workspace/map_layers_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'map_layers_api_test.dart' as f;

void main() {
  test(
    'headerless UTF8 transport preserves Chinese without trusting Latin1 fallback',
    () async {
      final wire = f.mapTestView();
      (wire['items'] as List).first['title'] = '阿伯丁公开地点 café 中文';
      final api = MapLayersApi(
        apiBaseUrl: 'http://owned.test',
        client: MockClient(
          (_) async =>
              http.Response.bytes(utf8.encode(jsonEncode({'data': wire})), 200),
        ),
      );
      addTearDown(api.dispose);
      final view = await api.read('owned-city', f.mapTestBounds);
      expect(view.items.first.title, '阿伯丁公开地点 café 中文');
    },
  );

  test(
    'headerless malformed UTF8 cannot become authorized source text',
    () async {
      final wire = f.mapTestView();
      (wire['items'] as List).first['title'] = 'byte_marker';
      final parts = jsonEncode({'data': wire}).split('byte_marker');
      final api = MapLayersApi(
        apiBaseUrl: 'http://owned.test',
        client: MockClient(
          (_) async => http.Response.bytes([
            ...utf8.encode(parts[0]),
            0xc3,
            0x28,
            ...utf8.encode(parts[1]),
          ], 200),
        ),
      );
      addTearDown(api.dispose);
      await expectLater(
        api.read('owned-city', f.mapTestBounds),
        throwsFormatException,
      );
    },
  );

  test(
    'native JSON UTF8 without optional charset preserves Chinese current map title',
    () async {
      final wire = f.mapTestView();
      (wire['items'] as List).first['title'] = '阿伯丁公开地点 café 中文';
      final api = MapLayersApi(
        apiBaseUrl: 'http://owned.test',
        client: MockClient(
          (_) async => http.Response.bytes(
            utf8.encode(jsonEncode({'data': wire})),
            200,
            headers: {'content-type': 'application/json'},
          ),
        ),
      );
      addTearDown(api.dispose);
      final view = await api.read('owned-city', f.mapTestBounds);
      expect(view.items.first.title, '阿伯丁公开地点 café 中文');
    },
  );

  test('invalid UTF8 cannot become a valid Latin1 source title', () async {
    final wire = f.mapTestView();
    (wire['items'] as List).first['title'] = 'unique_byte_marker';
    final raw = jsonEncode({'data': wire});
    final parts = raw.split('unique_byte_marker');
    final bytes = [
      ...utf8.encode(parts[0]),
      0xc3,
      0x28,
      ...utf8.encode(parts[1]),
    ];
    final api = MapLayersApi(
      apiBaseUrl: 'http://owned.test',
      client: MockClient(
        (_) async => http.Response.bytes(
          bytes,
          200,
          headers: {'content-type': 'application/json'},
        ),
      ),
    );
    addTearDown(api.dispose);
    await expectLater(
      api.read('owned-city', f.mapTestBounds),
      throwsFormatException,
    );
  });
}
