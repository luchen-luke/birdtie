import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import '../content/moment_control.dart';
import 'human_causal_control.dart';
import 'moment_thread_api.dart';
import 'moment_thread_pending_store.dart';
import 'public_moment_media.dart';
import '../content/moment_locations.dart';

class MomentThreadController extends ChangeNotifier {
  MomentThreadController({
    required this.api,
    required this.ownerID,
    MomentThreadJournal? journal,
  }) : journal = journal ?? MomentThreadJournal(),
       origin = api.origin;
  final MomentThreadAPI api;
  final String ownerID, origin;
  final MomentThreadJournal journal;
  List<MomentThreadDraftEntry> _entries = [];
  List<MomentThreadDraftEntry> get entries => List.unmodifiable(_entries);
  int activeIndex = 0;
  String localScope = '';
  String ownedThreadID = '';
  int ownedThreadVersion = 0;
  String baseMomentID = '';
  final List<Map<String, Object>> removedItems = [];
  final List<MomentThreadDraftEntry> removedEntries = [];
  String activeField = 'body';
  OwnedMomentThread? ownedSource;
  bool get existingThread => ownedThreadID.isNotEmpty;
  bool busy = false, _retired = false, _disposed = false;
  bool unsavedDraft = false;
  bool _restoreFailed = false;
  int _draftWriteGeneration = 0;
  int _epoch = 0;
  String? notice;
  MomentThreadPreview? previewValue;
  MomentThreadReceipt? receipt;
  MomentThreadPending? pending;
  String? _approvalAuthorization;
  Timer? _lease;
  Timer? _ownedLease;
  Future<void> _saving = Future<void>.value();
  bool get current =>
      !_retired &&
      !_disposed &&
      humanControlID(ownerID) &&
      api.current() &&
      api.origin == origin;
  bool get uncertain => pending != null && previewValue == null;
  bool get editable =>
      current && !busy && pending == null && previewValue == null;
  String get activeText => _entries.isEmpty
      ? ''
      : activeField == 'title'
      ? _entries[activeIndex].title
      : _entries[activeIndex].text;
  MomentThreadJournalState get _state => MomentThreadJournalState(
    localScope,
    entries.map((entry) => entry.copy(media: const [])).toList(),
    activeIndex,
    pending,
    threadID: ownedThreadID,
    threadVersion: ownedThreadVersion,
    baseMomentID: baseMomentID,
    removedItems: List.unmodifiable(removedItems),
    removedEntries: removedEntries.map((v) => v.copy(media: const [])).toList(),
  );
  bool _same(int epoch) => current && epoch == _epoch;
  void _notify() {
    if (!_disposed) {
      notifyListeners();
    }
  }

  void invalidateApproval() {
    if (_disposed || _retired || busy || previewValue == null) return;
    previewValue = null;
    _approvalAuthorization = null;
    _lease?.cancel();
    notice = '登录状态已更新；本机材料保留，请核实原操作或停止后重新检查。';
    _notify();
  }

  Future<void> begin({
    String cityID = '',
    String placeID = '',
    String initialText = '',
    MomentThreadDraftEntry? initialEntry,
    String? materialScope,
  }) async {
    if (!current || busy || pending != null) {
      return;
    }
    final restored = await restore();
    if (!current || restored || uncertain || _restoreFailed) {
      return;
    }
    ownedThreadID = '';
    ownedThreadVersion = 0;
    ownedSource = null;
    removedItems.clear();
    removedEntries.clear();
    localScope = materialScope ?? newMomentOperationID();
    _entries = [
      initialEntry ??
          MomentThreadDraftEntry(
            localID: newMomentOperationID(),
            cityID: cityID,
            placeID: placeID,
            text: initialText,
            locations: cityID.isEmpty && placeID.isEmpty ? const [] : null,
          ),
    ];
    activeIndex = 0;
    baseMomentID = _entries.first.momentID;
    _saveDraft();
    _notify();
  }

  Future<bool> beginOwned(
    OwnedMomentThread view,
    List<MomentThreadDraftEntry> entries, {
    String? selectedID,
  }) async {
    if (!current || busy || pending != null || !view.current || entries.isEmpty)
      return false;
    final available = view.members
        .where((member) => member['status'] == 'AVAILABLE')
        .toList();
    if (available.isEmpty) {
      if (entries.length != 1 ||
          entries.single.originalStatus != 'withdrawn' ||
          !view.retiredMembers.any(
            (member) =>
                member['momentId'] == entries.single.momentID &&
                member['revision'] == entries.single.expectedRevision,
          )) {
        return false;
      }
    } else {
      if (entries.length != available.length) return false;
      for (var i = 0; i < entries.length; i++) {
        if (entries[i].momentID != available[i]['momentId'] ||
            entries[i].expectedRevision != available[i]['revision']) {
          return false;
        }
      }
    }
    final restored = await restore();
    if (!current || pending != null || _restoreFailed) return false;
    if (restored && _entries.isNotEmpty && ownedThreadID != view.threadID) {
      notice = '另一个串文草稿已恢复，请先处理后再打开原串文。';
      _notify();
      return false;
    }
    if (restored && ownedThreadID == view.threadID) {
      if (ownedThreadVersion != view.version ||
          entries.any(
            (source) =>
                !_entries.any(
                  (local) =>
                      local.momentID == source.momentID &&
                      local.expectedRevision == source.expectedRevision,
                ) &&
                !removedItems.any(
                  (v) =>
                      v['momentId'] == source.momentID &&
                      v['expectedRevision'] == source.expectedRevision,
                ),
          )) {
        notice = '原动态已变化，本机文字保留；请先核对当前动态。';
        _notify();
        return false;
      }
    } else {
      _entries = List.of(entries);
      localScope = newMomentOperationID();
    }
    ownedThreadID = view.threadID;
    baseMomentID = '';
    ownedThreadVersion = view.version;
    _bindOwnedSource(view);
    final selected = _entries.indexWhere((e) => e.momentID == selectedID);
    activeIndex = selected < 0 ? 0 : selected;
    _saveDraft();
    _notify();
    return true;
  }

  void savedOwnedEntry(MomentThreadDraftEntry updated, OwnedMomentThread view) {
    if (!editable || !existingThread || view.threadID != ownedThreadID) return;
    final index = _entries.indexWhere((e) => e.momentID == updated.momentID);
    if (index < 0) return;
    _entries[index] = updated.copy(media: _entries[index].media);
    ownedThreadVersion = view.version;
    _bindOwnedSource(view);
    notice = '这条动态已保存，原串文顺序保留。';
    _saveDraft();
    _notify();
  }

  void _bindOwnedSource(OwnedMomentThread view) {
    ownedSource = view;
    _ownedLease?.cancel();
    _ownedLease = Timer(view.validUntil.difference(DateTime.now().toUtc()), () {
      if (current && identical(ownedSource, view)) {
        ownedSource = null;
        _notify();
      }
    });
  }

  Future<bool> restore() async {
    if (!current || busy) {
      return false;
    }
    final epoch = _epoch;
    _restoreFailed = false;
    busy = true;
    _notify();
    try {
      final state = await journal.load(ownerID, origin);
      if (!_same(epoch)) {
        return false;
      }
      if (state != null) {
        localScope = state.localScope;
        ownedThreadID = state.threadID;
        ownedThreadVersion = state.threadVersion;
        baseMomentID = state.baseMomentID;
        removedItems
          ..clear()
          ..addAll(state.removedItems);
        removedEntries
          ..clear()
          ..addAll(state.removedEntries);
        // Media is reconstructed by its original stage operations. A private
        // journal cannot restore a use-rights declaration or publication grant.
        _entries = state.entries
            .map((entry) => entry.copy(media: const []))
            .toList();
        activeIndex = state.activeIndex;
        pending = state.pending;
        if (pending != null) {
          notice = '原串文操作需要核实；本机记录不代表已经发布。';
        }
        return state.entries.isNotEmpty || pending != null;
      }
    } catch (_) {
      if (_same(epoch)) {
        _restoreFailed = true;
        notice = '本机私人草稿暂不可读取，请重试。';
      }
    } finally {
      if (_same(epoch)) {
        busy = false;
        _notify();
      }
    }
    return false;
  }

  void _saveDraft() {
    final state = _state, epoch = _epoch;
    final writeGeneration = ++_draftWriteGeneration;
    unsavedDraft = true;
    _saving = _saving.then((_) async {
      if (!_same(epoch)) {
        return;
      }
      try {
        await journal.saveDraft(ownerID, origin, state);
        if (_same(epoch) && writeGeneration == _draftWriteGeneration) {
          unsavedDraft = false;
          _notify();
        }
      } catch (_) {
        if (_same(epoch)) {
          notice = '本机草稿未能保存或超过1MiB；当前输入仍保留。';
          _notify();
        }
      }
    });
  }

  Future<bool> flushDraft() async {
    await _saving;
    return current && !unsavedDraft;
  }

  Future<void> saveLocal() async {
    if (!editable) return;
    _saveDraft();
    await _saving;
    if (!current) return;
    notice = unsavedDraft ? '尚未保存到本机，文字仍保留，请稍后重试。' : '本机草稿已保存，尚未公开新改动。';
    _notify();
  }

  void approveRestoreActive() {
    if (!editable || _entries.isEmpty) return;
    final entry = _entries[activeIndex];
    if (entry.originalStatus != 'withdrawn') return;
    _entries[activeIndex] = entry.copy(
      restore: true,
      preserveMedia: false,
      locations: entry.locations,
    );
    _saveDraft();
    _notify();
  }

  void restoreEntry(MomentThreadDraftEntry entry) {
    if (!editable ||
        entries.length >= 20 ||
        entry.momentID.isEmpty ||
        _entries.any((v) => v.momentID == entry.momentID))
      return;
    removedItems.removeWhere((v) => v['momentId'] == entry.momentID);
    removedEntries.removeWhere((v) => v.momentID == entry.momentID);
    _entries.add(
      entry.copy(
        restore: true,
        preserveMedia: false,
        media: const [],
        materialReferences: const [],
      ),
    );
    activeIndex = _entries.length - 1;
    _saveDraft();
    _notify();
  }

  void updateActiveText(String text) {
    if (!editable || _entries.isEmpty) {
      return;
    }
    _entries[activeIndex] = activeField == 'title'
        ? _entries[activeIndex].copy(title: text)
        : _entries[activeIndex].copy(text: text);
    notice = null;
    _saveDraft();
    _notify();
  }

  void selectField(String field) {
    if (!editable || !const {'body', 'title'}.contains(field)) return;
    activeField = field;
    _notify();
  }

  void updateActiveTitle(String title) {
    if (!editable || _entries.isEmpty) return;
    _entries[activeIndex] = _entries[activeIndex].copy(title: title);
    _saveDraft();
    _notify();
  }

  void setActiveContext({required String cityID, String placeID = ''}) {
    if (!editable || _entries.isEmpty) {
      return;
    }
    _entries[activeIndex] = _entries[activeIndex].copy(
      cityID: cityID,
      placeID: placeID,
    );
    _saveDraft();
    _notify();
  }

  void setActiveMedia(List<PublicMomentMediaSelection> media) {
    if (!editable || _entries.isEmpty) {
      return;
    }
    _entries[activeIndex] = _entries[activeIndex].copy(
      media: media,
      preserveMedia: false,
    );
    _saveDraft();
    _notify();
  }

  void rememberEntryMaterials(
    String entryID,
    List<MomentDraftMaterialReference> references,
  ) {
    if (!editable) return;
    final index = _entries.indexWhere((v) => v.localID == entryID);
    if (index < 0) return;
    if (references.length > 9 ||
        references.map((v) => v.operationID).toSet().length !=
            references.length ||
        references.any(
          (v) =>
              !humanControlID(v.operationID) ||
              !const {'IMAGE', 'VIDEO'}.contains(v.kind),
        ) ||
        references.any((v) => v.kind == 'VIDEO') && references.length != 1) {
      // A recovered, unidentified placeholder is not evidence that it is the
      // same file as a new stage. Keep the last valid journal until the user
      // explicitly removes that placeholder; never write an invalid bundle.
      notice = '还有未完成材料，请先明确移除后再准备新材料；原草稿仍保留。';
      _notify();
      return;
    }
    final old = _entries[index];
    if (jsonEncode(old.materialReferences.map((v) => v.json).toList()) ==
        jsonEncode(references.map((v) => v.json).toList()))
      return;
    _entries[index] = old.copy(materialReferences: references);
    _saveDraft();
    _notify();
  }

  void removeMissingMaterial(String entryID, String operationID) {
    final entry = _entries.where((v) => v.localID == entryID).firstOrNull;
    if (entry == null) return;
    rememberEntryMaterials(
      entryID,
      entry.materialReferences
          .where((v) => v.operationID != operationID)
          .toList(),
    );
  }

  void setActiveLocations(List<MomentLocationInput> locations) {
    if (!editable || _entries.isEmpty) return;
    List<MomentLocationInput> checked;
    try {
      checked = decodeMomentLocationInputs(
        locations.map((v) => v.toJson()).toList(),
      );
    } on FormatException catch (error) {
      notice = error.message;
      _notify();
      return;
    }
    _entries[activeIndex] = _entries[activeIndex].copy(
      cityID: '',
      placeID: '',
      locations: checked,
    );
    notice = null;
    _saveDraft();
    _notify();
  }

  void addActiveLocation(MomentLocationInput location) {
    if (!editable || _entries.isEmpty) return;
    final entry = _entries[activeIndex];
    final existing =
        entry.locations ??
        [
          if (entry.placeID.isNotEmpty)
            MomentLocationInput(
              id: newMomentOperationID(),
              kind: 'PLACE',
              placeID: entry.placeID,
              precision: 'place',
            )
          else if (entry.cityID.isNotEmpty)
            MomentLocationInput(
              id: newMomentOperationID(),
              kind: 'CITY',
              cityID: entry.cityID,
              precision: 'city',
            ),
        ];
    setActiveLocations([...existing, location]);
  }

  void selectEntry(int index) {
    if (editable && index >= 0 && index < _entries.length) {
      activeIndex = index;
      activeField = 'body';
      _saveDraft();
      _notify();
    }
  }

  void addEntry() {
    if (!editable || _entries.length >= 20 || _entries.isEmpty) {
      return;
    }
    _entries.add(
      MomentThreadDraftEntry(
        localID: newMomentOperationID(),
        cityID: '',
        locations: const [],
      ),
    );
    activeIndex = _entries.length - 1;
    activeField = 'body';
    _saveDraft();
    _notify();
  }

  void removeEntry(int index, {MomentThreadDraftEntry? summary}) {
    if (!editable ||
        _entries.length <= 1 ||
        index < 0 ||
        index >= _entries.length) {
      return;
    }
    final selected = _entries[activeIndex].localID;
    final removed = _entries.removeAt(index);
    if (removed.momentID.isNotEmpty) {
      removedItems.add({
        'momentId': removed.momentID,
        'expectedRevision': removed.expectedRevision,
      });
      removedEntries.add((summary ?? removed).copy(media: const []));
    }
    activeIndex = _entries.indexWhere((entry) => entry.localID == selected);
    activeField = 'body';
    if (activeIndex < 0) {
      activeIndex = index < _entries.length ? index : _entries.length - 1;
    }
    _saveDraft();
    _notify();
  }

  void moveEntry(int from, int to) {
    if (!editable ||
        from < 0 ||
        from >= _entries.length ||
        to < 0 ||
        to >= _entries.length ||
        from == to) {
      return;
    }
    final selected = _entries[activeIndex].localID;
    final entry = _entries.removeAt(from);
    _entries.insert(to, entry);
    activeIndex = _entries.indexWhere((entry) => entry.localID == selected);
    _saveDraft();
    _notify();
  }

  Future<void> preview() async {
    if (!editable) {
      return;
    }
    if (removedItems.any(
      (removed) => !removedEntries.any(
        (entry) =>
            entry.momentID == removed['momentId'] &&
            entry.expectedRevision == removed['expectedRevision'],
      ),
    )) {
      notice = '待移除内容需要重新读取具体摘要；本次尚未提交。';
      _notify();
      return;
    }
    await _preview(
      MomentThreadInput(
        operationID: newMomentOperationID(),
        action: 'EDIT',
        threadID: ownedThreadID,
        baseMomentID: baseMomentID,
        removedItems: removedItems,
        expectedVersion: ownedThreadVersion,
        localScope: localScope,
        entries: entries,
      ),
    );
  }

  Future<void> previewWithdraw(String threadID) async {
    if (!current || busy || pending != null) {
      return;
    }
    final epoch = _epoch;
    busy = true;
    _notify();
    try {
      final view = await api.view(threadID, byMoment: false);
      if (!_same(epoch)) {
        return;
      }
      if (!view.current || !view.canWithdraw) {
        throw StateError('当前身份不能撤回这串内容');
      }
      busy = false;
      if (localScope.isEmpty) {
        localScope = newMomentOperationID();
      }
      await _preview(
        MomentThreadInput(
          operationID: newMomentOperationID(),
          action: 'WITHDRAW',
          threadID: threadID,
          expectedVersion: view.version,
        ),
      );
    } catch (_) {
      if (_same(epoch)) {
        notice = '当前整串内容暂不可撤回，请重新读取。';
      }
    } finally {
      if (_same(epoch)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> _preview(MomentThreadInput input) async {
    final epoch = _epoch;
    busy = true;
    notice = null;
    _notify();
    try {
      input.validate();
      if (utf8.encode(jsonEncode(_state.json)).length >
          momentThreadJournalMaxBytes - 500) {
        throw const FormatException('草稿超出本机1MiB保存限额');
      }
      await _saving;
      if (!_same(epoch)) {
        return;
      }
      final authorization = api.authorizationHeader();
      final value = await api.preview(input);
      if (!_same(epoch) || authorization != api.authorizationHeader()) {
        return;
      }
      final record = MomentThreadPending(
        input.operationID,
        input.action,
        input.digest,
        value.sourceVersion,
        'PREPARED',
      );
      await journal.prepare(ownerID, origin, _state, record);
      if (!_same(epoch)) {
        return;
      }
      pending = record;
      unsavedDraft = false;
      previewValue = value;
      _approvalAuthorization = authorization;
      _lease?.cancel();
      _lease = Timer(value.expiresAt.difference(DateTime.now().toUtc()), () {
        if (_same(epoch) && identical(previewValue, value)) {
          previewValue = null;
          _approvalAuthorization = null;
          notice = '具体批准已到期，请核实原操作或停止后重新预览。';
          _notify();
        }
      });
    } catch (error) {
      if (_same(epoch)) {
        notice = error is FormatException ? error.message : '串文预览失败；草稿仍保留，请重试。';
      }
    } finally {
      if (_same(epoch)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> cancelPreview() async {
    if (!current || busy || previewValue == null || pending == null) {
      return;
    }
    final epoch = _epoch, record = pending!;
    busy = true;
    _notify();
    try {
      await journal.finish(ownerID, origin, record);
      if (!_same(epoch)) {
        return;
      }
      pending = null;
      previewValue = null;
      _approvalAuthorization = null;
      _lease?.cancel();
      notice = null;
    } catch (_) {
      if (_same(epoch)) {
        notice = '本机原记录暂不可更新，请先核实。';
      }
    } finally {
      if (_same(epoch)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> confirm() async {
    final value = previewValue, record = pending;
    if (!current ||
        busy ||
        value == null ||
        record == null ||
        !value.current ||
        _approvalAuthorization != api.authorizationHeader()) {
      previewValue = null;
      _notify();
      return;
    }
    final epoch = _epoch;
    busy = true;
    _notify();
    try {
      await journal.markUnknown(ownerID, origin, record);
      if (!_same(epoch) ||
          !identical(previewValue, value) ||
          !value.current ||
          _approvalAuthorization != api.authorizationHeader()) {
        previewValue = null;
        return;
      }
      pending = record.unknown;
      previewValue = null;
      _approvalAuthorization = null;
      _lease?.cancel();
      final result = await api.commit(value);
      if (!_same(epoch)) {
        return;
      }
      await _finish(record, result, epoch);
    } catch (_) {
      if (_same(epoch)) {
        previewValue = null;
        notice = '结果尚未核实。请读取原操作结果；不要再次发布。';
      }
    } finally {
      if (_same(epoch)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> recover({bool stop = false}) async {
    final record = pending;
    if (!current || busy || record == null) {
      return;
    }
    final epoch = _epoch;
    busy = true;
    previewValue = null;
    _notify();
    try {
      final result = await api.recover(
        record.operationID,
        stop: stop,
        digest: record.digest,
        action: record.action,
      );
      if (!_same(epoch)) {
        return;
      }
      await _finish(record, result, epoch);
    } catch (error) {
      if (_same(epoch)) {
        notice = error is MomentThreadHTTPFailure && error.status == 404
            ? '尚未找到原操作事实；可停止原操作后重新预览。'
            : '原操作暂不可核实；本机记录仍保留。';
      }
    } finally {
      if (_same(epoch)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> _finish(
    MomentThreadPending record,
    MomentThreadReceipt result,
    int epoch,
  ) async {
    List<MomentThreadDraftEntry>? edited;
    MomentThreadJournalState? replacement;
    if (result.status == 'SUCCEEDED' && result.action == 'EDIT') {
      if (result.members.length != _entries.length)
        throw StateError('原动态结果条数不符');
      edited = List.generate(_entries.length, (i) {
        final entry = _entries[i], member = result.members[i];
        if (entry.momentID.isNotEmpty && entry.momentID != member.momentID) {
          throw StateError('原动态结果身份不符');
        }
        return entry.copy(
          momentID: member.momentID,
          expectedRevision: member.revision,
          originalStatus: 'published',
          preserveMedia: true,
          restore: false,
          media: const [],
          materialReferences: const [],
        );
      });
      replacement = MomentThreadJournalState(
        localScope,
        edited,
        activeIndex,
        null,
        threadID: result.threadID,
        threadVersion: result.appliedVersion,
      );
    } else if (result.status == 'SUCCEEDED' && result.action == 'WITHDRAW') {
      final withdrawn = {
        for (final member in result.members) member.momentID: member,
      };
      edited = _entries.map((entry) {
        final member = withdrawn[entry.momentID];
        return member == null
            ? entry
            : entry.copy(
                expectedRevision: member.revision,
                originalStatus: 'withdrawn',
                preserveMedia: false,
                locations: const [],
                media: const [],
                restore: false,
              );
      }).toList();
      replacement = MomentThreadJournalState(
        localScope,
        edited,
        activeIndex,
        null,
        threadID: result.threadID,
        threadVersion: result.appliedVersion,
      );
    }
    await journal.finish(
      ownerID,
      origin,
      record,
      replacement: replacement,
      clearDraft:
          result.status == 'SUCCEEDED' &&
          {'CREATE_PUBLISH', 'PUBLISH'}.contains(result.action),
    );
    if (!_same(epoch)) {
      return;
    }
    receipt = result;
    pending = null;
    unsavedDraft = false;
    previewValue = null;
    _approvalAuthorization = null;
    _lease?.cancel();
    if (edited != null) {
      _entries = edited;
      ownedThreadID = result.threadID;
      ownedThreadVersion = result.appliedVersion;
      baseMomentID = '';
      removedItems.clear();
      removedEntries.clear();
      ownedSource = null;
    }
    if (result.status == 'SUCCEEDED' &&
        {'CREATE_PUBLISH', 'PUBLISH'}.contains(result.action)) {
      _entries = [];
      activeIndex = 0;
      ownedThreadID = '';
      ownedThreadVersion = 0;
      ownedSource = null;
    }
    notice = result.status == 'STOPPED'
        ? '原串文操作已停止；草稿仍保留。'
        : result.action == 'WITHDRAW'
        ? '整串已撤回，每条原位置仍保留。'
        : result.action == 'EDIT'
        ? (result.members.length == 1
              ? '这条动态已发布，可以继续修改或再接一条。'
              : '全部动态已一次保存，可以继续修改或再接一条。')
        : '整串已一次发布。';
  }

  void retire() {
    if (_retired) {
      return;
    }
    _retired = true;
    _ownedLease?.cancel();
    _epoch++;
    _lease?.cancel();
    previewValue = null;
    _approvalAuthorization = null;
    _entries = [];
    ownedSource = null;
    receipt = null;
    pending = null;
    unawaited(journal.retire(ownerID, origin).catchError((_) {}));
    _notify();
  }

  @override
  void dispose() {
    retire();
    _disposed = true;
    super.dispose();
  }
}
