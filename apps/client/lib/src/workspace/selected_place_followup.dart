/// A short-lived UI selector. It contains no public/private content, coordinate,
/// membership, generation or permission; the server must reread the native row.
class SelectedPlaceFollowup {
  SelectedPlaceFollowup({
    required this.placeID,
    required this.cityID,
    required this.taskID,
    required bool Function() current,
  }) : _current = current;

  final String placeID, cityID;
  final String? taskID;
  final bool Function() _current;
  static final _uuid = RegExp(
    r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
  );
  bool get current =>
      cityID.isNotEmpty &&
      _uuid.hasMatch(placeID) &&
      placeID != '00000000-0000-0000-0000-000000000000' &&
      _current();
  Map<String, Object> get selector => {'placeId': placeID};
}

bool isSelectedPlaceFollowupQuery(String query) {
  final text = query.trim().toLowerCase().replaceAll(
    RegExp(r'[ ?？!！。.]+$'),
    '',
  );
  if (const {
    '把这个加入第二天',
    '把这个地方加入第二天',
    '把它加入第二天',
    '将这个加入第二天',
    '将它加入第二天',
    '加入第二天',
    'add this to day 2',
    'add this place to day 2',
    'add it to day 2',
    '地址',
    '地址是什么',
    'address',
    'its address',
    'what is its address',
    'what is this place\'s address',
    'what is the address of it',
    'what is the address of this place',
    'where is it',
    'where is this place',
    'what are its opening hours',
    'its opening hours',
    'what are this place\'s opening hours',
    'what are the opening hours of it',
    'what are the opening hours for it',
    'what are the opening hours of this place',
    'what are the opening hours for this place',
    'what time does it open',
    'what time does it close',
    'when does it open',
  }.contains(text))
    return true;
  // This only selects the transport path. The server's closed parser remains
  // authoritative and rejects unrelated/new named/private/action requests.
  return RegExp(
    r'^(它|这家|这个地方|那里)(今天|明天|周末|的)?(地址(是什么)?|在哪里|几点开门|几点关门|营业时间(是什么)?|开放时间(是什么)?)$',
  ).hasMatch(text);
}
