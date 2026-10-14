import 'dart:async';
import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'entity_share_pending_store.dart' show newEntityShareOperationID;
import 'saved_items.dart';
import 'typed_local_draft.dart';
import 'external_plan_import.dart';

String _text(Object? raw, int max, {bool empty = true}) {
  if (raw is! String ||
      utf8.encode(raw).length > max ||
      !empty && raw.isEmpty ||
      raw.contains('\u0000')) {
    throw const FormatException('计划文字或标识无效');
  }
  return raw;
}

Map<String, dynamic> _object(Object? raw, Set<String> keys) {
  if (raw is! Map<String, dynamic> ||
      raw.length != keys.length ||
      raw.keys.any((k) => !keys.contains(k))) {
    throw const FormatException('计划结构含未知字段');
  }
  return raw;
}

class LocalPlanDay {
  LocalPlanDay({
    String? id,
    required this.label,
    this.text = '',
    this.input,
    List<String> places = const [],
  }) : id = id ?? newEntityShareOperationID(),
       places = List.unmodifiable(places);
  final String id, label, text;
  final List<String> places;
  final ExternalPlanDayInput? input;
  LocalPlanDay edit({
    String? label,
    String? text,
    List<String>? places,
    ExternalPlanDayInput? input,
  }) => LocalPlanDay(
    id: id,
    label: label ?? this.label,
    text: text ?? this.text,
    places: places ?? this.places,
    input: input ?? this.input,
  );
  Map<String, dynamic> toJson() => {
    'id': id,
    'label': label,
    'text': text,
    'places': places,
    if (input != null) 'input': input!.toJson(),
  };
  factory LocalPlanDay.fromJson(Object? raw) {
    if (raw is! Map<String, dynamic>) throw const FormatException('计划日结构无效');
    final j = _object(raw, {
      'id',
      'label',
      'text',
      'places',
      if (raw.containsKey('input')) 'input',
    });
    if (j['places'] is! List || (j['places'] as List).length > 20) {
      throw const FormatException('每天最多20个地点');
    }
    final places = (j['places'] as List)
        .map((p) => _text(p, 160, empty: false))
        .toList();
    if (places.toSet().length != places.length)
      throw const FormatException('一天内地点重复');
    return LocalPlanDay(
      id: _text(j['id'], 160, empty: false),
      label: _text(j['label'], 160, empty: false),
      text: _text(j['text'], 6144),
      places: places,
      input: j.containsKey('input')
          ? ExternalPlanDayInput.fromJson(j['input'])
          : null,
    );
  }
}

/// Editable user material only. No geometry, travel time, GPS, approval or
/// executable provider instruction can be restored from imported text.
class LocalRouteValue {
  LocalRouteValue({
    this.label = '路线草稿',
    this.mode = 'walking',
    this.startPlaceID,
    List<LocalPlanDay>? days,
    this.sourceText = '',
    this.selectedDayID,
    List<String> importUnknownFields = const [],
  }) : days = List.unmodifiable(days ?? [LocalPlanDay(label: '第1天')]),
       importUnknownFields = List.unmodifiable(importUnknownFields);
  final String label, mode, sourceText;
  final String? startPlaceID;
  final List<LocalPlanDay> days;
  final String? selectedDayID;
  final List<String> importUnknownFields;
  String get currentDayID => selectedDayID ?? days.first.id;
  static const modes = {'walking', 'cycling', 'driving', 'transit'};
  String get sourceDigest => sha256.convert(utf8.encode(sourceText)).toString();
  Map<String, dynamic> toJson() => {
    'schemaVersion': 'birdtie-route-draft-v1',
    'label': label,
    'mode': mode,
    'startPlaceId': startPlaceID,
    'days': days.map((d) => d.toJson()).toList(),
    'sourceText': sourceText,
    'selectedDayId': currentDayID,
    if (importUnknownFields.isNotEmpty)
      'importUnknownFields': importUnknownFields,
  };
  factory LocalRouteValue.fromJson(Object? raw) {
    if (raw is! Map<String, dynamic>) throw const FormatException('计划结构无效');
    final j = _object(raw, {
      'schemaVersion',
      'label',
      'mode',
      'startPlaceId',
      'days',
      'sourceText',
      if (raw.containsKey('selectedDayId')) 'selectedDayId',
      if (raw.containsKey('importUnknownFields')) 'importUnknownFields',
    });
    if (j['schemaVersion'] != 'birdtie-route-draft-v1' ||
        !modes.contains(j['mode']) ||
        j['days'] is! List ||
        (j['days'] as List).isEmpty ||
        (j['days'] as List).length > 14) {
      throw const FormatException('计划类型或天数无效');
    }
    final days = (j['days'] as List).map(LocalPlanDay.fromJson).toList();
    final selected = j.containsKey('selectedDayId')
        ? _text(j['selectedDayId'], 160, empty: false)
        : days.first.id;
    final unknown = j.containsKey('importUnknownFields')
        ? j['importUnknownFields']
        : const [];
    if (!days.any((day) => day.id == selected) ||
        unknown is! List ||
        unknown.length > 32 ||
        days.fold<int>(0, (n, d) => n + (d.input?.stops.length ?? 0)) > 50) {
      throw const FormatException('所选日或导入信息无效');
    }
    if (days.map((d) => d.id).toSet().length != days.length ||
        days.fold<int>(0, (n, d) => n + d.places.length) > 50 ||
        <String>{
              if (j['startPlaceId'] is String) j['startPlaceId'] as String,
              for (final d in days) ...d.places,
            }.length >
            50) {
      throw const FormatException('计划最多50个地点，天标识不可重复');
    }
    return LocalRouteValue(
      label: _text(j['label'], 320),
      mode: j['mode'],
      startPlaceID: j['startPlaceId'] == null
          ? null
          : _text(j['startPlaceId'], 160, empty: false),
      days: days,
      sourceText: _text(j['sourceText'], 6144),
      selectedDayID: selected,
      importUnknownFields: unknown
          .map((v) => _text(v, 320, empty: false))
          .toList(),
    );
  }
  LocalRouteValue edit({
    String? label,
    String? mode,
    String? startPlaceID,
    bool clearStart = false,
    List<LocalPlanDay>? days,
    String? sourceText,
    String? selectedDayID,
    List<String>? importUnknownFields,
  }) => LocalRouteValue(
    label: label ?? this.label,
    mode: mode ?? this.mode,
    startPlaceID: clearStart ? null : startPlaceID ?? this.startPlaceID,
    days: days ?? this.days,
    sourceText: sourceText ?? this.sourceText,
    selectedDayID:
        selectedDayID ??
        ((days ?? this.days).any((d) => d.id == currentDayID)
            ? currentDayID
            : (days ?? this.days).first.id),
    importUnknownFields: importUnknownFields ?? this.importUnknownFields,
  );

  /// Headings group supplied lines; place names and commands stay unresolved.
  /// No geocoding, model, network, reservation, message or publication occurs.
  static LocalRouteValue importText(String raw) {
    _text(raw, 6144, empty: false);
    if (raw.trimLeft().startsWith('{') || raw.trimLeft().startsWith('[')) {
      final imported = ExternalPlanImport.parse(raw);
      final value = LocalRouteValue(
        label: imported.title,
        sourceText: raw,
        days: [
          for (final day in imported.days)
            LocalPlanDay(label: day.title, text: day.text, input: day.input),
        ],
        importUnknownFields: imported.unknownFields,
      );
      return LocalRouteValue.fromJson(value.toJson());
    }
    final days = <LocalPlanDay>[];
    var label = '第1天', started = false;
    final lines = <String>[];
    final heading = RegExp(
      r'^(?:第\s*\d{1,2}\s*天|Day\s*\d{1,2}|\d{4}-\d{2}-\d{2})(?:\s*[:：].*)?$',
      caseSensitive: false,
    );
    void finish({bool force = false}) {
      if (lines.isNotEmpty || force) {
        days.add(LocalPlanDay(label: label, text: lines.join('\n')));
        lines.clear();
      }
    }

    for (final line in raw.replaceAll('\r\n', '\n').split('\n')) {
      if (heading.hasMatch(line.trim())) {
        finish(force: started);
        label = line.trim();
        started = true;
      } else {
        lines.add(line);
      }
    }
    finish(force: started);
    if (days.isEmpty) days.add(LocalPlanDay(label: label));
    if (days.length > 14) throw const FormatException('请拆分为不超过14天的计划');
    final value = LocalRouteValue(label: '导入计划草稿', days: days, sourceText: raw);
    return LocalRouteValue.fromJson(value.toJson());
  }
}

class RouteDraftController extends ChangeNotifier {
  RouteDraftController({
    required this.origin,
    required this.owner,
    required this.cityID,
    required this.saved,
    required this.boundaryChanges,
    required this.sourceCurrent,
    this.localOwnerCurrent,
    TypedLocalDraftStore? store,
    TypedLocalDraft? initial,
  }) : _store = store ?? SecureTypedLocalDraftStore(),
       _draft = TypedLocalDraft(
         owner: owner,
         origin: origin,
         kind: 'route',
         text: '',
         id: initial?.id,
         revision: initial?.revision ?? 1,
         taskID: initial?.taskID,
         slotID: 'route:$cityID',
         references: initial?.references ?? const [],
         attachments: initial?.attachments ?? const [],
       ) {
    if (_draft.owner != owner ||
        _draft.origin != origin ||
        _draft.kind != 'route' ||
        _draft.recipientID != null ||
        _draft.workspaceID != null) {
      throw ArgumentError('路线草稿主体不符');
    }
    if (initial != null &&
        (!initial.matches(origin, owner, null) ||
            initial.kind != 'route' ||
            initial.recipientID != null)) {
      throw ArgumentError('原转换草稿来源不符');
    }
    if (initial?.text.isNotEmpty == true)
      value = LocalRouteValue(
        days: [LocalPlanDay(label: '第1天', text: _text(initial!.text, 6144))],
      );
    _incomingMaterial =
        initial != null &&
        (initial.text.isNotEmpty ||
            initial.references.isNotEmpty ||
            initial.attachments.isNotEmpty);
    boundaryChanges.addListener(_changed);
    saved.addListener(_changed);
    _rearm();
  }
  final String origin, owner, cityID;
  final SavedController saved;
  final Listenable boundaryChanges;
  final bool Function() sourceCurrent;
  // Retaining one's own text after a rejected session grants no source read,
  // map projection or domain effect. Explicit logout/account switch disallows it.
  final bool Function()? localOwnerCurrent;
  final TypedLocalDraftStore _store;
  TypedLocalDraft _draft;
  LocalRouteValue value = LocalRouteValue();
  bool _disposed = false, _retired = false, _edited = false;
  bool _incomingMaterial = false;
  bool _restoreReady = false;
  bool get localNamespaceReadComplete => _restoreReady;
  Future<void>? _restoreRead;
  TypedLocalDraft? _restoreChoice;
  bool get restoreChoicePending => _restoreChoice != null;
  void chooseRestoredDraft(bool useStored) {
    if (!current || _restoreChoice == null) return;
    final stored = _restoreChoice!;
    try {
      if (useStored) {
        final restored = LocalRouteValue.fromJson(jsonDecode(stored.text));
        value = restored;
        _draft = stored;
      }
      _restoreChoice = null;
      _incomingMaterial = false;
      error = null;
      if (!useStored) edit(value);
      notifyListeners();
    } catch (_) {
      error = '已保存计划无法读取，本次转换稿仍保留。';
      notifyListeners();
    }
  }

  int _reads = 0;
  Timer? _saveTimer, _expiry;
  String? error;
  int get selectedDay =>
      value.days.indexWhere((day) => day.id == value.currentDayID);
  TypedLocalDraft get envelope => _draft;
  bool get current => !_disposed && !_retired && sourceCurrent();
  bool get _canPersist => !_disposed && (localOwnerCurrent?.call() ?? current);
  List<SavedItem> get choices => !current || saved.failed || saved.pageChanged
      ? const []
      : saved.items
            .where(
              (i) => i.mapCurrent && i.kind == 'place' && i.cityId == cityID,
            )
            .toList(growable: false);
  SavedItem? place(String id) {
    for (final item in choices) {
      if (item.targetId == id) return item;
    }
    return null;
  }

  List<SavedItem>? points([int? day]) {
    if (!current) return null;
    final ids = <String>{
      if (value.startPlaceID != null) value.startPlaceID!,
      ...value.days[day ?? selectedDay].places,
    };
    if (ids.isEmpty) return const [];
    final rows = <SavedItem>[];
    for (final id in ids) {
      final item = place(id);
      if (item == null) return null;
      rows.add(item);
    }
    return List.unmodifiable(rows);
  }

  void _changed() {
    if (_disposed) return;
    if (!sourceCurrent()) {
      _retired = true;
      _saveTimer?.cancel();
      unawaited(flush());
    }
    _rearm();
    notifyListeners();
  }

  void _rearm() {
    _expiry?.cancel();
    if (!current) return;
    final deadlines = choices.map((p) => p.sourceExpiresAt!).toList()..sort();
    if (deadlines.isNotEmpty) {
      _expiry = Timer(deadlines.first.difference(DateTime.now().toUtc()), () {
        if (!_disposed) {
          _rearm();
          notifyListeners();
        }
      });
    }
  }

  Future<void> restore() {
    if (!_canPersist || _restoreReady) return Future<void>.value();
    if (_restoreRead != null) return _restoreRead!;
    final reading = _restoreLocalNamespace();
    _restoreRead = reading.whenComplete(() => _restoreRead = null);
    return _restoreRead!;
  }

  Future<void> _restoreLocalNamespace() async {
    final read = ++_reads;
    try {
      final rows = await _store.read(origin, owner);
      if (!_canPersist || read != _reads) return;
      for (final row in rows) {
        if (!const {'route', 'external_plan'}.contains(row.kind) ||
            row.slotID != 'route:$cityID' ||
            row.workspaceID != null ||
            row.recipientID != null)
          continue;
        if (row.owner != owner || row.origin != origin)
          throw const FormatException();
        final restored = LocalRouteValue.fromJson(jsonDecode(row.text));
        if ((_incomingMaterial || _edited) &&
            (row.id != _draft.id ||
                jsonEncode(restored.toJson()) != jsonEncode(value.toJson()) ||
                jsonEncode(row.references.map((r) => r.toJson()).toList()) !=
                    jsonEncode(
                      _draft.references.map((r) => r.toJson()).toList(),
                    ) ||
                jsonEncode(row.attachments.map((a) => a.toJson()).toList()) !=
                    jsonEncode(
                      _draft.attachments.map((a) => a.toJson()).toList(),
                    ))) {
          _restoreChoice = row;
          _restoreReady = true;
          error = null;
          notifyListeners();
          return;
        }
        value = restored;
        _draft = row;
        _incomingMaterial = false;
        break;
      }
      error = null;
      _restoreReady = true;
      if (_incomingMaterial) {
        _incomingMaterial = false;
        edit(value);
      }
      if (_edited) await flush();
      notifyListeners();
    } catch (_) {
      if (current) {
        error = '本机路线草稿暂时无法恢复，未执行任何操作。';
        notifyListeners();
      }
    }
  }

  void edit(LocalRouteValue next) {
    if (!current || restoreChoicePending) return;
    try {
      final checked = LocalRouteValue.fromJson(next.toJson());
      final text = jsonEncode(checked.toJson());
      _text(text, 16384);
      final nextDraft = _draft.edit(
        text: text,
        kind: checked.sourceText.isEmpty ? 'route' : 'external_plan',
        references: [
          ..._draft.references.where((r) => r.purpose != 'route_stop'),
          for (final id in <String>{
            if (checked.startPlaceID != null) checked.startPlaceID!,
            for (final day in checked.days) ...day.places,
          })
            LocalDraftReference(kind: 'place', id: id, purpose: 'route_stop'),
        ],
      );
      TypedLocalDraft.fromJson(nextDraft.toJson());
      value = checked;
      _draft = nextDraft;
      _edited = true;
      error = null;
      _saveTimer?.cancel();
      _saveTimer = Timer(
        const Duration(milliseconds: 300),
        () => unawaited(flush()),
      );
      notifyListeners();
    } catch (_) {
      error = '计划超过本机草稿限制或格式无效，原草稿仍保留。';
      notifyListeners();
    }
  }

  void selectDay(int index) {
    if (!current || index < 0 || index >= value.days.length) return;
    if (value.days[index].id == value.currentDayID) return;
    edit(value.edit(selectedDayID: value.days[index].id));
  }

  void add(String id) {
    if (place(id) == null) return;
    final day = value.days[selectedDay];
    if (day.places.contains(id)) return;
    replaceDay(day.edit(places: [...day.places, id]));
  }

  void replaceDay(LocalPlanDay day) {
    if (!current || day.id != value.days[selectedDay].id) return;
    final days = [...value.days];
    days[selectedDay] = day;
    edit(value.edit(days: days));
  }

  void move(int index, int delta) {
    final day = value.days[selectedDay], to = index + delta;
    if (!current ||
        index < 0 ||
        to < 0 ||
        index >= day.places.length ||
        to >= day.places.length)
      return;
    final rows = [...day.places];
    rows.insert(to, rows.removeAt(index));
    replaceDay(day.edit(places: rows));
  }

  Future<void> flush() async {
    _saveTimer?.cancel();
    if (!_canPersist || !_edited) return;
    if (!_restoreReady) {
      // An in-flight namespace read must finish before an auto-save can decide
      // that this slot is empty. The page gates editing until that first read.
      if (_restoreRead != null) return;
      await restore();
    }
    if (!_canPersist || !_edited || !_restoreReady || restoreChoicePending)
      return;
    final draft = _draft;
    try {
      await _store.save(origin, owner, [draft]);
      if (_canPersist && identical(draft, _draft)) {
        final hadError = error != null;
        _edited = false;
        error = null;
        if (hadError) notifyListeners();
      }
    } catch (_) {
      if (current) {
        error = '本机计划尚未保存，当前编辑仍保留。';
        notifyListeners();
      }
    }
  }

  @override
  void dispose() {
    _saveTimer?.cancel();
    _expiry?.cancel();
    boundaryChanges.removeListener(_changed);
    saved.removeListener(_changed);
    _disposed = true;
    super.dispose();
  }
}
