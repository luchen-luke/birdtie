import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'opportunity_reasons.dart';

final _socialNowID = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
);
String _id(dynamic value) {
  if (value is! String || !_socialNowID.hasMatch(value)) {
    throw const FormatException('Invalid entity');
  }
  return value;
}

String _label(dynamic value) {
  if (value is! String || value.trim().isEmpty || value.runes.length > 500) {
    throw const FormatException('Invalid label');
  }
  return value;
}

DateTime _time(dynamic value) {
  if (value is! String || !RegExp(r'(Z|[+-]\d{2}:\d{2})$').hasMatch(value)) {
    throw const FormatException('Missing time zone');
  }
  final time = DateTime.parse(value);
  if (time.year < 1 || time.year > 9999) {
    throw const FormatException('Invalid time');
  }
  return time;
}

class SocialNowIntent {
  const SocialNowIntent(
    this.id,
    this.ownerID,
    this.title,
    this.modality,
    this.expiresAt,
  );
  final String id, ownerID, title, modality;
  final DateTime expiresAt;
  static SocialNowIntent? read(
    Map<String, dynamic> raw,
    Set<String> friends,
    DateTime now,
  ) {
    final id = _id(raw['id']);
    final owner = _id(raw['creatorAccountId']);
    if (!friends.contains(owner) ||
        raw['status'] != 'ACTIVE' ||
        raw['audience'] == 'PRIVATE') {
      return null;
    }
    if (!const [
          'FRIENDS',
          'COMMUNITY',
          'LOCAL',
          'PUBLIC',
          'INVITE_ONLY',
        ].contains(raw['audience']) ||
        !const [
          'FIND_ACTIVITY',
          'FIND_COMPANION',
          'ORGANIZE',
          'ASK_HELP',
          'OTHER',
        ].contains(raw['type']) ||
        !const ['IN_PERSON', 'ONLINE', 'HYBRID'].contains(raw['modality'])) {
      throw const FormatException('Unknown intent');
    }
    final expires = _time(raw['expiresAt']);
    if (!expires.isAfter(now)) return null;
    return SocialNowIntent(
      id,
      owner,
      _label(raw['title']),
      raw['modality'] as String,
      expires,
    );
  }
}

class SocialNowActivity {
  SocialNowActivity(
    this.id,
    this.title,
    this.placeName,
    this.startsAt,
    List<String> codes,
  ) : codes = List.unmodifiable(codes),
      reasons = projectOpportunityReasons(
        codes,
        scheme: OpportunityReasonScheme.activityPlaceV2,
      );
  final String id, title, placeName;
  final DateTime startsAt;
  final List<String> codes, reasons;
  factory SocialNowActivity.read(Map<String, dynamic> raw) {
    final entity = raw['entity'] as Map<String, dynamic>;
    final place = raw['place'] as Map<String, dynamic>;
    final action = raw['action'] as Map<String, dynamic>;
    final target = action['target'] as Map<String, dynamic>;
    final id = _id(entity['id']);
    final intent = _id(raw['intentId']);
    _id(place['id']);
    if (entity['type'] != 'ACTIVITY' ||
        place['type'] != 'PLACE' ||
        action['type'] != 'OPEN_ACTIVITY' ||
        target['type'] != 'ACTIVITY' ||
        target['id'] != id ||
        raw['id'] != '$intent:$id' ||
        raw['ruleVersion'] != 'activity-place-v2' ||
        !const [
          'EXISTING_TIE',
          'SHARED_COMMUNITY',
          'FOLLOWED_PUBLIC',
          'PUBLIC',
          'OTHER_VISIBLE',
        ].contains(raw['routeTier'])) {
      throw const FormatException('Unknown opportunity');
    }
    final codes = List<String>.from(raw['reasonCodes'] as List);
    if (codes.length > 20) throw const FormatException('Too many reasons');
    if ((raw['routeTier'] == 'EXISTING_TIE') !=
            codes.contains('TIE_ORGANIZER') ||
        (raw['routeTier'] == 'SHARED_COMMUNITY') !=
            codes.contains('JOINED_COMMUNITY')) {
      throw const FormatException('Conflicting reason');
    }
    return SocialNowActivity(
      id,
      _label(raw['title']),
      _label(raw['placeName']),
      _time(raw['startsAt']),
      codes,
    );
  }
}

// Ordinary human read projections only. Shape validation and the injected clock
// do not prove native current authorization, Agent/model permission or consent.
class SocialNowController extends ChangeNotifier {
  SocialNowController({
    required this.authorizationHeader,
    required this.accountID,
    String? Function()? organizationWorkspaceID,
    http.Client? client,
    String? apiBaseUrl,
    DateTime Function()? now,
  }) : organizationWorkspaceID = organizationWorkspaceID ?? (() => null),
       _client = client ?? http.Client(),
       _ownsClient = client == null,
       base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
       _now = now ?? DateTime.now;
  final String? Function() authorizationHeader,
      accountID,
      organizationWorkspaceID;
  final http.Client _client;
  final bool _ownsClient;
  final String base;
  final DateTime Function() _now;
  String? _token, _owner, _workspace;
  int _generation = 0, _serial = 0;
  bool _closed = false, busy = false, loaded = false;
  String? error;
  List<SocialNowIntent> intents = const [];
  List<SocialNowActivity> friendActivities = const [],
      communityActivities = const [],
      otherActivities = const [];
  bool limited = false;
  int get generation => _generation;
  bool get personal =>
      authorizationHeader() != null &&
      accountID() != null &&
      organizationWorkspaceID() == null;
  void _notify() {
    if (!_closed) notifyListeners();
  }

  void _clear() {
    intents = const [];
    friendActivities = const [];
    communityActivities = const [];
    otherActivities = const [];
    loaded = false;
    limited = false;
    error = null;
  }

  void synchronizeIdentity() {
    if (_closed) return;
    final token = authorizationHeader(),
        owner = accountID(),
        workspace = organizationWorkspaceID();
    if (token == _token && owner == _owner && workspace == _workspace) return;
    _token = token;
    _owner = owner;
    _workspace = workspace;
    _generation++;
    _serial++;
    busy = false;
    _clear();
    _notify();
  }

  bool current(int generation) =>
      !_closed &&
      generation == _generation &&
      personal &&
      authorizationHeader() == _token &&
      accountID() == _owner &&
      organizationWorkspaceID() == _workspace;
  Uri url(String path) =>
      Uri.parse('${base.replaceFirst(RegExp(r'/$'), '')}/v1/$path');
  Future<Map<String, dynamic>> _get(String path, String token) async {
    final response = await _client
        .get(url(path), headers: {'Authorization': token})
        .timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) throw StateError('Unavailable');
    return jsonDecode(utf8.decode(response.bodyBytes)) as Map<String, dynamic>;
  }

  List<Map<String, dynamic>> _rows(Map<String, dynamic> envelope, int limit) {
    final rows = (envelope['data'] as List).cast<Map<String, dynamic>>();
    if (rows.length > limit) throw const FormatException('Source limit');
    return rows;
  }

  Set<String> _friends(Map<String, dynamic> envelope, String owner) {
    final result = <String>{};
    for (final row in _rows(envelope, 100)) {
      _id(row['id']);
      final other = _id(row['otherAccountId']);
      if (other == owner) throw const FormatException('Invalid tie');
      result.add(other);
    }
    return result;
  }

  Future<bool> load() async {
    synchronizeIdentity();
    if (_closed || busy || !personal || base.isEmpty) return false;
    final serial = ++_serial, generation = _generation;
    final token = _token!, owner = _owner!;
    busy = true;
    _clear();
    _notify();
    try {
      final responses = await Future.wait([
        _get('me/ties', token),
        _get('social-intents', token),
        _get('me/opportunities', token),
      ]);
      if (!current(generation) || serial != _serial) return false;
      final friends = _friends(responses[0], owner);
      final visible = _rows(responses[1], 50);
      final opportunity = responses[2];
      if (opportunity['source'] != 'RULE_BASED' ||
          opportunity['ruleVersion'] != 'activity-place-v2') {
        throw const FormatException('Unknown source');
      }
      final signals = <SocialNowIntent>[];
      final seenIntent = <String>{};
      for (final raw in visible) {
        final signal = SocialNowIntent.read(raw, friends, _now());
        if (signal != null && seenIntent.add(signal.id)) signals.add(signal);
      }
      final friend = <SocialNowActivity>[],
          community = <SocialNowActivity>[],
          other = <SocialNowActivity>[];
      final seenActivity = <String>{};
      for (final raw in _rows(opportunity, 50)) {
        final activity = SocialNowActivity.read(raw);
        if (!seenActivity.add(activity.id)) continue;
        if (activity.codes.contains('TIE_ORGANIZER')) {
          friend.add(activity);
        } else if (activity.codes.contains('JOINED_COMMUNITY')) {
          community.add(activity);
        } else {
          other.add(activity);
        }
      }
      intents = List.unmodifiable(signals.take(10));
      friendActivities = List.unmodifiable(friend.take(5));
      communityActivities = List.unmodifiable(community.take(5));
      otherActivities = List.unmodifiable(other.take(10));
      limited =
          visible.length == 50 ||
          _rows(responses[0], 100).length == 100 ||
          _rows(opportunity, 50).length == 50 ||
          signals.length > 10 ||
          friend.length > 5 ||
          community.length > 5 ||
          other.length > 10;
      loaded = true;
      return true;
    } catch (_) {
      if (current(generation) && serial == _serial) {
        _clear();
        error = '社交近况暂不可用，请重新读取。未确认的来源不会显示。';
      }
      return false;
    } finally {
      if (current(generation) && serial == _serial) {
        busy = false;
        _notify();
      }
    }
  }

  Future<SocialNowIntent?> readIntent(SocialNowIntent signal) async {
    synchronizeIdentity();
    if (!personal || busy || !intents.contains(signal)) return null;
    final generation = _generation,
        serial = ++_serial,
        token = _token!,
        owner = _owner!;
    busy = true;
    error = null;
    _notify();
    try {
      final friends = _friends(await _get('me/ties', token), owner);
      if (!current(generation) || serial != _serial) return null;
      final envelope = await _get('social-intents/${signal.id}', token);
      if (!current(generation) || serial != _serial) return null;
      final currentSignal = SocialNowIntent.read(
        envelope['data'] as Map<String, dynamic>,
        friends,
        _now(),
      );
      if (currentSignal == null ||
          currentSignal.id != signal.id ||
          currentSignal.ownerID != signal.ownerID) {
        throw const FormatException('Unavailable source');
      }
      return currentSignal;
    } catch (_) {
      if (current(generation) && serial == _serial) {
        _clear();
        error = '这条好友意图当前不可查看，请刷新近况。';
      }
      return null;
    } finally {
      if (current(generation) && serial == _serial) {
        busy = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    _generation++;
    _serial++;
    if (_ownsClient) _client.close();
    super.dispose();
  }
}
