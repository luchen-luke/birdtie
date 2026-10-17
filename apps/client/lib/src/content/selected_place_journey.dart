import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import '../workspace/selected_place_followup.dart';
import 'collection_control.dart';
import 'collection_control_pending_store.dart';
import 'collection_publication_pending_store.dart';
import 'journey_detail.dart';
import 'personal_collection_controller.dart';

/// A current selector becomes editable material, never an approval or a write.
JourneyDetails selectedPlaceJourneyDraft({
  required JourneyDetails original,
  required String entryID,
  required String placeID,
  required String cityID,
  required String name,
  required int day,
}) {
  if (day != 2 ||
      cityID.isEmpty ||
      name.trim().isEmpty ||
      original.itinerary.any(
        (e) => e.text('placeId') == placeID && e.day('dayIndex') == day,
      )) {
    throw const FormatException('这个地点已在第2天，或地点资料已变化。');
  }
  final row = JourneyEntry.decode(
    {
      'id': entryID,
      'entryType': 'ARRANGEMENT',
      'position': original.itinerary.length + 1,
      for (final field in journeyTextFields) field: '',
      for (final field in journeyDayFields) field: null,
      'title': name,
      'placeId': placeID,
      'cityId': cityID,
      'kind': 'place',
      'dayIndex': day,
    },
    original.itinerary.length + 1,
    input: true,
  );
  return original.changed(
    days: original.days < day ? day : original.days,
    itinerary: [...original.itinerary, row],
  );
}

class SelectedPlaceJourneyReceipt {
  const SelectedPlaceJourneyReceipt(this.id, this.revision, this.title);
  final String id, title;
  final int revision;
}

/// Uses the existing Journey editor and its create/control/recovery ports.
/// This adapter only chooses, stages, and verifies their authoritative result.
class SelectedPlaceJourneyController extends ChangeNotifier {
  SelectedPlaceJourneyController({
    required this.selection,
    required this.day,
    required this.ownerID,
    required this.authorizationHeader,
    required this.sourceCurrent,
    required this.identityChanges,
    required this.client,
    required this.apiBaseUrl,
    PersonalCollectionRetainedLocalDraft? retainedLocalDraft,
    ValueChanged<PersonalCollectionRetainedLocalDraft>? onRetainLocalDraft,
    CollectionControlPendingStore? controlPendingStore,
    Future<Map<String, dynamic>?> Function(String id)?
    publicationRecoveryReader,
  }) : _owner = ownerID(),
       _token = authorizationHeader() {
    if (_owner == null || _token == null || day != 2 || !selection.current) {
      throw ArgumentError('请重新选择当前地点。');
    }
    if (retainedLocalDraft != null &&
        (retainedLocalDraft.ownerID != _owner ||
            retainedLocalDraft.cityID != selection.cityID ||
            retainedLocalDraft.apiBaseUrl != apiBaseUrl ||
            retainedLocalDraft.kind != 'journey')) {
      throw ArgumentError('原操作属于其他账号、城市或来源。');
    }
    collections = PersonalCollectionController(
      kind: 'journey',
      cityID: selection.cityID,
      ownerID: ownerID,
      authorizationHeader: authorizationHeader,
      sourceCurrent: () => current,
      identityChanges: identityChanges,
      client: client,
      apiBaseUrl: apiBaseUrl,
      controlPendingStore: controlPendingStore,
      retainedLocalDraft: retainedLocalDraft,
      onRetainLocalDraft: onRetainLocalDraft,
      publicationRecoveryReader:
          publicationRecoveryReader ??
          (id) => CollectionPublicationPendingStore(
            origin: apiBaseUrl,
            owner: _owner!,
            kind: 'journey',
            id: id,
          ).load(),
    );
    collections.addListener(_changed);
    identityChanges.addListener(_boundary);
    if (retainedLocalDraft?.unknown == true) {
      recoveringOriginal = true;
      editing = true;
    }
  }

  final SelectedPlaceFollowup selection;
  final int day;
  final String? Function() ownerID, authorizationHeader;
  final bool Function() sourceCurrent;
  final Listenable identityChanges;
  final http.Client client;
  final String apiBaseUrl;
  final String? _owner, _token;
  late final PersonalCollectionController collections;
  bool _disposed = false, _retired = false, _verifyQueued = false;
  bool busy = false, editing = false, verifying = false;
  bool recoveringOriginal = false;
  String? error, placeName;
  String? _entryID, _targetID;
  int? _expectedRevision;
  SelectedPlaceJourneyReceipt? receipt;

  bool get current =>
      !_disposed &&
      !_retired &&
      ownerID() == _owner &&
      authorizationHeader() == _token &&
      selection.contextCurrent &&
      sourceCurrent();
  bool _target(PersonalCollection v) =>
      v.ownerID == _owner &&
      v.kind == 'journey' &&
      v.cityID == selection.cityID &&
      v.status == 'draft' &&
      v.visibility == 'private' &&
      v.journeyDetails != null;
  List<PersonalCollection> get targets => !current
      ? const []
      : collections.items.where(_target).toList(growable: false);

  Uri _uri(String path) =>
      Uri.parse('${apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}$path');
  Map<String, String> get _headers => {'Authorization': _token!};
  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void _boundary() {
    if (!current) {
      _retired = true;
      placeName = null;
      receipt = null;
      error = '账号、城市、任务或所选地点已变化，请返回重新选择。';
    }
    _notify();
  }

  Future<bool> _readPlace() async {
    final response = await client
        .get(
          _uri('/v1/places/${Uri.encodeComponent(selection.placeID)}'),
          headers: _headers,
        )
        .timeout(const Duration(seconds: 12));
    if (!current) return false;
    if (const {401, 403, 404, 410}.contains(response.statusCode)) {
      _retired = true;
      placeName = null;
      receipt = null;
      error = '当前地点已不可读取，请返回重新选择；尚未保存。';
      throw const FormatException();
    }
    if (response.statusCode != 200) throw const FormatException();
    final value =
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as Map<String, dynamic>;
    final name = value['name'];
    if (value['id'] != selection.placeID ||
        value['cityId'] != selection.cityID ||
        name is! String ||
        name.trim().isEmpty ||
        utf8.encode(name).length > 1000) {
      throw const FormatException();
    }
    placeName = name;
    return true;
  }

  Future<void> load({bool more = false}) async {
    if (!current || busy || editing) return;
    busy = true;
    error = null;
    _notify();
    try {
      if (!await _readPlace()) return;
      await collections.load(more: more);
      if (current && collections.error != null) error = collections.error;
    } catch (_) {
      if (current) error = '无法读取当前地点或本人旅程，请重试；尚未保存。';
    } finally {
      if (!_disposed) {
        busy = false;
        _notify();
      }
    }
  }

  Future<bool> choose([PersonalCollection? target]) async {
    if (!current ||
        busy ||
        editing ||
        placeName == null ||
        (target != null && !targets.any((v) => v.id == target.id)))
      return false;
    busy = true;
    error = null;
    _notify();
    try {
      if (!await _readPlace()) return false;
      if (target == null) {
        await collections.loadControlPending();
        if (!current || !collections.canSwitchDraft) return false;
        collections.newDraft();
      } else if (!await collections.openID(target.id)) {
        if (current) error = collections.error ?? '请先核实此旅程原操作。';
        return false;
      }
      if (!current ||
          !collections.editable ||
          (target != null &&
              (collections.editing == null ||
                  !_target(collections.editing!)))) {
        if (current) error = '旅程版本或状态已变化，请返回重新选择。';
        return false;
      }
      final original = collections.draftJourneyDetails;
      if (original == null) throw const FormatException();
      final entryID = newCollectionControlOperationID();
      final staged = selectedPlaceJourneyDraft(
        original: original,
        entryID: entryID,
        placeID: selection.placeID,
        cityID: selection.cityID,
        name: placeName!,
        day: day,
      );
      _targetID = target?.id;
      _expectedRevision = target == null
          ? 1
          : collections.editing!.revision + 1;
      _entryID = entryID;
      if (target == null) collections.setText(title: '第$day天安排');
      collections.setJourneyDetails(staged);
      editing = true;
      _notify();
      return true;
    } on FormatException catch (e) {
      if (current) error = e.message.isEmpty ? '地点或旅程已变化，请重新选择。' : e.message;
      return false;
    } catch (_) {
      if (current) error = '无法准备这份旅程，请重试；尚未保存。';
      return false;
    } finally {
      if (!_disposed) {
        busy = false;
        _notify();
      }
    }
  }

  bool _savedCandidate(PersonalCollection? value) =>
      current &&
      editing &&
      value != null &&
      _target(value) &&
      (_targetID == null || value.id == _targetID) &&
      value.revision == _expectedRevision &&
      !collections.dirty &&
      !collections.saving &&
      !collections.uncertain &&
      !collections.controlBlocked &&
      !collections.controlCurrentUnread &&
      value.journeyDetails!.itinerary.any(
        (e) =>
            e.id == _entryID &&
            e.text('placeId') == selection.placeID &&
            e.text('cityId') == selection.cityID &&
            e.day('dayIndex') == day,
      );

  void _changed() {
    if (!current) {
      _boundary();
      return;
    }
    if (recoveringOriginal) {
      _notify();
      return;
    }
    if (!_savedCandidate(collections.editing)) receipt = null;
    if (receipt == null &&
        !verifying &&
        !_verifyQueued &&
        _savedCandidate(collections.editing)) {
      _verifyQueued = true;
      scheduleMicrotask(() {
        _verifyQueued = false;
        if (!_disposed) unawaited(verifySaved());
      });
    }
    _notify();
  }

  Future<void> finishOriginalRecovery() async {
    if (!current || !recoveringOriginal) return;
    await collections.loadControlPending();
    if (!current || !collections.canSwitchDraft) return;
    collections.newDraft();
    recoveringOriginal = false;
    editing = false;
    await load();
  }

  Future<void> verifySaved() async {
    final candidate = collections.editing;
    if (verifying || !_savedCandidate(candidate)) return;
    verifying = true;
    error = null;
    _notify();
    try {
      final response = await client
          .get(
            _uri('/v1/me/personal-collections/journey/${candidate!.id}'),
            headers: _headers,
          )
          .timeout(const Duration(seconds: 12));
      if (!current ||
          !identical(candidate, collections.editing) ||
          !_savedCandidate(candidate))
        return;
      if (response.statusCode != 200) throw const FormatException();
      final saved = PersonalCollection.fromJson(
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as Map<String, dynamic>,
      );
      if (saved.id != candidate.id || !_savedCandidate(saved)) {
        throw const FormatException();
      }
      _targetID = saved.id;
      receipt = SelectedPlaceJourneyReceipt(
        saved.id,
        saved.revision,
        saved.title,
      );
    } catch (_) {
      if (current) error = '尚未读回准确的第$day天保存结果，请核实；不会自动重发。';
    } finally {
      if (!_disposed) {
        verifying = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    if (_disposed) return;
    _disposed = true;
    identityChanges.removeListener(_boundary);
    collections.removeListener(_changed);
    collections.dispose();
    super.dispose();
  }
}
