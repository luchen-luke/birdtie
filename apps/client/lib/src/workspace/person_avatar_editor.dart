import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:image_picker/image_picker.dart';

import '../content/moment_image_header.dart';
import 'entity_share_pending_store.dart' show newEntityShareOperationID;
import 'person_avatar.dart';

class PendingPersonAvatar {
  const PendingPersonAvatar(this.operationID, this.version, this.action);
  final String operationID, action;
  final int version;
  Map<String, Object> toJson() => {
    'operationId': operationID,
    'expectedVersion': version,
    'action': action,
  };
  static PendingPersonAvatar decode(String text) {
    final raw = jsonDecode(text);
    if (raw is! Map<String, dynamic> ||
        raw.length != 3 ||
        raw['operationId'] is! String ||
        !personAvatarID(raw['operationId'] as String) ||
        raw['expectedVersion'] is! int ||
        (raw['expectedVersion'] as int) < 0 ||
        !const {'SAVE', 'DELETE'}.contains(raw['action'])) {
      throw const FormatException('头像待核实操作无法读取');
    }
    return PendingPersonAvatar(
      raw['operationId'] as String,
      raw['expectedVersion'] as int,
      raw['action'] as String,
    );
  }
}

abstract interface class PersonAvatarPendingStore {
  Future<PendingPersonAvatar?> read(String base, String owner);
  Future<void> write(String base, String owner, PendingPersonAvatar pending);
  Future<void> remove(String base, String owner);
}

class SecurePersonAvatarPendingStore implements PersonAvatarPendingStore {
  final _storage = const FlutterSecureStorage();
  String _key(String base, String owner) =>
      'birdtie.avatar.pending.v1.${sha256.convert(utf8.encode(base))}.$owner';
  @override
  Future<PendingPersonAvatar?> read(String base, String owner) async {
    final raw = await _storage.read(key: _key(base, owner));
    return raw == null ? null : PendingPersonAvatar.decode(raw);
  }

  @override
  Future<void> write(String base, String owner, PendingPersonAvatar pending) =>
      _storage.write(
        key: _key(base, owner),
        value: jsonEncode(pending.toJson()),
      );
  @override
  Future<void> remove(String base, String owner) =>
      _storage.delete(key: _key(base, owner));
}

class MemoryPersonAvatarPendingStore implements PersonAvatarPendingStore {
  final _values = <(String, String), PendingPersonAvatar>{};
  @override
  Future<PendingPersonAvatar?> read(String base, String owner) async =>
      _values[(base, owner)];
  @override
  Future<void> write(
    String base,
    String owner,
    PendingPersonAvatar pending,
  ) async {
    _values[(base, owner)] = pending;
  }

  @override
  Future<void> remove(String base, String owner) async {
    _values.remove((base, owner));
  }
}

Future<Uint8List?> pickPersonAvatarImage() async {
  final file = await ImagePicker().pickImage(
    source: ImageSource.gallery,
    requestFullMetadata: false,
  );
  if (file == null) return null;
  final size = await file.length();
  if (size < 1 || size > personAvatarMaxInputBytes)
    throw const FormatException('请选择 5 MiB 以内的静态 JPEG 或 PNG 图片。');
  final bytes = BytesBuilder(copy: false);
  await for (final chunk in file.openRead()) {
    if (chunk.length > personAvatarMaxInputBytes - bytes.length)
      throw const FormatException('头像图片超过 5 MiB。');
    bytes.add(chunk);
  }
  final raw = bytes.takeBytes();
  if (raw.length != size) throw const FormatException('头像读取不完整，请重新选择。');
  return raw;
}

Future<Uint8List> prepareLocalPersonAvatar(Uint8List raw) async {
  if (raw.length > personAvatarMaxInputBytes)
    throw const FormatException('头像图片超过 5 MiB。');
  final header = MomentImageHeader.inspect(raw);
  if (header.width > 4096 || header.height > 4096)
    throw const FormatException('头像图片尺寸不能超过 4096 像素。');
  final codec = await ui.instantiateImageCodec(raw);
  ui.Image? image, thumb;
  ui.Picture? picture;
  final recorder = ui.PictureRecorder();
  try {
    if (codec.frameCount != 1) throw const FormatException('仅支持静态头像。');
    image = (await codec.getNextFrame()).image;
    final side = image.width < image.height
        ? image.width.toDouble()
        : image.height.toDouble();
    final canvas = ui.Canvas(recorder);
    canvas.drawColor(const ui.Color(0xffffffff), ui.BlendMode.src);
    canvas.drawImageRect(
      image,
      ui.Rect.fromLTWH(
        (image.width - side) / 2,
        (image.height - side) / 2,
        side,
        side,
      ),
      const ui.Rect.fromLTWH(0, 0, 128, 128),
      ui.Paint()..filterQuality = ui.FilterQuality.medium,
    );
    picture = recorder.endRecording();
    thumb = await picture.toImage(128, 128);
    final data = await thumb.toByteData(format: ui.ImageByteFormat.png);
    if (data == null || data.lengthInBytes > personAvatarMaxThumbnailBytes)
      throw const FormatException('头像处理失败，请重新选择。');
    return Uint8List.fromList(
      data.buffer.asUint8List(data.offsetInBytes, data.lengthInBytes),
    );
  } finally {
    if (recorder.isRecording) recorder.endRecording().dispose();
    picture?.dispose();
    thumb?.dispose();
    image?.dispose();
    codec.dispose();
  }
}

/// Explicit local selection -> preview -> save. Session/workspace changes
/// retire both the draft and any late mutation result; recovery never replays.
class PersonAvatarEditor extends StatefulWidget {
  const PersonAvatarEditor({
    super.key,
    required this.ownerID,
    required this.baseUrl,
    required this.authorizationHeader,
    required this.identityChanges,
    required this.current,
    this.client,
    this.pendingStore,
    this.picker,
    this.onChanged,
  });
  final String ownerID, baseUrl;
  final String? Function() authorizationHeader;
  final Listenable identityChanges;
  final bool Function() current;
  final http.Client? client;
  final PersonAvatarPendingStore? pendingStore;
  final Future<Uint8List?> Function()? picker;
  final VoidCallback? onChanged;
  @override
  State<PersonAvatarEditor> createState() => _PersonAvatarEditorState();
}

class _PersonAvatarEditorState extends State<PersonAvatarEditor> {
  late http.Client _client;
  late bool _ownsClient;
  late PersonAvatarPendingStore _pendingStore;
  PersonAvatarState? _state;
  PendingPersonAvatar? _pending;
  Uint8List? _draft;
  String? _message;
  bool _busy = false, _failed = false;
  int _serial = 0;
  Object? _binding;
  Object get _currentBinding => (
    widget.ownerID,
    widget.baseUrl,
    widget.authorizationHeader(),
    widget.current(),
    widget.client,
  );
  bool _current(Object binding, int serial) =>
      mounted &&
      widget.current() &&
      widget.authorizationHeader() != null &&
      _currentBinding == binding &&
      _serial == serial;
  PersonAvatarAPI get _api => PersonAvatarAPI(
    client: _client,
    baseUrl: widget.baseUrl,
    authorizationHeader: widget.authorizationHeader,
  );
  @override
  void initState() {
    super.initState();
    _client = widget.client ?? http.Client();
    _ownsClient = widget.client == null;
    _pendingStore = widget.pendingStore ?? SecurePersonAvatarPendingStore();
    widget.identityChanges.addListener(_changed);
    unawaited(_load());
  }

  void _changed() {
    if (_binding != _currentBinding) {
      unawaited(_load());
    }
  }

  @override
  void didUpdateWidget(PersonAvatarEditor old) {
    super.didUpdateWidget(old);
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges.removeListener(_changed);
      widget.identityChanges.addListener(_changed);
    }
    if (old.client != widget.client) {
      if (_ownsClient) _client.close();
      _client = widget.client ?? http.Client();
      _ownsClient = widget.client == null;
    }
    if (old.pendingStore != widget.pendingStore)
      _pendingStore = widget.pendingStore ?? SecurePersonAvatarPendingStore();
    if (_binding != _currentBinding || old.pendingStore != widget.pendingStore)
      unawaited(_load());
  }

  Future<void> _load() async {
    final binding = _currentBinding, serial = ++_serial;
    _binding = binding;
    setState(() {
      _state = null;
      _draft = null;
      _pending = null;
      _message = null;
      _failed = false;
      _busy = true;
    });
    if (!widget.current() || widget.authorizationHeader() == null) {
      setState(() => _busy = false);
      return;
    }
    final base = widget.baseUrl,
        owner = widget.ownerID,
        api = _api,
        store = _pendingStore;
    try {
      final pending = await store.read(base, owner);
      if (!_current(binding, serial)) return;
      final state = await api.own(owner);
      if (!_current(binding, serial)) return;
      setState(() {
        _state = state;
        _pending = pending;
        _message = pending == null ? null : '上次头像变更结果待核实，请先核实。';
      });
    } catch (e) {
      if (_current(binding, serial))
        setState(() {
          _failed = true;
          _message = e is PersonAvatarFailure ? e.message : '头像读取失败，请重试。';
        });
    } finally {
      if (_current(binding, serial)) setState(() => _busy = false);
    }
  }

  Future<void> _pick() async {
    if (_busy || _pending != null || _state == null) return;
    final binding = _currentBinding, serial = _serial;
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      final bytes = await (widget.picker ?? pickPersonAvatarImage)();
      if (!_current(binding, serial) || bytes == null) return;
      final thumb = await prepareLocalPersonAvatar(bytes);
      if (!_current(binding, serial)) return;
      setState(() => _draft = thumb);
    } catch (e) {
      if (_current(binding, serial))
        setState(
          () => _message = e is FormatException
              ? e.message.toString()
              : '未能选择头像，请重试。',
        );
    } finally {
      if (_current(binding, serial)) setState(() => _busy = false);
    }
  }

  Future<void> _change({required bool deleting}) async {
    if (_busy ||
        _pending != null ||
        _state == null ||
        !deleting && _draft == null)
      return;
    final binding = _currentBinding,
        serial = _serial,
        base = widget.baseUrl,
        owner = widget.ownerID,
        api = _api,
        store = _pendingStore;
    final pending = PendingPersonAvatar(
      newEntityShareOperationID(),
      _state!.version,
      deleting ? 'DELETE' : 'SAVE',
    );
    final bytes = deleting ? null : _draft;
    setState(() {
      _busy = true;
      _message = null;
    });
    var persisted = false;
    try {
      await store.write(base, owner, pending);
      persisted = true;
      if (!_current(binding, serial)) return;
      setState(() => _pending = pending);
      final result = await api.change(
        owner,
        pending.operationID,
        pending.version,
        bytes: bytes,
      );
      if (!_current(binding, serial)) return;
      await store.remove(base, owner);
      if (!_current(binding, serial)) return;
      setState(() {
        _state = result.current;
        _pending = null;
        _draft = null;
        _message = result.current.version == result.appliedVersion
            ? (deleting ? '头像已删除。' : '头像已保存。')
            : '变更已完成，当前头像随后又有更新。';
      });
      widget.onChanged?.call();
    } catch (e) {
      if (e is PersonAvatarFailure &&
          const {400, 401, 403, 409}.contains(e.status)) {
        if (persisted) await store.remove(base, owner);
        if (_current(binding, serial))
          setState(() {
            _pending = null;
            _message = e.message;
            _failed = e.status == 409;
          });
      } else if (_current(binding, serial))
        setState(
          () => _message = persisted
              ? '保存结果待核实，请先核实，避免重复修改。'
              : '无法保存待核实记录，请稍后重试。',
        );
    } finally {
      if (_current(binding, serial)) setState(() => _busy = false);
    }
  }

  Future<void> _delete() async {
    final binding = _currentBinding,
        serial = _serial,
        version = _state?.version;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除头像？'),
        content: const Text('删除后恢复为默认头像，之前的头像链接将失效。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('删除头像'),
          ),
        ],
      ),
    );
    if (confirmed == true &&
        _current(binding, serial) &&
        version == _state?.version)
      await _change(deleting: true);
  }

  Future<void> _reconcile() async {
    final pending = _pending;
    if (_busy || pending == null) return;
    final binding = _currentBinding,
        serial = _serial,
        base = widget.baseUrl,
        owner = widget.ownerID,
        api = _api,
        store = _pendingStore;
    setState(() => _busy = true);
    try {
      final result = await api.operation(
        owner,
        pending.operationID,
        action: pending.action,
        expectedVersion: pending.version,
      );
      if (!_current(binding, serial)) return;
      await store.remove(base, owner);
      if (!_current(binding, serial)) return;
      setState(() {
        _state = result.current;
        _pending = null;
        _draft = null;
        _message = '头像变更已核实，正在显示当前头像。';
      });
      widget.onChanged?.call();
    } catch (e) {
      if (_current(binding, serial))
        setState(
          () => _message = e is PersonAvatarFailure && e.status == 404
              ? '尚未查到该操作。可以稍后再核实，或重新查看当前头像后开始新的修改。'
              : e is PersonAvatarFailure
              ? e.message
              : '核实失败，请稍后重试。',
        );
    } finally {
      if (_current(binding, serial)) setState(() => _busy = false);
    }
  }

  Future<void> _viewCurrent() async {
    if (_busy) return;
    final binding = _currentBinding,
        serial = _serial,
        base = widget.baseUrl,
        owner = widget.ownerID,
        api = _api,
        store = _pendingStore;
    setState(() => _busy = true);
    try {
      final state = await api.own(owner);
      if (!_current(binding, serial)) return;
      await store.remove(base, owner);
      if (!_current(binding, serial)) return;
      setState(() {
        _state = state;
        _pending = null;
        _draft = null;
        _failed = false;
        _message = '已重新读取当前头像。先前变更不再自动提交。';
      });
    } catch (e) {
      if (_current(binding, serial))
        setState(
          () => _message = e is PersonAvatarFailure ? e.message : '读取失败，请稍后重试。',
        );
    } finally {
      if (_current(binding, serial)) setState(() => _busy = false);
    }
  }

  @override
  void dispose() {
    ++_serial;
    widget.identityChanges.removeListener(_changed);
    if (_ownsClient) _client.close();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.current() || widget.authorizationHeader() == null)
      return const SizedBox.shrink();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            _draft == null
                ? PersonAvatar(
                    metadata: _state?.avatar,
                    authorizationHeader: widget.authorizationHeader,
                    baseUrl: widget.baseUrl,
                    client: _client,
                    identityChanges: widget.identityChanges,
                    current: widget.current,
                    radius: 28,
                  )
                : ClipOval(
                    child: Image.memory(
                      _draft!,
                      key: const Key('person-avatar-local-preview'),
                      width: 56,
                      height: 56,
                      fit: BoxFit.cover,
                    ),
                  ),
            const SizedBox(width: 12),
            const Expanded(child: Text('头像\n可见范围沿用已保存的个人资料设置。')),
          ],
        ),
        if (_busy) const LinearProgressIndicator(),
        Wrap(
          spacing: 8,
          children: [
            TextButton.icon(
              key: const Key('person-avatar-select'),
              onPressed: _busy || _pending != null || _state == null
                  ? null
                  : _pick,
              icon: const Icon(Icons.photo_library_outlined),
              label: Text(_state?.avatar == null ? '选择头像' : '替换头像'),
            ),
            if (_draft != null && _pending == null)
              FilledButton(
                key: const Key('person-avatar-save'),
                onPressed: _busy ? null : () => _change(deleting: false),
                child: const Text('保存头像'),
              ),
            if (_draft != null && _pending == null)
              TextButton(
                onPressed: _busy ? null : () => setState(() => _draft = null),
                child: const Text('取消选择'),
              ),
            if (_state?.avatar != null && _pending == null)
              TextButton(
                key: const Key('person-avatar-delete'),
                onPressed: _busy ? null : _delete,
                child: const Text('删除头像'),
              ),
            if (_pending != null)
              TextButton(
                key: const Key('person-avatar-reconcile'),
                onPressed: _busy ? null : _reconcile,
                child: const Text('核实头像结果'),
              ),
            if (_pending != null)
              TextButton(
                onPressed: _busy ? null : _viewCurrent,
                child: const Text('重新查看当前头像'),
              ),
            if (_failed)
              TextButton(
                onPressed: _busy ? null : _load,
                child: const Text('刷新头像'),
              ),
          ],
        ),
        if (_draft != null) const Text('这是本机预览。保存后，头像才会向有权查看你资料的人展示。'),
        if (_message != null)
          Text(_message!, key: const Key('person-avatar-status')),
        const SizedBox(height: 12),
      ],
    );
  }
}
