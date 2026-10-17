import 'dart:io';
import 'dart:typed_data';
import 'package:video_player/video_player.dart';
import 'video_material_sink_io.dart' as streamed;

VideoPlayerController? createVideoMaterialPlayer(String path) {
  if (!Platform.isAndroid && !Platform.isIOS && !Platform.isMacOS) return null;
  return VideoPlayerController.file(
    File(path),
    videoPlayerOptions: VideoPlayerOptions(mixWithOthers: false),
  );
}

final _ownedVideoPaths = <String, Directory>{};
Future<String> createVideoMaterialMemoryPath(Uint8List bytes) async {
  final dir = await Directory.systemTemp.createTemp('birdtie-video-preview-');
  final file = File('${dir.path}${Platform.pathSeparator}video.mp4');
  try {
    await file.writeAsBytes(bytes, flush: true);
    _ownedVideoPaths[file.path] = dir;
    return file.path;
  } catch (_) {
    if (await file.exists()) await file.delete();
    await dir.delete();
    rethrow;
  }
}

Future<void> releaseVideoMaterialPath(String path) async {
  await streamed.releaseSinkVideoMaterial(path);
  final dir = _ownedVideoPaths.remove(path);
  if (dir == null) return; // Original gallery files are never deleted.
  final file = File(path);
  if (await file.exists()) await file.delete();
  if (await dir.exists()) await dir.delete();
}
