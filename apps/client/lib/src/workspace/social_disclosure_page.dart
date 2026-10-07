import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';
import '../config/birdtie_environment.dart';

class SocialDisclosurePage extends StatefulWidget {
  const SocialDisclosurePage({
    super.key,
    required this.auth,
    this.apiBaseUrl,
    this.client,
    this.current,
  });
  final BirdtieAuthController auth;
  final String? apiBaseUrl;
  final http.Client? client;
  final bool Function()? current;

  @override
  State<SocialDisclosurePage> createState() => _SocialDisclosurePageState();
}

class _SocialDisclosurePageState extends State<SocialDisclosurePage> {
  late final http.Client _client = widget.client ?? http.Client();
  Map<String, bool>? _values;
  String? _token;
  String? _error;
  bool _busy = false;
  int _serial = 0;
  String get _base => widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  Uri get _endpoint => Uri.parse(
    '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/social-disclosure',
  );

  @override
  void initState() {
    super.initState();
    _token = widget.auth.authorizationHeader;
    widget.auth.addListener(_authChanged);
    unawaited(_load());
  }

  bool get _entryCurrent => widget.current?.call() ?? true;

  void _authChanged() {
    final token = widget.auth.authorizationHeader;
    if (token == _token) return;
    _token = token;
    ++_serial;
    setState(() {
      _values = null;
      _busy = false;
      _error = null;
    });
    if (token != null) unawaited(_load());
  }

  @override
  void dispose() {
    ++_serial;
    widget.auth.removeListener(_authChanged);
    if (widget.client == null) _client.close();
    super.dispose();
  }

  Map<String, bool> _parse(http.Response response) {
    if (response.statusCode != 200) throw StateError('disclosure unavailable');
    final data =
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as Map<String, dynamic>;
    return {
      for (final key in ['mutualTies', 'sharedCommunities', 'sharedActivities'])
        key: data[key] as bool,
    };
  }

  bool _current(int serial, String token) =>
      mounted &&
      _entryCurrent &&
      serial == _serial &&
      widget.auth.authorizationHeader == token;

  Future<void> _load() async {
    if (!_entryCurrent) return;
    final token = widget.auth.authorizationHeader;
    if (token == null || _base.isEmpty) return;
    final serial = ++_serial;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      if (!_current(serial, token)) return;
      final values = _parse(
        await _client
            .get(_endpoint, headers: {'Authorization': token})
            .timeout(const Duration(seconds: 12)),
      );
      if (_current(serial, token)) {
        setState(() => _values = values);
      }
    } catch (_) {
      if (_current(serial, token)) {
        setState(() => _error = '展示设置暂不可用，请重试。');
      }
    } finally {
      if (_current(serial, token)) setState(() => _busy = false);
    }
  }

  Future<void> _save(String key, bool value) async {
    if (!_entryCurrent) return;
    final token = widget.auth.authorizationHeader;
    if (_busy || _values == null || token == null) return;
    final serial = ++_serial;
    final input = {..._values!, key: value};
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      if (!_current(serial, token)) return;
      final values = _parse(
        await _client
            .put(
              _endpoint,
              headers: {
                'Authorization': token,
                'Content-Type': 'application/json',
              },
              body: jsonEncode(input),
            )
            .timeout(const Duration(seconds: 12)),
      );
      if (_current(serial, token)) {
        setState(() => _values = values);
      }
    } catch (_) {
      if (_current(serial, token)) {
        setState(() => _error = '保存结果暂未确认，请刷新核对当前设置。');
      }
    } finally {
      if (_current(serial, token)) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('共同信息展示')),
    body: !_entryCurrent
        ? const Center(child: Text('工作身份或来源已变化，请返回当前入口重新核实。'))
        : widget.auth.authorizationHeader == null
        ? const Center(child: Text('登录后可管理共同信息的展示。'))
        : _base.isEmpty
        ? const Center(child: Text('请连接 Birdtie 后管理展示设置。'))
        : ListView(
            padding: const EdgeInsets.all(20),
            children: [
              const Text('默认不展示。开启后，同样允许展示的已登录用户可在你的公开资料页看到双方共同的信息。你可以随时关闭。'),
              const SizedBox(height: 16),
              if (_busy) const LinearProgressIndicator(),
              if (_error != null)
                TextButton(
                  onPressed: _busy ? null : _load,
                  child: Text(_error!),
                ),
              for (final entry in const {
                'mutualTies': ('共同好友数量', '仅统计允许展示的公开好友，不展示好友名单。'),
                'sharedCommunities': ('共同公开社群', '仅展示双方都加入的公开社群，不展示私密社群。'),
                'sharedActivities': (
                  '共同参加的公开活动',
                  '仅展示双方都已报名参加的公开活动，包括过往活动；不展示邀请、成员活动或已取消的报名。',
                ),
              }.entries)
                SwitchListTile(
                  title: Text(entry.value.$1),
                  subtitle: Text(entry.value.$2),
                  value: _values?[entry.key] ?? false,
                  onChanged: _busy || _values == null
                      ? null
                      : (value) => _save(entry.key, value),
                ),
            ],
          ),
  );
}
