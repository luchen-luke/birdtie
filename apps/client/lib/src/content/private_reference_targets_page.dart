import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../app/birdtie_app_bar.dart';
import '../config/birdtie_environment.dart';
import '../workspace/human_causal_control.dart';
import 'private_reference_media_view.dart';

bool _referenceTargetsCursor(Object? value) {
  if (value is! String ||
      value.length > 4096 ||
      !RegExp(r'^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$').hasMatch(value)) {
    return false;
  }
  try {
    return value
        .split('.')
        .every(
          (part) =>
              base64Url
                  .encode(base64Url.decode(base64Url.normalize(part)))
                  .replaceAll('=', '') ==
              part,
        );
  } on FormatException {
    return false;
  }
}

class PrivateReferenceTarget {
  const PrivateReferenceTarget(this.id, this.name, this.imageCount);
  final String id, name;
  final int imageCount;
}

class PrivateReferenceTargetsPageData {
  const PrivateReferenceTargetsPageData(
    this.items,
    this.validUntil,
    this.sourceVersion,
    this.nextCursor,
  );
  final List<PrivateReferenceTarget> items;
  final DateTime validUntil;
  final String sourceVersion;
  final String? nextCursor;

  factory PrivateReferenceTargetsPageData.decode(Object? raw) {
    final value = humanControlObject(raw, {
      'schemaVersion',
      'targetType',
      'items',
      'observedAt',
      'validUntil',
      'sourceVersion',
      'nextCursor',
    });
    final rows = value['items'], next = value['nextCursor'];
    if (value['schemaVersion'] != 'owned-reference-image-targets-v1' ||
        value['targetType'] != 'PLACE' ||
        rows is! List ||
        rows.length > 20 ||
        !humanControlHash(value['sourceVersion']) ||
        next != null && (!_referenceTargetsCursor(next) || rows.length != 20)) {
      throw const FormatException('旧地点图片列表无法核实');
    }
    final observed = humanControlTime(value['observedAt']);
    final until = humanControlTime(value['validUntil']);
    humanControlLease(observed, until);
    if (until.difference(observed) > const Duration(seconds: 30)) {
      throw const FormatException('旧地点图片资料已到期');
    }
    final ids = <String>{};
    final items = <PrivateReferenceTarget>[];
    for (final row in rows) {
      final item = humanControlObject(row, {'targetId', 'name', 'imageCount'});
      if (!humanControlID(item['targetId']) ||
          !ids.add(item['targetId']) ||
          item['name'] is! String ||
          (item['name'] as String).isEmpty ||
          utf8.encode(item['name']).length > 16000 ||
          (item['name'] as String).contains('\u0000') ||
          item['imageCount'] is! int ||
          item['imageCount'] < 1 ||
          item['imageCount'] > 10000) {
        throw const FormatException('旧地点图片目标无法核实');
      }
      items.add(
        PrivateReferenceTarget(
          item['targetId'],
          item['name'],
          item['imageCount'],
        ),
      );
    }
    return PrivateReferenceTargetsPageData(
      List.unmodifiable(items),
      until,
      value['sourceVersion'],
      next,
    );
  }
}

/// The list discovers original image contributions. It assigns no Place
/// ownership and grants no public access; each opened target is fetched again.
class PrivateReferenceTargetsPage extends StatefulWidget {
  const PrivateReferenceTargetsPage({
    super.key,
    required this.authorizationHeader,
    required this.ownerID,
    required this.sourceCurrent,
    this.identityChanges,
    this.client,
    this.apiBaseUrl,
  });
  final String? Function() authorizationHeader, ownerID;
  final bool Function() sourceCurrent;
  final Listenable? identityChanges;
  final http.Client? client;
  final String? apiBaseUrl;

  @override
  State<PrivateReferenceTargetsPage> createState() => _TargetsState();
}

class _TargetsState extends State<PrivateReferenceTargetsPage>
    with WidgetsBindingObserver {
  late final http.Client _client;
  late final String _origin;
  late final String? _owner;
  late final Listenable _changes;
  final _boundary = ValueNotifier<int>(0);
  String? _token, _next, _source, _error;
  List<PrivateReferenceTarget> _items = const [];
  DateTime? _until;
  Timer? _lease;
  StreamIterator<List<int>>? _reader;
  final _cursors = <String>{};
  bool _retired = false, _foreground = true, _loading = false, _expired = false;
  int _serial = 0;

  bool get _current =>
      mounted &&
      !_retired &&
      _foreground &&
      widget.sourceCurrent() &&
      widget.ownerID() == _owner &&
      _owner != null &&
      widget.authorizationHeader() == _token &&
      _token != null;

  @override
  void initState() {
    super.initState();
    _client = widget.client ?? http.Client();
    _origin = (widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl).replaceFirst(
      RegExp(r'/$'),
      '',
    );
    _owner = widget.ownerID();
    _token = widget.authorizationHeader();
    _changes = Listenable.merge([widget.identityChanges, _boundary]);
    widget.identityChanges?.addListener(_observe);
    WidgetsBinding.instance.addObserver(this);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_current) unawaited(_load());
    });
  }

  void _clear() {
    _serial++;
    _lease?.cancel();
    unawaited(_reader?.cancel());
    _reader = null;
    _items = const [];
    _next = _source = null;
    _until = null;
    _cursors.clear();
    _loading = false;
    _boundary.value++;
  }

  void _observe() {
    if (!mounted || _retired) return;
    if (widget.ownerID() != _owner ||
        !widget.sourceCurrent() ||
        widget.authorizationHeader() == null) {
      _retired = true;
      _clear();
      setState(() => _error = null);
    } else if (_token != widget.authorizationHeader()) {
      _clear();
      _token = widget.authorizationHeader();
      setState(() => _error = null);
      if (_foreground) unawaited(_load());
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _foreground = true;
      if (_current) unawaited(_load());
    } else {
      _foreground = false;
      _clear();
      if (mounted) setState(() {});
    }
  }

  Future<void> _load({bool more = false}) async {
    if (!_current || _loading || more && _next == null) return;
    if (more && (_until == null || !_until!.isAfter(DateTime.now().toUtc()))) {
      _expire();
      return;
    }
    final serial = ++_serial, token = _token;
    final cursor = more ? _next : null;
    bool current() =>
        _current &&
        serial == _serial &&
        token == _token &&
        (!more || _until?.isAfter(DateTime.now().toUtc()) == true);
    setState(() {
      _loading = true;
      _error = null;
    });
    StreamIterator<List<int>>? reader;
    final bytes = BytesBuilder(copy: true);
    try {
      final response = await _client
          .send(
            http.Request(
                'GET',
                Uri.parse('$_origin/v1/me/reference-image-targets').replace(
                  queryParameters: {
                    'targetType': 'PLACE',
                    if (cursor != null) 'cursor': cursor,
                  },
                ),
              )
              ..followRedirects = false
              ..headers['Authorization'] = token!,
          )
          .timeout(const Duration(seconds: 12));
      if (!current() ||
          response.contentLength != null &&
              response.contentLength! > 512 * 1024) {
        await response.stream.listen(null).cancel();
        throw const FormatException('旧地点图片列表来源已变化');
      }
      reader = StreamIterator(response.stream);
      _reader = reader;
      final deadline = DateTime.now().add(const Duration(seconds: 20));
      while (true) {
        final left = deadline.difference(DateTime.now());
        if (left <= Duration.zero) throw TimeoutException('旧地点图片读取超时');
        if (!await reader.moveNext().timeout(left)) break;
        if (!current() || bytes.length + reader.current.length > 512 * 1024) {
          throw const FormatException('旧地点图片列表来源已变化');
        }
        bytes.add(reader.current);
      }
      if (!current()) return;
      if (response.statusCode != 200) {
        if (response.statusCode == 401 || response.statusCode == 403) {
          _retired = true;
          _clear();
          if (mounted) setState(() {});
          return;
        }
        if (response.statusCode == 409) {
          _clear();
          if (mounted) setState(() => _error = '地点图片资料已变化，请重新读取。');
          return;
        }
        throw const FormatException('旧地点图片暂不可读取，请重新读取。');
      }
      if (!(response.headers['content-type'] ?? '').startsWith(
        'application/json',
      )) {
        throw const FormatException('旧地点图片列表格式不符');
      }
      final data = humanControlObject(
        jsonDecode(utf8.decode(bytes.takeBytes())),
        {'data'},
      );
      final page = PrivateReferenceTargetsPageData.decode(data['data']);
      if (!current()) return;
      if (more &&
          (page.sourceVersion != _source ||
              _cursors.contains(page.nextCursor) ||
              page.nextCursor == cursor ||
              page.items.any(
                (item) => _items.any((old) => old.id == item.id),
              ))) {
        _clear();
        if (mounted) setState(() => _error = '地点图片资料已变化，请重新读取。');
        return;
      }
      if (!more) _cursors.clear();
      if (cursor != null) _cursors.add(cursor);
      _items = List.unmodifiable([if (more) ..._items, ...page.items]);
      _next = page.nextCursor;
      _source = page.sourceVersion;
      _until = more && _until!.isBefore(page.validUntil)
          ? _until
          : page.validUntil;
      _expired = false;
      _lease?.cancel();
      _lease = Timer(_until!.difference(DateTime.now().toUtc()), _expire);
    } catch (_) {
      if (mounted && !_retired && serial == _serial && !_expired) {
        setState(() => _error = '旧地点图片暂不可读取，请重新读取。');
      }
    } finally {
      await reader?.cancel();
      if (identical(_reader, reader)) _reader = null;
      final discarded = bytes.takeBytes();
      discarded.fillRange(0, discarded.length, 0);
      if (mounted && serial == _serial) setState(() => _loading = false);
    }
  }

  void _expire() {
    if (!mounted || _retired) return;
    _items = const [];
    _next = _source = null;
    _until = null;
    _cursors.clear();
    setState(() => _expired = true);
  }

  Future<void> _open(PrivateReferenceTarget target) async {
    if (!_current || _until?.isAfter(DateTime.now().toUtc()) != true) return;
    final boundary = _boundary.value;
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => Scaffold(
          appBar: birdtieAppBar(context, title: const Text('旧地点图片')),
          body: SingleChildScrollView(
            padding: const EdgeInsets.all(16),
            child: PrivateReferenceMediaView(
              targetType: 'PLACE',
              targetID: target.id,
              authorizationHeader: widget.authorizationHeader,
              ownerID: widget.ownerID,
              sourceCurrent: () => _current && boundary == _boundary.value,
              identityChanges: _changes,
              client: _client,
              apiBaseUrl: _origin,
            ),
          ),
        ),
      ),
    );
    if (_current) await _load();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: birdtieAppBar(context, title: const Text('我的旧地点图片')),
    body: !_current
        ? const Center(child: Text('当前个人身份已变化，请返回后重新打开。'))
        : ListView(
            padding: const EdgeInsets.all(16),
            children: [
              const Text('仅本人可见的原贡献图片。地点和图片不会因此公开。'),
              TextButton.icon(
                onPressed: _loading ? null : () => _load(),
                icon: const Icon(Icons.refresh),
                label: const Text('重新读取地点图片'),
              ),
              if (_loading) const LinearProgressIndicator(),
              if (_expired) const Text('这批地点资料已到期，请重新读取。'),
              if (_error != null) Text(_error!),
              for (final item in _items)
                ListTile(
                  title: Text(item.name),
                  subtitle: Text('${item.imageCount} 张原图片 · 仅本人可见'),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => _open(item),
                ),
              if (!_loading && !_expired && _error == null && _items.isEmpty)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 24),
                  child: Text('暂无可读取的旧地点图片。'),
                ),
              if (_next != null)
                OutlinedButton(
                  onPressed: _loading ? null : () => _load(more: true),
                  child: const Text('继续读取地点图片'),
                ),
            ],
          ),
  );

  @override
  void dispose() {
    widget.identityChanges?.removeListener(_observe);
    WidgetsBinding.instance.removeObserver(this);
    _retired = true;
    _clear();
    _boundary.dispose();
    if (widget.client == null) _client.close();
    super.dispose();
  }
}
