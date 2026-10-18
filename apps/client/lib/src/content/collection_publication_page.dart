import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../workspace/now_ui_tokens.dart';
import 'collection_publication_controller.dart';

class CollectionPublicationPage extends StatelessWidget {
  const CollectionPublicationPage({
    super.key,
    required this.controller,
    required this.cityName,
    this.embedded = false,
    this.onClose,
  });
  final CollectionPublicationController controller;
  final String cityName;
  final bool embedded;
  final VoidCallback? onClose;
  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: controller,
    builder: (context, _) {
      final c = controller, current = c.current, v = c.record;
      return Scaffold(
        appBar: AppBar(
          automaticallyImplyLeading: !embedded,
          title: const Text('公开发布'),
          actions: [
            if (onClose != null)
              IconButton(
                tooltip: '返回旅程',
                onPressed: onClose,
                icon: const Icon(Icons.close),
              ),
          ],
        ),
        body: !current || v == null
            ? Center(child: Text(c.error ?? '请先保存并重新打开这份内容。'))
            : ListView(
                padding: const EdgeInsets.all(16),
                children: [
                  Text(v.title, style: NowUiTokens.of(context).sheetTitle),
                  const SizedBox(height: 8),
                  Text(
                    '$cityName · 当前个人账号',
                    style: NowUiTokens.of(context).metadata,
                  ),
                  if (v.privatePlaceCopyRestricted)
                    const Padding(
                      padding: EdgeInsets.symmetric(vertical: 12),
                      child: Text(
                        '原复制许可只允许私人保存，不能把这些复制地点再次公开。可继续编辑本人标题、备注和顺序；先明确移除并保存，再从当前地点重新选择，才能按自己的资料发布。',
                      ),
                    ),
                  const SizedBox(height: 12),
                  Text(
                    c.richJourney
                        ? '仅发布下面明确勾选的站点。私人安排、预算、成员、住宿和来源不会进入公开版本；后续修改私人计划不会自动更新公开版本。'
                        : v.status == 'published'
                        ? '已公开。其他人可通过分享代码读取当前内容。'
                        : '请检查以下完整内容。名称、说明和每个地点的备注都会公开；每个地点的位置范围由你选择。',
                  ),
                  const SizedBox(height: 12),
                  if (v.summary.isNotEmpty) SelectableText(v.summary),
                  if (c.richJourney) ...[
                    if (v.publishedSnapshot case final published?)
                      Text(
                        '当前公开版本 ${published.publicationRevision} · ${published.stopCount} 站。重新发布会替换公开版本，私人计划保持独立。',
                      ),
                    if (utf8.encode(v.title).length > 160 ||
                        utf8.encode(v.summary).length > 5000)
                      const Text('公开名称或说明超过服务长度限制。请先返回修改并保存，再检查公开内容。'),
                    for (final stop in c.journeyStops)
                      Padding(
                        padding: const EdgeInsets.symmetric(vertical: 8),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            CheckboxListTile(
                              contentPadding: EdgeInsets.zero,
                              title: Text(
                                '${stop.values['position']}. ${stop.text('title')}',
                              ),
                              subtitle: stop.text('note').isEmpty
                                  ? null
                                  : Text(stop.text('note')),
                              value: c.selectedStops.contains(stop.id),
                              onChanged: c.canConfigure
                                  ? (value) =>
                                        c.selectStop(stop.id, value == true)
                                  : null,
                            ),
                            if (c.selectedStops.contains(stop.id)) ...[
                              if (utf8.encode(stop.text('title')).length >
                                      160 ||
                                  utf8.encode(stop.text('note')).length > 2000)
                                const Text('此站标题或备注超过公开长度限制；请返回调整后重新检查。'),
                              DropdownButtonFormField<String>(
                                key: Key(
                                  'journey-publication-precision:${stop.id}',
                                ),
                                initialValue:
                                    c.stopPrecision[stop.id] ?? 'hidden',
                                decoration: const InputDecoration(
                                  labelText: '本次公开位置范围',
                                ),
                                items: const [
                                  DropdownMenuItem(
                                    value: 'hidden',
                                    child: Text('不显示地点'),
                                  ),
                                  DropdownMenuItem(
                                    value: 'city',
                                    child: Text('隐藏精确地点'),
                                  ),
                                  DropdownMenuItem(
                                    value: 'place',
                                    child: Text('显示原站点的当前公开地点'),
                                  ),
                                ],
                                onChanged: c.canConfigure
                                    ? (value) {
                                        if (value != null)
                                          c.setStopPrecision(stop.id, value);
                                      }
                                    : null,
                              ),
                              TextButton(
                                onPressed: c.canConfigure && !c.loadingMoments
                                    ? () => unawaited(c.loadMomentChoices())
                                    : null,
                                child: const Text('读取本人已发布动态以选择引用'),
                              ),
                              if (c.stopMoments[stop.id] case final moment?)
                                ListTile(
                                  contentPadding: EdgeInsets.zero,
                                  title: Text(
                                    moment.title.isEmpty
                                        ? moment.body
                                        : moment.title,
                                    maxLines: 3,
                                  ),
                                  subtitle: Text(
                                    '已选动态 · 版本 ${moment.revision}',
                                  ),
                                  trailing: IconButton(
                                    tooltip: '移除动态引用',
                                    onPressed: c.canConfigure
                                        ? () => c.setStopMoment(stop.id, null)
                                        : null,
                                    icon: const Icon(Icons.close),
                                  ),
                                ),
                              if (c.momentChoices.isNotEmpty)
                                DropdownButtonFormField<String>(
                                  key: Key(
                                    'journey-publication-moment:${stop.id}:${c.stopMoments[stop.id]?.id}:${c.momentChoices.map((m) => '${m.id}:${m.revision}').join(',')}',
                                  ),
                                  isExpanded: true,
                                  initialValue: null,
                                  decoration: const InputDecoration(
                                    labelText: '明确选择本次关联动态',
                                  ),
                                  items: [
                                    for (final moment in c.momentChoices)
                                      DropdownMenuItem(
                                        value: moment.id,
                                        child: Text(
                                          moment.title.isEmpty
                                              ? moment.body
                                              : moment.title,
                                          maxLines: 1,
                                          overflow: TextOverflow.ellipsis,
                                        ),
                                      ),
                                  ],
                                  onChanged: c.canConfigure
                                      ? (id) {
                                          for (final m in c.momentChoices) {
                                            if (m.id == id) {
                                              c.setStopMoment(stop.id, m);
                                              break;
                                            }
                                          }
                                        }
                                      : null,
                                ),
                              if (c.stopMoments[stop.id] != null)
                                CheckboxListTile(
                                  contentPadding: EdgeInsets.zero,
                                  title: const Text('用这条动态承载此站的评论入口'),
                                  value: c.commentStops.contains(stop.id),
                                  onChanged: c.canConfigure
                                      ? (v) =>
                                            c.setStopComment(stop.id, v == true)
                                      : null,
                                ),
                            ],
                          ],
                        ),
                      ),
                    if (c.selectedStops.length > 50)
                      const Text('每个公开版本最多选择50站，请减少本次选择。'),
                    if (c.journeyStops.isEmpty) const Text('这份计划暂无可公开的停留站点。'),
                    if (c.loadingMoments) const LinearProgressIndicator(),
                    if (c.momentsError != null) Text(c.momentsError!),
                  ],
                  for (var i = 0; i < v.entries.length; i++)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 12),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            '${i + 1}. ${v.entries[i].sourceCurrent ? v.entries[i].placeName : '地点来源当前不可用'}',
                          ),
                          if (v.entries[i].note.isNotEmpty)
                            SelectableText(v.entries[i].note),
                          DropdownButtonFormField<String>(
                            key: Key('publication-precision-$i'),
                            initialValue: c.precision[i],
                            decoration: const InputDecoration(
                              labelText: '公开位置范围',
                            ),
                            items: const [
                              DropdownMenuItem(
                                value: 'hidden',
                                child: Text('不显示地点'),
                              ),
                              DropdownMenuItem(
                                value: 'city',
                                child: Text('仅显示城市'),
                              ),
                              DropdownMenuItem(
                                value: 'place',
                                child: Text('显示所选公开地点'),
                              ),
                            ],
                            onChanged: c.canPublish
                                ? (value) {
                                    if (value != null) c.setPrecision(i, value);
                                  }
                                : null,
                          ),
                        ],
                      ),
                    ),
                  const Text(
                    '不显示地点不会改写你在名称、说明或备注中写出的地点信息。公开分享只展示这份内容，不证明你到访过，也不授权模型处理。',
                  ),
                  if (v.status == 'draft' && !c.richJourney) ...[
                    SwitchListTile.adaptive(
                      key: const Key('publication-allow-private-place-copy'),
                      contentPadding: EdgeInsets.zero,
                      title: const Text('允许他人复制公开地点到私人副本'),
                      subtitle: const Text(
                        '默认关闭。仅允许复制当前公开可见的地点及顺序；不复制你的名称、说明、备注或坐标。',
                      ),
                      value: c.allowPrivatePlaceCopy,
                      onChanged: c.canPublish
                          ? c.setAllowPrivatePlaceCopy
                          : null,
                    ),
                  ],
                  const SizedBox(height: 20),
                  if (c.notice != null)
                    Semantics(liveRegion: true, child: Text(c.notice!)),
                  if (c.error != null)
                    Text(
                      c.error!,
                      style: TextStyle(
                        color: Theme.of(context).colorScheme.error,
                      ),
                    ),
                  if (c.busy) const LinearProgressIndicator(),
                  const SizedBox(height: 12),
                  if (c.uncertain) ...[
                    FilledButton(
                      onPressed: current && !c.busy
                          ? () => unawaited(c.recover())
                          : null,
                      child: const Text('核实原操作结果'),
                    ),
                    OutlinedButton(
                      onPressed: current && !c.busy
                          ? () async {
                              final original = c.source.publicationPending;
                              if (original == null) return;
                              final approve = await showDialog<bool>(
                                context: context,
                                builder: (context) => AnimatedBuilder(
                                  animation: c,
                                  builder: (context, _) {
                                    final valid =
                                        c.current &&
                                        !c.busy &&
                                        identical(
                                          c.source.publicationPending,
                                          original,
                                        );
                                    return AlertDialog(
                                      title: const Text('结束并核实原操作'),
                                      content: Text(
                                        valid
                                            ? '${v.title}\n${original['action'] == 'PUBLISH' ? '原公开发布' : '原撤回公开'}\n若原操作已成功，将读取实际回执；否则会阻止同一操作继续提交。不会重新发布、撤回或删除内容。'
                                            : '账号或原操作已变化，请返回重新核实。',
                                      ),
                                      actions: [
                                        TextButton(
                                          onPressed: () =>
                                              Navigator.pop(context, false),
                                          child: const Text('保留待核实状态'),
                                        ),
                                        FilledButton(
                                          onPressed: valid
                                              ? () =>
                                                    Navigator.pop(context, true)
                                              : null,
                                          child: const Text('结束并核实'),
                                        ),
                                      ],
                                    );
                                  },
                                ),
                              );
                              if (approve == true &&
                                  c.current &&
                                  c.uncertain &&
                                  identical(
                                    c.source.publicationPending,
                                    original,
                                  ) &&
                                  !c.busy)
                                await c.closeOriginal();
                            }
                          : null,
                      child: const Text('结束并核实原操作'),
                    ),
                  ] else if (v.status == 'published' ||
                      v.publishedSnapshot != null) ...[
                    FilledButton.icon(
                      onPressed: c.canUnpublish
                          ? () => unawaited(c.unpublish())
                          : null,
                      icon: const Icon(Icons.lock_outline),
                      label: const Text('撤回公开，保留本人草稿'),
                    ),
                    OutlinedButton.icon(
                      onPressed: c.canUnpublish
                          ? () async {
                              await Clipboard.setData(
                                ClipboardData(
                                  text: CollectionShareReference(
                                    v.kind,
                                    v.id,
                                  ).code,
                                ),
                              );
                              if (context.mounted && c.current)
                                ScaffoldMessenger.of(context).showSnackBar(
                                  const SnackBar(
                                    content: Text(
                                      '分享代码已复制，可在旅程或主题地图的“打开分享”中读取。',
                                    ),
                                  ),
                                );
                            }
                          : null,
                      icon: const Icon(Icons.copy),
                      label: const Text('复制分享代码'),
                    ),
                  ] else ...[
                    if (c.source.dirty) const Text('有未保存修改。请返回保存，再重新打开公开预览。'),
                    FilledButton(
                      onPressed: c.canPublish
                          ? () => unawaited(c.publish())
                          : null,
                      child: const Text('将以上内容公开发布'),
                    ),
                  ],
                  if (c.richJourney &&
                      v.publishedSnapshot != null &&
                      !c.uncertain)
                    FilledButton(
                      onPressed: c.canPublish
                          ? () => unawaited(c.publish())
                          : null,
                      child: const Text('以本次勾选内容更新公开版本'),
                    ),
                ],
              ),
      );
    },
  );
}

class PublicCollectionPage extends StatefulWidget {
  const PublicCollectionPage({
    super.key,
    required this.controller,
    this.onOpenPlace,
    this.canOpenPlace,
    this.cityNameFor,
    this.onCopyPlaces,
  });
  final PublicCollectionController controller;
  final void Function(String cityID, String placeID)? onOpenPlace;
  final bool Function(String cityID)? canOpenPlace;
  final String? Function(String cityID)? cityNameFor;
  final Future<void> Function(PublicCollectionRecord)? onCopyPlaces;
  @override
  State<PublicCollectionPage> createState() => _PublicCollectionPageState();
}

class _PublicCollectionPageState extends State<PublicCollectionPage>
    with WidgetsBindingObserver {
  PublicCollectionController get c => widget.controller;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    unawaited(c.load());
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) unawaited(c.load());
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: c,
    builder: (context, _) {
      final current = c.current, v = c.record;
      return Scaffold(
        appBar: AppBar(
          title: Text(c.reference.kind == 'journey' ? '公开旅程' : '公开主题地图'),
        ),
        body: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            if (c.loading) const LinearProgressIndicator(),
            if (v != null && current) ...[
              Text(v.title, style: NowUiTokens.of(context).sheetTitle),
              Text(
                v.journeySnapshot != null
                    ? '公开旅程版本 ${v.revision}'
                    : widget.cityNameFor?.call(v.cityID) ?? '公开城市尚未载入',
                style: NowUiTokens.of(context).metadata,
              ),
              const SizedBox(height: 12),
              if (v.summary.isNotEmpty) SelectableText(v.summary),
              if (v.journeySnapshot case final snapshot?)
                for (final stop in snapshot.stops)
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    leading: Text('${stop['position']}'),
                    title: Text(
                      stop['available'] == true
                          ? (stop['title'] as String).isEmpty
                                ? '停留点'
                                : stop['title'] as String
                          : '当前不可读',
                    ),
                    subtitle: stop['available'] != true
                        ? null
                        : Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              if ((stop['note'] as String).isNotEmpty)
                                Text(stop['note'] as String),
                              if (stop['placeName'] case final String name)
                                Text(name),
                            ],
                          ),
                  ),
              for (var i = 0; i < v.entries.length; i++)
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  leading: Text('${i + 1}'),
                  title: Text(
                    v.entries[i]['placeName'] as String? ??
                        (v.entries[i]['locationVisibility'] == 'city'
                            ? '仅公开城市：${widget.cityNameFor?.call(v.cityID) ?? '尚未载入'}'
                            : v.entries[i]['locationVisibility'] == 'place'
                            ? '地点来源当前不可用'
                            : '未公开地点'),
                  ),
                  subtitle: (v.entries[i]['note'] as String).isEmpty
                      ? null
                      : Text(v.entries[i]['note'] as String),
                  onTap:
                      v.entries[i]['placeId'] != null &&
                          widget.onOpenPlace != null &&
                          (widget.canOpenPlace?.call(v.cityID) ?? true)
                      ? () {
                          if (c.current && identical(c.record, v))
                            widget.onOpenPlace!(
                              v.cityID,
                              v.entries[i]['placeId'] as String,
                            );
                        }
                      : null,
                ),
              const Text('这是作者明确公开的内容；内容中的地点不表示已核验到访。'),
              if (v.copyGrant != null &&
                  v.copyGrant!.current &&
                  v.copyGrant!.eligiblePlaceIDs.isNotEmpty &&
                  widget.onCopyPlaces != null)
                FilledButton.icon(
                  key: const Key('public-collection-copy-places'),
                  onPressed: () {
                    if (c.current &&
                        identical(c.record, v) &&
                        v.copyGrant!.current &&
                        !c.loading)
                      unawaited(widget.onCopyPlaces!(v));
                  },
                  icon: const Icon(Icons.copy_outlined),
                  label: const Text('复制公开地点到本人私人副本'),
                ),
              if (v.copyGrant == null) const Text('作者未允许复制地点；公开阅读不等于复制许可。'),
              if (v.copyGrant != null && v.copyGrant!.eligiblePlaceIDs.isEmpty)
                const Text('当前没有获准复制的公开地点。'),
              if (widget.canOpenPlace?.call(v.cityID) == false)
                const Text('这份分享来自其他城市。返回切换城市后，再查看地点详情。'),
            ],
            if (c.error != null) Text(c.error!),
            if (v == null && !c.loading && c.error == null)
              const Text('尚未读取当前公开内容。'),
            const SizedBox(height: 12),
            OutlinedButton(
              onPressed: current && !c.loading
                  ? () => unawaited(c.load())
                  : null,
              child: const Text('重新读取'),
            ),
          ],
        ),
      );
    },
  );
}
