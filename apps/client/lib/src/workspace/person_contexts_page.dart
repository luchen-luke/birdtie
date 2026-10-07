import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';
import '../config/birdtie_environment.dart';

class PersonContextsPage extends StatefulWidget {
  const PersonContextsPage({
    super.key,
    required this.auth,
    this.apiBaseUrl,
    this.client,
    this.current,
  });

  final BirdtieAuthController auth;
  final String? apiBaseUrl;
  final http.Client? client;
  // Optional lifetime of a caller-owned entry; legacy direct callers are unchanged.
  final bool Function()? current;

  @override
  State<PersonContextsPage> createState() => _PersonContextsPageState();
}

class _PersonContextsPageState extends State<PersonContextsPage> {
  late final http.Client _client = widget.client ?? http.Client();
  final _source = TextEditingController();
  List<Map<String, dynamic>> _declarations = const [];
  List<Map<String, dynamic>> _cities = const [];
  String _mode = 'current_city';
  String? _cityID;
  String? _error;
  bool _loading = true;
  bool _saving = false;
  int _serial = 0;
  String? _identityToken;

  bool get _current => mounted && (widget.current?.call() ?? true);

  String get _base => widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  Uri _uri(String path) =>
      Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}$path');

  @override
  void initState() {
    super.initState();
    _identityToken = widget.auth.authorizationHeader;
    widget.auth.addListener(_authChanged);
    unawaited(_load());
  }

  void _authChanged() {
    if (!_current) return;
    final token = widget.auth.authorizationHeader;
    if (token == _identityToken) return;
    _identityToken = token;
    ++_serial;
    setState(() {
      _declarations = const [];
      _loading = token != null;
    });
    if (token != null) unawaited(_load());
  }

  @override
  void dispose() {
    ++_serial;
    widget.auth.removeListener(_authChanged);
    _source.dispose();
    if (widget.client == null) _client.close();
    super.dispose();
  }

  Future<void> _load() async {
    if (!_current) return;
    final token = widget.auth.authorizationHeader;
    if (token == null || _base.isEmpty) {
      if (_current) setState(() => _loading = false);
      return;
    }
    final serial = ++_serial;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      if (!_current) return;
      final responses = await Future.wait([
        _client.get(_uri('/v1/me/contexts'), headers: {'Authorization': token}),
        _client.get(_uri('/v1/cities')),
      ]).timeout(const Duration(seconds: 12));
      if (!_current) return;
      if (responses.any((r) => r.statusCode != 200)) {
        throw StateError('load failed');
      }
      if (!_current ||
          serial != _serial ||
          widget.auth.authorizationHeader != token) {
        return;
      }
      final declarations =
          (jsonDecode(utf8.decode(responses[0].bodyBytes))
                  as Map<String, dynamic>)['data']
              as List<dynamic>;
      final cities =
          (jsonDecode(utf8.decode(responses[1].bodyBytes))
                  as Map<String, dynamic>)['data']
              as List<dynamic>;
      setState(() {
        _declarations = declarations.cast<Map<String, dynamic>>();
        _cities = cities.cast<Map<String, dynamic>>();
        if (_cityID == null || !_cities.any((city) => city['id'] == _cityID)) {
          _cityID = _cities.isEmpty ? null : _cities.first['id'] as String?;
        }
      });
    } catch (_) {
      if (_current && serial == _serial) {
        setState(() => _error = '生活情境暂不可用，请稍后重试。');
      }
    } finally {
      if (_current && serial == _serial) setState(() => _loading = false);
    }
  }

  Future<void> _save() async {
    final token = widget.auth.authorizationHeader;
    if (!_current || _saving || token == null) return;
    final mode = _mode;
    final city = mode.endsWith('_city');
    final source = city ? _cityID : _source.text.trim();
    if (source == null || source.isEmpty) {
      setState(() => _error = city ? '请先选择城市。' : '请填写情境名称。');
      return;
    }
    final (type, relation) = switch (mode) {
      'current_city' => ('CITY', 'current'),
      'past_city' => ('CITY', 'past'),
      'destination_city' => ('CITY', 'destination'),
      'past_institution' => ('INSTITUTION', 'past'),
      _ => ('ONLINE', 'interest'),
    };
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      if (!_current) return;
      final response = await _client
          .post(
            _uri('/v1/me/contexts'),
            headers: {
              'Authorization': token,
              'Content-Type': 'application/json',
            },
            body: jsonEncode({
              'contextType': type,
              'sourceKey': source,
              'relation': relation,
            }),
          )
          .timeout(const Duration(seconds: 12));
      if (!_current) return;
      if (response.statusCode != 201) throw StateError('save failed');
      if (_current && widget.auth.authorizationHeader == token) {
        _source.clear();
        await _load();
      }
    } catch (_) {
      if (_current) setState(() => _error = '保存失败，请检查内容后重试。');
    } finally {
      if (_current) setState(() => _saving = false);
    }
  }

  Future<void> _remove(Map<String, dynamic> item) async {
    final token = widget.auth.authorizationHeader;
    if (!_current || _saving || token == null) return;
    setState(() => _saving = true);
    try {
      if (!_current) return;
      final response = await _client
          .delete(
            _uri('/v1/me/contexts/${item['contextId']}/${item['relation']}'),
            headers: {'Authorization': token},
          )
          .timeout(const Duration(seconds: 12));
      if (!_current) return;
      if (response.statusCode != 204) throw StateError('remove failed');
      if (_current && widget.auth.authorizationHeader == token) await _load();
    } catch (_) {
      if (_current) setState(() => _error = '移除失败，请重试。');
    } finally {
      if (_current) setState(() => _saving = false);
    }
  }

  String _relationLabel(String? relation) => switch (relation) {
    'current' => '当前城市',
    'past' => '过去的城市或学校',
    'destination' => '计划前往',
    'home' => '家乡',
    'affiliation' => '学校关联',
    'interest' => '线上兴趣',
    _ => '生活情境',
  };

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('我的生活情境')),
    body: !widget.auth.signedIn
        ? const Center(child: Text('登录后管理自己的生活情境。'))
        : RefreshIndicator(
            onRefresh: _load,
            child: ListView(
              padding: const EdgeInsets.all(20),
              children: [
                const Text('这些信息仅自己可见，不代表学校或社群已认证你的身份。'),
                const SizedBox(height: 20),
                Text('已保存的情境', style: Theme.of(context).textTheme.titleMedium),
                if (_loading) const LinearProgressIndicator(),
                if (!_loading && _declarations.isEmpty)
                  const Text('还没有保存生活情境。'),
                for (final item in _declarations)
                  ListTile(
                    title: Text(item['label'] as String? ?? '情境'),
                    subtitle: Text(_relationLabel(item['relation'] as String?)),
                    trailing: IconButton(
                      tooltip: '移除此情境',
                      onPressed: _saving ? null : () => _remove(item),
                      icon: const Icon(Icons.close),
                    ),
                  ),
                const Divider(height: 32),
                Text('添加情境', style: Theme.of(context).textTheme.titleMedium),
                DropdownButtonFormField<String>(
                  initialValue: _mode,
                  decoration: const InputDecoration(labelText: '情境类型'),
                  items: const [
                    DropdownMenuItem(
                      value: 'current_city',
                      child: Text('当前城市'),
                    ),
                    DropdownMenuItem(
                      value: 'past_city',
                      child: Text('过去生活的城市'),
                    ),
                    DropdownMenuItem(
                      value: 'destination_city',
                      child: Text('计划前往的城市'),
                    ),
                    DropdownMenuItem(
                      value: 'past_institution',
                      child: Text('过去就读的学校'),
                    ),
                    DropdownMenuItem(value: 'online', child: Text('线上兴趣或社群')),
                  ],
                  onChanged: _saving
                      ? null
                      : (value) => setState(() => _mode = value ?? _mode),
                ),
                const SizedBox(height: 10),
                if (_mode.endsWith('_city'))
                  DropdownButtonFormField<String>(
                    key: ValueKey('context-city-$_cityID'),
                    initialValue: _cityID,
                    decoration: const InputDecoration(labelText: '城市'),
                    items: [
                      for (final city in _cities)
                        DropdownMenuItem(
                          value: city['id'] as String,
                          child: Text(
                            city['name'] as String? ?? city['id'] as String,
                          ),
                        ),
                    ],
                    onChanged: _saving
                        ? null
                        : (value) => setState(() => _cityID = value),
                  )
                else
                  TextField(
                    controller: _source,
                    maxLength: 160,
                    decoration: InputDecoration(
                      labelText: _mode == 'past_institution'
                          ? '学校名称'
                          : '线上情境名称',
                    ),
                  ),
                if (_error != null)
                  Text(_error!, style: const TextStyle(color: Colors.red)),
                const SizedBox(height: 12),
                FilledButton(
                  onPressed: _saving || _loading ? null : _save,
                  child: const Text('保存情境'),
                ),
              ],
            ),
          ),
  );
}
