import 'dart:typed_data';
import 'package:video_player/video_player.dart';

VideoPlayerController? createVideoMaterialPlayer(String path) => null;
Future<String> createVideoMaterialMemoryPath(Uint8List bytes) async =>
    throw const FormatException('此平台暂不支持视频预览。');
Future<void> releaseVideoMaterialPath(String path) async {}
