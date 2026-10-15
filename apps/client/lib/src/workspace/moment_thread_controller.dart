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
  OwnedMomentThread? ownedSource;
  bool get existingThread => ownedThreadID.isNotEmpty;
  bool busy = false, _retired = false, _disposed = false;
  bool unsavedDraft = false;
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
  String get activeText => _entries.isEmpty ? '' : _entries[activeIndex].text;
  MomentThreadJournalState get _state => MomentThreadJournalState(
    localScope,
    entries.map((entry) => entry.copy(media: const [])).toList(),
    activeIndex,
    pending,
    threadID: ownedThreadID,
    threadVersion: ownedThreadVersion,
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
  }) async {
    if (!current || busy || pending != null) {
      return;
    }
    final restored = await restore();
    if (!current || restored || uncertain) {
      return;
    }
    localScope = newMomentOperationID();
    _entries = [
      MomentThreadDraftEntry(
        localID: newMomentOperationID(),
        cityID: cityID,
        placeID: placeID,
        text: initialText,
        locations: cityID.isEmpty && placeID.isEmpty ? const [] : null,
      ),
      MomentThreadDraftEntry(
        localID: newMomentOperationID(),
        cityID: cityID,
        placeID: placeID,
        locations: cityID.isEmpty && placeID.isEmpty ? const [] : null,
      ),
    ];
    activeIndex = 0;
    _saveDraft();
    _notify();
  }

  Future<bool> beginOwned(
    OwnedMomentThread view,
    List<MomentThreadDraftEntry> entries, {
    String? selectedID,
  }) async {
    if (!current ||
        busy ||
        pending != null ||
        !view.current ||
        entries.length != view.members.length ||
        entries.isEmpty)
      return false;
    for (var i = 0; i < entries.length; i++) {
      if (entries[i].momentID != view.members[i]['momentId'] ||
          entries[i].expectedRevision != view.members[i]['revision'])
        return false;
    }
    final restored = await restore();
    if (!current || pending != null) return false;
    if (restored && _entries.isNotEmpty && ownedThreadID != view.threadID) {
      notice = '另一个串文草稿已恢复，请先处理后再打开原串文。';
      _notify();
      return false;
    }
    if (restored && ownedThreadID == view.threadID) {
      if (_entries.length != entries.length ||
          List.generate(
            entries.length,
            (i) =>
                _entries[i].momentID != entries[i].momentID ||
                _entries[i].expectedRevision != entries[i].expectedRevision,
          ).any((v) => v)) {
        notice = '原动态已变化，本机文字保留；请先核对当前动态。';
        _notify();
        return false;
      }
    } else {
      _entries = List.of(entries);
      localScope = newMomentOperationID();
    }
    ownedThreadID = view.threadID;
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
          notice = '本机草稿未能保存或超过16KiB；当前输入仍保留。';
          _notify();
        }
      }
    });
  }

  void updateActiveText(String text) {
    if (!editable || _entries.isEmpty) {
      return;
    }
    _entries[activeIndex] = _entries[activeIndex].copy(text: text);
    notice = null;
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
    _entries[activeIndex] = _entries[activeIndex].copy(media: media);
    _saveDraft();
    _notify();
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
      _saveDraft();
      _notify();
    }
  }

  void addEntry() {
    if (!editable ||
        existingThread ||
        _entries.length >= 20 ||
        _entries.isEmpty) {
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
    _saveDraft();
    _notify();
  }

  void removeEntry(int index) {
    if (!editable ||
        existingThread ||
        _entries.length <= 2 ||
        index < 0 ||
        index >= _entries.length) {
      return;
    }
    final selected = _entries[activeIndex].localID;
    _entries.removeAt(index);
    activeIndex = _entries.indexWhere((entry) => entry.localID == selected);
    if (activeIndex < 0) {
      activeIndex = index < _entries.length ? index : _entries.length - 1;
    }
    _saveDraft();
    _notify();
  }

  void moveEntry(int from, int to) {
    if (!editable ||
        existingThread ||
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
    await _preview(
      MomentThreadInput(
        operationID: newMomentOperationID(),
        action: existingThread ? 'PUBLISH' : 'CREATE_PUBLISH',
        threadID: ownedThreadID,
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
        throw const FormatException('草稿超出本机16KiB保存限额');
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
    await journal.finish(
      ownerID,
      origin,
      record,
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
