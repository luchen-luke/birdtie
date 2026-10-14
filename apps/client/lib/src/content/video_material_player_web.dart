import 'dart:js_interop';
import 'dart:typed_data';
import 'package:video_player/video_player.dart';
import 'package:web/web.dart' as web;

VideoPlayerController? createVideoMaterialPlayer(String path) {
  final uri = Uri.tryParse(path);
  if (uri == null || uri.scheme != 'blob') return null;
  return VideoPlayerController.networkUrl(
    uri,
    videoPlayerOptions: VideoPlayerOptions(mixWithOthers: false),
  );
}

Future<String> createVideoMaterialMemoryPath(Uint8List bytes) async =>
    web.URL.createObjectURL(
      web.Blob(
        <JSAny>[bytes.toJS].toJS,
        web.BlobPropertyBag(type: 'video/mp4'),
      ),
    );
Future<void> releaseVideoMaterialPath(String path) async {
  if (Uri.tryParse(path)?.scheme == 'blob') web.URL.revokeObjectURL(path);
}
