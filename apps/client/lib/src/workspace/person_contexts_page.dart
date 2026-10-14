import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:uuid/uuid.dart';
import '../app/birdtie_app_bar.dart';
import '../auth/birdtie_auth_controller.dart';
import '../config/birdtie_environment.dart';
import 'human_causal_control.dart';
import 'human_causal_pending_store.dart';
import 'human_causal_operation_controller.dart';

class PersonContextsPage extends StatefulWidget {
  const PersonContextsPage({
    super.key,
    required this.auth,
    this.apiBaseUrl,
    this.client,
    this.current,
    this.identityChanges,
    this.pendingStore,
  });
  final BirdtieAuthController auth;
  final String? apiBaseUrl;
  final http.Client? client;
  final bool Function()? current;
  final Listenable? identityChanges;
  final HumanCausalPendingStore? pendingStore;
  @override
  State<PersonContextsPage> createState() => _PersonContextsPageState();
}

class _PersonContextsPageState extends State<PersonContextsPage> {
  late http.Client _client;
  late bool _ownsClient;
  late HumanCausalOperationController _data;
  final _source = TextEditingController();
  List<Map<String, dynamic>> _cities = const [];
  String _mode = 'current_city';
  String? _cityID, _formError, _citiesError;
  int _read = 0;
  void _bind() {
    _client = widget.client ?? http.Client();
    _ownsClient = widget.client == null;
    _data = HumanCausalOperationController(
      api: HumanCausalApi(
        client: _client,
        baseUrl: widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
        authorizationHeader: () => widget.auth.authorizationHeader,
      ),
      family: HumanCausalFamily.context,
      ownerID: () => widget.auth.accountID,
      identityChanges: Listenable.merge([widget.auth, widget.identityChanges]),
      sourceCurrent: () => mounted && (widget.current?.call() ?? true),
      targetKey: () => (widget.auth, widget.client, widget.apiBaseUrl),
      pendingStore: widget.pendingStore,
    );
    _data.addListener(_changed);
    final c = _data;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && identical(c, _data)) unawaited(_load());
    });
  }

  void _changed() {
    if (!mounted) return;
    if (!_data.current) {
      _source.clear();
      _cities = const [];
      _cityID = null;
      ++_read;
    }
    setState(() {});
  }

  @override
  void initState() {
    super.initState();
    _bind();
  }

  @override
  void didUpdateWidget(PersonContextsPage old) {
    super.didUpdateWidget(old);
    if (old.auth != widget.auth ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.identityChanges != widget.identityChanges ||
        old.pendingStore != widget.pendingStore) {
      _data.removeListener(_changed);
      _data.dispose();
      if (_ownsClient) _client.close();
      _source.clear();
      _cities = const [];
      _cityID = null;
      ++_read;
      _bind();
    }
  }

  @override
  void dispose() {
    ++_read;
    _data.removeListener(_changed);
    _data.dispose();
    _source.dispose();
    if (_ownsClient) _client.close();
    super.dispose();
  }

  Future<void> _load() async {
    final c = _data;
    await c.initialize();
    if (!mounted ||
        !identical(c, _data) ||
        !c.current ||
        !humanControlID(c.owner))
      return;
    final guard = c.capture(), serial = ++_read;
    try {
      final raw = await c.api.json('GET', '/v1/cities', current: guard);
      if (!mounted || !identical(c, _data) || !guard() || serial != _read)
        return;
      if (raw is! List) throw const FormatException();
      final cities = <Map<String, dynamic>>[];
      for (final row in raw) {
        if (row is! Map<String, dynamic> ||
            !humanControlText(row['id'], 1, 160) ||
            row['name'] is! String)
          throw const FormatException();
        cities.add(Map.unmodifiable(row));
      }
      if (cities.map((r) => r['id']).toSet().length != cities.length)
        throw const FormatException();
      setState(() {
        _cities = List.unmodifiable(cities);
        _citiesError = null;
        if (!_cities.any((r) => r['id'] == _cityID))
          _cityID = _cities.isEmpty ? null : _cities.first['id'] as String;
      });
    } catch (_) {
      if (mounted && identical(c, _data) && guard() && serial == _read)
        setState(() {
          _cities = const [];
          _cityID = null;
          _citiesError = '城市列表暂不可读，请重新读取后选择。';
        });
    }
  }

  Future<void> _save() async {
    final c = _data, s = c.snapshot;
    if (!c.canStart || s is! HumanContextSnapshot) return;
    final city = _mode.endsWith('_city'),
        source = city ? _cityID : _source.text.trim();
    if (source == null || source.isEmpty) {
      setState(() => _formError = city ? '请先选择城市。' : '请填写名称。');
      return;
    }
    final (type, relation) = switch (_mode) {
      'current_city' => ('CITY', 'current'),
      'past_city' => ('CITY', 'past'),
      'destination_city' => ('CITY', 'destination'),
      'past_institution' => ('INSTITUTION', 'past'),
      _ => ('ONLINE', 'interest'),
    };
    final label = city
        ? (_cities.where((r) => r['id'] == source).firstOrNull?['name']
              as String?)
        : source;
    final input = HumanContextInput(
      operationID: const Uuid().v4(),
      action: 'DECLARE',
      expectedVersion: s.version,
      relation: relation,
      contextType: type,
      sourceKey: source,
      localLabel: label,
    );
    if (!input.valid) {
      setState(() => _formError = '名称需1–160字，不能包含换行。');
      return;
    }
    final r = await c.checkAndConfirm(context, input);
    if (mounted && identical(c, _data) && c.current && r?.status == 'SUCCEEDED')
      setState(() {
        _source.clear();
        _formError = null;
      });
  }

  String _relation(String? relation) => switch (relation) {
    'current' => '所在城市',
    'past' => '曾生活或就读',
    'destination' => '想去的地方',
    'home' => '家乡',
    'affiliation' => '学校关联',
    'interest' => '兴趣',
    _ => '已保存',
  };
  @override
  Widget build(BuildContext context) {
    final c = _data, s = c.snapshot, e = c.epoch;
    final current = c.current && humanControlID(c.owner);
    bool frame() =>
        mounted &&
        identical(c, _data) &&
        c.current &&
        e == c.epoch &&
        identical(c.snapshot, s) &&
        s?.current == true;
    final rows = current && s is HumanContextSnapshot
        ? s.items
        : const <Map<String, dynamic>>[];
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('我的城市与兴趣')),
      body: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          if (!c.current)
            Text(c.error ?? '当前个人身份或来源已变化。')
          else ...[
            HumanCausalOperationPanel(controller: c),
            if (current && s is HumanContextSnapshot) ...[
              const Text('仅自己可见'),
              const SizedBox(height: 20),
              if (rows.isEmpty)
                const Text('暂无已保存的城市或兴趣。')
              else
                Text('已保存', style: Theme.of(context).textTheme.titleMedium),
              for (final row in rows)
                ListTile(
                  title: Text(row['label'] as String),
                  subtitle: Text(_relation(row['relation'] as String)),
                  trailing: IconButton(
                    tooltip: '移除记录',
                    onPressed: c.canStart
                        ? () {
                            if (frame())
                              unawaited(
                                c.checkAndConfirm(
                                  context,
                                  HumanContextInput(
                                    operationID: const Uuid().v4(),
                                    action: 'REMOVE',
                                    expectedVersion: s.version,
                                    relation: row['relation'] as String,
                                    contextID: row['contextId'] as String,
                                    localLabel: row['label'] as String,
                                  ),
                                ),
                              );
                          }
                        : null,
                    icon: const Icon(Icons.close),
                  ),
                ),
              const Divider(height: 32),
              Text('添加城市或兴趣', style: Theme.of(context).textTheme.titleMedium),
              DropdownButtonFormField<String>(
                key: const Key('context-type'),
                initialValue: _mode,
                decoration: const InputDecoration(labelText: '类型'),
                items: const [
                  DropdownMenuItem(value: 'current_city', child: Text('所在城市')),
                  DropdownMenuItem(value: 'past_city', child: Text('过去生活的城市')),
                  DropdownMenuItem(
                    value: 'destination_city',
                    child: Text('想去的城市'),
                  ),
                  DropdownMenuItem(
                    value: 'past_institution',
                    child: Text('过去就读的学校'),
                  ),
                  DropdownMenuItem(value: 'online', child: Text('线上兴趣')),
                ],
                onChanged: !c.busy && c.pending == null
                    ? (v) {
                        if (frame() && v != null) {
                          c.invalidatePreview();
                          setState(() => _mode = v);
                        }
                      }
                    : null,
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
                          (city['name'] as String).isEmpty
                              ? '城市名称暂不可用'
                              : city['name'] as String,
                        ),
                      ),
                  ],
                  onChanged: !c.busy && c.pending == null
                      ? (v) {
                          if (frame()) {
                            c.invalidatePreview();
                            setState(() => _cityID = v);
                          }
                        }
                      : null,
                )
              else
                TextField(
                  key: const Key('context-source'),
                  controller: _source,
                  enabled: !c.busy && c.pending == null,
                  maxLength: 160,
                  decoration: InputDecoration(
                    labelText: _mode == 'past_institution' ? '学校名称' : '兴趣名称',
                  ),
                  onChanged: (_) => c.invalidatePreview(),
                ),
              if (_citiesError != null) Text(_citiesError!),
              if (_formError != null) Text(_formError!),
              const SizedBox(height: 12),
              FilledButton(
                key: const Key('context-check'),
                onPressed: c.canStart
                    ? () {
                        if (frame()) unawaited(_save());
                      }
                    : null,
                child: const Text('检查保存'),
              ),
              if (_citiesError != null)
                TextButton(
                  onPressed: !c.busy ? () => unawaited(_load()) : null,
                  child: const Text('重新读取城市'),
                ),
            ] else if (!c.busy)
              const Text('当前本人生活资料暂不可读，不能开始新的修改。'),
          ],
        ],
      ),
    );
  }
}
