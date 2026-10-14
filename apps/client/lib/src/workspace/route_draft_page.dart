import 'dart:async';
import 'package:flutter/material.dart';
import '../app/birdtie_app_bar.dart';
import 'now_ui_tokens.dart';
import 'route_draft.dart';
import 'saved_items.dart';
import 'external_plan_import.dart';

class RouteDraftPage extends StatefulWidget {
  const RouteDraftPage({
    super.key,
    required this.controller,
    required this.onMap,
    required this.onAddToCollection,
    this.importOnOpen = false,
  });
  final RouteDraftController controller;
  final ValueChanged<List<SavedItem>> onMap, onAddToCollection;
  final bool importOnOpen;
  @override
  State<RouteDraftPage> createState() => _RouteDraftPageState();
}

class _RouteDraftPageState extends State<RouteDraftPage>
    with WidgetsBindingObserver {
  final _title = TextEditingController(), _dayText = TextEditingController();
  RouteDraftController get c => widget.controller;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    c.addListener(_changed);
    _sync();
    unawaited(c.restore());
    if (widget.importOnOpen)
      WidgetsBinding.instance.addPostFrameCallback((_) => _import());
  }

  void _sync() {
    if (_title.text != c.value.label) _title.text = c.value.label;
    final text = c.value.days[c.selectedDay].text;
    if (_dayText.text != text) _dayText.text = text;
  }

  void _changed() {
    if (mounted) {
      _sync();
      setState(() {});
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    c.removeListener(_changed);
    _title.dispose();
    _dayText.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) unawaited(c.flush());
  }

  Future<void> _import() async {
    if (!mounted || !c.current) return;
    final input = TextEditingController();
    final navigator = Navigator.of(context);
    late final DialogRoute<String> dialog;
    void retire() {
      if (!c.current) {
        input.clear();
        if (dialog.isActive) navigator.removeRoute(dialog);
      }
    }

    dialog = DialogRoute<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('导入本人提供的计划'),
        content: SizedBox(
          width: 420,
          child: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Text('支持按天分组的文字或结构化计划。日期、地点和时间仍需核对；导入不会预订、发信或发布。'),
                const ExpansionTile(
                  title: Text('查看结构化格式'),
                  children: [
                    Padding(
                      padding: EdgeInsets.all(8),
                      child: SelectableText(
                        '{"title":"本人计划","days":[{"title":"第1天","date":"日期待核对",'
                        '"text":"当天待核对的安排","stops":[{"name":"地点名称待核对",'
                        '"time":"时间待核对","placeId":"可选的输入地点编号","note":"本人备注"}]}]}',
                      ),
                    ),
                  ],
                ),
                TextField(
                  controller: input,
                  maxLines: 8,
                  decoration: const InputDecoration(labelText: '粘贴获准使用的计划文字'),
                ),
              ],
            ),
          ),
        ),
        actions: [
          TextButton(
            onPressed: () {
              if (dialog.isActive) Navigator.pop(context);
            },
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () {
              if (c.current && dialog.isActive)
                Navigator.pop(context, input.text);
            },
            child: const Text('生成本机草稿'),
          ),
        ],
      ),
    );
    c.addListener(retire);
    String? raw;
    try {
      if (c.current) {
        raw = await navigator.push(dialog);
        await dialog.completed;
      }
    } finally {
      c.removeListener(retire);
      input.dispose();
    }
    if (!mounted || !c.current || raw == null || raw.trim().isEmpty) return;
    try {
      c.edit(LocalRouteValue.importText(raw));
    } catch (e) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            '${e is FormatException ? e.message : '计划格式无效或超过本机限制'}；原草稿仍保留。',
          ),
        ),
      );
    }
  }

  void _editInput(
    String dayID,
    ExternalPlanDayInput Function(ExternalPlanDayInput) change,
  ) {
    if (!c.current) return;
    final day = c.value.days[c.selectedDay];
    if (day.id != dayID || day.input == null) return;
    try {
      c.replaceDay(day.edit(input: change(day.input!)));
    } on FormatException catch (e) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(e.message)));
    }
  }

  void _editStop(
    String dayID,
    int index,
    ExternalPlanStopInput Function(ExternalPlanStopInput) change,
  ) {
    _editInput(dayID, (input) {
      if (index < 0 || index >= input.stops.length) return input;
      final stops = [...input.stops];
      stops[index] = change(stops[index]);
      return input.edit(stops: stops);
    });
  }

  void _mapDay(String dayID, int revision) {
    if (!c.current ||
        c.value.currentDayID != dayID ||
        c.envelope.revision != revision)
      return;
    final points = c.points();
    if (points == null || points.isEmpty) return;
    widget.onMap(points);
  }

  void _addDay(String dayID, int revision) {
    if (!c.current ||
        c.value.currentDayID != dayID ||
        c.envelope.revision != revision ||
        c.points() == null)
      return;
    final ids = c.value.days[c.selectedDay].places;
    if (ids.isEmpty) return;
    final rows = <SavedItem>[];
    for (final id in ids) {
      final point = c.place(id);
      if (point == null) return;
      rows.add(point);
    }
    widget.onAddToCollection(rows);
  }

  Widget _inputFields(LocalPlanDay day) => ExpansionTile(
    key: ValueKey('plan-input-${day.id}'),
    initiallyExpanded: true,
    title: const Text('导入信息 · 尚未核验'),
    subtitle: const Text('日期、时间和地点标识仅为本人材料；不表示到访、时区或当前地点。'),
    children: [
      TextFormField(
        key: ValueKey('plan-input-date-${day.id}'),
        initialValue: day.input!.date,
        decoration: const InputDecoration(labelText: '输入日期（未提供时保留空白）'),
        onChanged: (v) => _editInput(day.id, (input) => input.edit(date: v)),
      ),
      if (day.input!.stops.isEmpty)
        const Padding(
          padding: EdgeInsets.symmetric(vertical: 12),
          child: Text('当天没有导入地点条目。'),
        ),
      for (var n = 0; n < day.input!.stops.length; n++) ...[
        const SizedBox(height: 12),
        Text('输入地点 ${n + 1} · 未关联当前地点'),
        TextFormField(
          key: ValueKey('plan-input-name-${day.id}-$n'),
          initialValue: day.input!.stops[n].name,
          decoration: const InputDecoration(labelText: '输入地点名称（未核验）'),
          onChanged: (v) => _editStop(day.id, n, (stop) => stop.edit(name: v)),
        ),
        TextFormField(
          key: ValueKey('plan-input-time-${day.id}-$n'),
          initialValue: day.input!.stops[n].time,
          decoration: const InputDecoration(labelText: '输入时间（不推断时区）'),
          onChanged: (v) => _editStop(day.id, n, (stop) => stop.edit(time: v)),
        ),
        TextFormField(
          key: ValueKey('plan-input-id-${day.id}-$n'),
          initialValue: day.input!.stops[n].placeID,
          decoration: const InputDecoration(labelText: '输入地点编号（尚未匹配）'),
          onChanged: (v) =>
              _editStop(day.id, n, (stop) => stop.edit(placeID: v)),
        ),
        TextFormField(
          key: ValueKey('plan-input-note-${day.id}-$n'),
          initialValue: day.input!.stops[n].note,
          maxLines: 2,
          decoration: const InputDecoration(labelText: '输入备注（仅本人材料）'),
          onChanged: (v) => _editStop(day.id, n, (stop) => stop.edit(note: v)),
        ),
      ],
      const SizedBox(height: 12),
    ],
  );

  Future<void> _pick() async {
    if (!c.current) return;
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      builder: (context) => AnimatedBuilder(
        animation: c,
        builder: (context, _) => SizedBox(
          height: MediaQuery.sizeOf(context).height * .72,
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              const Text('选择当前已加载的同城收藏地点'),
              if (!c.current)
                const Text('身份或城市已变化，请返回。')
              else if (c.choices.isEmpty)
                const Text('尚无当前可用地点。请在收藏中搜索、加载后重新打开。')
              else
                for (final p in c.choices)
                  ListTile(
                    title: Text(p.title),
                    subtitle: Text(p.summary),
                    trailing: const Icon(Icons.add),
                    onTap: () => c.add(p.targetId),
                  ),
            ],
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.of(context),
        day = c.value.days[c.selectedDay],
        revision = c.envelope.revision,
        points = c.points();
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('路线与计划草稿')),
      body: !c.current
          ? const Center(child: Text('身份或城市已变化，请返回。'))
          : !c.localNamespaceReadComplete
          ? ListView(
              padding: const EdgeInsets.all(16),
              children: [
                const LinearProgressIndicator(),
                const SizedBox(height: 12),
                Text(c.error ?? '正在读取本人本机计划，原稿尚未改动。'),
                if (c.error != null)
                  OutlinedButton(
                    onPressed: () => unawaited(c.restore()),
                    child: const Text('重试恢复本机计划'),
                  ),
              ],
            )
          : c.restoreChoicePending
          ? ListView(
              padding: const EdgeInsets.all(16),
              children: [
                const Text('本次转换材料与这座城市的已保存计划不同。请选择要继续的草稿；继续转换稿会替换该城市的本机计划。'),
                const SizedBox(height: 16),
                FilledButton(
                  onPressed: () => c.chooseRestoredDraft(true),
                  child: const Text('恢复已保存计划'),
                ),
                OutlinedButton(
                  onPressed: () => c.chooseRestoredDraft(false),
                  child: const Text('继续本次转换稿'),
                ),
                if (c.error != null) Text(c.error!),
              ],
            )
          : ListView(
              padding: const EdgeInsets.all(16),
              keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
              children: [
                TextField(
                  controller: _title,
                  decoration: const InputDecoration(labelText: '计划名称'),
                  onChanged: (v) => c.edit(c.value.edit(label: v)),
                ),
                Text('本机草稿；关闭编辑不发送消息、不报名或公开。', style: tokens.metadata),
                const SizedBox(height: 12),
                DropdownButtonFormField<String>(
                  key: ValueKey('route-mode-${c.value.mode}'),
                  initialValue: c.value.mode,
                  decoration: const InputDecoration(labelText: '交通方式'),
                  items: [
                    for (final m in [
                      ('walking', '步行'),
                      ('cycling', '骑行'),
                      ('driving', '驾车'),
                      ('transit', '公共交通'),
                    ])
                      DropdownMenuItem(value: m.$1, child: Text(m.$2)),
                  ],
                  onChanged: (v) {
                    if (v != null) c.edit(c.value.edit(mode: v));
                  },
                ),
                const SizedBox(height: 12),
                DropdownButtonFormField<String>(
                  key: ValueKey('route-start-${c.value.startPlaceID}'),
                  initialValue: c.value.startPlaceID ?? '',
                  isExpanded: true,
                  decoration: const InputDecoration(
                    labelText: '起点（明确选择，不推断当前位置）',
                  ),
                  items: [
                    const DropdownMenuItem(value: '', child: Text('尚未指定起点')),
                    if (c.value.startPlaceID != null &&
                        c.place(c.value.startPlaceID!) == null)
                      DropdownMenuItem(
                        value: c.value.startPlaceID!,
                        child: const Text('原起点当前不可用，需重新选择'),
                      ),
                    for (final p in c.choices)
                      DropdownMenuItem(
                        value: p.targetId,
                        child: Text(
                          p.title,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                  ],
                  onChanged: (v) => c.edit(
                    c.value.edit(startPlaceID: v, clearStart: v == ''),
                  ),
                ),
                const SizedBox(height: 12),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (var i = 0; i < c.value.days.length; i++)
                      ChoiceChip(
                        label: Text(c.value.days[i].label),
                        selected: i == c.selectedDay,
                        onSelected: (_) => c.selectDay(i),
                      ),
                  ],
                ),
                const SizedBox(height: 8),
                TextField(
                  controller: _dayText,
                  maxLines: 4,
                  decoration: const InputDecoration(labelText: '当天待核对的文字'),
                  onChanged: (v) => c.replaceDay(day.edit(text: v)),
                ),
                if (day.input != null) _inputFields(day),
                if (c.value.importUnknownFields.isNotEmpty)
                  ExpansionTile(
                    title: const Text('未识别的输入字段'),
                    initiallyExpanded: true,
                    children: [
                      for (final field in c.value.importUnknownFields)
                        ListTile(dense: true, title: Text(field)),
                    ],
                  ),
                const SizedBox(height: 8),
                if (day.places.isEmpty)
                  const Text('当天尚未选择当前可用地点；导入名称和标识不会自动上图。'),
                for (var i = 0; i < day.places.length; i++)
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    title: Text(
                      '${i + 1}. ${c.place(day.places[i])?.title ?? '原地点当前不可用'}',
                    ),
                    subtitle: c.place(day.places[i]) == null
                        ? const Text('重新加载并选择后才可上图或加入集合。')
                        : null,
                    trailing: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        IconButton(
                          tooltip: '上移',
                          onPressed: i == 0 ? null : () => c.move(i, -1),
                          icon: const Icon(Icons.arrow_upward),
                        ),
                        IconButton(
                          tooltip: '下移',
                          onPressed: i == day.places.length - 1
                              ? null
                              : () => c.move(i, 1),
                          icon: const Icon(Icons.arrow_downward),
                        ),
                        IconButton(
                          tooltip: '移除地点',
                          onPressed: () => c.replaceDay(
                            day.edit(places: [...day.places]..removeAt(i)),
                          ),
                          icon: const Icon(Icons.close),
                        ),
                      ],
                    ),
                  ),
                OutlinedButton.icon(
                  onPressed: _pick,
                  icon: const Icon(Icons.add_location_alt_outlined),
                  label: const Text('选择收藏地点'),
                ),
                Text(
                  '路线计算不可用：当前没有获准的路线计算服务。地图只显示所选地点，不提供连线、距离或估时。',
                  style: tokens.metadata,
                ),
                const SizedBox(height: 12),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    OutlinedButton.icon(
                      onPressed: points == null || points.isEmpty
                          ? null
                          : () => _mapDay(day.id, revision),
                      icon: const Icon(Icons.map_outlined),
                      label: const Text('当天地点上原地图'),
                    ),
                    OutlinedButton.icon(
                      onPressed: points == null || day.places.isEmpty
                          ? null
                          : () => _addDay(day.id, revision),
                      icon: const Icon(Icons.playlist_add),
                      label: const Text('加入已有旅程或主题地图'),
                    ),
                    TextButton(onPressed: _import, child: const Text('导入外部计划')),
                  ],
                ),
                if (c.value.sourceText.isNotEmpty)
                  ExpansionTile(
                    title: const Text('原导入文字与来源'),
                    children: [
                      Padding(
                        padding: const EdgeInsets.all(12),
                        child: SelectableText(
                          '${c.value.sourceText}\n\n本机原文 SHA256：${c.value.sourceDigest}',
                        ),
                      ),
                    ],
                  ),
                if (c.error != null) Text(c.error!, style: tokens.body),
                TextButton(
                  onPressed: () => Navigator.maybePop(context),
                  child: const Text('关闭编辑'),
                ),
              ],
            ),
    );
  }
}
