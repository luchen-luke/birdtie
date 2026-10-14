import 'dart:async';
import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'human_causal_control.dart';

class HumanCausalPending {
  const HumanCausalPending(this.selector, {this.stage = 'UNKNOWN'});
  final HumanCausalSelector selector;
  final String stage;
  String get key => '${selector.family.wire}:${selector.scope}';
  bool get valid =>
      selector.valid && const {'PREPARED', 'UNKNOWN'}.contains(stage);
  bool same(HumanCausalPending other) =>
      selector.same(other.selector) && stage == other.stage;
  Map<String, Object> toJson() => {
    'selector': selector.toJson(),
    'stage': stage,
  };
  factory HumanCausalPending.fromJson(Object? raw) {
    final m = humanControlObject(raw, {'selector', 'stage'});
    if (m['stage'] is! String) throw const FormatException('本机准备阶段不符');
    final p = HumanCausalPending(
      HumanCausalSelector.fromJson(m['selector']),
      stage: m['stage'],
    );
    if (!p.valid) throw const FormatException('本机原操作资料不符');
    return p;
  }
}

abstract interface class HumanCausalPendingStore {
  Future<List<HumanCausalPending>> read(String origin, String owner);
  Future<void> write(String origin, String owner, HumanCausalPending pending);
  Future<void> remove(String origin, String owner, HumanCausalPending pending);
}

class SecureHumanCausalPendingStore implements HumanCausalPendingStore {
  SecureHumanCausalPendingStore({FlutterSecureStorage? storage})
    : _storage = storage ?? const FlutterSecureStorage();
  final FlutterSecureStorage _storage;
  static final _tails = <String, Future<void>>{};
  String _key(String origin, String owner) =>
      'birdtie.human-causal.v1.${sha256.convert(utf8.encode(jsonEncode([origin, owner])))}';
  void _context(String origin, String owner) {
    final uri = Uri.tryParse(origin);
    if (!humanControlID(owner) ||
        uri == null ||
        !const {'http', 'https'}.contains(uri.scheme) ||
        uri.host.isEmpty ||
        uri.userInfo.isNotEmpty ||
        uri.hasQuery ||
        uri.hasFragment)
      throw const FormatException('本机原操作账号或来源不符');
  }

  Future<T> _serial<T>(String key, Future<T> Function() action) async {
    final previous = _tails[key], done = Completer<void>();
    _tails[key] = done.future;
    try {
      if (previous != null) await previous;
      return await action();
    } finally {
      if (identical(_tails[key], done.future)) _tails.remove(key);
      done.complete();
    }
  }

  Future<List<HumanCausalPending>> _read(String origin, String owner) async {
    final value = await _storage.read(key: _key(origin, owner));
    if (value == null) return const [];
    if (utf8.encode(value).length > 512 * 1024)
      throw const FormatException('本机原操作记录过大');
    final m = humanControlObject(jsonDecode(value), {
      'origin',
      'owner',
      'operations',
    });
    if (m['origin'] != origin ||
        m['owner'] != owner ||
        m['operations'] is! List)
      throw const FormatException('本机原操作来源不符');
    final rows = (m['operations'] as List)
        .map(HumanCausalPending.fromJson)
        .toList(growable: false);
    if (rows.map((p) => p.key).toSet().length != rows.length)
      throw const FormatException('本机原操作索引不符');
    return List.unmodifiable(rows);
  }

  Future<void> _save(
    String origin,
    String owner,
    List<HumanCausalPending> rows,
  ) async {
    if (rows.isEmpty) {
      await _storage.delete(key: _key(origin, owner));
      return;
    }
    await _storage.write(
      key: _key(origin, owner),
      value: jsonEncode({
        'origin': origin,
        'owner': owner,
        'operations': rows.map((p) => p.toJson()).toList(),
      }),
    );
  }

  @override
  Future<List<HumanCausalPending>> read(String origin, String owner) {
    _context(origin, owner);
    return _serial(_key(origin, owner), () => _read(origin, owner));
  }

  @override
  Future<void> write(String origin, String owner, HumanCausalPending p) {
    _context(origin, owner);
    return _serial(_key(origin, owner), () async {
      if (!p.valid) throw const FormatException('本机原操作记录不符');
      final rows = await _read(origin, owner),
          matches = rows.where((v) => v.key == p.key);
      if (p.stage == 'UNKNOWN' && matches.isEmpty)
        throw const FormatException('本机准备已取消，不能继续提交');
      if (matches.isNotEmpty &&
          (!matches.single.selector.same(p.selector) ||
              matches.single.stage == 'UNKNOWN' && p.stage == 'PREPARED'))
        throw const FormatException('此类别仍有本人原操作尚未核实');
      await _save(origin, owner, [...rows.where((v) => v.key != p.key), p]);
    });
  }

  @override
  Future<void> remove(String origin, String owner, HumanCausalPending p) {
    _context(origin, owner);
    return _serial(_key(origin, owner), () async {
      final rows = await _read(origin, owner),
          matches = rows.where((v) => v.key == p.key);
      if (matches.isNotEmpty && !matches.single.same(p))
        throw const FormatException('本机原操作已变化');
      await _save(origin, owner, rows.where((v) => v.key != p.key).toList());
    });
  }
}

class MemoryHumanCausalPendingStore implements HumanCausalPendingStore {
  final values = <(String, String), Map<String, HumanCausalPending>>{};
  @override
  Future<List<HumanCausalPending>> read(String origin, String owner) async =>
      List.unmodifiable(
        values[(origin, owner)]?.values ?? <HumanCausalPending>[],
      );
  @override
  Future<void> write(String origin, String owner, HumanCausalPending p) async {
    if (!p.valid) throw const FormatException('本机原操作记录不符');
    final rows = values.putIfAbsent((origin, owner), () => {}),
        old = rows[p.key];
    if (p.stage == 'UNKNOWN' && old == null)
      throw const FormatException('本机准备已取消，不能继续提交');
    if (old != null &&
        (!old.selector.same(p.selector) ||
            old.stage == 'UNKNOWN' && p.stage == 'PREPARED'))
      throw const FormatException('此类别仍有本人原操作尚未核实');
    rows[p.key] = p;
  }

  @override
  Future<void> remove(String origin, String owner, HumanCausalPending p) async {
    final rows = values[(origin, owner)], old = rows?[p.key];
    if (old != null && !old.same(p)) throw const FormatException('本机原操作已变化');
    rows?.remove(p.key);
  }
}
