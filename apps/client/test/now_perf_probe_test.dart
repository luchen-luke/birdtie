import 'dart:convert';
import 'dart:ui';

import 'package:birdtie_client/src/workspace/now_perf_probe.dart';
import 'package:crypto/crypto.dart' show sha256;
import 'package:flutter_test/flutter_test.dart';

FrameTiming frame(int start, int ui, int raster) => FrameTiming(
  vsyncStart: start,
  buildStart: start,
  buildFinish: start + ui,
  rasterStart: start + ui,
  rasterFinish: start + ui + raster,
  rasterFinishWallTime: 0,
);

// Independent consumer contract, not the root collector implementation.
Map<String, dynamic> reassembleQaLines(List<String> lines) {
  final begin = RegExp(
    r'^BIRDTIE_NOW_PERF_V2 BEGIN ([0-9a-f]{64}) ([1-9][0-9]*) ([1-9][0-9]*)$',
  ).firstMatch(lines.first);
  if (begin == null) throw const FormatException('Missing transport BEGIN');
  final digest = begin.group(1)!;
  final total = int.parse(begin.group(2)!);
  final length = int.parse(begin.group(3)!);
  if (lines.length != total + 2 ||
      lines.last != 'BIRDTIE_NOW_PERF_V2 END $digest $total $length') {
    throw const FormatException('Incomplete transport');
  }
  final payload = StringBuffer();
  for (var index = 1; index <= total; index++) {
    final prefix = 'BIRDTIE_NOW_PERF_V2 CHUNK $digest $index/$total ';
    if (!lines[index].startsWith(prefix)) {
      throw const FormatException('Out-of-order transport');
    }
    payload.write(lines[index].substring(prefix.length));
  }
  final bytes = ascii.encode(payload.toString());
  if (bytes.length != length || sha256.convert(bytes).toString() != digest) {
    throw const FormatException('Transport integrity mismatch');
  }
  return jsonDecode(payload.toString()) as Map<String, dynamic>;
}

NowPerfReport transportReport({bool empty = false}) => NowPerfReport(
  scene: NowPerfScene.startup,
  reason: NowPerfEnd.elapsed,
  windowMicros: 60000000,
  refreshRateHz: 119.88,
  uiMicros: empty ? [] : [1000, 9999, 20100],
  rasterMicros: empty ? [] : [500, 8888, 25200],
  totalMicros: empty ? [] : [1600, 18987, 45400],
  milestoneMicros: {
    for (var n = 0; n < NowPerfMilestone.values.length; n++)
      NowPerfMilestone.values[n]: (n + 1) * 110011,
  },
  discardedFrames: 0,
);

void main() {
  test('QA transport preserves complete report with bounded ASCII lines', () {
    final report = transportReport();
    final original = report.toJson();
    expect(jsonEncode(original).length, greaterThan(1006));
    final lines = report.toQaLogLines();
    expect(lines.length, greaterThan(3));
    for (final line in lines) {
      expect(line.length, lessThanOrEqualTo(400));
      expect(line.codeUnits.every((code) => code < 128), true);
    }
    expect(reassembleQaLines(lines), original);
    expect(report.toJson(), original);
    expect(report.toQaLogLines(), lines);
    expect(reassembleQaLines(lines)['nativeMapPresentTimingsIncluded'], false);
    expect(reassembleQaLines(lines)['absolutePerformancePass'], false);
    expect(() => lines.add('extra'), throwsUnsupportedError);
  });

  test(
    'QA transport cannot reconstruct missing reordered or corrupt chunks',
    () {
      final lines = transportReport().toQaLogLines();
      expect(
        () => reassembleQaLines(lines.sublist(0, lines.length - 1)),
        throwsFormatException,
      );
      final missing = [...lines]..removeAt(1);
      expect(() => reassembleQaLines(missing), throwsFormatException);
      final reordered = [...lines];
      final first = reordered[1];
      reordered[1] = reordered[2];
      reordered[2] = first;
      expect(() => reassembleQaLines(reordered), throwsFormatException);
      final duplicate = [...lines];
      duplicate[2] = duplicate[1];
      expect(() => reassembleQaLines(duplicate), throwsFormatException);
      final corrupt = [...lines];
      corrupt[1] = '${corrupt[1]}x';
      expect(() => reassembleQaLines(corrupt), throwsFormatException);
    },
  );

  test('QA transport retains no-frame and debug-ineligible fields', () {
    final report = transportReport(empty: true);
    final decoded = reassembleQaLines(report.toQaLogLines());
    expect(decoded, report.toJson());
    expect(decoded['frameCount'], 0);
    expect(decoded['measurementStatus'], 'NO_FRAMES');
    expect((decoded['uiBuild'] as Map)['p99Ms'], isNull);
    expect(decoded['buildMode'], 'debug');
    expect(decoded['eligibleForReleaseComparison'], false);
    expect(NowPerfProbe.environmentEnabled, false);
  });

  test('disabled probe does not register, collect or report', () async {
    var registrations = 0;
    var reports = 0;
    final probe = NowPerfProbe(
      enabled: false,
      onReport: (_) => reports++,
      addTimingsListener: (_) => registrations++,
      removeTimingsListener: (_) => registrations++,
      clockMicros: () => throw StateError('Disabled probe consulted clock'),
    );
    expect(
      probe.beginWindow(scene: NowPerfScene.mapPan, refreshRateHz: 60),
      false,
    );
    probe.mark(NowPerfMilestone.mapLoaded);
    expect(await probe.stopWindow(), isNull);
    probe.dispose();
    expect(registrations, 0);
    expect(reports, 0);
  });

  testWidgets('late batch excludes frames outside the exact window', (
    tester,
  ) async {
    var clock = 1000;
    late NowPerfTimingsListener deliver;
    var added = 0;
    var removed = 0;
    final reports = <NowPerfReport>[];
    final probe = NowPerfProbe(
      enabled: true,
      onReport: reports.add,
      clockMicros: () => clock,
      drainDuration: const Duration(milliseconds: 100),
      addTimingsListener: (listener) {
        deliver = listener;
        added++;
      },
      removeTimingsListener: (_) => removed++,
    );
    expect(
      probe.beginWindow(scene: NowPerfScene.mapPan, refreshRateHz: 120),
      true,
    );
    expect(
      probe.beginWindow(scene: NowPerfScene.keyboard, refreshRateHz: 60),
      false,
    );
    deliver([frame(999, 99999, 99999), frame(1000, 10000, 20000)]);
    clock = 21000;
    probe.mark(NowPerfMilestone.mapFirstInteraction);
    clock = 41000;
    probe.mark(NowPerfMilestone.mapFirstInteraction);
    clock = 100000;
    final stopped = probe.stopWindow();
    expect(probe.active, false);
    deliver([frame(99999, 5000, 7000), frame(100000, 99999, 99999)]);
    await tester.pump(const Duration(milliseconds: 100));
    final report = (await stopped)!.toJson();
    expect(report['frameCount'], 2);
    expect(report['windowMs'], 99);
    expect(report['milestonesMs'], {'mapFirstInteraction': 20.0});
    final ui = report['uiBuild']! as Map<String, Object?>;
    final raster = report['raster']! as Map<String, Object?>;
    expect(ui['p95Ms'], 10);
    expect(ui['p99Ms'], 10);
    expect(ui['overDisplayBudgetCount'], 1);
    expect(ui['overDisplayBudgetRatio'], .5);
    expect(ui['over60HzBudgetCount'], 0);
    expect(raster['over60HzBudgetCount'], 1);
    expect(raster['over60HzBudgetRatio'], .5);
    expect(report['buildMode'], 'debug');
    expect(report['eligibleForReleaseComparison'], false);
    expect(report['nativeMapPresentTimingsIncluded'], false);
    expect(report['absolutePerformancePass'], false);
    expect(added, 1);
    expect(removed, 1);
    expect(reports, hasLength(1));
    probe.dispose();
    expect(removed, 1);
  });

  testWidgets('nearest rank percentiles retain rare slow frames', (
    tester,
  ) async {
    var clock = 1000;
    late NowPerfTimingsListener deliver;
    final probe = NowPerfProbe(
      enabled: true,
      onReport: (_) {},
      drainDuration: Duration.zero,
      clockMicros: () => clock,
      addTimingsListener: (listener) => deliver = listener,
      removeTimingsListener: (_) {},
    );
    probe.beginWindow(scene: NowPerfScene.sustained, refreshRateHz: 60);
    deliver([
      for (var n = 1; n <= 100; n++) frame(1000 + n, n * 1000, n * 2000),
    ]);
    clock = 1000000;
    final future = probe.stopWindow();
    await tester.pump(Duration.zero);
    final data = (await future)!.toJson();
    final ui = data['uiBuild']! as Map<String, Object?>;
    expect(ui['p95Ms'], 95);
    expect(ui['p99Ms'], 99);
    expect(ui['maxMs'], 100);
    expect(ui['over60HzBudgetCount'], 84);
    expect(ui['over60HzBudgetRatio'], .84);
    probe.dispose();
  });

  testWidgets('bounded window removes listener without requesting frames', (
    tester,
  ) async {
    var clock = 1;
    var removed = 0;
    final reports = <NowPerfReport>[];
    final probe = NowPerfProbe(
      enabled: true,
      onReport: reports.add,
      drainDuration: const Duration(milliseconds: 10),
      clockMicros: () => clock,
      addTimingsListener: (_) {},
      removeTimingsListener: (_) => removed++,
    );
    probe.beginWindow(
      scene: NowPerfScene.startup,
      refreshRateHz: 60,
      duration: const Duration(milliseconds: 20),
    );
    clock = 20001;
    await tester.pump(const Duration(milliseconds: 20));
    expect(reports, isEmpty);
    await tester.pump(const Duration(milliseconds: 10));
    expect(removed, 1);
    expect(reports.single.toJson()['measurementStatus'], 'NO_FRAMES');
    expect(reports.single.toJson()['endReason'], 'elapsed');
    expect(tester.binding.hasScheduledFrame, false);
    probe.dispose();
  });

  testWidgets(
    'dispose immediately removes callback and preserves prior report',
    (tester) async {
      var clock = 1000;
      late NowPerfTimingsListener deliver;
      final reports = <NowPerfReport>[];
      var removed = 0;
      final probe = NowPerfProbe(
        enabled: true,
        onReport: reports.add,
        clockMicros: () => clock,
        addTimingsListener: (listener) => deliver = listener,
        removeTimingsListener: (_) => removed++,
      );
      probe.beginWindow(scene: NowPerfScene.socialToggle, refreshRateHz: 60);
      deliver([frame(1000, 1000, 2000)]);
      clock = 5000;
      final stopped = probe.stopWindow();
      probe.dispose();
      deliver([frame(2000, 99999, 99999)]);
      expect((await stopped)!.toJson()['frameCount'], 1);
      expect(reports.single.toJson()['endReason'], 'disposed');
      expect(removed, 1);
      expect(
        probe.beginWindow(scene: NowPerfScene.mapPan, refreshRateHz: 60),
        false,
      );
      await tester.pump(const Duration(seconds: 2));
      expect(reports, hasLength(1));
    },
  );

  testWidgets('new windows reset samples and cannot contain free text', (
    tester,
  ) async {
    var clock = 1000;
    late NowPerfTimingsListener deliver;
    final probe = NowPerfProbe(
      enabled: true,
      onReport: (_) {},
      drainDuration: Duration.zero,
      clockMicros: () => clock,
      addTimingsListener: (listener) => deliver = listener,
      removeTimingsListener: (_) {},
    );
    probe.beginWindow(scene: NowPerfScene.mapPan, refreshRateHz: 60);
    deliver([frame(1000, 1000, 2000)]);
    clock = 5000;
    final first = probe.stopWindow();
    await tester.pump(Duration.zero);
    expect((await first)!.toJson()['frameCount'], 1);
    probe.beginWindow(scene: NowPerfScene.keyboard, refreshRateHz: 60);
    clock = 6000;
    final second = probe.stopWindow();
    await tester.pump(Duration.zero);
    final data = (await second)!.toJson();
    expect(data['frameCount'], 0);
    expect(data['milestonesMs'], isEmpty);
    expect(data.keys, isNot(contains('account')));
    expect(data.keys, isNot(contains('message')));
    expect(data.keys, isNot(contains('location')));
    probe.dispose();
  });
}
