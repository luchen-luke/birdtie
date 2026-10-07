// The current pilot publishes only in Aberdeen. These rules convert the
// editor's Aberdeen wall clock to UTC without using the device time zone.
int _lastSunday(int year, int month) {
  final last = DateTime.utc(year, month + 1, 0);
  return last.day - (last.weekday % 7);
}

DateTime? aberdeenLocalToUtc(DateTime date, int hour, int minute) {
  final year = date.year;
  final marchSunday = _lastSunday(year, 3);
  final octoberSunday = _lastSunday(year, 10);
  if ((date.month == 3 && date.day == marchSunday && hour == 1) ||
      (date.month == 10 && date.day == octoberSunday && hour == 1)) {
    return null; // Skipped or repeated local hour: request an unambiguous time.
  }
  final wall = DateTime.utc(year, date.month, date.day, hour, minute);
  final start = DateTime.utc(year, 3, marchSunday, 2);
  final end = DateTime.utc(year, 10, octoberSunday, 1);
  return wall.subtract(
    wall.isBefore(start) || !wall.isBefore(end)
        ? Duration.zero
        : const Duration(hours: 1),
  );
}

DateTime utcToAberdeenLocal(DateTime time) {
  final utc = time.toUtc();
  final start = DateTime.utc(utc.year, 3, _lastSunday(utc.year, 3), 1);
  final end = DateTime.utc(utc.year, 10, _lastSunday(utc.year, 10), 1);
  final offset = !utc.isBefore(start) && utc.isBefore(end) ? 1 : 0;
  return utc.add(Duration(hours: offset));
}
