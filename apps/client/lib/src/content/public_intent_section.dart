import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';

class PublicIntentSection extends StatefulWidget {
  const PublicIntentSection({
    super.key,
    required this.auth,
    required this.city,
  });

  final BirdtieAuthController auth;
  final PublicCityController city;

  @override
  State<PublicIntentSection> createState() => _PublicIntentSectionState();
}

class _PublicIntentSectionState extends State<PublicIntentSection> {
  static const _apiBase = String.fromEnvironment('BIRDTIE_API_BASE_URL');
  final _client = http.Client();
  final _name = TextEditingController();
  final _bio = TextEditingController();
  final _topic = TextEditingController();
  final _details = TextEditingController();
  final _area = TextEditingController();
  List<Map<String, dynamic>> _intents = const [];
  bool _public = false;
  bool _confirmed = false;
  bool _loading = false;
  bool _saving = false;
  bool _signedIn = false;
  bool _failed = false;
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
    if (_signedIn) {
      if (_apiBase.isNotEmpty) _load();
    } else {
      ++_serial;
      _name.clear();
      _bio.clear();
      _topic.clear();
      _details.clear();
      _area.clear();
      setState(() {
        _intents = const [];
        _public = false;
        _confirmed = false;
        _message = null;
        _loading = false;
        _saving = false;
      });
    }
  }

  Future<void> _load() async {
    final serial = ++_serial;
    setState(() {
      _loading = true;
      _failed = false;
    });
    try {
      final me = await _client
          .get(_endpoint('/v1/me'), headers: _headers())
          .timeout(const Duration(seconds: 10));
      if (me.statusCode != 200) throw StateError('Account unavailable');
      final accountID =
          ((jsonDecode(me.body) as Map<String, dynamic>)['data']
                  as Map<String, dynamic>)['id']
              as String;
      final responses = await Future.wait([
        _client.get(
          _endpoint('/v1/accounts/$accountID/profile'),
          headers: _headers(),
        ),
        _client.get(_endpoint('/v1/me/intents'), headers: _headers()),
      ]).timeout(const Duration(seconds: 10));
      if (responses.any((response) => response.statusCode != 200)) {
        throw StateError('Profile or intents unavailable');
      }
      final profile =
          (jsonDecode(responses[0].body) as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      final records =
          (jsonDecode(responses[1].body) as Map<String, dynamic>)['data']
              as List<dynamic>;
      if (!mounted || serial != _serial || !widget.auth.signedIn) return;
      setState(() {
        _name.text = profile['displayName'] as String;
        _bio.text = profile['bio'] as String;
        _public = profile['visibility'] == 'public';
        _intents = [for (final raw in records) raw as Map<String, dynamic>];
      });
    } catch (_) {
      if (mounted && serial == _serial) setState(() => _failed = true);
    } finally {
      if (mounted && serial == _serial) setState(() => _loading = false);
    }
  }

  Future<void> _saveProfile() async {
    if (_saving || !widget.auth.signedIn) return;
    final serial = _serial;
    setState(() {
      _saving = true;
      _message = null;
    });
    try {
      final response = await _client
          .put(
            _endpoint('/v1/me/profile'),
            headers: _headers(json: true),
            body: jsonEncode({
              'displayName': _name.text.trim(),
              'bio': _bio.text.trim(),
              'visibility': _public ? 'public' : 'private',
            }),
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 200) throw StateError('Profile rejected');
      if (!mounted || serial != _serial || !widget.auth.signedIn) return;
      widget.auth.updateProfileDisplayName(_name.text.trim());
      await _load();
      if (mounted) {
        setState(() => _message = '资料已保存。若资料有变动，原待审或公开意图已撤回；如需展示，请重新提交。');
      }
    } catch (_) {
      if (mounted && serial == _serial) {
        setState(() => _message = '资料保存失败，请检查名称并重试。');
      }
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _submit() async {
    final city = widget.city.selectedCity;
    if (city == null ||
        !_public ||
        !_confirmed ||
        _saving ||
        _topic.text.trim().isEmpty ||
        _area.text.trim().isEmpty) {
      setState(() => _message = '请选择城市、公开资料并确认意图和粗略区域。');
      return;
    }
    final serial = _serial;
    setState(() {
      _saving = true;
      _message = null;
    });
    try {
      final start = DateTime.now().toUtc();
      final end = start.add(const Duration(days: 7));
      final response = await _client
          .post(
            _endpoint('/v1/cities/${Uri.encodeComponent(city.id)}/intents'),
            headers: _headers(json: true),
            body: jsonEncode({
              'confirmed': true,
              'topic': _topic.text.trim(),
              'details': _details.text.trim(),
              'coarseAreaLabel': _area.text.trim(),
              'availableFrom': start.toIso8601String(),
              'availableUntil': end.toIso8601String(),
              'expiresAt': end.toIso8601String(),
              'timeZone': city.timeZone,
            }),
          )
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 201) throw StateError('Intent rejected');
      if (!mounted || serial != _serial || !widget.auth.signedIn) return;
      _topic.clear();
      _details.clear();
      _area.clear();
      _confirmed = false;
      await _load();
      if (mounted) setState(() => _message = '已提交城市审核；审核通过后才会进入 People 结果。');
    } catch (_) {
      if (mounted && serial == _serial) {
        setState(() => _message = '提交失败。请确认资料已保存为公开，或稍后重试。');
      }
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _withdraw(String id) async {
    final serial = _serial;
    setState(() => _saving = true);
    try {
      final response = await _client
          .post(
            _endpoint('/v1/me/intents/${Uri.encodeComponent(id)}/withdraw'),
            headers: _headers(),
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 204) throw StateError('Withdraw failed');
      if (mounted && serial == _serial) await _load();
    } catch (_) {
      if (mounted && serial == _serial) {
        setState(() => _message = '撤回失败，请重试。');
      }
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  void dispose() {
    ++_serial;
    widget.auth.removeListener(_onAuthChanged);
    _client.close();
    _name.dispose();
    _bio.dispose();
    _topic.dispose();
    _details.dispose();
    _area.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (_apiBase.isEmpty || !widget.auth.signedIn) {
      return const SizedBox.shrink();
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Text(
          'PROFILE & PEOPLE',
          style: TextStyle(fontSize: 12, fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 8),
        const Text('公开资料和人员意图会用于 Agent 的 People 结果。只填写粗略区域，不填写住址。'),
        if (_loading) const LinearProgressIndicator(),
        if (_failed) TextButton(onPressed: _load, child: const Text('读取失败，重试')),
        TextField(
          controller: _name,
          enabled: !_loading && !_saving,
          maxLength: 80,
          decoration: const InputDecoration(labelText: '显示名称'),
        ),
        TextField(
          controller: _bio,
          enabled: !_loading && !_saving,
          maxLength: 500,
          maxLines: 2,
          decoration: const InputDecoration(labelText: '简介（可选）'),
        ),
        SwitchListTile.adaptive(
          contentPadding: EdgeInsets.zero,
          title: const Text('公开个人资料'),
          subtitle: const Text('公开后，任何人都可能看到名称与简介。'),
          value: _public,
          onChanged: _saving || _loading
              ? null
              : (value) => setState(() => _public = value),
        ),
        OutlinedButton(
          onPressed: _saving || _loading ? null : _saveProfile,
          child: const Text('保存个人资料'),
        ),
        const Divider(height: 36),
        const Text(
          '我的人员意图',
          style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
        ),
        for (final item in _intents)
          ListTile(
            contentPadding: EdgeInsets.zero,
            title: Text(item['topic'] as String),
            subtitle: Text(switch (item['state'] as String) {
              'draft' => '待城市审核',
              'active' => '已公开 · 到期前可被发现',
              _ => '已撤回或未通过',
            }),
            trailing: item['state'] == 'draft' || item['state'] == 'active'
                ? TextButton(
                    onPressed: _saving
                        ? null
                        : () => _withdraw(item['id'] as String),
                    child: const Text('撤回'),
                  )
                : null,
          ),
        const Text(
          '提交一个意图',
          style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
        ),
        const Text('将在当前城市的未来 7 天有效；审核通过且个人资料保持公开后，才会出现在 People 结果。'),
        TextField(
          controller: _topic,
          enabled: !_loading && !_saving,
          maxLength: 160,
          decoration: const InputDecoration(labelText: '想和人一起做什么？'),
        ),
        TextField(
          controller: _details,
          enabled: !_loading && !_saving,
          maxLength: 3000,
          maxLines: 2,
          decoration: const InputDecoration(labelText: '补充说明（可选）'),
        ),
        TextField(
          controller: _area,
          enabled: !_loading && !_saving,
          maxLength: 160,
          decoration: const InputDecoration(labelText: '粗略区域，例如 Aberdeen 市中心'),
        ),
        CheckboxListTile(
          contentPadding: EdgeInsets.zero,
          value: _confirmed,
          onChanged: _saving || _loading
              ? null
              : (value) => setState(() => _confirmed = value ?? false),
          title: const Text('我确认提交此公开意图，并同意城市审核'),
        ),
        FilledButton(
          onPressed: _saving || _loading ? null : _submit,
          child: const Text('提交审核'),
        ),
        if (_message != null)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(_message!),
          ),
      ],
    );
  }
}
