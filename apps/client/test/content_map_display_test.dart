import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'content_map_controller_test.dart' as f;
import 'content_map_reader_test.dart' show ReaderFixture;

void main() {
  test('bounded display image stays scoped to its actual moment and media', () {
    final original = ReaderFixture().media(1);
    final display = {
      ...original,
      'contentPath': '${original['contentPath']}&variant=display',
    };
    OwnedMapMomentMedia decode(Map<String, Object?> v) =>
        OwnedMapMomentMedia.decode(v, f.id(1), 'SELF_PRIVATE');
    expect(decode(original).width, 160);
    expect(decode(display).contentPath, endsWith('&variant=display'));
    for (final changes in <Map<String, Object?>>[
      {'width': 1281},
      {'height': 1281},
      {'byteCount': 262145},
      {'mimeType': 'image/png'},
      {'contentPath': '${display['contentPath']}&variant=display'},
      {'contentPath': '${display['contentPath']}&extra=true'},
      {'contentPath': '${original['contentPath']}&variant=unknown'},
      {'contentPath': 'https://other.test${display['contentPath']}'},
      {
        'contentPath': (display['contentPath'] as String).replaceFirst(
          f.id(1),
          f.id(2),
        ),
      },
      {
        ...ReaderFixture().media(1, isVideo: true),
        'contentPath': display['contentPath'],
      },
    ]) {
      expect(() => decode({...display, ...changes}), throwsFormatException);
    }
  });
}
