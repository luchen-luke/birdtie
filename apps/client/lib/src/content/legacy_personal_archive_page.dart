import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../app/birdtie_app_bar.dart';
import '../config/birdtie_environment.dart';
import 'legacy_personal_archive_api.dart';

class LegacyPersonalArchivePage extends StatefulWidget {
  const LegacyPersonalArchivePage({
    super.key,
    required this.authorizationHeader,
    required this.ownerID,
    this.organizationWorkspaceID,
    this.identityChanges,
    this.sourceCurrent,
    this.sourceKey,
    this.client,
    this.apiBaseUrl,
  });
  final String? Function() authorizationHeader, ownerID;
  final String? Function()? organizationWorkspaceID;
  final Listenable? identityChanges;
  final bool Function()? sourceCurrent;
  final Object? Function()? sourceKey;
  final http.Client? client;
  final String? apiBaseUrl;
  @override
  State<LegacyPersonalArchivePage> createState() => _PersonalState();
}

class _PersonalState extends State<LegacyPersonalArchivePage> {
  late final LegacyPersonalArchiveAPI _api;
  late final Object _binding;
  bool _retired = false, _busy = false;
  int _serial = 0;
  String _kind = 'journey';
  String? _header, _next, _error;
  List<LegacyPersonalSummary> _items = [];
  LegacyPersonalDetail? _detail;
  Object get _scope => (
    widget.ownerID(),
    widget.organizationWorkspaceID?.call(),
    widget.sourceKey?.call(),
  );
  bool get _current =>
      mounted &&
      !_retired &&
      _scope == _binding &&
      widget.sourceCurrent?.call() != false &&
      widget.organizationWorkspaceID?.call() == null;
  @override
  void initState() {
    super.initState();
    _binding = _scope;
    _header = widget.authorizationHeader();
    _api = LegacyPersonalArchiveAPI(
      authorizationHeader: widget.authorizationHeader,
      ownerID: widget.ownerID,
      current: () => _current,
      baseUrl: widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
      client: widget.client,
    );
    widget.identityChanges?.addListener(_changed);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_current) _load();
    });
  }

  void _changed() {
    if (!_current) {
      _retired = true;
      ++_serial;
      _items = [];
      _detail = null;
      _next = null;
      _busy = false;
    } else if (_header != widget.authorizationHeader()) {
      _header = widget.authorizationHeader();
      ++_serial;
      _items = [];
      _detail = null;
      _next = null;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (_current) _load();
      });
    }
    if (mounted) {
      if (SchedulerBinding.instance.schedulerPhase ==
          SchedulerPhase.persistentCallbacks) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted) setState(() {});
        });
      } else {
        setState(() {});
      }
    }
  }

  Future<void> _load({bool more = false}) async {
    if (!_current || _busy) return;
    final serial = ++_serial, kind = _kind, cursor = more ? _next : null;
    setState(() {
      _busy = true;
      _error = null;
      _detail = null;
      if (!more) {
        _items = [];
        _next = null;
      }
    });
    try {
      final p = await _api.page(kind, cursor: cursor);
      if (!_current || serial != _serial || kind != _kind) return;
      final old = _items.map((v) => v.id).toSet();
      if (more && p.items.any((v) => old.contains(v.id)))
        throw const FormatException('分页来源变化，请重新读取。');
      setState(() {
        _items = [if (more) ..._items, ...p.items];
        _next = p.next;
      });
    } catch (e) {
      if (_current && serial == _serial)
        setState(() => _error = '旧资料暂不可读取，请重新核实。');
    } finally {
      if (_current && serial == _serial) setState(() => _busy = false);
    }
  }

  Future<void> _read(LegacyPersonalSummary s) async {
    if (!_current || _busy) return;
    final serial = ++_serial;
    setState(() {
      _busy = true;
      _detail = null;
      _error = null;
    });
    try {
      final d = await _api.detail(s.id);
      if (_current && serial == _serial) setState(() => _detail = d);
    } catch (_) {
      if (_current && serial == _serial)
        setState(() => _error = '这条旧资料当前不可读取。');
    } finally {
      if (_current && serial == _serial) setState(() => _busy = false);
    }
  }

  List<Widget> _content(LegacyPersonalDetail d) {
    if (d.summary.deleted) return [const Text('原记录已删除，仅保留删除记录。')];
    final p = d.payload!, meta = p['metadata'] as Map<String, dynamic>?;
    final out = <Widget>[
      Text(
        d.summary.title.isEmpty ? '旧收藏记录' : d.summary.title,
        style: Theme.of(context).textTheme.titleLarge,
      ),
    ];
    for (final key in [
      'sourceCityLabel',
      'sourceDateLabel',
      'sourceStartDate',
      'sourceSummary',
      'sourceScopeLabel',
    ]) {
      final v = meta?[key];
      if (v is String && v.isNotEmpty)
        out.add(
          Padding(padding: const EdgeInsets.only(top: 8), child: Text(v)),
        );
    }
    const sections = {
      'stops': '原行程站点',
      'arrangements': '原安排',
      'notes': '原备注',
      'entries': '原主题条目',
      'sources': '原收藏来源',
    };
    for (final entry in sections.entries) {
      final rows = p[entry.key];
      if (rows is! List || rows.isEmpty) continue;
      out.add(
        Padding(
          padding: const EdgeInsets.only(top: 20, bottom: 8),
          child: Text(
            entry.value,
            style: Theme.of(context).textTheme.titleMedium,
          ),
        ),
      );
      final ordered = [...rows]
        ..sort(
          (a, b) => ((a['position'] as num?) ?? 0).compareTo(
            (b['position'] as num?) ?? 0,
          ),
        );
      for (final r in ordered) {
        final texts = [
          for (final k in [
            'sourceName',
            'sourceSection',
            'sourceBody',
            'sourceTimeLabel',
            'sourceDepartureName',
            'sourceArrivalName',
            'sourceServiceNumber',
            'sourcePrivateNote',
            'sourceText',
            'sourceCreatorNote',
            'sourceTitleSnapshot',
          ])
            if (r[k] is String && (r[k] as String).isNotEmpty) r[k] as String,
        ];
        out.add(
          Padding(
            padding: const EdgeInsets.only(bottom: 12),
            child: Text(texts.isEmpty ? '原来源记录' : texts.join('\n')),
          ),
        );
      }
    }
    if (p['media'] is List && (p['media'] as List).isNotEmpty)
      out.add(const Text('旧媒体仍待核验，本页保留关联记录。'));
    if (d.summary.kind == 'saved_place')
      out.add(const Text('保留原收藏和来源，地点尚未适配为当前可访问地点。'));
    return out;
  }

  @override
  void dispose() {
    ++_serial;
    widget.identityChanges?.removeListener(_changed);
    _api.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: birdtieAppBar(context, title: const Text('旧收藏和集合档案')),
    body: SafeArea(
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          if (!_current)
            const Text('身份或来源已变化，请从当前个人身份重新打开。')
          else ...[
            const Text('本人历史资料以私人只读档案保留，原删除记录不会恢复为内容。'),
            Wrap(
              spacing: 8,
              children: [
                for (final e in const {
                  'journey': '行程',
                  'theme_map': '主题地图',
                  'saved_place': '收藏',
                }.entries)
                  ChoiceChip(
                    label: Text(e.value),
                    selected: _kind == e.key,
                    onSelected: _busy
                        ? null
                        : (v) {
                            if (v) {
                              setState(() => _kind = e.key);
                              _load();
                            }
                          },
                  ),
              ],
            ),
            if (_busy) const LinearProgressIndicator(),
            if (_error != null)
              Semantics(liveRegion: true, child: Text(_error!)),
            if (_detail case final d?) ...[
              TextButton(
                onPressed: _busy ? null : () => setState(() => _detail = null),
                child: const Text('返回列表'),
              ),
              ..._content(d),
            ] else ...[
              if (!_busy && _items.isEmpty && _error == null)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 24),
                  child: Text('当前账号没有已绑定的这类旧资料。'),
                ),
              for (final s in _items)
                ListTile(
                  title: Text(
                    s.deleted
                        ? '已删除的原记录'
                        : s.title.isEmpty
                        ? '旧收藏记录'
                        : s.title,
                  ),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: _busy ? null : () => _read(s),
                ),
              if (_next != null)
                TextButton(
                  onPressed: _busy ? null : () => _load(more: true),
                  child: const Text('加载更多'),
                ),
            ],
            TextButton(
              onPressed: _busy ? null : () => _load(),
              child: const Text('重新读取'),
            ),
          ],
        ],
      ),
    ),
  );
}
