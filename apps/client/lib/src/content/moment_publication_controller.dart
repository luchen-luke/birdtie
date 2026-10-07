import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';

class MomentPublicPreview {
  const MomentPublicPreview({
    required this.id,
    required this.placeID,
    required this.placeName,
    required this.title,
    required this.body,
    required this.revision,
    required this.snapshot,
    required this.expiresAt,
    required this.withdrawal,
  });
  final String id, placeID, placeName, title, body, snapshot;
  final int revision;
  final DateTime expiresAt;
  final bool withdrawal;
}

class MomentPublicationController extends ChangeNotifier {
  MomentPublicationController({
    required this.momentID,
    required this.placeID,
    required this.authorizationHeader,
    String? Function()? organizationWorkspaceID,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _organizationWorkspace = organizationWorkspaceID ?? (() => null),
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  final String momentID, placeID, _base;
  final String? Function() authorizationHeader;
  final String? Function() _organizationWorkspace;
  final http.Client _client;
  final bool _ownsClient;
  String? _actor, _workspace, _error, _resolvedStatus;
  int _epoch = 0, _request = 0;
  bool _closed = false, _loading = false, _saving = false, _uncertain = false;
  MomentPublicPreview? _preview;
  static final _uuid = RegExp(
    r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
  );
  static final _snapshot = RegExp(
    r'^mp1\.[1-9][0-9]{0,18}\.[1-9][0-9]{0,18}\.[0-9a-f]{64}$',
  );
  String? _sync() {
    final next = authorizationHeader(), workspace = _organizationWorkspace();
    if (next != _actor || workspace != _workspace) {
      _actor = next;
      _workspace = workspace;
      _epoch++;
      _request++;
      _preview = null;
      _error = workspace == null ? null : '当前为组织工作区，个人记录和公开操作已暂停。';
      _loading = false;
      _saving = false;
      _uncertain = false;
      _resolvedStatus = null;
    }
    return next;
  }

  bool _current(String token, int epoch) =>
      !_closed && _sync() == token && epoch == _epoch;
  void invalidate({bool deferNotification = false}) {
    _sync();
    _epoch++;
    _request++;
    _preview = null;
    _loading = false;
    _saving = false;
    _uncertain = false;
    _resolvedStatus = null;
    _error = _workspace == null ? '登录状态已变化，请重新查看记录。' : '当前为组织工作区，个人记录和公开操作已暂停。';
    if (deferNotification) {
      scheduleMicrotask(_notify);
    } else {
      _notify();
    }
  }

  void _notify() {
    if (!_closed) notifyListeners();
  }

  MomentPublicPreview? get preview {
    _sync();
    return _preview;
  }

  String? get error {
    _sync();
    return _error;
  }

  bool get loading {
    _sync();
    return _loading;
  }

  bool get saving {
    _sync();
    return _saving;
  }

  bool get uncertain {
    _sync();
    return _uncertain;
  }

  String? get resolvedStatus {
    _sync();
    return _resolvedStatus;
  }

  Future<void> checkCurrentStatus({required bool withdrawal}) async {
    if (_closed || _saving || _loading) return;
    final token = _sync(), epoch = _epoch, request = ++_request;
    if (_workspace != null) {
      _notify();
      return;
    }
    if (token == null) return;
    _loading = true;
    _error = null;
    _preview = null;
    _notify();
    bool draft = false;
    try {
      final response = await _client
          .get(_url(''), headers: {'Authorization': token})
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) throw const FormatException();
      final d = _data(response);
      if (d['id'] != momentID ||
          d['placeId'] != placeID ||
          d['revision'] is! int ||
          d['revision'] < 1) {
        throw const FormatException();
      }
      final status = d['status'], visibility = d['visibility'];
      if (!((status == 'draft' && visibility == 'private') ||
          (status == 'withdrawn' && visibility == 'private') ||
          (status == 'published' && visibility == 'public'))) {
        throw const FormatException();
      }
      if (_current(token, epoch) && request == _request) {
        _uncertain = false;
        if (status == 'draft') {
          draft = true;
          _resolvedStatus = null;
        } else {
          _resolvedStatus = status;
          _error = status == 'published' ? '已重新核对：当前记录已公开。' : '已重新核对：当前记录已撤回。';
        }
      }
    } catch (_) {
      if (_current(token, epoch) && request == _request) {
        _uncertain = true;
        _error = '当前状态仍未确认，请稍后再读取，不要重复提交。';
      }
    } finally {
      if (_current(token, epoch) && request == _request) {
        _loading = false;
        _notify();
      }
    }
    if (draft && _current(token, epoch)) await load(withdrawal: withdrawal);
  }

  Uri _url(String suffix) => Uri.parse(
    '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/moments/${Uri.encodeComponent(momentID)}$suffix',
  );
  DateTime _date(dynamic raw) {
    if (raw is! String || !RegExp(r'(Z|[+-]\d{2}:\d{2})$').hasMatch(raw)) {
      throw const FormatException();
    }
    return DateTime.parse(raw).toUtc();
  }

  Map<String, dynamic> _data(http.Response response) {
    final j = jsonDecode(utf8.decode(response.bodyBytes));
    if (j is! Map<String, dynamic> ||
        j.length != 1 ||
        j['data'] is! Map<String, dynamic>) {
      throw const FormatException();
    }
    return j['data'] as Map<String, dynamic>;
  }

  Future<void> load({bool withdrawal = false}) async {
    if (_closed || _saving) return;
    if (_uncertain) {
      await checkCurrentStatus(withdrawal: withdrawal);
      return;
    }
    final token = _sync(), epoch = _epoch, request = ++_request;
    _preview = null;
    _error = null;
    _uncertain = false;
    _resolvedStatus = null;
    if (token == null ||
        _workspace != null ||
        !_uuid.hasMatch(momentID) ||
        !_uuid.hasMatch(placeID) ||
        _base.isEmpty) {
      _error = _workspace == null ? '请登录后查看自己的记录。' : '当前为组织工作区，个人记录和公开操作已暂停。';
      _notify();
      return;
    }
    _loading = true;
    _notify();
    try {
      final response = await _client
          .get(
            _url(withdrawal ? '' : '/publication'),
            headers: {'Authorization': token},
          )
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) {
        throw StateError('${response.statusCode}');
      }
      final d = _data(response),
          id = d[withdrawal ? 'id' : 'momentId'],
          target = d['placeId'],
          revision = d['revision'];
      if (id != momentID ||
          target != placeID ||
          revision is! int ||
          revision < 1 ||
          d['title'] is! String ||
          d['body'] is! String) {
        throw const FormatException();
      }
      final title = d['title'] as String, body = d['body'] as String;
      if (title.isEmpty ||
          utf8.encode(title).length > 160 ||
          utf8.encode(body).length > 5000) {
        throw const FormatException();
      }
      late final DateTime expiry;
      late final String snapshot, placeName;
      if (withdrawal) {
        if (d['status'] != 'published' || d['visibility'] != 'public') {
          throw const FormatException();
        }
        expiry = DateTime.now().toUtc().add(const Duration(seconds: 90));
        snapshot = '';
        placeName = '当前关联地点';
      } else {
        const keys = {
          'momentId',
          'placeId',
          'cityId',
          'placeName',
          'title',
          'body',
          'revision',
          'snapshot',
          'expiresAt',
        };
        if (d.keys.toSet().difference(keys).isNotEmpty ||
            d.length != keys.length ||
            d['cityId'] is! String ||
            d['cityId'].isEmpty ||
            d['placeName'] is! String ||
            d['placeName'].isEmpty ||
            d['snapshot'] is! String ||
            !_snapshot.hasMatch(d['snapshot'])) {
          throw const FormatException();
        }
        snapshot = d['snapshot'] as String;
        placeName = d['placeName'] as String;
        expiry = _date(d['expiresAt']);
        if (!expiry.isAfter(DateTime.now().toUtc())) {
          throw const FormatException();
        }
      }
      if (_current(token, epoch) && request == _request) {
        _preview = MomentPublicPreview(
          id: momentID,
          placeID: placeID,
          placeName: placeName,
          title: title,
          body: body,
          revision: revision,
          snapshot: snapshot,
          expiresAt: expiry,
          withdrawal: withdrawal,
        );
      }
    } catch (_) {
      if (_current(token, epoch) && request == _request) {
        _error = withdrawal ? '公开记录已变化或暂不可用，请重新查看。' : '无法读取公开预览，请先检查记录当前状态。';
      }
    } finally {
      if (_current(token, epoch) && request == _request) {
        _loading = false;
        _notify();
      }
    }
  }

  Future<bool> confirm(
    MomentPublicPreview expected, {
    required bool checked,
  }) async {
    final token = _sync(), epoch = _epoch;
    if (_closed ||
        _saving ||
        _loading ||
        _uncertain ||
        token == null ||
        _workspace != null ||
        !checked ||
        !identical(expected, _preview) ||
        !expected.expiresAt.isAfter(DateTime.now().toUtc())) {
      _preview = null;
      _error = '请重新查看当前内容并确认。';
      _notify();
      return false;
    }
    _saving = true;
    _error = null;
    _notify();
    bool sent = false;
    try {
      sent = true;
      final response =
          await (expected.withdrawal
                  ? _client.delete(
                      _url('?revision=${expected.revision}'),
                      headers: {'Authorization': token},
                    )
                  : _client.post(
                      _url('/publication'),
                      headers: {
                        'Authorization': token,
                        'Content-Type': 'application/json',
                      },
                      body: jsonEncode({
                        'revision': expected.revision,
                        'snapshot': expected.snapshot,
                        'confirmPublic': true,
                      }),
                    ))
              .timeout(const Duration(seconds: 12));
      if (!_current(token, epoch)) return false;
      if (expected.withdrawal && response.statusCode == 204) {
        _preview = null;
        return true;
      }
      if (!expected.withdrawal && response.statusCode == 200) {
        final d = _data(response);
        const keys = {
          'momentId',
          'placeId',
          'revision',
          'status',
          'publishedAt',
        };
        if (d.keys.toSet().difference(keys).isNotEmpty ||
            d.length != 5 ||
            d['momentId'] != momentID ||
            d['placeId'] != placeID ||
            d['revision'] != expected.revision + 1 ||
            d['status'] != 'published') {
          throw const FormatException();
        }
        _date(d['publishedAt']);
        _preview = null;
        return true;
      }
      _preview = null;
      if (response.statusCode >= 500) {
        _uncertain = true;
        _error = '提交结果尚未确认，请重新读取记录状态，不要重复提交。';
      } else {
        _error = '记录或登录状态已变化，请重新查看后再确认。';
      }
      return false;
    } catch (_) {
      if (_current(token, epoch)) {
        _preview = null;
        _uncertain = sent;
        _error = '提交结果尚未确认，请重新读取记录状态，不要重复提交。';
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
    _epoch++;
    _preview = null;
    if (_ownsClient) _client.close();
    super.dispose();
  }
}
