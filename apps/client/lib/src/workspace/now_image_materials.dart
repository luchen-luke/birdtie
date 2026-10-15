import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../content/moment_image_picker.dart';
import 'local_image_draft.dart';

/// Local pixels only. This basket carries no remote approval or media receipt.
class NowImageMaterials extends ChangeNotifier {
  Object? _scope;
  int _serial = 0;
  int revision = 0;
  bool busy = false;
  bool _disposed = false;
  String? error;
  final _images = <Uint8List>[];
  List<Uint8List> get images =>
      List.unmodifiable(_images.map((bytes) => bytes.asUnmodifiableView()));
  bool current(Object scope) => !_disposed && _scope == scope;

  void bind(Object scope) {
    if (_scope == scope) return;
    clear();
    _scope = scope;
  }

  Future<void> select({
    required Object scope,
    required bool Function() current,
    required MomentImagePicker picker,
  }) async {
    if (_disposed || busy || !current()) return;
    bind(scope);
    final serial = ++_serial;
    bool valid() =>
        !_disposed && serial == _serial && _scope == scope && current();
    busy = true;
    error = null;
    notifyListeners();
    final prepared = <Uint8List>[];
    List<MomentImageSelection> selections = const [];
    try {
      selections = await picker.selectMany(limit: 9 - _images.length);
      if (!valid()) return;
      for (final selected in selections) {
        if (selected.bytes.length > 5 * 1024 * 1024) {
          throw const FormatException('公开图片每张最多 5 MiB');
        }
        final pixels = await decodeLocalImagePixels(selected);
        try {
          if (!valid()) return;
          // Keep selected bytes in RAM. The server removes source metadata and
          // produces the canonical display/thumbnail before any publication.
          prepared.add(Uint8List.fromList(selected.bytes));
        } finally {
          pixels.dispose();
        }
      }
      if (!valid()) return;
      _images.addAll(prepared);
      prepared.clear();
      ++revision;
    } on FormatException catch (e) {
      if (valid()) error = e.message;
    } catch (_) {
      if (valid()) error = '图片暂时无法读取，请重新选择。';
    } finally {
      for (final selection in selections) {
        selection.bytes.fillRange(0, selection.bytes.length, 0);
      }
      for (final bytes in prepared) {
        bytes.fillRange(0, bytes.length, 0);
      }
      if (!_disposed && serial == _serial && _scope == scope) {
        busy = false;
        notifyListeners();
      }
    }
  }

  void remove(int index) {
    if (_disposed || busy || index < 0 || index >= _images.length) return;
    final bytes = _images.removeAt(index);
    bytes.fillRange(0, bytes.length, 0);
    ++revision;
    notifyListeners();
  }

  void move(int index, int delta) {
    final next = index + delta;
    if (_disposed ||
        busy ||
        index < 0 ||
        index >= _images.length ||
        next < 0 ||
        next >= _images.length)
      return;
    final image = _images.removeAt(index);
    _images.insert(next, image);
    ++revision;
    notifyListeners();
  }

  void clear() {
    ++_serial;
    ++revision;
    busy = false;
    error = null;
    for (final bytes in _images) {
      bytes.fillRange(0, bytes.length, 0);
    }
    _images.clear();
  }

  @override
  void dispose() {
    if (_disposed) return;
    clear();
    _disposed = true;
    super.dispose();
  }
}

class NowImageMaterialsPreview extends StatelessWidget {
  const NowImageMaterialsPreview({
    super.key,
    required this.materials,
    required this.onAdd,
    this.editable = true,
  });
  final NowImageMaterials materials;
  final VoidCallback onAdd;
  final bool editable;

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: materials,
    builder: (context, _) => Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('${materials.images.length}/9 张图片 · 仍在本机'),
        if (materials.images.isNotEmpty)
          SizedBox(
            height: 152,
            child: ListView.separated(
              scrollDirection: Axis.horizontal,
              itemCount: materials.images.length,
              separatorBuilder: (_, _) => const SizedBox(width: 8),
              itemBuilder: (context, index) => SizedBox(
                width: 156,
                child: Column(
                  children: [
                    Expanded(
                      child: ClipRRect(
                        borderRadius: BorderRadius.circular(8),
                        child: Image.memory(
                          materials.images[index],
                          fit: BoxFit.cover,
                          width: 156,
                          cacheWidth: 384,
                          cacheHeight: 256,
                          gaplessPlayback: false,
                        ),
                      ),
                    ),
                    Row(
                      mainAxisAlignment: MainAxisAlignment.center,
                      children: [
                        IconButton(
                          tooltip: '第 ${index + 1} 张前移',
                          onPressed: editable && !materials.busy && index > 0
                              ? () => materials.move(index, -1)
                              : null,
                          icon: const Icon(Icons.chevron_left),
                        ),
                        IconButton(
                          tooltip: '移除第 ${index + 1} 张',
                          onPressed: editable && !materials.busy
                              ? () => materials.remove(index)
                              : null,
                          icon: const Icon(Icons.close),
                        ),
                        IconButton(
                          tooltip: '第 ${index + 1} 张后移',
                          onPressed:
                              editable &&
                                  !materials.busy &&
                                  index + 1 < materials.images.length
                              ? () => materials.move(index, 1)
                              : null,
                          icon: const Icon(Icons.chevron_right),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            ),
          ),
        if (materials.error != null)
          Text(
            materials.error!,
            style: TextStyle(color: Theme.of(context).colorScheme.error),
          ),
        if (editable)
          TextButton.icon(
            onPressed: materials.busy || materials.images.length == 9
                ? null
                : onAdd,
            icon: materials.busy
                ? const SizedBox.square(
                    dimension: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.add_photo_alternate_outlined),
            label: const Text('添加图片'),
          ),
        const Text('图片未写入本机草稿存储；关闭或切换账号后需要重新选择。'),
      ],
    ),
  );
}
