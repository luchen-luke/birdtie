import 'dart:convert';
import 'dart:io';

import 'package:birdtie_client/src/workspace/place_history_pagination.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  const directory = String.fromEnvironment('BIRDTIE_PAGE_NATIVE_WIRE_DIR');
  test(
    'exact registered PostgreSQL HTTP pages decode as five plus two',
    () {
      final firstRaw = File('$directory/first.json').readAsStringSync();
      final nextRaw = File('$directory/next.json').readAsStringSync();
      final firstData =
          (jsonDecode(firstRaw) as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      final nextData =
          (jsonDecode(nextRaw) as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      final place = firstData['placeId'] as String;
      final first = PlaceHistoryPage.decode(firstData, place, true);
      final next = PlaceHistoryPage.decode(nextData, place, true);
      expect(first.items.length, 5);
      expect(next.items.length, 2);
      expect(first.count, 7);
      expect(next.count, 7);
      expect(first.nextCursor, isNotNull);
      expect(next.nextCursor, isNull);
      expect(next.sourceVersion, first.sourceVersion);
      expect(next.windowEnd, first.windowEnd);
      expect(next.validUntil, first.validUntil);
      final all = [...first.items, ...next.items];
      expect(all.map((item) => item.id).toSet().length, 7);
      expect(all.every((item) => item.authorPublicProfile == null), isTrue);
      // Historical wire decoding does not authorize rendering an expired page.
      // No timestamp rewriting, fresh transport or synthetic DTO reconstruction.
    },
    skip: directory.isEmpty ? 'Root-owned native HTTP bytes required' : false,
  );
}
