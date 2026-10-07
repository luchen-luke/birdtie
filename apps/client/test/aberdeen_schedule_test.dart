import 'package:birdtie_client/src/workspace/aberdeen_schedule.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('Aberdeen winter and summer wall clocks convert to UTC', () {
    expect(
      aberdeenLocalToUtc(DateTime(2026, 1, 15), 18, 0),
      DateTime.utc(2026, 1, 15, 18),
    );
    expect(
      aberdeenLocalToUtc(DateTime(2026, 7, 15), 18, 0),
      DateTime.utc(2026, 7, 15, 17),
    );
    expect(
      utcToAberdeenLocal(DateTime.utc(2026, 7, 15, 17)),
      DateTime.utc(2026, 7, 15, 18),
    );
  });

  test('clock-change ambiguous and missing hours require a new time', () {
    expect(aberdeenLocalToUtc(DateTime(2026, 3, 29), 1, 30), isNull);
    expect(aberdeenLocalToUtc(DateTime(2026, 10, 25), 1, 30), isNull);
    expect(
      aberdeenLocalToUtc(DateTime(2026, 3, 29), 2, 0),
      DateTime.utc(2026, 3, 29, 1),
    );
    expect(
      aberdeenLocalToUtc(DateTime(2026, 10, 25), 2, 0),
      DateTime.utc(2026, 10, 25, 2),
    );
  });
}
