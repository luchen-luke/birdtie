import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';
import '../config/birdtie_environment.dart';

typedef _PublicIntentBinding = (
  BirdtieAuthController,
  String?,
  String?,
  http.Client,
  String,
  String?,
);

class PublicIntentSection extends StatefulWidget {
  const PublicIntentSection({
    super.key,
    required this.auth,
    required this.city,
    this.client,
    this.apiBaseUrl,
    this.workspaceChanges,
    this.organizationWorkspaceID,
  });

  final BirdtieAuthController auth;
  final PublicCityController city;
  final http.Client? client;
  final String? apiBaseUrl;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;

  @override
  State<PublicIntentSection> createState() => _PublicIntentSectionState();
}

class _PublicIntentSectionState extends State<PublicIntentSection> {
  late http.Client _client;
  late bool _ownsClient;
  late _PublicIntentBinding _binding;
  int _bindingEpoch = 0;
  String get _apiBase => widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  _PublicIntentBinding get _currentBinding => (
    widget.auth,
    widget.auth.accountID,
    widget.auth.authorizationHeader,
    _client,
    _apiBase,
    widget.organizationWorkspaceID?.call(),
  );
  bool get _personalReady =>
      widget.auth.signedIn &&
      widget.auth.accountID?.isNotEmpty == true &&
      widget.auth.authorizationHeader != null &&
      _apiBase.isNotEmpty &&
      widget.organizationWorkspaceID?.call() == null;
  bool _current(_PublicIntentBinding binding, int epoch) =>
      mounted &&
      _personalReady &&
      epoch == _bindingEpoch &&
      binding == _currentBinding;
  static Uri _boundEndpoint(_PublicIntentBinding binding, String path) =>
      Uri.parse('${binding.$5.replaceFirst(RegExp(r'/$'), '')}$path');
  static Map<String, String> _boundHeaders(
    _PublicIntentBinding binding, {
    bool json = false,
  }) => {
    'Authorization': binding.$3!,
    if (json) 'Content-Type': 'application/json',
  };
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
  bool _profileLoaded = false;
  bool _failed = false;
  int _durationDays = 7;
  String _publicMapZone = '';
  int _serial = 0;
  String? _message;

  @override
  void initState() {
    super.initState();
    _client = widget.client ?? http.Client();
    _ownsClient = widget.client == null;
    _binding = _currentBinding;
    widget.auth.addListener(_onBindingChanged);
    widget.workspaceChanges?.addListener(_onBindingChanged);
    if (_personalReady) unawaited(_load());
  }

  void _onBindingChanged() {
    if (_binding == _currentBinding) return;
    _resetBinding();
  }

  void _resetBinding() {
    ++_bindingEpoch;
    ++_serial;
    _binding = _currentBinding;
    _name.clear();
    _bio.clear();
    _topic.clear();
    _details.clear();
    _area.clear();
    setState(() {
      _intents = const [];
      _public = _confirmed = _profileLoaded = false;
      _durationDays = 7;
      _publicMapZone = '';
      _message = null;
      _loading = _saving = _failed = false;
    });
    if (_personalReady) unawaited(_load());
  }

  @override
  void didUpdateWidget(PublicIntentSection old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth) {
      old.auth.removeListener(_onBindingChanged);
      widget.auth.addListener(_onBindingChanged);
    }
    if (old.workspaceChanges != widget.workspaceChanges) {
      old.workspaceChanges?.removeListener(_onBindingChanged);
      widget.workspaceChanges?.addListener(_onBindingChanged);
    }
    if (old.client != widget.client) {
      if (_ownsClient) _client.close();
      _client = widget.client ?? http.Client();
      _ownsClient = widget.client == null;
    }
    if (old.auth != widget.auth ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.workspaceChanges != widget.workspaceChanges ||
        old.organizationWorkspaceID != widget.organizationWorkspaceID ||
        _binding != _currentBinding) {
      _resetBinding();
    }
  }

  Future<void> _load() async {
    if (!_personalReady) return;
    final binding = _currentBinding, epoch = _bindingEpoch;
    final serial = ++_serial;
    bool current() => _current(binding, epoch) && serial == _serial;
    setState(() {
      _loading = true;
      _failed = false;
    });
    try {
      final me = await binding.$4
          .get(
            _boundEndpoint(binding, '/v1/me'),
            headers: _boundHeaders(binding),
          )
          .timeout(const Duration(seconds: 10));
      if (!current()) return;
      if (me.statusCode != 200) throw StateError('Account unavailable');
      final accountID =
          ((jsonDecode(me.body) as Map<String, dynamic>)['data']
                  as Map<String, dynamic>)['id']
              as String;
      if (accountID != binding.$2) throw StateError('Account binding changed');
      final responses = await Future.wait([
        binding.$4.get(
          _boundEndpoint(binding, '/v1/accounts/$accountID/profile'),
          headers: _boundHeaders(binding),
        ),
        binding.$4.get(
          _boundEndpoint(binding, '/v1/me/intents'),
          headers: _boundHeaders(binding),
        ),
      ]).timeout(const Duration(seconds: 10));
      if (!current()) return;
      if (responses.any((response) => response.statusCode != 200)) {
        throw StateError('Profile or intents unavailable');
      }
      final profile =
          (jsonDecode(responses[0].body) as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
      final records =
          (jsonDecode(responses[1].body) as Map<String, dynamic>)['data']
              as List<dynamic>;
      if (profile['accountId'] != null && profile['accountId'] != accountID) {
        throw StateError('Profile binding changed');
      }
      setState(() {
        _name.text = profile['displayName'] as String;
        _bio.text = profile['bio'] as String;
        _public = profile['visibility'] == 'public';
        _intents = [for (final raw in records) raw as Map<String, dynamic>];
        _profileLoaded = true;
      });
    } catch (_) {
      if (current()) setState(() => _failed = true);
    } finally {
      if (current()) setState(() => _loading = false);
    }
  }

  Future<void> _saveProfile() async {
    if (_saving || _loading || !_profileLoaded || !_personalReady) return;
    final binding = _currentBinding, epoch = _bindingEpoch;
    final name = _name.text.trim(), bio = _bio.text.trim(), public = _public;
    setState(() {
      _saving = true;
      _message = null;
    });
    try {
      final response = await binding.$4
          .put(
            _boundEndpoint(binding, '/v1/me/profile'),
            headers: _boundHeaders(binding, json: true),
            body: jsonEncode({
              'displayName': name,
              'bio': bio,
              'visibility': public ? 'public' : 'private',
            }),
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 200) throw StateError('Profile rejected');
      if (!_current(binding, epoch)) return;
      binding.$1.updateProfileDisplayName(name);
      await _load();
      if (_current(binding, epoch)) {
        setState(
          () => _message = public ? '资料已保存。原公开意图仍按有效期展示。' : '资料已设为私密，原公开意图已撤回。',
        );
      }
    } catch (_) {
      if (_current(binding, epoch)) {
        setState(() => _message = '资料保存失败，请检查名称并重试。');
      }
    } finally {
      if (_current(binding, epoch)) setState(() => _saving = false);
    }
  }

  Future<void> _submit() async {
    if (!_personalReady || !_profileLoaded || _loading || _saving) return;
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
    final binding = _currentBinding, epoch = _bindingEpoch;
    setState(() {
      _saving = true;
      _message = null;
    });
    try {
      final start = DateTime.now().toUtc();
      final end = start.add(Duration(days: _durationDays));
      final response = await binding.$4
          .post(
            _boundEndpoint(
              binding,
              '/v1/cities/${Uri.encodeComponent(city.id)}/intents',
            ),
            headers: _boundHeaders(binding, json: true),
            body: jsonEncode({
              'confirmed': true,
              'topic': _topic.text.trim(),
              'details': _details.text.trim(),
              'coarseAreaLabel': _area.text.trim(),
              'publicMapZone': _publicMapZone,
              'availableFrom': start.toIso8601String(),
              'availableUntil': end.toIso8601String(),
              'expiresAt': end.toIso8601String(),
              'timeZone': city.timeZone,
            }),
          )
          .timeout(const Duration(seconds: 12));
      if (!_current(binding, epoch)) return;
      if (response.statusCode == 409) {
        setState(() => _message = '请先保存公开个人资料；每人暂时最多同时发布 3 条有效意图。');
        return;
      }
      if (response.statusCode != 201) throw StateError('Intent rejected');
      _topic.clear();
      _details.clear();
      _area.clear();
      _confirmed = false;
      _publicMapZone = '';
      await _load();
      if (_current(binding, epoch)) {
        setState(() => _message = '意图已公开；在有效期内可出现在用户搜索结果中。');
      }
    } catch (_) {
      if (_current(binding, epoch)) {
        setState(() => _message = '提交失败。请确认资料已保存为公开，或稍后重试。');
      }
    } finally {
      if (_current(binding, epoch)) setState(() => _saving = false);
    }
  }

  Future<void> _withdraw(String id) async {
    if (!_personalReady || !_profileLoaded || _loading || _saving) return;
    final binding = _currentBinding, epoch = _bindingEpoch;
    setState(() => _saving = true);
    try {
      final response = await binding.$4
          .post(
            _boundEndpoint(
              binding,
              '/v1/me/intents/${Uri.encodeComponent(id)}/withdraw',
            ),
            headers: _boundHeaders(binding),
          )
          .timeout(const Duration(seconds: 10));
      if (response.statusCode != 204) throw StateError('Withdraw failed');
      if (_current(binding, epoch)) await _load();
    } catch (_) {
      if (_current(binding, epoch)) {
        setState(() => _message = '撤回失败，请重试。');
      }
    } finally {
      if (_current(binding, epoch)) setState(() => _saving = false);
    }
  }

  @override
  void dispose() {
    ++_serial;
    ++_bindingEpoch;
    widget.auth.removeListener(_onBindingChanged);
    widget.workspaceChanges?.removeListener(_onBindingChanged);
    if (_ownsClient) _client.close();
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
    if (widget.organizationWorkspaceID?.call() != null) {
      return const Text('请切换到个人身份后管理个人资料和人员意图。');
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Text(
          '个人资料与社交',
          style: TextStyle(fontSize: 12, fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 8),
        const Text('公开资料和人员意图会用于智能体的用户搜索结果。只填写大致区域，不要填写住址。'),
        if (_loading) const LinearProgressIndicator(),
        if (_failed) TextButton(onPressed: _load, child: const Text('读取失败，重试')),
        TextField(
          controller: _name,
          enabled: _profileLoaded && !_loading && !_saving,
          maxLength: 80,
          decoration: const InputDecoration(labelText: '显示名称'),
        ),
        TextField(
          controller: _bio,
          enabled: _profileLoaded && !_loading && !_saving,
          maxLength: 500,
          maxLines: 2,
          decoration: const InputDecoration(labelText: '简介（可选）'),
        ),
        SwitchListTile.adaptive(
          contentPadding: EdgeInsets.zero,
          title: const Text('公开个人资料'),
          subtitle: const Text('公开后，任何人都可能看到名称与简介。'),
          value: _public,
          onChanged: !_profileLoaded || _saving || _loading
              ? null
              : (value) => setState(() => _public = value),
        ),
        OutlinedButton(
          onPressed: !_profileLoaded || _saving || _loading
              ? null
              : _saveProfile,
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
            subtitle: Text(
              '${switch (item['state'] as String) {
                'draft' => '未公开的旧提交',
                'active' => '已公开 · 到期前可被发现',
                _ => '已撤回或未通过',
              }}${(item['publicMapZone'] as String? ?? '').isEmpty ? '' : ' · 已主动公开粗略地图区域'}',
            ),
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
        const Text('从提交时起在当前城市有效；个人资料保持公开时，可出现在用户搜索结果中。'),
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
        const Text(
          '地图区域标记可选；仅展示城市级大范围的示意锚点，不代表你的位置。',
          style: TextStyle(color: Color(0xFF747B73), fontSize: 12),
        ),
        DropdownButtonFormField<String>(
          key: ValueKey(_publicMapZone),
          initialValue: _publicMapZone,
          decoration: const InputDecoration(labelText: '公开地图区域（可选）'),
          items: const [
            DropdownMenuItem(value: '', child: Text('不在地图上显示')),
            DropdownMenuItem(value: 'city_centre', child: Text('城市中心范围')),
            DropdownMenuItem(value: 'north', child: Text('城市北部范围')),
            DropdownMenuItem(value: 'south', child: Text('城市南部范围')),
            DropdownMenuItem(value: 'east', child: Text('城市东部范围')),
            DropdownMenuItem(value: 'west', child: Text('城市西部范围')),
          ],
          onChanged:
              _saving || _loading || widget.city.selectedCity?.map == null
              ? null
              : (value) => setState(() => _publicMapZone = value ?? ''),
        ),
        Row(
          children: [
            const Text('有效期：'),
            DropdownButton<int>(
              value: _durationDays,
              items: const [
                DropdownMenuItem(value: 1, child: Text('1 天')),
                DropdownMenuItem(value: 3, child: Text('3 天')),
                DropdownMenuItem(value: 7, child: Text('7 天')),
                DropdownMenuItem(value: 14, child: Text('14 天')),
                DropdownMenuItem(value: 30, child: Text('30 天')),
              ],
              onChanged: _saving || _loading
                  ? null
                  : (value) => setState(() => _durationDays = value ?? 7),
            ),
          ],
        ),
        const Text(
          '每人暂时最多同时发布 3 条有效意图。',
          style: TextStyle(color: Color(0xFF747B73), fontSize: 12),
        ),
        CheckboxListTile(
          contentPadding: EdgeInsets.zero,
          value: _confirmed,
          onChanged: _saving || _loading
              ? null
              : (value) => setState(() => _confirmed = value ?? false),
          title: const Text('我确认公开此意图'),
        ),
        FilledButton(
          onPressed: _saving || _loading ? null : _submit,
          child: const Text('公开意图'),
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
