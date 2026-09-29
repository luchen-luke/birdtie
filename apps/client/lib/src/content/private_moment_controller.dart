import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

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
  });

  final String id;
  final String cityID;
  final String placeID;
  final String title;
  final String body;
  final String timePrecision;
  final String locationPrecision;
  final String status;
  final int revision;

  factory PrivateMoment.fromJson(Map<String, dynamic> json) => PrivateMoment(
    id: json['id'] as String,
    cityID: json['cityId'] as String,
    placeID: json['placeId'] as String? ?? '',
    title: json['title'] as String,
    body: json['body'] as String? ?? '',
    timePrecision: json['timePrecision'] as String,
    locationPrecision: json['locationPrecision'] as String,
    status: json['status'] as String,
    revision: json['revision'] as int,
  );

  bool get isTextOnlyDraft =>
      status == 'draft' &&
      placeID.isEmpty &&
      timePrecision == 'unknown' &&
      locationPrecision == 'city';
}

class PrivateMomentController extends ChangeNotifier {
  PrivateMomentController({required this.authorizationHeader})
    : _client = http.Client();

  static const _apiBase = String.fromEnvironment('BIRDTIE_API_BASE_URL');
  final String? Function() authorizationHeader;
  final http.Client _client;
  List<PrivateMoment> _moments = const [];
  bool _loading = false;
  bool _saving = false;
  String? _error;
  bool _closed = false;
  int _request = 0;

  List<PrivateMoment> get moments => _moments;
  bool get loading => _loading;
  bool get saving => _saving;
  String? get error => _error;

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBase.replaceFirst(RegExp(r'/$'), '')}$path');

  void _notify() {
    if (!_closed) notifyListeners();
  }

  Future<void> refresh() async {
    final token = authorizationHeader();
    final request = ++_request;
    if (token == null || _apiBase.isEmpty) {
      _moments = const [];
      _error = null;
      _loading = false;
      _notify();
      return;
    }
    _loading = true;
    _error = null;
    _notify();
    try {
      final response = await _client
          .get(_endpoint('/v1/me/moments'), headers: {'Authorization': token})
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 200) {
        throw const FormatException('moment drafts unavailable');
      }
      final body = jsonDecode(response.body) as Map<String, dynamic>;
      final records = body['data'] as List<dynamic>;
      final next = records
          .map((item) => PrivateMoment.fromJson(item as Map<String, dynamic>))
          .toList(growable: false);
      if (request == _request && authorizationHeader() == token) {
        _moments = List.unmodifiable(next);
      }
    } catch (_) {
      if (request == _request) _error = '无法读取私人草稿，请稍后重试。';
    } finally {
      if (request == _request) {
        _loading = false;
        _notify();
      }
    }
  }

  Future<bool> create({
    required String cityID,
    required String title,
    required String body,
  }) async {
    final token = authorizationHeader();
    if (token == null || _apiBase.isEmpty || _saving) return false;
    _saving = true;
    _error = null;
    _notify();
    try {
      final response = await _client
          .post(
            _endpoint('/v1/me/moments'),
            headers: {
              'Authorization': token,
              'Content-Type': 'application/json',
            },
            body: jsonEncode({
              'cityId': cityID,
              'placeId': '',
              'title': title,
              'body': body,
              'timePrecision': 'unknown',
              'locationPrecision': 'city',
            }),
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 201) {
        throw const FormatException('moment draft rejected');
      }
      if (authorizationHeader() != token) return false;
      final created = PrivateMoment.fromJson(
        (jsonDecode(response.body) as Map<String, dynamic>)['data']
            as Map<String, dynamic>,
      );
      _moments = List.unmodifiable([created, ..._moments]);
      _notify();
      await refresh();
      return true;
    } catch (_) {
      _error = '草稿未保存，请检查内容或稍后重试。';
      _notify();
      return false;
    } finally {
      _saving = false;
      _notify();
    }
  }

  Future<bool> update({
    required PrivateMoment moment,
    required String title,
    required String body,
  }) async {
    final token = authorizationHeader();
    if (token == null || _apiBase.isEmpty || _saving || !moment.isTextOnlyDraft) {
      return false;
    }
    _saving = true;
    _error = null;
    _notify();
    try {
      final response = await _client
          .put(
            _endpoint('/v1/me/moments/${moment.id}'),
            headers: {
              'Authorization': token,
              'Content-Type': 'application/json',
            },
            body: jsonEncode({
              'cityId': moment.cityID,
              'placeId': '',
              'title': title,
              'body': body,
              'timePrecision': 'unknown',
              'locationPrecision': 'city',
              'revision': moment.revision,
            }),
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode == 409) {
        throw const FormatException('draft changed');
      }
      if (response.statusCode != 200) {
        throw const FormatException('draft rejected');
      }
      if (authorizationHeader() != token) return false;
      final updated = PrivateMoment.fromJson(
        (jsonDecode(response.body) as Map<String, dynamic>)['data']
            as Map<String, dynamic>,
      );
      _moments = List.unmodifiable([
        for (final item in _moments)
          if (item.id == updated.id) updated else item,
      ]);
      _notify();
      return true;
    } catch (error) {
      _error = error is FormatException && error.message == 'draft changed'
          ? '草稿已在别处更新，请刷新后重试。'
          : '草稿未更新，请稍后重试。';
      _notify();
      return false;
    } finally {
      _saving = false;
      _notify();
    }
  }

  Future<bool> withdraw(PrivateMoment moment) async {
    final token = authorizationHeader();
    if (token == null || _apiBase.isEmpty || _saving) return false;
    _saving = true;
    _error = null;
    _notify();
    try {
      final endpoint = _endpoint(
        '/v1/me/moments/${moment.id}',
      ).replace(queryParameters: {'revision': '${moment.revision}'});
      final response = await _client
          .delete(endpoint, headers: {'Authorization': token})
          .timeout(const Duration(seconds: 10));
      if (response.statusCode == 409) {
        throw const FormatException('draft changed');
      }
      if (response.statusCode != 204) {
        throw const FormatException('withdraw rejected');
      }
      if (authorizationHeader() != token) return false;
      _moments = List.unmodifiable([
        for (final item in _moments)
          if (item.id != moment.id) item,
      ]);
      _notify();
      return true;
    } catch (error) {
      _error = error is FormatException && error.message == 'draft changed'
          ? '草稿已在别处更新，请刷新后重试。'
          : '撤回失败，请稍后重试。';
      _notify();
      return false;
    } finally {
      _saving = false;
      _notify();
    }
  }

  @override
  void dispose() {
    _closed = true;
    _client.close();
    super.dispose();
  }
}
