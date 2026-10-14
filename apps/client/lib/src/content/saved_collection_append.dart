import 'dart:async';
import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import '../workspace/entity_share_pending_store.dart';
import '../workspace/saved_items.dart';
import 'personal_collection_controller.dart';
import 'collection_publication_pending_store.dart';

/// Durable selectors only. They cannot approve or replay an append.
abstract class CollectionAppendJournal {
  Future<Map<String, dynamic>?> read(String key);
  Future<Map<String, dynamic>> claim(String key, Map<String, dynamic> value);
  Future<void> clear(String key, String operationID);
}

class SecureCollectionAppendJournal implements CollectionAppendJournal {
  static final _tails = <String, Future<void>>{};
  static const _storage = FlutterSecureStorage();
  Future<T> _serial<T>(String key, Future<T> Function() action) async {
    final before = _tails[key] ?? Future<void>.value();
    final done = Completer<void>();
    _tails[key] = done.future;
    try {
      await before;
      return await action();
    } finally {
      done.complete();
      if (identical(_tails[key], done.future)) _tails.remove(key);
    }
  }

  @override
  Future<Map<String, dynamic>?> read(String key) => _serial(key, () async {
    final raw = await _storage.read(key: key);
    if (raw == null) return null;
    final value = jsonDecode(raw) as Map<String, dynamic>;
    _validateSelector(value);
    return Map.unmodifiable(value);
  });
  @override
  Future<Map<String, dynamic>> claim(String key, Map<String, dynamic> value) =>
      _serial(key, () async {
        final existing = await _storage.read(key: key);
        if (existing != null) {
          final previous = jsonDecode(existing) as Map<String, dynamic>;
          _validateSelector(previous);
          return Map.unmodifiable(previous);
        }
        _validateSelector(value);
        await _storage.write(key: key, value: jsonEncode(value));
        final stored = await _storage.read(key: key);
        if (stored != jsonEncode(value)) throw StateError('本机记录未确认');
        return Map.unmodifiable(value);
      });
  @override
  Future<void> clear(String key, String operationID) => _serial(key, () async {
    final raw = await _storage.read(key: key);
    if (raw == null) return;
    final value = jsonDecode(raw) as Map<String, dynamic>;
    _validateSelector(value);
    if (value['operationId'] == operationID) await _storage.delete(key: key);
  });
}

void _validateSelector(Map<String, dynamic> value) {
  const keys = {
    'operationId',
    'kind',
    'collectionId',
    'cityId',
    'expectedRevision',
  };
  if (value.keys.toSet().difference(keys).isNotEmpty ||
      value.length != keys.length ||
      !const ['journey', 'theme_map'].contains(value['kind']) ||
      !chatEntityUUID.hasMatch(value['operationId'] as String? ?? '') ||
      !chatEntityUUID.hasMatch(value['collectionId'] as String? ?? '') ||
      value['cityId'] is! String ||
      (value['cityId'] as String).isEmpty ||
      utf8.encode(value['cityId'] as String).length > 80 ||
      (value['cityId'] as String).contains(RegExp(r'[\x00-\x1f]')) ||
      value['expectedRevision'] is! int ||
      value['expectedRevision'] < 1 ||
      value['expectedRevision'] > 8999999999999999) {
    throw const FormatException('本机原操作记录无效');
  }
}

class SavedCollectionAppendController extends ChangeNotifier {
  SavedCollectionAppendController({
    required this.saved,
    required this.selected,
    required this.ownerID,
    required this.sourceCurrent,
    required this.identityChanges,
    CollectionAppendJournal? journal,
    this.publicationRecoveryReader,
  }) : journal = journal ?? SecureCollectionAppendJournal() {
    _owner = ownerID();
    _authorization = saved.authorizationHeader();
    identityChanges.addListener(_boundary);
    _city = selected.isEmpty ? '' : selected.first.cityId;
    _createControllers();
  }
  void _createControllers() {
    for (final kind in ['journey', 'theme_map']) {
      collections[kind] = PersonalCollectionController(
        kind: kind,
        cityID: _city,
        ownerID: ownerID,
        authorizationHeader: saved.authorizationHeader,
        sourceCurrent: () => current,
        identityChanges: identityChanges,
        client: saved.followClient,
        apiBaseUrl: saved.followApiBaseUrl,
        publicationRecoveryReader: (id) => publicationRecoveryReader != null
            ? publicationRecoveryReader!(kind, id)
            : CollectionPublicationPendingStore(
                origin: saved.followApiBaseUrl,
                owner: _owner!,
                kind: kind,
                id: id,
              ).load(),
      );
    }
  }

  final SavedController saved;
  final List<SavedItem> selected;
  final String? Function() ownerID;
  final bool Function() sourceCurrent;
  final Listenable identityChanges;
  final CollectionAppendJournal journal;
  final Future<Map<String, dynamic>?> Function(String kind, String id)?
  publicationRecoveryReader;
  final collections = <String, PersonalCollectionController>{};
  String? _owner;
  String? _authorization;
  late final String _city;
  bool _retired = false, _disposed = false;
  int _epoch = 0;
  bool busy = false, uncertain = false, succeeded = false;
  String? error, notice, recoveryTitle;
  PersonalCollection? target;
  List<SavedItem> additions = const [];
  Map<String, dynamic>? pending;
  String get cityID => _city;
  bool get current =>
      !_disposed &&
      !_retired &&
      _owner != null &&
      ownerID() == _owner &&
      saved.authorizationHeader() != null &&
      sourceCurrent();
  String get _journalKey =>
      'birdtie.collection-append.v1.${sha256.convert(utf8.encode('${saved.followApiBaseUrl}|$_owner'))}';
  bool _valid(int epoch) => current && epoch == _epoch;
  void _boundary() {
    if (!current && !_retired) {
      _retired = true;
      ++_epoch;
      target = null;
      additions = const [];
      _notify();
    } else if (current && _authorization != saved.authorizationHeader()) {
      _authorization = saved.authorizationHeader();
      for (final c in collections.values) c.dispose();
      collections.clear();
      _createControllers();
      if (target == null && !busy && !uncertain) unawaited(load());
      _notify();
    }
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  Uri _uri(String kind, String id, String suffix) => Uri.parse(
    '${saved.followApiBaseUrl.replaceFirst(RegExp(r'/$'), '')}/v1/me/personal-collections/$kind/$id${suffix.isEmpty ? '' : '/$suffix'}',
  );
  Map<String, String> get _headers => {
    'Authorization': saved.authorizationHeader()!,
    'Content-Type': 'application/json',
  };
  Future<void> load() async {
    if (!current || busy) return;
    final epoch = _epoch;
    busy = true;
    error = null;
    _notify();
    try {
      pending = await journal.read(_journalKey);
      if (!_valid(epoch)) return;
      if (pending != null) {
        _validateSelector(pending!);
        uncertain = true;
        await _loadRecoveryTitle(epoch);
        return;
      }
      if (selected.isEmpty ||
          selected.length > 50 ||
          selected.any(
            (i) =>
                !i.available ||
                i.kind != 'place' ||
                i.cityId != _city ||
                !chatEntityUUID.hasMatch(i.targetId),
          )) {
        error = '请选择同一城市的当前可见地点。';
        return;
      }
      for (final c in collections.values) await c.load();
    } catch (_) {
      if (_valid(epoch)) error = '无法读取本机记录，请重试；暂不提交。';
    } finally {
      if (_valid(epoch)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<void> _loadRecoveryTitle(int epoch) async {
    final p = pending;
    recoveryTitle = null;
    if (p == null) return;
    try {
      final r = await saved.followClient
          .get(
            _uri(p['kind'] as String, p['collectionId'] as String, ''),
            headers: _headers,
          )
          .timeout(const Duration(seconds: 12));
      if (!_valid(epoch) || r.statusCode != 200) return;
      final data =
          (jsonDecode(utf8.decode(r.bodyBytes))
              as Map<String, dynamic>)['data'];
      final value = PersonalCollection.fromJson(data as Map<String, dynamic>);
      if (value.id == p['collectionId'] &&
          value.kind == p['kind'] &&
          value.ownerID == _owner &&
          value.cityID == p['cityId']) {
        recoveryTitle = value.title;
      }
    } catch (_) {
      // The original selector remains recoverable even if its target retired.
    }
  }

  Future<void> choose(PersonalCollection value) async {
    if (!current || busy || uncertain || succeeded || value.status != 'draft')
      return;
    final c = collections[value.kind];
    if (c == null || !await c.open(value) || !current) return;
    if (c.publicationPending != null || !c.editable) {
      target = null;
      additions = const [];
      error = '此记录有原公开操作待核实，请先在原编辑器处理。';
      _notify();
      return;
    }
    final latest = c.editing;
    if (latest == null ||
        latest.status != 'draft' ||
        latest.ownerID != _owner ||
        latest.cityID != _city)
      return;
    final existing = latest.entries.map((e) => e.placeID).toSet();
    final unique = <String, SavedItem>{};
    for (final item in selected)
      if (!existing.contains(item.targetId)) unique[item.targetId] = item;
    if (latest.entries.length + unique.length > 50 ||
        latest.entries.any((e) => !e.sourceCurrent)) {
      error = '此草稿含不可用地点或加入后超过50项，请先在原编辑器检查。';
      target = null;
    } else {
      target = latest;
      additions = List.unmodifiable(unique.values);
      error = additions.isEmpty ? '所选地点已在此记录中，无需重复加入。' : null;
    }
    _notify();
  }

  void back() {
    if (!busy && !uncertain) {
      target = null;
      additions = const [];
      error = null;
      _notify();
    }
  }

  Future<bool> submit() async {
    final t = target;
    if (!current ||
        busy ||
        uncertain ||
        succeeded ||
        t == null ||
        additions.isEmpty)
      return false;
    if (t.entries.any((e) => !e.sourceCurrent)) {
      error = '地点资料已到期，请返回重新读取。';
      target = null;
      additions = const [];
      _notify();
      return false;
    }
    final epoch = _epoch;
    busy = true;
    error = null;
    _notify();
    final operation = newEntityShareOperationID();
    final selector = <String, dynamic>{
      'operationId': operation,
      'kind': t.kind,
      'collectionId': t.id,
      'cityId': t.cityID,
      'expectedRevision': t.revision,
    };
    final entries = additions
        .map(
          (i) => {
            'placeId': i.targetId,
            'note': '',
            'locationVisibility': 'hidden',
          },
        )
        .toList();
    final body = {...selector}
      ..remove('kind')
      ..remove('collectionId');
    body['entries'] = entries;
    try {
      final claimed = await journal.claim(_journalKey, selector);
      if (!_valid(epoch)) return false;
      pending = claimed;
      _validateSelector(claimed);
      uncertain = true;
      if (claimed['operationId'] != operation) {
        await _loadRecoveryTitle(epoch);
        error = '已有原操作待核实，不会提交另一条。';
        return false;
      }
      // Content is frozen before asynchronous storage; never copy Saved.note.
      final r = await saved.followClient
          .post(
            _uri(t.kind, t.id, 'append-places'),
            headers: _headers,
            body: jsonEncode(body),
          )
          .timeout(const Duration(seconds: 12));
      if (!_valid(epoch)) return false;
      // Final authorization can reject after commit; only a causal receipt
      // proves whether the original operation applied.
      if (r.statusCode != 200) throw const FormatException();
      return await _accept(r, selector, epoch);
    } catch (_) {
      if (_valid(epoch)) {
        uncertain = pending != null;
        error = uncertain ? '结果待核实，保留原操作；不会自动重发。' : '无法保存本机记录，尚未提交。';
      }
      return false;
    } finally {
      if (_valid(epoch)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<bool> _accept(
    http.Response r,
    Map<String, dynamic> expected,
    int epoch,
  ) async {
    final v =
        (jsonDecode(utf8.decode(r.bodyBytes)) as Map<String, dynamic>)['data']
            as Map<String, dynamic>;
    if (v['operationId'] != expected['operationId'] ||
        v['kind'] != expected['kind'] ||
        v['collectionId'] != expected['collectionId'] ||
        !const ['SUCCEEDED', 'STOPPED'].contains(v['status']) ||
        (v['status'] == 'SUCCEEDED' &&
            v['appliedRevision'] !=
                (expected['expectedRevision'] as int) + 1) ||
        (v['status'] == 'STOPPED' && v['appliedRevision'] != 0))
      throw const FormatException();
    await journal.clear(_journalKey, expected['operationId'] as String);
    if (!_valid(epoch)) return false;
    pending = null;
    uncertain = false;
    succeeded = v['status'] == 'SUCCEEDED';
    if (!succeeded) {
      target = null;
      additions = const [];
      notice = '原操作已结束，尚未生效的追加不会再执行。';
      for (final c in collections.values) await c.load();
      if (!_valid(epoch)) return false;
    }
    error = null;
    _notify();
    return true;
  }

  Future<bool> recover() async {
    final p = pending;
    if (!current || busy || !uncertain || p == null) return false;
    final epoch = _epoch;
    busy = true;
    error = null;
    _notify();
    try {
      final r = await saved.followClient
          .get(
            _uri(
              p['kind'] as String,
              p['collectionId'] as String,
              'append-operations/${p['operationId']}',
            ),
            headers: _headers,
          )
          .timeout(const Duration(seconds: 12));
      if (!_valid(epoch)) return false;
      if (r.statusCode != 200) {
        error = '尚未取得原操作回执，请稍后核实；不会重发。';
        return false;
      }
      return await _accept(r, p, epoch);
    } catch (_) {
      if (_valid(epoch)) error = '无法核实原操作，请重试读取。';
      return false;
    } finally {
      if (_valid(epoch)) {
        busy = false;
        _notify();
      }
    }
  }

  Future<bool> stop() async {
    final p = pending;
    if (!current || busy || !uncertain || p == null) return false;
    final epoch = _epoch;
    busy = true;
    error = null;
    _notify();
    try {
      final r = await saved.followClient
          .post(
            _uri(
              p['kind'] as String,
              p['collectionId'] as String,
              'append-operations/${p['operationId']}/stop',
            ),
            headers: _headers,
            body: jsonEncode({
              'cityId': p['cityId'],
              'expectedRevision': p['expectedRevision'],
            }),
          )
          .timeout(const Duration(seconds: 12));
      if (!_valid(epoch)) return false;
      if (r.statusCode != 200) throw const FormatException();
      return await _accept(r, p, epoch);
    } catch (_) {
      if (_valid(epoch)) error = '结束结果仍待核实，请继续读取原回执。';
      return false;
    } finally {
      if (_valid(epoch)) {
        busy = false;
        _notify();
      }
    }
  }

  @override
  void dispose() {
    _disposed = true;
    ++_epoch;
    identityChanges.removeListener(_boundary);
    for (final c in collections.values) c.dispose();
    super.dispose();
  }
}

class SavedCollectionAppendPage extends StatefulWidget {
  const SavedCollectionAppendPage({super.key, required this.controller});
  final SavedCollectionAppendController controller;
  @override
  State<SavedCollectionAppendPage> createState() =>
      _SavedCollectionAppendPageState();
}

class _SavedCollectionAppendPageState extends State<SavedCollectionAppendPage> {
  bool approved = false;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) unawaited(widget.controller.load());
    });
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: Listenable.merge([
      widget.controller,
      ...widget.controller.collections.values,
    ]),
    builder: (context, _) {
      final c = widget.controller, t = c.target;
      return Scaffold(
        appBar: AppBar(title: const Text('加入已有旅程或主题地图')),
        body: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            if (!c.current)
              const Text('身份或来源已变化，请返回重新选择。')
            else ...[
              if (c.busy) const LinearProgressIndicator(),
              if (c.error != null) Text(c.error!),
              if (c.notice != null) Text(c.notice!),
              if (c.succeeded)
                const Text('原操作已确认生效，可在原旅程或主题地图查看。')
              else if (c.uncertain) ...[
                Text(
                  c.recoveryTitle == null
                      ? '原目标当前无法读取，仍可核实或结束原操作。'
                      : '原目标：${c.recoveryTitle}',
                ),
                const Text('上次操作的结果尚未确认，请先读取原回执。'),
                FilledButton(
                  onPressed: c.busy ? null : () => unawaited(c.recover()),
                  child: const Text('核实原操作'),
                ),
                TextButton(
                  onPressed: c.busy ? null : () => unawaited(c.stop()),
                  child: const Text('结束原操作'),
                ),
                const Text('结束会阻止尚未生效的追加；若已生效，原结果保留，不移除地点。'),
              ] else if (t == null) ...[
                const Text('选择同城的本人草稿。已公开记录需先在原编辑器处理公开状态。'),
                for (final source in c.collections.values) ...[
                  Text(
                    source.label,
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  if (source.error != null) Text(source.error!),
                  for (final item in source.items)
                    ListTile(
                      title: Text(item.title),
                      subtitle: Text(
                        item.status == 'draft'
                            ? '本人草稿 · ${item.entries.length}个地点'
                            : '已公开，当前不能追加',
                      ),
                      enabled: item.status == 'draft' && !c.busy,
                      onTap: item.status == 'draft' && !c.busy
                          ? () {
                              approved = false;
                              unawaited(c.choose(item));
                            }
                          : null,
                    ),
                  if (source.nextCursor != null)
                    TextButton(
                      onPressed: source.loading
                          ? null
                          : () => unawaited(source.load(more: true)),
                      child: const Text('继续读取'),
                    ),
                  if (source.items.isEmpty && !source.loading)
                    const Text('暂无同城记录'),
                ],
              ] else ...[
                Text(t.title, style: Theme.of(context).textTheme.titleLarge),
                if (t.summary.isNotEmpty) Text(t.summary),
                const SizedBox(height: 16),
                const Text('将追加这些地点，保留已有内容和顺序：'),
                for (final item in c.additions)
                  ListTile(title: Text(item.title)),
                const Text('不复制私人收藏备注；新增地点默认隐藏公开位置。本次只修改本人草稿，不公开发布。'),
                CheckboxListTile(
                  contentPadding: EdgeInsets.zero,
                  value: approved,
                  onChanged: c.busy
                      ? null
                      : (v) => setState(() => approved = v == true),
                  title: const Text('确认将这些地点加入此记录'),
                ),
                FilledButton(
                  onPressed: approved && !c.busy && c.additions.isNotEmpty
                      ? () => unawaited(c.submit())
                      : null,
                  child: const Text('确认加入'),
                ),
                TextButton(
                  onPressed: c.busy
                      ? null
                      : () {
                          approved = false;
                          c.back();
                        },
                  child: const Text('返回选择'),
                ),
              ],
            ],
          ],
        ),
      );
    },
  );
}
