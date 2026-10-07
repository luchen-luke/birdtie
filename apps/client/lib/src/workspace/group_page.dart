import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';
import '../config/birdtie_environment.dart';

class GroupPage extends StatefulWidget {
  const GroupPage({super.key, required this.auth, required this.city});
  final BirdtieAuthController auth;
  final PublicCityController city;

  @override
  State<GroupPage> createState() => _GroupPageState();
}

class _GroupPageState extends State<GroupPage> {
  static const _apiBase = BirdtieEnvironment.apiBaseUrl;
  final _client = http.Client();
  final _name = TextEditingController();
  final _summary = TextEditingController();
  final _sourceLabel = TextEditingController();
  final _sourceURL = TextEditingController();
  final _rightsNote = TextEditingController();
  List<Map<String, dynamic>> _groups = const [];
  bool _busy = false;
  bool _failed = false;
  bool _signedIn = false;
  int _serial = 0;
  String? _message;

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBase.replaceFirst(RegExp(r'/$'), '')}$path');
  Map<String, String> _headers({bool json = false}) => {
    'Authorization': widget.auth.authorizationHeader!,
    if (json) 'Content-Type': 'application/json',
  };

  @override
  void initState() {
    super.initState();
    _signedIn = widget.auth.signedIn;
    widget.auth.addListener(_onAuthChanged);
    if (_signedIn && _apiBase.isNotEmpty) _load();
  }

  void _onAuthChanged() {
    if (_signedIn == widget.auth.signedIn) return;
    _signedIn = widget.auth.signedIn;
    if (!_signedIn) {
      ++_serial;
      _name.clear();
      _summary.clear();
      _sourceLabel.clear();
      _sourceURL.clear();
      _rightsNote.clear();
      setState(() {
        _groups = const [];
        _busy = false;
        _failed = false;
        _message = null;
      });
    } else if (_apiBase.isNotEmpty) {
      _load();
    }
  }

  Future<void> _load() async {
    final serial = ++_serial;
    setState(() {
      _busy = true;
      _failed = false;
    });
    try {
      final response = await _client
          .get(_endpoint('/v1/me/communities'), headers: _headers())
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 200) throw StateError('Groups unavailable');
      final records =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as List<dynamic>;
      if (!mounted || serial != _serial) return;
      setState(
        () =>
            _groups = [for (final raw in records) raw as Map<String, dynamic>],
      );
    } catch (_) {
      if (mounted && serial == _serial) setState(() => _failed = true);
    } finally {
      if (mounted && serial == _serial) setState(() => _busy = false);
    }
  }

  Future<void> _submit() async {
    final serial = _serial;
    final cityID = widget.city.selectedCity?.id;
    if (cityID == null) {
      setState(() => _message = '请先选择城市。');
      return;
    }
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      final response = await _client
          .post(
            _endpoint('/v1/cities/${Uri.encodeComponent(cityID)}/communities'),
            headers: _headers(json: true),
            body: jsonEncode({
              'name': _name.text.trim(),
              'summary': _summary.text.trim(),
              'sourceLabel': _sourceLabel.text.trim(),
              'sourceUrl': _sourceURL.text.trim(),
              'rightsNote': _rightsNote.text.trim(),
              'expiresAt': DateTime.now()
                  .toUtc()
                  .add(const Duration(days: 90))
                  .toIso8601String(),
            }),
          )
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 201) {
        throw StateError('请检查社群资料和可选的来源信息。');
      }
      if (!mounted || serial != _serial || !widget.auth.signedIn) return;
      _name.clear();
      _summary.clear();
      _sourceLabel.clear();
      _sourceURL.clear();
      _rightsNote.clear();
      if (!mounted) return;
      setState(() => _message = '社群已发布，你可以随时撤回。');
      await _load();
    } catch (_) {
      if (mounted && serial == _serial && widget.auth.signedIn) {
        setState(() => _message = '提交失败，请检查填写内容后重试。');
      }
    } finally {
      if (mounted && widget.auth.signedIn) setState(() => _busy = false);
    }
  }

  Future<void> _withdraw(String id) async {
    final serial = _serial;
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      final response = await _client
          .post(
            _endpoint('/v1/me/communities/${Uri.encodeComponent(id)}/withdraw'),
            headers: _headers(),
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 204) throw StateError('Withdraw failed');
      if (!mounted || serial != _serial || !widget.auth.signedIn) return;
      await _load();
    } catch (_) {
      if (mounted && serial == _serial && widget.auth.signedIn) {
        setState(() => _message = '撤回社群失败，请重试。');
      }
    } finally {
      if (mounted && widget.auth.signedIn) setState(() => _busy = false);
    }
  }

  @override
  void dispose() {
    ++_serial;
    widget.auth.removeListener(_onAuthChanged);
    _client.close();
    _name.dispose();
    _summary.dispose();
    _sourceLabel.dispose();
    _sourceURL.dispose();
    _rightsNote.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (_apiBase.isEmpty) {
      return const Center(child: Text('请连接 Birdtie API 后管理社群。'));
    }
    if (!widget.auth.signedIn) {
      return const Center(child: Text('请先在个人资料页面登录，再管理社群。'));
    }
    return ListView(
      padding: const EdgeInsets.all(20),
      children: [
        const Text(
          '我的社群',
          style: TextStyle(fontSize: 21, fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 8),
        const Text(
          '你发布的社群会在 Birdtie 展示，直到内容过期或你主动撤回。',
          style: TextStyle(color: Color(0xFF747B73)),
        ),
        if (_busy) const LinearProgressIndicator(),
        if (_failed)
          TextButton(onPressed: _load, child: const Text('社群加载失败，点击重试')),
        if (!_failed && !_busy && _groups.isEmpty)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 20),
            child: Text('你还没有提交社群。'),
          ),
        for (final group in _groups)
          ListTile(
            contentPadding: EdgeInsets.zero,
            title: Text(group['name'] as String),
            subtitle: Text(switch (group['status'] as String) {
              'draft' => '未发布的历史提交',
              'published' => '已发布',
              _ => '已隐藏',
            }),
            trailing: group['status'] == 'hidden'
                ? null
                : TextButton(
                    onPressed: _busy
                        ? null
                        : () => _withdraw(group['id'] as String),
                    child: const Text('撤回'),
                  ),
          ),
        const Divider(height: 36),
        const Text(
          '发布社群',
          style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 8),
        const Text(
          '仅发布你拥有或获授权代表的社群。外部来源为可选项；如填写，请提供公开 HTTPS 链接。',
          style: TextStyle(color: Color(0xFF747B73)),
        ),
        _field(_name, '社群名称', 160),
        _field(_summary, '社群介绍', 3000, lines: 3),
        _field(_sourceLabel, '外部来源名称（选填）', 120),
        _field(_sourceURL, '公开 HTTPS 来源链接（选填）', 1000),
        _field(_rightsNote, '你的发布权限或授权说明（选填）', 1000, lines: 3),
        if (_message != null)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 8),
            child: Text(
              _message!,
              style: const TextStyle(color: Color(0xFF193B32)),
            ),
          ),
        const SizedBox(height: 12),
        FilledButton(
          onPressed: _busy ? null : _submit,
          child: const Text('发布社群'),
        ),
      ],
    );
  }

  Widget _field(
    TextEditingController controller,
    String label,
    int limit, {
    int lines = 1,
  }) => Padding(
    padding: const EdgeInsets.only(top: 14),
    child: TextField(
      controller: controller,
      maxLength: limit,
      maxLines: lines,
      decoration: InputDecoration(
        labelText: label,
        border: const OutlineInputBorder(),
      ),
    ),
  );
}
