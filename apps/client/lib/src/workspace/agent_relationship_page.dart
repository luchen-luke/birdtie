import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../auth/birdtie_auth_controller.dart';
import '../config/birdtie_environment.dart';

class AgentRelationshipPage extends StatefulWidget {
  const AgentRelationshipPage({
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
  State<AgentRelationshipPage> createState() => _AgentRelationshipPageState();
}

class _AgentRelationshipPageState extends State<AgentRelationshipPage> {
  late final http.Client _client = widget.client ?? http.Client();
  String? _token, _error;
  bool? _enabled;
  bool _busy = false, _truncated = false;
  int _serial = 0;
  List<Map<String, dynamic>> _peers = [];
  String get _base => widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  Uri _url(String path) =>
      Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}/v1/me/$path');
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
      _enabled = null;
      _peers = [];
      _error = null;
      _busy = false;
      _truncated = false;
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

  Map<String, dynamic> _data(http.Response response) {
    if (response.statusCode != 200) throw StateError('unavailable');
    return (jsonDecode(utf8.decode(response.bodyBytes))
            as Map<String, dynamic>)['data']
        as Map<String, dynamic>;
  }

  bool _current(int serial, String token) =>
      mounted &&
      _entryCurrent &&
      serial == _serial &&
      widget.auth.authorizationHeader == token;
  Future<void> _load({bool? save}) async {
    if (!_entryCurrent) return;
    final token = widget.auth.authorizationHeader;
    if (_busy || token == null || _base.isEmpty) return;
    final serial = ++_serial;
    setState(() {
      _busy = true;
      _error = null;
      _peers = [];
      _truncated = false;
    });
    try {
      if (!_current(serial, token)) return;
      final headers = {'Authorization': token};
      final consent = _data(
        await (save == null
                ? _client.get(
                    _url('agent-relationship-consent'),
                    headers: headers,
                  )
                : _client.put(
                    _url('agent-relationship-consent'),
                    headers: {...headers, 'Content-Type': 'application/json'},
                    body: jsonEncode({'enabled': save}),
                  ))
            .timeout(const Duration(seconds: 12)),
      );
      if (!_current(serial, token)) return;
      final enabled = consent['enabled'] as bool;
      setState(() => _enabled = enabled);
      if (!enabled || !_current(serial, token)) return;
      final signals = _data(
        await _client
            .get(_url('agent-relationship-context'), headers: headers)
            .timeout(const Duration(seconds: 12)),
      );
      if (!_current(serial, token)) return;
      setState(() {
        _enabled = signals['enabled'] as bool;
        _peers = _enabled!
            ? (signals['peers'] as List<dynamic>).cast<Map<String, dynamic>>()
            : [];
        _truncated = signals['truncated'] as bool? ?? false;
      });
    } catch (_) {
      if (_current(serial, token)) {
        setState(
          () => _error = save == null
              ? '关系信号暂不可用，请重试。停用的个人 Agent 无法读取。'
              : '设置或信号读取失败，请刷新确认当前授权状态。',
        );
      }
    } finally {
      if (_current(serial, token)) setState(() => _busy = false);
    }
  }

  String _frequency(String? value) => switch (value) {
    'RECENT_REPEATED' => '多日有联系记录',
    'RECENT' => '近期有联系记录',
    _ => '暂无近期对话记录',
  };
  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('Agent 关系信号'),
      actions: [
        IconButton(
          onPressed: _busy ? null : _load,
          icon: const Icon(Icons.refresh),
          tooltip: '刷新',
        ),
      ],
    ),
    body: !_entryCurrent
        ? const Center(child: Text('工作身份或来源已变化，请返回当前入口重新核实。'))
        : widget.auth.authorizationHeader == null
        ? const Center(child: Text('登录后管理本人的 Agent 关系信号。'))
        : _base.isEmpty
        ? const Center(child: Text('请连接 Birdtie 后管理关系信号。'))
        : ListView(
            padding: const EdgeInsets.all(20),
            children: [
              const Text(
                '默认关闭。开启后，仅本人的 Personal Agent 可使用当前好友关系、最近 30 天本人发送消息的数量和活跃日数。你可以随时关闭。',
              ),
              const SizedBox(height: 12),
              const Text(
                '不读取私聊正文、私密记忆或位置；记录频次不代表亲密程度。共同活动仅在双方允许展示时提供，表示共同报名，不代表真实到场。',
              ),
              const SizedBox(height: 16),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('允许个人 Agent 使用关系信号'),
                value: _enabled ?? false,
                onChanged: _busy || _enabled == null
                    ? null
                    : (value) => _load(save: value),
              ),
              if (_busy) const LinearProgressIndicator(),
              if (_error != null)
                TextButton(
                  onPressed: _busy ? null : _load,
                  child: Text(_error!),
                ),
              if (!_busy && _error == null && _enabled == false)
                const Text('尚未开启，不向 Agent 提供关系信号。'),
              if (!_busy &&
                  _error == null &&
                  _enabled == true &&
                  _peers.isEmpty)
                const Text('暂无符合当前授权的好友信号。'),
              if (_truncated) const Text('当前显示最多 50 位好友，按本人近期对话记录排序。'),
              for (final peer in _peers)
                Padding(
                  padding: const EdgeInsets.only(top: 20),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        peer['displayName'] as String,
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      Text(
                        '${_frequency(peer['frequency'] as String?)} · 本人发送 ${peer['sentMessages']} 条 · ${peer['activeDays']} 天',
                      ),
                      for (final activity
                          in peer['sharedActivities'] as List<dynamic>)
                        Text(
                          '共同报名：${(activity as Map<String, dynamic>)['title']}',
                        ),
                      if (peer['activitiesTruncated'] == true)
                        const Text('仅显示最近 10 个可展示活动。'),
                    ],
                  ),
                ),
            ],
          ),
  );
}
