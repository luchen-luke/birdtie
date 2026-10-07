import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import 'moment_time_choice.dart';

class PrivateMoment {
  const PrivateMoment({
    required this.id,
    required this.cityID,
    required this.placeID,
    required this.title,
    required this.body,
    required this.timePrecision,
    required this.locationPrecision,
    required this.status,
    required this.revision,
    required this.activityIDs,
    required this.communityID,
    required this.organizationID,
    this.authorAccountID,
    this.visibility,
    this.occurredAt,
    this.createdAt,
    this.updatedAt,
  });
  final String id,
      cityID,
      placeID,
      title,
      body,
      timePrecision,
      locationPrecision,
      status;
  final int revision;
  final List<String> activityIDs;
  final String communityID, organizationID;
  final String? authorAccountID;
  final String? visibility;
  String get visibilityLabel => switch (visibility) {
    'private' => '仅自己可见',
    'public' => '公开可见',
    _ => '可见范围未确认',
  };
  String get statusLabel => switch (status) {
    'draft' => '草稿',
    'published' => '已发布',
    'withdrawn' => '已撤回',
    _ => '状态未确认',
  };
  final DateTime? occurredAt, createdAt, updatedAt;
  MomentTimeValue get time =>
      MomentTimeValue.fromWire(timePrecision, occurredAt);
  factory PrivateMoment.fromJson(Map<String, dynamic> json) {
    DateTime? date(String key) {
      if (json[key] == null) return null;
      final value = json[key] as String;
      if (!RegExp(r'(Z|[+-]\d{2}:\d{2})$').hasMatch(value)) {
        throw const FormatException('timestamp needs an offset');
      }
      return DateTime.parse(value).toUtc();
    }

    final occurrence = date('occurredAt');
    final precision = json['timePrecision'] as String;
    MomentTimeValue.fromWire(precision, occurrence);
    return PrivateMoment(
      id: json['id'] as String,
      authorAccountID: json['authorAccountId'] as String?,
      visibility: json['visibility'] as String?,
      cityID: json['cityId'] as String,
      placeID: json['placeId'] as String? ?? '',
      title: json['title'] as String,
      body: json['body'] as String? ?? '',
      timePrecision: precision,
      occurredAt: occurrence,
      createdAt: date('createdAt'),
      updatedAt: date('updatedAt'),
      locationPrecision: json['locationPrecision'] as String,
      status: json['status'] as String,
      revision: json['revision'] as int,
      activityIDs: List.unmodifiable(
        (json['activityIds'] as List<dynamic>? ?? const []).cast<String>(),
      ),
      communityID: json['communityId'] as String? ?? '',
      organizationID: json['organizationId'] as String? ?? '',
    );
  }
  bool get isEditableDraft => status == 'draft' && visibility != 'public';
}

class PrivateMomentController extends ChangeNotifier {
  PrivateMomentController({
    required this.authorizationHeader,
    this.ownerID,
    this.identityChanges,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _apiBase = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl {
    _syncActor();
    identityChanges?.addListener(_onIdentityChanged);
  }
  final String _apiBase;
  final String? Function() authorizationHeader;
  final String? Function()? ownerID;
  final Listenable? identityChanges;
  final http.Client _client;
  // Read-only references for another human consumer of this same Moment source.
  // The borrower does not own or close this client.
  http.Client get borrowedClient => _client;
  String get apiBaseUrl => _apiBase;
  int get identityEpoch {
    _syncActor();
    return _epoch;
  }

  List<PrivateMoment> _moments = const [];
  String? _actor, _owner, _loadedToken, _loadedOwner, _error;
  bool _loading = false,
      _saving = false,
      _closed = false,
      _creationUncertain = false;
  int _epoch = 0, _request = 0;
  int? _readStatus;
  int? get readStatus {
    _syncActor();
    return _readStatus;
  }

  // Detect observed account changes before both reads and writes. A late response
  // from a previous actor cannot restore its data, errors or saving state.
  String? _syncActor() {
    final token = authorizationHeader();
    final owner = ownerID?.call();
    if (token != _actor || owner != _owner) {
      _actor = token;
      _owner = owner;
      _epoch++;
      _request++;
      _loadedToken = null;
      _loadedOwner = null;
      _moments = const [];
      _loading = false;
      _saving = false;
      _error = null;
      _readStatus = null;
      _creationUncertain = false;
    }
    return token;
  }

  void _onIdentityChanged() {
    if (_closed) return;
    final epoch = _epoch;
    _syncActor();
    if (epoch == _epoch) return;
    // Retire on the actual event, not the next widget frame. A -> B -> A
    // receives new epochs and authoritative reads for each current identity.
    unawaited(refresh());
  }

  bool get _ownerReady => ownerID == null || _owner?.isNotEmpty == true;

  void _checkAuthor(PrivateMoment moment, String? owner) {
    if (ownerID != null && (owner == null || moment.authorAccountID != owner)) {
      throw const FormatException('moment author does not match current owner');
    }
  }

  List<PrivateMoment> get moments {
    _syncActor();
    return _ownerReady && _loadedToken == _actor && _loadedOwner == _owner
        ? _moments
        : const [];
  }

  bool get loading {
    _syncActor();
    return _loading;
  }

  bool get saving {
    _syncActor();
    return _saving;
  }

  String? get error {
    _syncActor();
    return _error;
  }

  bool get creationUncertain {
    _syncActor();
    return _creationUncertain;
  }

  void confirmCreationChecked() {
    _syncActor();
    _creationUncertain = false;
    _error = null;
    _notify();
  }

  bool _current(String token, int epoch) =>
      !_closed && _syncActor() == token && _epoch == epoch;
  Uri _endpoint(String path) =>
      Uri.parse('${_apiBase.replaceFirst(RegExp(r'/$'), '')}$path');
  void _notify() {
    if (!_closed) notifyListeners();
  }

  bool _ownsVersion(PrivateMoment moment) =>
      _loadedToken == _actor &&
      _loadedOwner == _owner &&
      _ownerReady &&
      (ownerID == null || moment.authorAccountID == _owner) &&
      _moments.any(
        (item) => identical(item, moment) && item.revision == moment.revision,
      );

  Future<void> refresh({bool Function()? current}) async {
    if (_closed || current?.call() == false) return;
    final token = _syncActor(), epoch = _epoch, request = ++_request;
    final owner = _owner;
    if (token == null || _apiBase.isEmpty) {
      _loading = false;
      _notify();
      return;
    }
    if (!_ownerReady) {
      _loading = false;
      _error = '登录身份尚未确认，请重新登录后读取私人草稿。';
      _notify();
      return;
    }
    _loading = true;
    _error = null;
    _readStatus = null;
    _notify();
    try {
      if (current?.call() == false ||
          !_current(token, epoch) ||
          request != _request) {
        return;
      }
      final response = await _client
          .get(_endpoint('/v1/me/moments'), headers: {'Authorization': token})
          .timeout(const Duration(seconds: 10));
      if (current?.call() == false ||
          !_current(token, epoch) ||
          request != _request) {
        return;
      }
      _readStatus = response.statusCode;
      if (response.statusCode != 200) {
        throw const FormatException('unavailable');
      }
      final rows =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as List<dynamic>;
      final next = rows
          .map((row) => PrivateMoment.fromJson(row as Map<String, dynamic>))
          .toList();
      for (final moment in next) {
        _checkAuthor(moment, owner);
      }
      if (_current(token, epoch) &&
          request == _request &&
          current?.call() != false) {
        _moments = List.unmodifiable(next);
        _loadedToken = token;
        _loadedOwner = owner;
      }
    } catch (_) {
      if (_current(token, epoch) &&
          request == _request &&
          current?.call() != false) {
        _error = '无法读取私人草稿，请稍后重试。';
      }
    } finally {
      if (_current(token, epoch) && request == _request) {
        _loading = false;
        _notify();
      }
    }
  }

  Future<bool> create({
    required String cityID,
    required String title,
    required String body,
    String placeID = '',
    String activityID = '',
    String communityID = '',
    String organizationID = '',
    MomentTimeValue time = MomentTimeValue.unknown,
  }) async {
    return _write('POST', '/v1/me/moments', {
      'cityId': cityID,
      'placeId': placeID,
      'activityId': activityID,
      'communityId': communityID,
      'organizationId': organizationID,
      'title': title,
      'body': body,
      ...time.payload,
      'locationPrecision': placeID.isEmpty ? 'city' : 'place',
    });
  }

  Future<bool> update({
    required PrivateMoment moment,
    required String title,
    required String body,
    required String placeID,
    String? activityID,
    String? communityID,
    String? organizationID,
    MomentTimeValue? time,
  }) async {
    _syncActor();
    if (!moment.isEditableDraft || !_ownsVersion(moment)) return false;
    return _write('PUT', '/v1/me/moments/${moment.id}', {
      'cityId': moment.cityID,
      'placeId': placeID,
      'title': title,
      'body': body,
      ...(time ?? moment.time).payload,
      'locationPrecision': placeID.isEmpty ? 'city' : 'place',
      'revision': moment.revision,
      'activityId': ?activityID,
      'communityId': ?communityID,
      'organizationId': ?organizationID,
    }, moment: moment);
  }

  Future<bool> withdraw(PrivateMoment moment) async {
    _syncActor();
    if (!_ownsVersion(moment)) return false;
    return _write(
      'DELETE',
      '/v1/me/moments/${moment.id}',
      const {},
      moment: moment,
    );
  }

  Future<bool> _write(
    String method,
    String path,
    Map<String, dynamic> payload, {
    PrivateMoment? moment,
  }) async {
    if (_closed) return false;
    final token = _syncActor(), epoch = _epoch;
    final owner = _owner;
    if (token == null ||
        _apiBase.isEmpty ||
        !_ownerReady ||
        _saving ||
        (method == 'POST' && _creationUncertain)) {
      return false;
    }
    _request++;
    _loading = false;
    _saving = true;
    _error = null;
    _notify();
    var received = false, accepted = false;
    try {
      final headers = {
        'Authorization': token,
        'Content-Type': 'application/json',
      };
      final uri = _endpoint(path);
      final response = await (switch (method) {
        'POST' => _client.post(
          uri,
          headers: headers,
          body: jsonEncode(payload),
        ),
        'PUT' => _client.put(uri, headers: headers, body: jsonEncode(payload)),
        _ => _client.delete(
          uri.replace(queryParameters: {'revision': '${moment!.revision}'}),
          headers: headers,
        ),
      }).timeout(const Duration(seconds: 10));
      received = true;
      if (response.statusCode == 409) throw const FormatException('changed');
      if (response.statusCode !=
          (method == 'POST'
              ? 201
              : method == 'PUT'
              ? 200
              : 204)) {
        throw const FormatException('rejected');
      }
      accepted = true;
      if (!_current(token, epoch)) return false;
      _request++;
      _loading = false;
      _loadedToken = token;
      _loadedOwner = owner;
      if (method == 'DELETE') {
        _moments = List.unmodifiable(
          _moments.where((item) => item.id != moment!.id),
        );
      } else {
        final result = PrivateMoment.fromJson(
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as Map<String, dynamic>,
        );
        _checkAuthor(result, owner);
        if (method == 'PUT' &&
            (result.id != moment!.id || result.revision <= moment.revision)) {
          throw const FormatException('invalid result');
        }
        _moments = List.unmodifiable([
          result,
          ..._moments.where((item) => item.id != result.id),
        ]);
      }
      _notify();
      if (method == 'POST') await refresh();
      return _current(token, epoch);
    } catch (error) {
      if (_current(token, epoch)) {
        // The API has no create idempotency key. A lost response is not proof
        // of failure, so require review instead of automatically repeating it.
        _creationUncertain = method == 'POST' && (!received || accepted);
        _error = _creationUncertain
            ? '保存结果尚未确认，请返回列表检查，避免重复保存。'
            : error is FormatException && error.message == 'changed'
            ? '草稿已在别处更新，请刷新后重新打开。'
            : method == 'DELETE'
            ? '撤回未确认，请刷新列表检查。'
            : '保存未确认，请检查内容并刷新列表。';
        _notify();
      }
      return false;
    } finally {
      if (_current(token, epoch)) {
        _saving = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    identityChanges?.removeListener(_onIdentityChanged);
    _epoch++;
    _request++;
    _client.close();
    super.dispose();
  }
}
