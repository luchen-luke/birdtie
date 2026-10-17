import 'package:birdtie_client/src/workspace/map_media_read_metrics.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'numeric media diagnostics are bounded and never claim a video first frame',
    () {
      MapMediaReadMetrics.clear();
      for (var n = 0; n < 40; n++) {
        final trace = MapMediaReadMetrics.begin(MapMediaReadKind.video)
          ..bytes = 100 + n
          ..requests = 1;
        trace.mark(MapMediaReadStage.verified);
        trace.mark(MapMediaReadStage.verified);
        trace.mark(MapMediaReadStage.initialized);
        trace.mark(MapMediaReadStage.playRequested);
      }
      final values = MapMediaReadMetrics.recent;
      expect(values.length, MapMediaReadMetrics.capacity);
      for (final value in values) {
        expect(value.keys.toSet(), {
          'operation',
          'kind',
          'stage',
          'elapsedUs',
          'bytes',
          'requests',
          'cacheHit',
        });
        expect(value['elapsedUs'], isA<int>());
        expect(value['bytes'], isA<int>());
        expect(value['operation'], isA<int>());
        expect(value['stage'].toString(), isNot(contains('firstFrame')));
      }
      final cached = MapMediaReadMetrics.begin(MapMediaReadKind.image)
        ..cacheHit = true;
      cached.mark(MapMediaReadStage.imageVisible);
      expect(MapMediaReadMetrics.recent.last['cacheHit'], true);
      expect(MapMediaReadMetrics.recent.last['requests'], 0);
      MapMediaReadMetrics.clear();
    },
  );
}
