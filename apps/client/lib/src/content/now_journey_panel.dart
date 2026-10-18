import 'dart:async';
import 'package:flutter/material.dart';
import 'package:uuid/uuid.dart';
import '../workspace/now_ui_tokens.dart';
import 'now_journey_controller.dart';
import 'personal_collection_controller.dart';
import 'collection_publication_controller.dart';
import 'journey_detail.dart';
import 'journey_detail_editor.dart';

/// The Now host owns the map and composer. This panel owns no text field.
class NowJourneyPanel extends StatefulWidget {
  const NowJourneyPanel({
    super.key,
    required this.controller,
    required this.onClose,
    required this.onEditField,
    required this.onOpenMoment,
    required this.onViewPlan,
    required this.onPublish,
  });
  final NowJourneyController controller;
  final VoidCallback onClose;
  final ValueChanged<NowJourneyFieldEdit> onEditField;
  final void Function(String momentID, bool Function() current) onOpenMoment;
  final ValueChanged<PersonalCollectionController> onViewPlan, onPublish;
  @override
  State<NowJourneyPanel> createState() => _NowJourneyPanelState();
}

class _NowJourneyPanelState extends State<NowJourneyPanel> {
  bool _visibleTab = false;
  int _epoch = 0;
  NowJourneyController get c => widget.controller;
  @override
  void initState() {
    super.initState();
    if (c.mine.items.isEmpty && !c.mine.loading) unawaited(c.load());
  }

  @override
  void dispose() {
    ++_epoch;
    super.dispose();
  }

  bool _editing(PersonalCollectionController source) =>
      mounted &&
      c.current &&
      identical(c.selectedMine, source) &&
      source.current &&
      source.editable;
  void _field(
    PersonalCollectionController source,
    String key,
    String label,
    String value,
    VoidCallback Function(String) change,
  ) {
    final record = source.editing;
    if (record == null || !_editing(source)) return;
    final details = source.draftJourneyDetails,
        entries = source.draftEntries,
        title = source.title,
        summary = source.summary;
    bool current() =>
        _editing(source) &&
        identical(source.editing, record) &&
        identical(source.draftJourneyDetails, details) &&
        identical(source.draftEntries, entries) &&
        source.title == title &&
        source.summary == summary;
    widget.onEditField(
      NowJourneyFieldEdit(
        journeyID: record.id,
        fieldID: const Uuid().v5(
          '6ba7b811-9dad-11d1-80b4-00c04fd430c8',
          'birdtie:journey:${record.id}:$key',
        ),
        label: label,
        value: value,
        current: current,
        apply: (text) {
          if (!current()) return false;
          try {
            change(text)();
            return true;
          } catch (_) {
            return false;
          }
        },
      ),
    );
  }

  void _rowField(
    PersonalCollectionController source,
    JourneyEntry row,
    String field,
    String label,
  ) {
    final details = source.draftJourneyDetails!;
    _field(
      source,
      '${row.id}:$field',
      label,
      row.text(field),
      (text) => () {
        final changed = row.changed({field: text});
        source.setJourneyDetails(
          details.changed(
            itinerary: [
              for (final e in details.itinerary) e.id == row.id ? changed : e,
            ],
          ),
        );
      },
    );
  }

  void _move(PersonalCollectionController source, int index, int delta) {
    final d = source.draftJourneyDetails;
    if (d == null ||
        !_editing(source) ||
        index + delta < 0 ||
        index + delta >= d.itinerary.length)
      return;
    final rows = [...d.itinerary], row = rows.removeAt(index);
    rows.insert(index + delta, row);
    source.setJourneyDetails(d.changed(itinerary: rows));
  }

  Future<void> _save(PersonalCollectionController source) async {
    if (!_editing(source)) return;
    final epoch = _epoch;
    final preview = await source.prepareControl('UPDATE');
    bool current() =>
        mounted &&
        epoch == _epoch &&
        c.current &&
        identical(c.selectedMine, source) &&
        source.controlPreviewCurrent(preview!);
    if (preview == null || !current()) return;
    final tick = ValueNotifier(0);
    final timer = Timer(
      preview.expiresAt.difference(DateTime.now().toUtc()),
      () => tick.value++,
    );
    final body = preview.input.input!;
    bool approved = false;
    try {
      final route = DialogRoute<bool>(
        context: context,
        builder: (inner) => ListenableBuilder(
          listenable: Listenable.merge([source, c, tick]),
          builder: (inner, _) => AlertDialog(
            title: const Text('检查旅程修改'),
            content: SingleChildScrollView(
              child: current()
                  ? Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text('仅自己可见 · 原版本 ${preview.input.expectedRevision}'),
                        Text(body.title),
                        if (body.summary.isNotEmpty) Text(body.summary),
                        if (body.journeyDetails case final d?) ...[
                          Text('${d.startDate} — ${d.endDate} ${d.dateLabel}'),
                          for (final row in d.itinerary)
                            Text(JourneyDetailEditor.reviewText(row, d)),
                        ],
                        for (var i = 0; i < body.entries.length; i++)
                          Text('${i + 1}. ${body.entries[i].note}'),
                        const Text('这次保存不会发布或更新公开版本。'),
                      ],
                    )
                  : const Text('当前来源或草稿已变化，请重新检查。'),
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(inner, false),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: current() ? () => Navigator.pop(inner, true) : null,
                child: const Text('确认保存修改'),
              ),
            ],
          ),
        ),
      );
      approved = await Navigator.of(context).push(route) == true;
      await route.completed;
    } finally {
      timer.cancel();
      tick.dispose();
    }
    if (approved && current()) {
      await source.commitControl(preview);
    } else {
      source.cancelControlPreview(preview);
    }
  }

  Widget _mine(PersonalCollectionController source) {
    final record = source.editing, d = source.draftJourneyDetails;
    if (record == null) return const Center(child: Text('旅程正在读取。'));
    return ListView(
      key: PageStorageKey('now-journey-edit:${record.id}'),
      padding: const EdgeInsets.all(16),
      children: [
        Text(
          source.title.isEmpty ? '未命名旅程' : source.title,
          style: NowUiTokens.of(context).sheetTitle,
        ),
        Text(
          '我的计划 · 仅自己可见 · ${record.journeyLifecycle?.label ?? '日期待确定'}',
          style: NowUiTokens.of(context).metadata,
        ),
        if (record.publishedSnapshot case final snapshot?)
          Text('另有已发布版本 ${snapshot.publicationRevision}；本页编辑不会自动更改它。'),
        Wrap(
          spacing: 8,
          children: [
            TextButton(
              onPressed: _editing(source)
                  ? () => _field(
                      source,
                      'title',
                      '旅程名称',
                      source.title,
                      (text) =>
                          () => source.setText(title: text),
                    )
                  : null,
              child: const Text('修改名称'),
            ),
            TextButton(
              onPressed: _editing(source)
                  ? () => _field(
                      source,
                      'summary',
                      '旅程说明',
                      source.summary,
                      (text) =>
                          () => source.setText(summary: text),
                    )
                  : null,
              child: const Text('修改说明'),
            ),
            TextButton(
              onPressed: source.current && !source.dirty
                  ? () => widget.onViewPlan(source)
                  : null,
              child: const Text('在地图查看已保存计划'),
            ),
            TextButton(
              onPressed: source.publicationVersionCurrent && !source.dirty
                  ? () => widget.onPublish(source)
                  : null,
              child: const Text('检查公开版本'),
            ),
          ],
        ),
        if (source.summary.isNotEmpty) Text(source.summary),
        if (d != null) ...[
          ListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('计划日期'),
            subtitle: Text(
              '${d.startDate.isEmpty ? '未确定' : d.startDate} — ${d.endDate.isEmpty ? '未确定' : d.endDate} ${d.dateLabel}',
            ),
            trailing: PopupMenuButton<String>(
              tooltip: '编辑计划日期',
              enabled: _editing(source),
              onSelected: (field) => _field(
                source,
                field,
                field == 'startDate' ? '开始日期（YYYY-MM-DD）' : '结束日期（YYYY-MM-DD）',
                field == 'startDate' ? d.startDate : d.endDate,
                (text) =>
                    () => source.setJourneyDetails(
                      field == 'startDate'
                          ? d.changed(startDate: text.trim())
                          : d.changed(endDate: text.trim()),
                    ),
              ),
              itemBuilder: (_) => const [
                PopupMenuItem(value: 'startDate', child: Text('开始日期')),
                PopupMenuItem(value: 'endDate', child: Text('结束日期')),
              ],
            ),
          ),
          for (var i = 0; i < d.itinerary.length; i++)
            _entry(source, d.itinerary[i], i),
          if (d.itinerary.isEmpty) const Text('还没有安排，可从当前真实地点继续添加。'),
          TextButton.icon(
            onPressed: _editing(source)
                ? () => _field(
                    source,
                    'place-search',
                    '搜索要加入旅程的地点',
                    '',
                    (text) =>
                        () => unawaited(source.searchPlaces(text)),
                  )
                : null,
            icon: const Icon(Icons.add_location_alt_outlined),
            label: const Text('添加地点'),
          ),
          if (source.searching) const LinearProgressIndicator(),
          if (source.searchError != null) Text(source.searchError!),
          for (final place in source.places)
            ListTile(
              title: Text(place.name),
              trailing: const Icon(Icons.add),
              onTap: !_editing(source)
                  ? null
                  : () {
                      final current = source.draftJourneyDetails!;
                      if (current.itinerary.length >= 500) return;
                      final row = JourneyEntry.decode({
                        'id': const Uuid().v4(),
                        'entryType': 'STOP',
                        'position': current.itinerary.length + 1,
                        for (final key in journeyTextFields) key: '',
                        for (final key in journeyDayFields) key: null,
                        'title': place.name,
                        'placeId': place.id,
                        'cityId': source.cityID,
                      }, current.itinerary.length + 1);
                      source.setJourneyDetails(
                        current.changed(itinerary: [...current.itinerary, row]),
                      );
                    },
            ),
        ] else ...[
          for (var i = 0; i < source.draftEntries.length; i++)
            ListTile(
              title: Text('${i + 1}. ${source.draftEntries[i].placeName}'),
              subtitle: Text(source.draftEntries[i].note),
              trailing: PopupMenuButton<String>(
                tooltip: '调整地点',
                enabled: _editing(source),
                onSelected: (value) {
                  if (!_editing(source)) return;
                  if (value == 'remove')
                    source.removeEntry(i);
                  else
                    source.moveEntry(i, value == 'up' ? -1 : 1);
                },
                itemBuilder: (_) => [
                  if (i > 0)
                    const PopupMenuItem(value: 'up', child: Text('上移')),
                  if (i + 1 < source.draftEntries.length)
                    const PopupMenuItem(value: 'down', child: Text('下移')),
                  const PopupMenuItem(value: 'remove', child: Text('从私人草稿移除')),
                ],
              ),
              onTap: _editing(source)
                  ? () => _field(
                      source,
                      'place-note:${source.draftEntries[i].placeID}',
                      '地点备注',
                      source.draftEntries[i].note,
                      (text) =>
                          () => source.editEntry(i, text),
                    )
                  : null,
            ),
          TextButton.icon(
            onPressed: _editing(source)
                ? () => _field(
                    source,
                    'place-search',
                    '搜索要加入旅程的地点',
                    '',
                    (text) =>
                        () => unawaited(source.searchPlaces(text)),
                  )
                : null,
            icon: const Icon(Icons.add_location_alt_outlined),
            label: const Text('添加地点'),
          ),
          if (source.searching) const LinearProgressIndicator(),
          if (source.searchError != null) Text(source.searchError!),
          for (final place in source.places)
            ListTile(
              title: Text(place.name),
              trailing: const Icon(Icons.add),
              onTap: _editing(source) ? () => source.addPlace(place) : null,
            ),
        ],
        if (source.error != null)
          Semantics(liveRegion: true, child: Text(source.error!)),
        if (source.controlReadFailed || !source.controlReady)
          TextButton(
            onPressed: () => unawaited(source.loadControlPending()),
            child: const Text('读取原保存记录'),
          ),
        for (final pending in source.controlPending)
          TextButton(
            onPressed: source.controlBusy
                ? null
                : () => unawaited(source.recoverControl(pending)),
            child: const Text('核实原保存结果'),
          ),
        if (source.controlBusy) const LinearProgressIndicator(),
        FilledButton(
          onPressed: _editing(source) && source.dirty && !source.controlBusy
              ? () => unawaited(_save(source))
              : null,
          child: const Text('检查并保存计划'),
        ),
      ],
    );
  }

  Widget _entry(
    PersonalCollectionController source,
    JourneyEntry row,
    int index,
  ) {
    final d = source.draftJourneyDetails!;
    const labels = {
      'title': '标题',
      'note': '备注',
      'startDate': '开始日期',
      'endDate': '结束日期',
      'startTime': '开始时间',
      'endTime': '结束时间',
      'timeLabel': '时间说明',
      'departureName': '出发地',
      'arrivalName': '到达地',
      'serviceNumber': '班次',
      'customAddress': '地址',
      'transportMode': '交通方式',
    };
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  '${index + 1}. ${row.text('title').isEmpty
                      ? row.type == 'STOP'
                            ? '停留点'
                            : '安排'
                      : row.text('title')}',
                  style: NowUiTokens.of(context).placeTitle,
                ),
              ),
              IconButton(
                tooltip: '上移',
                onPressed: _editing(source) && index > 0
                    ? () => _move(source, index, -1)
                    : null,
                icon: const Icon(Icons.arrow_upward),
              ),
              IconButton(
                tooltip: '下移',
                onPressed: _editing(source) && index + 1 < d.itinerary.length
                    ? () => _move(source, index, 1)
                    : null,
                icon: const Icon(Icons.arrow_downward),
              ),
              IconButton(
                tooltip: '从私人草稿移除此项',
                onPressed: _editing(source)
                    ? () => source.setJourneyDetails(
                        d.changed(
                          itinerary: [
                            for (final item in d.itinerary)
                              if (item.id != row.id) item,
                          ],
                        ),
                      )
                    : null,
                icon: const Icon(Icons.remove_circle_outline),
              ),
              PopupMenuButton<String>(
                tooltip: '编辑这项安排',
                enabled: _editing(source),
                onSelected: (field) =>
                    _rowField(source, row, field, labels[field]!),
                itemBuilder: (_) => [
                  for (final e in labels.entries)
                    PopupMenuItem(value: e.key, child: Text(e.value)),
                ],
              ),
            ],
          ),
          Text(JourneyDetailEditor.reviewText(row, d)),
        ],
      ),
    );
  }

  Widget _public(PublicCollectionRecord record) {
    final snapshot = record.journeySnapshot;
    return ListView(
      key: PageStorageKey(
        'now-journey-public:${record.reference.id}:${record.revision}',
      ),
      padding: const EdgeInsets.all(16),
      children: [
        Text(record.title, style: NowUiTokens.of(context).sheetTitle),
        Text(
          '已发布版本 ${record.revision}',
          style: NowUiTokens.of(context).metadata,
        ),
        if (record.summary.isNotEmpty) Text(record.summary),
        if (snapshot != null)
          for (final stop in snapshot.stops)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: stop['available'] != true
                  ? Text('第 ${stop['position']} 站 · 当前不可读')
                  : Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          '${stop['position']}. ${stop['title']}',
                          style: NowUiTokens.of(context).placeTitle,
                        ),
                        if ((stop['note'] as String).isNotEmpty)
                          Text(stop['note']),
                        if (stop['placeName'] case final String name)
                          Text(name),
                        if (stop['momentId'] case final String mid)
                          TextButton(
                            onPressed: c.publicCurrent(record)
                                ? () => widget.onOpenMoment(
                                    mid,
                                    () => c.publicCurrent(record),
                                  )
                                : null,
                            child: Text(
                              stop['commentMomentId'] == mid
                                  ? '阅读并评论这条动态'
                                  : '阅读关联动态',
                            ),
                          ),
                      ],
                    ),
            ),
        if (snapshot == null)
          for (var i = 0; i < record.entries.length; i++)
            ListTile(
              title: Text(
                '${i + 1}. ${record.entries[i]['placeName'] ?? '未公开地点'}',
              ),
              subtitle: Text(record.entries[i]['note'] as String),
            ),
        const Padding(
          padding: EdgeInsets.only(top: 12),
          child: Text('站点顺序表示内容关联，不是导航或实际轨迹。'),
        ),
      ],
    );
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: c,
    builder: (context, _) => Column(
      children: [
        Row(
          children: [
            if (c.selectedMine != null || c.selectedPublic != null)
              IconButton(
                tooltip: '返回旅程列表',
                onPressed: () {
                  ++_epoch;
                  c.back();
                },
                icon: const Icon(Icons.arrow_back),
              ),
            Expanded(
              child: Padding(
                padding: const EdgeInsets.all(16),
                child: Text('旅程', style: NowUiTokens.of(context).sheetTitle),
              ),
            ),
            IconButton(
              tooltip: '关闭旅程',
              onPressed: widget.onClose,
              icon: const Icon(Icons.close),
            ),
          ],
        ),
        if (c.selectedMine == null && c.selectedPublic == null)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: SegmentedButton<bool>(
              segments: const [
                ButtonSegment(value: false, label: Text('我的旅程')),
                ButtonSegment(value: true, label: Text('可见旅程')),
              ],
              selected: {_visibleTab},
              onSelectionChanged: (value) {
                setState(() => _visibleTab = value.single);
                if (_visibleTab && c.visible.isEmpty)
                  unawaited(c.loadVisible());
              },
            ),
          ),
        if (c.error != null)
          Padding(
            padding: const EdgeInsets.all(12),
            child: Semantics(liveRegion: true, child: Text(c.error!)),
          ),
        if (c.loading || c.opening || c.mine.loading)
          const LinearProgressIndicator(),
        Expanded(
          child: !c.current
              ? const Center(child: Text('请重新打开旅程。'))
              : c.selectedMine != null
              ? _mine(c.selectedMine!)
              : c.selectedPublic != null
              ? _public(c.selectedPublic!)
              : ListView(
                  key: PageStorageKey('now-journey-list:$_visibleTab'),
                  padding: const EdgeInsets.all(16),
                  children: [
                    if (!_visibleTab) ...[
                      const Text('本人有权读取的全部城市计划'),
                      if (c.mine.error != null) Text(c.mine.error!),
                      if (!c.mine.loading && c.mine.items.isEmpty)
                        const Padding(
                          padding: EdgeInsets.symmetric(vertical: 24),
                          child: Text('还没有已保存的旅程。'),
                        ),
                      for (final v in c.mine.items)
                        ListTile(
                          title: Text(v.title.isEmpty ? '未命名旅程' : v.title),
                          subtitle: Text(
                            '${v.journeyLifecycle?.label ?? '日期待确定'} · ${v.visibility == 'public' ? '已公开' : '我的计划'}',
                          ),
                          trailing: const Icon(Icons.chevron_right),
                          onTap: c.opening
                              ? null
                              : () => unawaited(c.openMine(v)),
                        ),
                      TextButton(
                        onPressed: c.mine.loading
                            ? null
                            : () => unawaited(
                                c.mine.load(more: c.mine.nextCursor != null),
                              ),
                        child: Text(
                          c.mine.nextCursor == null ? '刷新我的旅程' : '加载更多旅程',
                        ),
                      ),
                    ] else ...[
                      const Text('全部城市 · 当前可见的公开旅程'),
                      if (!c.loading && c.visible.isEmpty)
                        const Padding(
                          padding: EdgeInsets.symmetric(vertical: 24),
                          child: Text('暂无当前可见的已发布旅程。'),
                        ),
                      for (final v in c.visible)
                        ListTile(
                          title: Text(v.title),
                          subtitle: Text(
                            v.summary,
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                          ),
                          trailing: const Icon(Icons.chevron_right),
                          onTap: c.opening
                              ? null
                              : () => unawaited(c.openPublic(v)),
                        ),
                      TextButton(
                        onPressed: c.loading
                            ? null
                            : () => unawaited(
                                c.loadVisible(more: c.nextCursor != null),
                              ),
                        child: Text(c.nextCursor == null ? '刷新可见旅程' : '加载更多旅程'),
                      ),
                    ],
                  ],
                ),
        ),
      ],
    ),
  );
}
