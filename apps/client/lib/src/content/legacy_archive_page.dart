import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;
import '../app/birdtie_app_bar.dart';
import 'legacy_archive_api.dart';
import 'legacy_personal_archive_page.dart';
import 'private_reference_targets_page.dart';

/// A personal historical reader. It has no native content or media writer.
class LegacyArchivePage extends StatefulWidget {
  const LegacyArchivePage({
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
  State<LegacyArchivePage> createState() => _LegacyArchivePageState();
}

class _LegacyArchivePageState extends State<LegacyArchivePage> {
  late final LegacyArchiveAPI _api;
  late final Object _binding;
  String? _header;
  int _serial = 0;
  bool _retired = false,
      _loading = true,
      _moreLoading = false,
      _badCursor = false;
  List<LegacyArchiveSummary> _items = const [];
  final _cursors = <String>{};
  String? _next, _error;
  LegacyArchiveSummary? _selected;
  LegacyArchiveDetail? _detail;

  Object get _scope => (
    widget.ownerID(),
    widget.organizationWorkspaceID?.call(),
    widget.sourceKey?.call(),
  );
  bool get _bound =>
      mounted &&
      !_retired &&
      _scope == _binding &&
      widget.sourceCurrent?.call() != false;
  void _redraw() {
    if (!mounted) return;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  void _retire() {
    if (_retired) return;
    _retired = true;
    ++_serial;
    _items = const [];
    _selected = null;
    _detail = null;
    _next = null;
    _loading = _moreLoading = false;
    _error = null;
    _redraw();
  }

  void _observe() {
    if (!mounted || _retired) return;
    if (_scope != _binding || widget.sourceCurrent?.call() == false) {
      _retire();
      return;
    }
    final header = widget.authorizationHeader();
    if (_header != header) {
      _header = header;
      ++_serial;
      _items = const [];
      _detail = null;
      _selected = null;
      _next = null;
      _loading = true;
      _moreLoading = false;
      _error = null;
      _scheduleFirst();
      _redraw();
    }
  }

  void _scheduleFirst() {
    final serial = _serial;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_bound && serial == _serial) unawaited(_first());
    });
  }

  @override
  void initState() {
    super.initState();
    _binding = _scope;
    _header = widget.authorizationHeader();
    _api = LegacyArchiveAPI(
      authorizationHeader: widget.authorizationHeader,
      ownerID: widget.ownerID,
      organizationWorkspaceID: widget.organizationWorkspaceID,
      current: () => _bound,
      sourceKey: widget.sourceKey,
      identityChanges: widget.identityChanges,
      client: widget.client,
      apiBaseUrl: widget.apiBaseUrl,
    );
    widget.identityChanges?.addListener(_observe);
    _scheduleFirst();
  }

  @override
  void didUpdateWidget(LegacyArchivePage old) {
    super.didUpdateWidget(old);
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges?.removeListener(_observe);
      widget.identityChanges?.addListener(_observe);
    }
    if (old.authorizationHeader != widget.authorizationHeader ||
        old.ownerID != widget.ownerID ||
        old.organizationWorkspaceID != widget.organizationWorkspaceID ||
        old.identityChanges != widget.identityChanges ||
        old.sourceCurrent != widget.sourceCurrent ||
        old.sourceKey != widget.sourceKey ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl) {
      _retire();
    } else {
      _observe();
    }
  }

  bool _requestCurrent(int serial, String? header) =>
      _bound && serial == _serial && widget.authorizationHeader() == header;
  void _failed(Object error, {bool paging = false}) {
    _loading = _moreLoading = false;
    _detail = null;
    _error = error is LegacyArchiveFailure
        ? error.toString()
        : error is FormatException
        ? '旧档案资料无法完整核对，请重新读取列表。'
        : '旧档案暂时无法读取，请稍后重试。';
    if (error is LegacyArchiveFailure && error.permission) {
      _items = const [];
      _selected = null;
      _next = null;
    }
    if (paging &&
        (error is FormatException ||
            error is LegacyArchiveFailure &&
                (error.status == 400 || error.status == 409))) {
      _badCursor = true;
      _next = null;
    }
  }

  Future<void> _first() async {
    if (!_bound) return;
    final serial = ++_serial, header = widget.authorizationHeader();
    _loading = true;
    _moreLoading = false;
    _error = null;
    _badCursor = false;
    _selected = null;
    _detail = null;
    _redraw();
    try {
      final page = await _api.page();
      if (!_requestCurrent(serial, header)) return;
      _items = page.items;
      _next = page.nextCursor;
      _cursors.clear();
      _loading = false;
    } catch (e) {
      if (!_requestCurrent(serial, header)) return;
      _failed(e);
      _items = const [];
      _next = null;
    }
    _redraw();
  }

  Future<void> _more(int originalSerial, String cursor) async {
    if (!_bound ||
        originalSerial != _serial ||
        _next != cursor ||
        _loading ||
        _moreLoading ||
        _selected != null)
      return;
    final serial = ++_serial, header = widget.authorizationHeader();
    _moreLoading = true;
    _error = null;
    _redraw();
    try {
      final page = await _api.page(cursor: cursor);
      if (!_requestCurrent(serial, header)) return;
      final ids = _items.map((e) => e.id).toSet();
      if (page.items.any((e) => ids.contains(e.id)) ||
          _cursors.contains(cursor) ||
          page.nextCursor == cursor ||
          page.nextCursor != null && _cursors.contains(page.nextCursor)) {
        throw const FormatException('Repeated archive page');
      }
      _cursors.add(cursor);
      _items = List.unmodifiable([..._items, ...page.items]);
      _next = page.nextCursor;
      _moreLoading = false;
    } catch (e) {
      if (!_requestCurrent(serial, header)) return;
      _failed(e, paging: true);
    }
    _redraw();
  }

  Future<void> _open(LegacyArchiveSummary row, int originalSerial) async {
    if (!_bound ||
        originalSerial != _serial ||
        _loading ||
        _moreLoading ||
        !_items.any((e) => identical(e, row)))
      return;
    final serial = ++_serial, header = widget.authorizationHeader();
    _selected = row;
    _detail = null;
    _loading = true;
    _error = null;
    _redraw();
    try {
      final detail = await _api.detail(row.id, expected: row);
      if (!_requestCurrent(serial, header)) return;
      _detail = detail;
      _loading = false;
    } catch (e) {
      if (!_requestCurrent(serial, header)) return;
      _failed(e);
    }
    _redraw();
  }

  void _backToList() {
    if (!_bound) return;
    ++_serial;
    _selected = null;
    _detail = null;
    _error = null;
    _loading = false;
    _redraw();
  }

  Widget _date(LegacyArchiveSummary row) => Text(
    row.recordedStartDate == null && row.recordedEndDate == null
        ? '原记录日期未提供'
        : '原记录日期：' +
              (row.recordedStartDate ?? '开始日期未提供') +
              (row.recordedEndDate == null ? '' : ' 至 ' + row.recordedEndDate!),
  );
  Widget _section(String title, List<Widget> children) => Padding(
    padding: const EdgeInsets.only(top: 20),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(title, style: Theme.of(context).textTheme.titleMedium),
        const SizedBox(height: 8),
        ...children,
      ],
    ),
  );
  List<Widget> _relationships(LegacyArchiveRelationships r) => [
    if (r.places.isNotEmpty)
      _section('原关联地点', [
        for (final p in r.places)
          Text(
            '原顺序 ' +
                p.position.toString() +
                ' · ' +
                (p.rawName.isEmpty ? '地点名称未记录' : p.rawName),
          ),
      ]),
    if (r.media.isNotEmpty)
      _section('原媒体', [
        Text('共 ' + r.media.length.toString() + ' 项，当前均不可读取。'),
        const Text('档案未恢复媒体文件或使用许可。'),
      ]),
    if (r.mediaLocations.isNotEmpty)
      _section('原媒体位置记录', [
        for (final p in r.mediaLocations)
          Text(
            '原地图标记：' +
                (p.mapEnabled ? '开启' : '关闭') +
                ' · 原关联地点 ' +
                p.sourcePlaceIDs.length.toString() +
                ' 个' +
                '\n原拍摄日期：' +
                (p.capturedDate ?? '未提供'),
          ),
        const Text('历史标记与坐标不会加入当前地图。'),
      ]),
    if (r.thread != null)
      _section('原记录组', [
        Text(
          '原组共 ' +
              r.thread!.entryCount.toString() +
              ' 条记录' +
              (r.thread!.position == null
                  ? ''
                  : ' · 本条原顺序 ' + r.thread!.position.toString()),
        ),
        Text('原创建日期：' + r.thread!.createdAt),
      ]),
    if (r.threadLegs.isNotEmpty)
      _section('原路线段', [
        for (final p in r.threadLegs)
          Text(
            '原顺序 ' +
                p.fromPosition.toString() +
                ' → ' +
                p.toPosition.toString() +
                ' · ' +
                (p.mode.isEmpty ? '方式未记录' : p.mode) +
                '\n原创建日期：' +
                p.createdAt +
                '\n原更新日期：' +
                p.updatedAt,
          ),
      ]),
  ];
  @override
  Widget build(BuildContext context) {
    final serial = _serial;
    final detail = _detail;
    final next = _next;
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('旧内容档案')),
      body: !_bound
          ? const Padding(
              padding: EdgeInsets.all(16),
              child: Text('查看来源已变化，请返回设置并在当前个人身份下重新打开。'),
            )
          : ListView(
              key: const Key('legacy-archive-scroll'),
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
              children: [
                const Text('仅本人可见的历史材料，不会自动成为当前地点、动态、记忆或公开内容。'),
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton(
                    onPressed: _bound
                        ? () => Navigator.of(context).push<void>(
                            MaterialPageRoute(
                              builder: (_) => PrivateReferenceTargetsPage(
                                authorizationHeader: widget.authorizationHeader,
                                ownerID: widget.ownerID,
                                sourceCurrent: () => _bound,
                                identityChanges: widget.identityChanges,
                                client: widget.client,
                                apiBaseUrl: widget.apiBaseUrl,
                              ),
                            ),
                          )
                        : null,
                    child: const Text('我的旧地点图片'),
                  ),
                ),
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton(
                    onPressed: _bound
                        ? () => Navigator.of(context).push<void>(
                            MaterialPageRoute(
                              builder: (_) => LegacyPersonalArchivePage(
                                authorizationHeader: widget.authorizationHeader,
                                ownerID: widget.ownerID,
                                organizationWorkspaceID:
                                    widget.organizationWorkspaceID,
                                identityChanges: widget.identityChanges,
                                sourceCurrent: () => _bound,
                                sourceKey: widget.sourceKey,
                                client: widget.client,
                                apiBaseUrl: widget.apiBaseUrl,
                              ),
                            ),
                          )
                        : null,
                    child: const Text('查看旧收藏和集合'),
                  ),
                ),
                if (_selected != null)
                  Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton(
                      key: const Key('legacy-archive-back-list'),
                      onPressed: _backToList,
                      child: const Text('返回档案列表'),
                    ),
                  ),
                if (_loading)
                  const Padding(
                    padding: EdgeInsets.symmetric(vertical: 24),
                    child: Center(child: CircularProgressIndicator()),
                  ),
                if (_error != null) ...[
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 16),
                    child: Text(
                      _error!,
                      key: const Key('legacy-archive-error'),
                    ),
                  ),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: OutlinedButton(
                      key: const Key('legacy-archive-reload'),
                      onPressed: () {
                        if (_bound && serial == _serial) unawaited(_first());
                      },
                      child: Text(
                        _badCursor || _selected != null ? '重新读取列表' : '重新读取',
                      ),
                    ),
                  ),
                ],
                if (!_loading && _selected == null) ...[
                  if (_items.isEmpty && _error == null)
                    const Padding(
                      padding: EdgeInsets.symmetric(vertical: 24),
                      child: Text('暂无旧内容档案。'),
                    ),
                  if (_items.isNotEmpty)
                    Text('已读取 ' + _items.length.toString() + ' 条'),
                  for (final row in _items)
                    ListTile(
                      key: ValueKey('legacy-archive-row-' + row.id),
                      contentPadding: EdgeInsets.zero,
                      title: Text(row.displayTitle),
                      subtitle: row.deleted
                          ? const Text('仅保留删除记录')
                          : _date(row),
                      onTap: () => unawaited(_open(row, serial)),
                    ),
                  if (next != null)
                    Align(
                      alignment: Alignment.centerLeft,
                      child: OutlinedButton(
                        key: const Key('legacy-archive-more'),
                        onPressed: _moreLoading
                            ? null
                            : () => unawaited(_more(serial, next)),
                        child: Text(_moreLoading ? '正在继续读取…' : '继续读取'),
                      ),
                    ),
                ],
                if (detail != null) ...[
                  const SizedBox(height: 16),
                  Text(
                    detail.summary.displayTitle,
                    key: const Key('legacy-archive-detail-title'),
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                  const SizedBox(height: 8),
                  if (detail.summary.deleted)
                    const Text('原内容已删除。档案只保留删除记录，正文与媒体不会恢复。')
                  else ...[
                    _date(detail.summary),
                    const SizedBox(height: 20),
                    Text(detail.body, key: const Key('legacy-archive-body')),
                    ..._relationships(detail.relationships!),
                  ],
                  const SizedBox(height: 24),
                  Text('归档时间（不是记录发生日期）：' + detail.summary.importedAtRaw),
                ],
              ],
            ),
    );
  }

  @override
  void dispose() {
    _retired = true;
    ++_serial;
    widget.identityChanges?.removeListener(_observe);
    _api.dispose();
    super.dispose();
  }
}
