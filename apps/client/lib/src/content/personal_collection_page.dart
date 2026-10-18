import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';

import '../workspace/now_ui_tokens.dart';
import '../workspace/legacy_place_reference_page.dart';
import 'personal_collection_controller.dart';
import 'journey_detail_editor.dart';
import 'journey_history_page.dart';
import 'collection_control.dart';
import 'collection_publication_controller.dart';
import 'collection_publication_page.dart';
import 'collection_publication_pending_store.dart';
import 'collection_copy_controller.dart';
import 'collection_copy_pending_store.dart';
import 'collection_copy_page.dart';
import 'private_reference_media_view.dart';

/// Ordinary creation uses the same saved object as subsequent reads and edits.
/// The caller owns this controller so returning to Now preserves a local draft.
class PersonalCollectionPage extends StatefulWidget {
  const PersonalCollectionPage({
    super.key,
    required this.controller,
    required this.cityName,
    this.cityNames = const {},
    this.onOpenPlace,
    this.onViewMap,
    this.copyPendingStore,
  });
  final PersonalCollectionController controller;
  final String cityName;
  final Map<String, String> cityNames;
  final ValueChanged<String>? onOpenPlace;
  final ValueChanged<PersonalCollection>? onViewMap;
  final CollectionCopyPendingStore? copyPendingStore;
  @override
  State<PersonalCollectionPage> createState() => _PersonalCollectionPageState();
}

class _PersonalCollectionPageState extends State<PersonalCollectionPage> {
  final _title = TextEditingController(), _summary = TextEditingController();
  final _search = TextEditingController();
  bool _showSaved = false, _openingSaved = false;
  int _pageEpoch = 0;
  Object? _pageTarget;
  bool _pageClosed = false, _dialogChangesDisposed = false;
  final _dialogChanges = ValueNotifier<int>(0);
  final _controlDialogs = <DialogRoute<bool>>{};
  void _retireDialogs() {
    ++_pageEpoch;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!_dialogChangesDisposed) _dialogChanges.value++;
      });
    } else if (!_dialogChangesDisposed) {
      _dialogChanges.value++;
    }
  }

  void _disposeDialogChanges() {
    if (!_dialogChangesDisposed) {
      _dialogChangesDisposed = true;
      _dialogChanges.dispose();
    }
  }

  CollectionCopyPending? _copyPending;
  bool _copyPendingFailed = false;
  final _defaultCopyStore = SecureCollectionCopyPendingStore();
  CollectionCopyPendingStore get _copyStore =>
      widget.copyPendingStore ?? _defaultCopyStore;
  PersonalCollectionController get c => widget.controller;
  VoidCallback _onceDispose(VoidCallback dispose) {
    var done = false;
    return () {
      if (!done) {
        done = true;
        dispose();
      }
    };
  }

  @override
  void initState() {
    super.initState();
    _pageTarget = (c, c.editing?.id, c.editing?.revision);
    _title.text = c.title;
    _summary.text = c.summary;
    c.addListener(_changed);
    _scheduleInitialRead();
  }

  void _scheduleInitialRead() {
    final controller = c;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && identical(c, controller) && controller.current) {
        unawaited(controller.load());
        unawaited(_readCopyPending());
      }
    });
  }

  @override
  void didUpdateWidget(PersonalCollectionPage old) {
    super.didUpdateWidget(old);
    if (!identical(old.controller, c)) {
      _retireDialogs();
      _pageTarget = (c, c.editing?.id, c.editing?.revision);
      old.controller.removeListener(_changed);
      c.addListener(_changed);
      _copyPending = null;
      _copyPendingFailed = false;
      _title.text = c.title;
      _summary.text = c.summary;
      _scheduleInitialRead();
    }
  }

  void _changed() {
    if (!mounted) return;
    final target = (c, c.editing?.id, c.editing?.revision);
    if (target != _pageTarget) {
      _pageTarget = target;
      _retireDialogs();
    }
    if (_title.text != c.title)
      _title.value = TextEditingValue(
        text: c.title,
        selection: TextSelection.collapsed(offset: c.title.length),
      );
    if (_summary.text != c.summary)
      _summary.value = TextEditingValue(
        text: c.summary,
        selection: TextSelection.collapsed(offset: c.summary.length),
      );
    setState(() {});
  }

  @override
  void dispose() {
    _pageClosed = true;
    _retireDialogs();
    if (_controlDialogs.isEmpty) _disposeDialogChanges();
    c.removeListener(_changed);
    _title.dispose();
    _summary.dispose();
    _search.dispose();
    super.dispose();
  }

  Future<bool> _replaceDraft() async {
    if (!c.dirty) return true;
    return await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: const Text('替换未保存草稿？'),
            content: const Text('当前内容尚未保存，打开另一条记录会替换这份本机草稿。'),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context, false),
                child: const Text('保留草稿'),
              ),
              TextButton(
                onPressed: () => Navigator.pop(context, true),
                child: const Text('打开记录'),
              ),
            ],
          ),
        ) ==
        true;
  }

  Future<void> _publication() async {
    if (!c.current ||
        c.saving ||
        c.uncertain ||
        c.controlBlocked ||
        c.editing == null)
      return;
    if (c.draftJourneyDetails != null) return;
    if (c.hasPrivateLegacyReferences && c.publicationPending == null) return;
    if (c.editing!.privatePlaceCopyRestricted &&
        c.editing!.status == 'draft' &&
        c.publicationPending == null)
      return;
    final controller = CollectionPublicationController(
      source: c,
      authorizationHeader: c.authorizationHeader,
      ownerID: c.ownerID,
      client: c.transport,
      apiBaseUrl: c.apiBaseUrl,
      pendingStore: CollectionPublicationPendingStore(
        origin: c.apiBaseUrl,
        owner: c.ownerID()!,
        kind: c.kind,
        id: c.editing!.id,
      ),
    );
    final release = _onceDispose(controller.dispose);
    try {
      await controller.restorePending();
      if (!mounted || !c.current) return;
      final route = MaterialPageRoute<void>(
        builder: (_) => CollectionPublicationPage(
          controller: controller,
          cityName: widget.cityName,
        ),
      );
      unawaited(route.completed.then((_) => release()));
      await Navigator.of(context).push(route);
      await route.completed;
    } finally {
      release();
    }
  }

  Future<void> _openShare() async {
    final input = TextEditingController();
    final source = c;
    final dialog = DialogRoute<String>(
      context: context,
      builder: (context) => AnimatedBuilder(
        animation: source,
        builder: (context, _) => AlertDialog(
          title: const Text('打开分享'),
          content: TextField(
            controller: input,
            maxLines: 3,
            decoration: const InputDecoration(labelText: '粘贴旅程或主题地图分享代码'),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: source.current
                  ? () {
                      if (source.current) Navigator.pop(context, input.text);
                    }
                  : null,
              child: const Text('读取公开内容'),
            ),
          ],
        ),
      ),
    );
    unawaited(dialog.completed.then((_) => input.dispose()));
    final code = await Navigator.of(context).push(dialog);
    if (!mounted || code == null || !identical(c, source) || !source.current)
      return;
    final ref = CollectionShareReference.parse(code);
    if (ref == null) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('分享代码无效，请复制完整代码。')));
      return;
    }
    final controller = PublicCollectionController(
      reference: ref,
      authorizationHeader: c.authorizationHeader,
      identityChanges: c.identityChanges,
      client: c.transport,
      apiBaseUrl: c.apiBaseUrl,
    );
    final release = _onceDispose(controller.dispose);
    try {
      final route = MaterialPageRoute<void>(
        builder: (_) => PublicCollectionPage(
          controller: controller,
          canOpenPlace: (cityID) => cityID == c.cityID && c.current,
          cityNameFor: (cityID) =>
              widget.cityNames[cityID] ??
              (cityID == c.cityID ? widget.cityName : null),
          onOpenPlace: (cityID, id) {
            if (c.current && controller.current && cityID == c.cityID)
              widget.onOpenPlace?.call(id);
          },
          onCopyPlaces: (record) =>
              _openCopy(public: controller, record: record),
        ),
      );
      unawaited(route.completed.then((_) => release()));
      await Navigator.of(context).push(route);
      await route.completed;
    } finally {
      release();
    }
    if (mounted && c.current) await _readCopyPending();
  }

  Future<void> _readCopyPending() async {
    final source = c,
        owner = source.ownerID(),
        header = source.authorizationHeader();
    if (!source.current || owner == null) return;
    try {
      final p = await _copyStore.read(
        source.apiBaseUrl.replaceFirst(RegExp(r'/$'), ''),
        owner,
      );
      if (mounted &&
          identical(c, source) &&
          source.current &&
          source.ownerID() == owner &&
          source.authorizationHeader() == header)
        setState(() {
          _copyPending = p;
          _copyPendingFailed = false;
        });
    } catch (_) {
      if (mounted && identical(c, source) && source.current)
        setState(() => _copyPendingFailed = true);
    }
  }

  Future<void> _openCopy({
    PublicCollectionController? public,
    PublicCollectionRecord? record,
  }) async {
    final source = c, owner = source.ownerID();
    if (!source.current ||
        owner == null ||
        source.authorizationHeader() == null)
      return;
    if (record != null &&
        (public?.current != true ||
            !identical(public!.record, record) ||
            record.copyGrant?.current != true))
      return;
    final ref = record == null
        ? null
        : CollectionCopyReference(record.reference.kind, record.reference.id);
    final changes = Listenable.merge([
      source,
      if (source.identityChanges != null) source.identityChanges!,
      if (public != null) public,
    ]);
    bool current() =>
        mounted &&
        identical(c, source) &&
        source.current &&
        source.ownerID() == owner;
    final controller = CollectionCopyController(
      api: CollectionCopyApi(
        client: source.transport,
        baseUrl: source.apiBaseUrl,
        authorizationHeader: source.authorizationHeader,
      ),
      ownerID: source.ownerID,
      sourceCurrent: current,
      sourceRef: ref,
      pendingStore: _copyStore,
      identityChanges: changes,
      localDraft: ref == null ? null : source.copyLocalDrafts[ref.code],
      onRetainLocalDraft: source.retainCopyLocalDraft,
      grant: () => public?.current == true && public!.loading == false
          ? public.record?.copyGrant
          : null,
    );
    final release = _onceDispose(controller.dispose);
    String? placeName(String id) {
      final live = public?.current == true && !public!.loading
          ? public.record
          : null;
      if (live == null || !current() || controller.currentGrant == null)
        return null;
      final rows = live.entries.where(
        (e) => e['placeId'] == id && e['locationVisibility'] == 'place',
      );
      return rows.isEmpty ? null : rows.first['placeName'] as String?;
    }

    try {
      final route = MaterialPageRoute<void>(
        builder: (_) => CollectionCopyPage(
          controller: controller,
          placeNameFor: placeName,
          onReloadSource: public == null ? null : public.load,
          onOpenPrivateCopy: (saved) =>
              _editCopiedRecord(controller, saved, current),
        ),
      );
      unawaited(route.completed.then((_) => release()));
      await Navigator.of(context).push(route);
      await route.completed;
    } finally {
      release();
    }
    if (mounted && current()) await _readCopyPending();
  }

  Future<void> _editCopiedRecord(
    CollectionCopyController copy,
    PersonalCollection record,
    bool Function() current,
  ) async {
    if (!current() || !copy.current || !identical(copy.destination, record))
      return;
    final source = c;
    final editor = PersonalCollectionController(
      kind: record.kind,
      cityID: record.cityID,
      authorizationHeader: source.authorizationHeader,
      ownerID: source.ownerID,
      sourceCurrent: current,
      identityChanges: source.identityChanges,
      client: source.transport,
      apiBaseUrl: source.apiBaseUrl,
      publicationRecoveryReader: (id) => CollectionPublicationPendingStore(
        origin: source.apiBaseUrl,
        owner: source.ownerID()!,
        kind: record.kind,
        id: id,
      ).load(),
    );
    final release = _onceDispose(editor.dispose);
    try {
      if (!await editor.openID(record.id) || !mounted || !current()) return;
      final route = MaterialPageRoute<void>(
        builder: (_) => PersonalCollectionPage(
          controller: editor,
          cityName:
              widget.cityNames[record.cityID] ??
              (record.cityID == source.cityID ? widget.cityName : '副本城市尚未载入'),
          cityNames: widget.cityNames,
          copyPendingStore: _copyStore,
          onOpenPlace: record.cityID == source.cityID
              ? widget.onOpenPlace
              : null,
        ),
      );
      unawaited(route.completed.then((_) => release()));
      await Navigator.of(context).push(route);
      await route.completed;
    } finally {
      release();
    }
  }

  Future<void> _openSavedRecord(PersonalCollection record) async {
    if (!c.current || c.saving || _openingSaved) return;
    if (record.cityID == c.cityID) {
      if (await _replaceDraft() &&
          mounted &&
          c.current &&
          await c.open(record) &&
          mounted) {
        setState(() => _showSaved = false);
      }
      return;
    }
    final source = c, owner = c.ownerID();
    if (owner == null || record.ownerID != owner) return;
    bool current() =>
        mounted &&
        !_pageClosed &&
        identical(c, source) &&
        source.current &&
        source.ownerID() == owner;
    setState(() => _openingSaved = true);
    final editor = PersonalCollectionController(
      kind: record.kind,
      cityID: record.cityID,
      authorizationHeader: source.authorizationHeader,
      ownerID: source.ownerID,
      sourceCurrent: current,
      identityChanges: source.identityChanges,
      client: source.transport,
      apiBaseUrl: source.apiBaseUrl,
      controlPendingStore: source.controlPendingStore,
      createPendingStore: source.createPendingStore,
      publicationRecoveryReader: (id) => CollectionPublicationPendingStore(
        origin: source.apiBaseUrl,
        owner: owner,
        kind: record.kind,
        id: id,
      ).load(),
    );
    final release = _onceDispose(editor.dispose);
    try {
      if (!await editor.openID(record.id) || !current()) {
        if (current())
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text(editor.error ?? '这条已保存内容暂时无法打开，请重试。')),
          );
        return;
      }
      final route = MaterialPageRoute<void>(
        builder: (_) => PersonalCollectionPage(
          controller: editor,
          cityName: record.cityID.isEmpty
              ? ''
              : widget.cityNames[record.cityID] ?? '城市名称暂不可读',
          cityNames: widget.cityNames,
          copyPendingStore: _copyStore,
        ),
      );
      // Reading has finished. The child route's lifetime is not a load.
      setState(() => _openingSaved = false);
      unawaited(route.completed.then((_) => release()));
      await Navigator.of(context).push(route);
      await route.completed;
    } finally {
      release();
      if (mounted && identical(c, source))
        setState(() => _openingSaved = false);
    }
  }

  Future<void> _openLegacyReference(PersonalCollectionEntry entry) async {
    final source = c, record = c.editing, reference = entry.legacyReference;
    final owner = c.ownerID(), token = c.authorizationHeader();
    if (!c.current ||
        !entry.readable ||
        reference == null ||
        record == null ||
        owner == null ||
        owner != record.ownerID ||
        token == null)
      return;
    bool current() =>
        mounted &&
        !_pageClosed &&
        identical(c, source) &&
        source.current &&
        source.ownerID() == owner &&
        source.authorizationHeader() == token &&
        source.editing?.id == record.id &&
        source.editing?.revision == record.revision &&
        source.draftEntries.any((e) => identical(e, entry));
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => LegacyPlaceReferencePage(
          reference: reference,
          authorizationHeader: source.authorizationHeader,
          sourceCurrent: current,
          apiBaseUrl: source.apiBaseUrl,
          client: source.transport,
          identityChanges: source.identityChanges,
          ownerID: source.ownerID,
        ),
      ),
    );
  }

  Future<void> _choosePlace() async {
    if (!c.editable) return;
    _search.clear();
    unawaited(c.searchPlaces(''));
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      builder: (sheet) => AnimatedBuilder(
        animation: c,
        builder: (context, _) => FractionallySizedBox(
          heightFactor: .8,
          child: Padding(
            padding: EdgeInsets.fromLTRB(
              16,
              0,
              16,
              MediaQuery.viewInsetsOf(context).bottom + 16,
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('添加地点', style: NowUiTokens.of(context).sheetTitle),
                const SizedBox(height: 12),
                TextField(
                  controller: _search,
                  enabled: c.editable,
                  decoration: InputDecoration(
                    labelText: '查找${widget.cityName}的地点',
                    suffixIcon: IconButton(
                      tooltip: '查找地点',
                      onPressed: c.editable
                          ? () => unawaited(c.searchPlaces(_search.text))
                          : null,
                      icon: const Icon(Icons.search),
                    ),
                  ),
                  onSubmitted: c.editable ? c.searchPlaces : null,
                ),
                if (c.searching) const LinearProgressIndicator(),
                if (c.searchError != null)
                  Row(
                    children: [
                      Expanded(child: Text(c.searchError!)),
                      TextButton(
                        onPressed: c.current
                            ? () => unawaited(c.searchPlaces(_search.text))
                            : null,
                        child: const Text('重试'),
                      ),
                    ],
                  ),
                Expanded(
                  child: !c.current
                      ? Center(child: Text(c.error ?? '当前来源已变化'))
                      : c.places.isEmpty && !c.searching
                      ? const Center(child: Text('没有可添加的地点，请调整关键词。'))
                      : ListView.builder(
                          itemCount: c.places.length,
                          itemBuilder: (context, index) {
                            final p = c.places[index],
                                selected = c.draftEntries.any(
                                  (e) => e.placeID == p.id,
                                );
                            return ListTile(
                              title: Text(p.name),
                              trailing: Icon(
                                selected ? Icons.check : Icons.add,
                              ),
                              enabled:
                                  c.editable &&
                                  !selected &&
                                  c.draftEntries.length < 50,
                              onTap: () {
                                c.addPlace(p);
                                Navigator.pop(sheet);
                              },
                            );
                          },
                        ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _editNote(int index) async {
    if (!c.editable) return;
    final original = c.draftEntries[index];
    final field = TextEditingController(text: original.note);
    try {
      final note = await showDialog<String>(
        context: context,
        builder: (context) => AlertDialog(
          title: Text(
            !original.readable || original.placeName.isEmpty
                ? '地点备注'
                : original.placeName,
          ),
          content: TextField(
            controller: field,
            minLines: 2,
            maxLines: 5,
            decoration: const InputDecoration(labelText: '备注（可选）'),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: const Text('取消'),
            ),
            TextButton(
              onPressed: () => Navigator.pop(context, field.text),
              child: const Text('保留备注'),
            ),
          ],
        ),
      );
      if (note != null &&
          c.editable &&
          index < c.draftEntries.length &&
          identical(c.draftEntries[index], original))
        c.editEntry(index, note);
    } finally {
      field.dispose();
    }
  }

  Future<void> _controlAction(
    String action, {
    PersonalCollectionController? expectedSource,
    int? expectedEpoch,
  }) async {
    if (expectedSource != null && !identical(c, expectedSource) ||
        expectedEpoch != null && expectedEpoch != _pageEpoch)
      return;
    final source = c, epoch = _pageEpoch;
    if (!source.current || source.controlBusy) return;
    final CollectionControlPreview? preview = await source.prepareControl(
      action,
    );
    if (!mounted ||
        !identical(c, source) ||
        epoch != _pageEpoch ||
        preview == null ||
        !source.controlPreviewCurrent(preview))
      return;
    final ticks = ValueNotifier<int>(0);
    var dialogOpen = true;
    bool binding() =>
        mounted &&
        !_pageClosed &&
        epoch == _pageEpoch &&
        identical(c, source) &&
        source.controlPreviewCurrent(preview);
    late final DialogRoute<bool> route;
    bool live() => dialogOpen && route.isCurrent && binding();
    final timer = Timer(
      preview.expiresAt.difference(DateTime.now().toUtc()),
      () {
        if (dialogOpen) ticks.value++;
      },
    );
    route = DialogRoute<bool>(
      context: context,
      builder: (inner) => ListenableBuilder(
        listenable: Listenable.merge([source, _dialogChanges, ticks]),
        builder: (inner, _) {
          final current = live(), body = preview.input.input;
          return AlertDialog(
            title: Text(action == 'UPDATE' ? '检查修改' : '检查移除'),
            content: SingleChildScrollView(
              child: current
                  ? Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          '当前版本：' + preview.input.expectedRevision.toString(),
                        ),
                        if (body != null) ...[
                          Text(
                            body.title,
                            key: const Key('collection-control-preview-title'),
                          ),
                          if (body.summary.isNotEmpty) Text(body.summary),
                          const SizedBox(height: 12),
                          Text(
                            body.journeyDetails == null
                                ? '将保存以下 ${body.entries.length} 个地点的顺序和本人备注：'
                                : '将保存以下 ${body.journeyDetails!.itinerary.length} 项安排：',
                          ),
                          if (body.journeyDetails != null) ...[
                            Text(
                              '旅程日期：${body.journeyDetails!.startDate} ${body.journeyDetails!.endDate} ${body.journeyDetails!.dateLabel}',
                            ),
                            if (body.journeyDetails!.days > 0)
                              Text('${body.journeyDetails!.days} 天'),
                            for (final row in body.journeyDetails!.itinerary)
                              Text(
                                JourneyDetailEditor.reviewText(
                                  row,
                                  body.journeyDetails!,
                                ),
                              ),
                          ],
                          for (var i = 0; i < body.entries.length; i++)
                            Text(
                              '地点 ' +
                                  (i + 1).toString() +
                                  ' · ' +
                                  const {
                                    'hidden': '不显示位置',
                                    'city': '仅显示城市',
                                    'place': '显示地点位置',
                                  }[body.entries[i].locationVisibility]! +
                                  (body.entries[i].note.isEmpty
                                      ? ''
                                      : ' · ' + body.entries[i].note),
                            ),
                          const Text('仍为本人草稿，不会公开发布。地点资料与地图权限以当前读取为准。'),
                        ] else
                          const Text('这条已保存内容将从本人列表移除。此确认只移除当前具体版本，不影响其他内容。'),
                      ],
                    )
                  : const Text('本次确认已失效，请重新检查。本人编辑稿在当前合法身份下仍保留。'),
            ),
            actions: [
              TextButton(
                onPressed: () {
                  if (dialogOpen && route.isCurrent) {
                    dialogOpen = false;
                    Navigator.of(inner).pop(false);
                  }
                },
                child: const Text('取消'),
              ),
              FilledButton(
                key: const Key('collection-control-confirm'),
                onPressed: current
                    ? () {
                        if (live()) {
                          dialogOpen = false;
                          Navigator.of(inner).pop(true);
                        }
                      }
                    : null,
                child: Text(action == 'UPDATE' ? '确认保存修改' : '确认移除'),
              ),
            ],
          );
        },
      ),
    );
    _controlDialogs.add(route);
    final release = _onceDispose(() {
      dialogOpen = false;
      timer.cancel();
      ticks.dispose();
      _controlDialogs.remove(route);
      if (_pageClosed && _controlDialogs.isEmpty) _disposeDialogChanges();
    });
    unawaited(route.completed.then((_) => release()));
    final confirmed = await Navigator.of(context).push(route);
    await route.completed;
    if (confirmed == true && binding()) {
      await source.commitControl(preview);
    } else {
      source.cancelControlPreview(preview);
    }
  }

  Widget _createRecovery() {
    final source = c, epoch = _pageEpoch, pending = c.createPending;
    bool current() =>
        mounted &&
        identical(c, source) &&
        epoch == _pageEpoch &&
        source.current;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (!c.createReady || c.createReadFailed) ...[
          const Text('请先读取本机原保存记录；当前不会提交新操作。'),
          OutlinedButton(
            key: const Key('collection-create-read-local'),
            onPressed: current() && !source.saving
                ? () {
                    if (current()) unawaited(source.loadCreatePending());
                  }
                : null,
            child: const Text('重新读取本机原保存'),
          ),
        ],
        if (pending != null) ...[
          Semantics(
            liveRegion: true,
            child: const Text('上次新建保存结果待核实。请读取原结果，不会再次提交。'),
          ),
          OutlinedButton(
            key: ValueKey('collection-create-recover-${pending.operationID}'),
            onPressed: current() && !source.saving && !source.createReadFailed
                ? () {
                    if (current()) unawaited(source.recoverCreate());
                  }
                : null,
            child: const Text('核实原保存'),
          ),
        ],
      ],
    );
  }

  Widget _controlRecovery() {
    final source = c, epoch = _pageEpoch;
    bool current() =>
        mounted &&
        identical(c, source) &&
        epoch == _pageEpoch &&
        source.current;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (!c.controlReady || c.controlReadFailed)
          OutlinedButton(
            key: const Key('collection-control-read-local'),
            onPressed: current() && !source.controlBusy
                ? () {
                    if (current()) unawaited(source.loadControlPending());
                  }
                : null,
            child: const Text('重新读取本机原操作'),
          ),
        for (final p in {
          for (final p in c.controlPending) p.operationID: p,
          if (c.controlNotSubmitted != null)
            c.controlNotSubmitted!.operationID: c.controlNotSubmitted!,
        }.values)
          Padding(
            padding: const EdgeInsets.only(bottom: 12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  p.stage == 'PREPARED' ||
                          c.controlNotSubmitted?.same(p) == true
                      ? '上次准备尚未确认发送，请先检查本机记录。'
                      : '上次' +
                            (p.action == 'UPDATE' ? '修改' : '移除') +
                            '结果待核实，不会再次提交。',
                ),
                Wrap(
                  spacing: 8,
                  children: [
                    OutlinedButton(
                      key: ValueKey(
                        'collection-control-recover-' + p.operationID,
                      ),
                      onPressed: current() && !source.controlBusy
                          ? () {
                              if (current())
                                unawaited(
                                  p.stage == 'PREPARED' ||
                                          source.controlNotSubmitted?.same(p) ==
                                              true
                                      ? source.repairControlPrepared(p)
                                      : source.recoverControl(p),
                                );
                            }
                          : null,
                      child: Text(
                        p.stage == 'PREPARED' ||
                                c.controlNotSubmitted?.same(p) == true
                            ? '检查本机准备记录'
                            : '核实原结果',
                      ),
                    ),
                    if (p.stage == 'UNKNOWN' &&
                        c.controlNotSubmitted?.same(p) != true)
                      TextButton(
                        key: ValueKey(
                          'collection-control-stop-' + p.operationID,
                        ),
                        onPressed: current() && !source.controlBusy
                            ? () {
                                if (current())
                                  unawaited(
                                    source.recoverControl(p, stop: true),
                                  );
                              }
                            : null,
                        child: const Text('停止原操作'),
                      ),
                  ],
                ),
              ],
            ),
          ),
        if (c.controlCurrentUnread)
          OutlinedButton(
            key: const Key('collection-control-refresh-current'),
            onPressed: current() && !source.controlBusy
                ? () {
                    if (current()) unawaited(source.refreshControlTarget());
                  }
                : null,
            child: const Text('读取当前内容'),
          ),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.of(context);
    return Scaffold(
      appBar: AppBar(
        title: Text(c.label),
        actions: [
          if (c.kind == 'journey')
            IconButton(
              tooltip: '原对话记录',
              icon: const Icon(Icons.history),
              onPressed: !c.current
                  ? null
                  : () {
                      final controller = c, id = c.editing?.id;
                      Navigator.of(context).push<void>(
                        MaterialPageRoute(
                          builder: (_) => JourneyHistoryPage(
                            client: controller.transport,
                            apiBaseUrl: controller.apiBaseUrl,
                            authorizationHeader: controller.authorizationHeader,
                            ownerID: controller.ownerID,
                            current: () => controller.current,
                            identityChanges: controller,
                            journeyID: id,
                          ),
                        ),
                      );
                    },
            ),
          if (_copyPending != null)
            IconButton(
              key: const Key('collection-copy-open-recovery'),
              tooltip: '核实本人原副本操作',
              onPressed: c.current ? () => _openCopy() : null,
              icon: const Icon(Icons.pending_actions),
            ),
          if (_copyPendingFailed)
            IconButton(
              key: const Key('collection-copy-retry-local-record'),
              tooltip: '重读本人原副本记录',
              onPressed: c.current ? _readCopyPending : null,
              icon: const Icon(Icons.refresh),
            ),
          IconButton(
            tooltip: '打开分享',
            onPressed: c.current ? _openShare : null,
            icon: const Icon(Icons.link),
          ),
          IconButton(
            tooltip: _showSaved ? '返回当前草稿' : '已保存${c.label}',
            onPressed: c.current && !c.saving
                ? () => setState(() => _showSaved = !_showSaved)
                : null,
            icon: Icon(
              _showSaved ? Icons.edit_outlined : Icons.folder_open_outlined,
            ),
          ),
        ],
      ),
      body: SafeArea(
        child: !c.current
            ? Center(
                child: Padding(
                  padding: const EdgeInsets.all(24),
                  child: Text(c.error ?? '请返回后重新打开。'),
                ),
              )
            : _showSaved
            ? _saved(tokens)
            : _editor(tokens),
      ),
    );
  }

  Widget _error() => Padding(
    padding: const EdgeInsets.only(bottom: 16),
    child: Semantics(liveRegion: true, child: Text(c.error!)),
  );
  Widget _saved(NowUiTokens tokens) => ListView(
    padding: const EdgeInsets.all(16),
    children: [
      Text('已保存${c.label}', style: tokens.sheetTitle),
      const SizedBox(height: 8),
      Text('本人的草稿与已公开内容', style: tokens.metadata),
      if (c.loading || _openingSaved) const LinearProgressIndicator(),
      if (c.error != null) _error(),
      if (c.items.isEmpty && !c.loading)
        const Padding(
          padding: EdgeInsets.symmetric(vertical: 32),
          child: Text('还没有已保存内容。'),
        ),
      for (final v in c.items)
        ListTile(
          contentPadding: EdgeInsets.zero,
          title: Text(v.title.isEmpty ? '未命名${c.label}' : v.title),
          subtitle: Text(
            '${v.status == 'published' ? '已公开 · ' : '本人草稿 · '}${v.journeyDetails == null ? '${v.entries.length} 个地点' : '${v.journeyDetails!.itinerary.length} 项安排'}${v.summary.isEmpty ? '' : '\n${v.summary}'}',
          ),
          trailing: const Icon(Icons.chevron_right),
          onTap: !c.saving && !_openingSaved
              ? () => unawaited(_openSavedRecord(v))
              : null,
        ),
      Wrap(
        spacing: 12,
        children: [
          TextButton(
            onPressed: !c.loading ? () => unawaited(c.load()) : null,
            child: const Text('重新读取'),
          ),
          if (c.nextCursor != null)
            TextButton(
              onPressed: !c.loading
                  ? () => unawaited(c.load(more: true))
                  : null,
              child: const Text('加载更多'),
            ),
          FilledButton(
            onPressed: c.canSwitchDraft
                ? () async {
                    if (await _replaceDraft() && c.canSwitchDraft && mounted) {
                      c.newDraft();
                      setState(() => _showSaved = false);
                    }
                  }
                : null,
            child: Text('新建${c.label}'),
          ),
        ],
      ),
    ],
  );
  Widget _editor(NowUiTokens tokens) {
    final source = c, epoch = _pageEpoch;
    final record = source.editing;
    return ListView(
      padding: const EdgeInsets.all(16),
      keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
      children: [
        Text(
          c.editing == null
              ? '创建${c.label}'
              : c.editing!.title.isEmpty
              ? '未命名${c.label}'
              : c.editing!.title,
          style: tokens.sheetTitle,
        ),
        const SizedBox(height: 8),
        Text(
          '${widget.cityName.isEmpty ? '' : '${widget.cityName} · '}${c.editing?.status == 'published' ? '已公开，撤回公开后可修改' : '仅自己可见'}',
          style: tokens.metadata,
        ),
        if (!c.createReady || c.createReadFailed || c.createPending != null)
          _createRecovery(),
        const SizedBox(height: 20),
        if (c.kind == 'journey' &&
            c.editing != null &&
            c.editing!.visibility == 'private' &&
            c.editing!.status == 'draft')
          PrivateReferenceMediaView(
            key: ValueKey(
              'journey-original-images-${c.editing!.id}-${c.editing!.revision}',
            ),
            targetType: 'JOURNEY',
            targetID: c.editing!.id,
            authorizationHeader: source.authorizationHeader,
            ownerID: source.ownerID,
            sourceCurrent: () =>
                identical(c, source) &&
                source.current &&
                source.editing?.id == record?.id &&
                source.editing?.revision == record?.revision &&
                source.editing?.visibility == 'private',
            apiBaseUrl: source.apiBaseUrl,
            client: source.transport,
            identityChanges: Listenable.merge([
              source,
              if (source.identityChanges != null) source.identityChanges!,
            ]),
          ),
        TextField(
          key: const Key('collection-title'),
          controller: _title,
          enabled: c.editable,
          decoration: InputDecoration(labelText: '${c.label}名称'),
          onChanged: (v) => c.setText(title: v),
        ),
        const SizedBox(height: 12),
        ExpansionTile(
          tilePadding: EdgeInsets.zero,
          title: const Text('补充说明'),
          initiallyExpanded: c.summary.isNotEmpty,
          children: [
            TextField(
              controller: _summary,
              enabled: c.editable,
              minLines: 2,
              maxLines: 5,
              decoration: const InputDecoration(labelText: '说明（可选）'),
              onChanged: (v) => c.setText(summary: v),
            ),
          ],
        ),
        const SizedBox(height: 16),
        if (c.draftJourneyDetails != null)
          JourneyDetailEditor(
            value: c.draftJourneyDetails!,
            changes: c,
            current: () =>
                identical(c, source) && source.editable && epoch == _pageEpoch,
            onChanged: c.setJourneyDetails,
          )
        else ...[
          Text(
            c.kind == 'journey' ? '按顺序安排地点' : '选择地图中的地点',
            style: tokens.placeTitle,
          ),
          const SizedBox(height: 8),
          if (c.draftEntries.isEmpty) const Text('添加现有地点，再保存这份草稿。'),
          for (var index = 0; index < c.draftEntries.length; index++)
            _entry(index),
          const SizedBox(height: 8),
          Align(
            alignment: Alignment.centerLeft,
            child: OutlinedButton.icon(
              onPressed: c.editable && c.draftEntries.length < 50
                  ? _choosePlace
                  : null,
              icon: const Icon(Icons.add_location_alt_outlined),
              label: const Text('添加地点'),
            ),
          ),
        ],
        const SizedBox(height: 24),
        if (c.error != null) _error(),
        if (c.saving) const LinearProgressIndicator(),
        _controlRecovery(),
        if (c.hasPrivateLegacyReferences)
          const Padding(
            padding: EdgeInsets.only(bottom: 12),
            child: Text('这些旧地点资料仅本人可读。可修改标题、备注和顺序，或移除地点；不会加入公开地点或地图。'),
          ),
        if (c.editing?.privatePlaceCopyRestricted == true)
          const Padding(
            padding: EdgeInsets.only(bottom: 12),
            child: Text(
              '复制许可仅限私人保存。你仍可编辑本人标题、备注和顺序；先明确移除复制的地点并保存，再重新选择当前地点，才能按自己的资料发布。',
              key: Key('collection-private-copy-restriction'),
            ),
          ),
        Wrap(
          spacing: 12,
          runSpacing: 8,
          children: [
            if (c.uncertain &&
                c.createPending == null &&
                !c.createReadFailed &&
                c.controlPending.isEmpty &&
                c.controlNotSubmitted == null) ...[
              FilledButton(
                onPressed: !c.saving ? () => unawaited(c.recover()) : null,
                child: const Text('核实结果'),
              ),
              OutlinedButton(
                onPressed: !c.saving
                    ? () => unawaited(
                        c.pendingWithdrawal ? c.withdraw() : c.save(),
                      )
                    : null,
                child: const Text('按原稿重试'),
              ),
            ] else
              FilledButton(
                key: const Key('collection-save'),
                onPressed: c.editable && c.controlReady
                    ? () {
                        if (!identical(c, source) ||
                            epoch != _pageEpoch ||
                            !source.current)
                          return;
                        if (source.editing == null) {
                          unawaited(source.save());
                        } else {
                          unawaited(
                            _controlAction(
                              'UPDATE',
                              expectedSource: source,
                              expectedEpoch: epoch,
                            ),
                          );
                        }
                      }
                    : null,
                child: Text(c.editing == null ? '保存本人草稿' : '保存修改'),
              ),
            if (c.editing != null &&
                !c.uncertain &&
                !c.hasPrivateLegacyReferences &&
                widget.onViewMap != null)
              OutlinedButton.icon(
                onPressed: c.current && !c.saving && !c.dirty
                    ? () {
                        if (!mounted ||
                            !identical(c, source) ||
                            epoch != _pageEpoch ||
                            !source.current ||
                            source.saving ||
                            source.dirty ||
                            source.uncertain ||
                            source.hasPrivateLegacyReferences ||
                            record == null ||
                            !identical(source.editing, record) ||
                            source.editing?.revision != record.revision)
                          return;
                        widget.onViewMap?.call(record);
                      }
                    : null,
                icon: const Icon(Icons.map_outlined),
                label: const Text('在地图查看'),
              ),
            if (c.editing != null &&
                !c.uncertain &&
                c.draftJourneyDetails == null)
              OutlinedButton.icon(
                key: const Key('collection-publication'),
                onPressed:
                    c.current &&
                        !c.saving &&
                        !c.controlBlocked &&
                        (!c.hasPrivateLegacyReferences ||
                            c.publicationPending != null) &&
                        (!c.editing!.privatePlaceCopyRestricted ||
                            c.editing!.status == 'published' ||
                            c.publicationPending != null)
                    ? _publication
                    : null,
                icon: const Icon(Icons.public),
                label: Text(
                  c.editing!.status == 'published' ||
                          c.publicationPending != null
                      ? '查看公开状态'
                      : '预览公开发布',
                ),
              ),
            if (c.editing != null && !c.uncertain)
              TextButton(
                onPressed: c.editable && c.controlReady
                    ? () => _controlAction(
                        'WITHDRAW',
                        expectedSource: source,
                        expectedEpoch: epoch,
                      )
                    : null,
                child: const Text('移除已保存内容'),
              ),
          ],
        ),
      ],
    );
  }

  Widget _entry(int index) {
    final e = c.draftEntries[index];
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: c.kind == 'journey'
                ? Text('${index + 1}', style: NowUiTokens.of(context).body)
                : const Icon(Icons.place_outlined),
            title: Text(e.readable ? e.placeName : '地点来源已变化，请重新读取'),
            subtitle: e.note.isEmpty || e.legacyReference != null && !e.readable
                ? null
                : Text(e.note),
            onTap: e.legacyReference != null && e.readable
                ? () => unawaited(_openLegacyReference(e))
                : e.sourceCurrent && widget.onOpenPlace != null
                ? () => widget.onOpenPlace!(e.placeID)
                : null,
          ),
          Wrap(
            spacing: 4,
            children: [
              TextButton(
                onPressed: c.editable
                    ? () => unawaited(_editNote(index))
                    : null,
                child: const Text('备注'),
              ),
              if (c.kind == 'journey' || c.hasPrivateLegacyReferences) ...[
                IconButton(
                  tooltip: '向前移动地点',
                  onPressed: c.editable && index > 0
                      ? () => c.moveEntry(index, -1)
                      : null,
                  icon: const Icon(Icons.arrow_upward),
                ),
                IconButton(
                  tooltip: '向后移动地点',
                  onPressed: c.editable && index + 1 < c.draftEntries.length
                      ? () => c.moveEntry(index, 1)
                      : null,
                  icon: const Icon(Icons.arrow_downward),
                ),
              ],
              IconButton(
                tooltip: '移除地点',
                onPressed: c.editable ? () => c.removeEntry(index) : null,
                icon: const Icon(Icons.close),
              ),
            ],
          ),
        ],
      ),
    );
  }
}
