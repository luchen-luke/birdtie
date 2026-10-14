import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter/material.dart';

import 'voice_input.dart';

/// A notice acknowledgement for this exact processing mode and service. This
/// is neither microphone permission nor permission to send a message.
abstract class VoiceInputConsentStore {
  Future<bool> acknowledged(VoiceSystemService? service);
  Future<void> acknowledge(VoiceSystemService? service);
}

String _scope(VoiceSystemService? service) => jsonEncode({
  'version': 2,
  'mode': service == null
      ? 'android_on_device'
      : service is VoiceBundledEngine
      ? 'birdtie_bundled_offline'
      : service is VoiceSystemActivity
      ? 'android_system_activity_may_network'
      : 'android_system_may_network',
  'serviceId': service?.id,
  'serviceName': service?.name,
});

class SecureVoiceInputConsentStore implements VoiceInputConsentStore {
  SecureVoiceInputConsentStore({this.storage = const FlutterSecureStorage()});
  final FlutterSecureStorage storage;
  static const _key = 'birdtie.voice-input.processing-notice.v2';
  final _accepted = <String>{};

  @override
  Future<bool> acknowledged(VoiceSystemService? service) async {
    final scope = _scope(service);
    if (_accepted.contains(scope)) return true;
    try {
      if (await storage.read(key: _key) == scope) {
        _accepted.add(scope);
        return true;
      }
    } catch (_) {
      // No stored acknowledgement can be established; disclose again.
    }
    return false;
  }

  @override
  Future<void> acknowledge(VoiceSystemService? service) async {
    final scope = _scope(service);
    _accepted.add(scope);
    try {
      await storage.write(key: _key, value: scope);
    } catch (_) {
      // This explicit session choice remains valid, but a future application
      // instance must disclose again if storage did not persist it.
    }
  }
}

class MemoryVoiceInputConsentStore implements VoiceInputConsentStore {
  final _accepted = <String>{};
  @override
  Future<bool> acknowledged(VoiceSystemService? service) async =>
      _accepted.contains(_scope(service));
  @override
  Future<void> acknowledge(VoiceSystemService? service) async {
    _accepted.add(_scope(service));
  }
}

Future<void> showVoiceInputInformation(BuildContext context) async {
  final port = NativeVoiceInputPort();
  VoiceAvailability? available;
  try {
    available = await port.availability();
  } finally {
    port.dispose();
  }
  if (!context.mounted) return;
  await showModalBottomSheet<void>(
    context: context,
    showDragHandle: true,
    isScrollControlled: true,
    useSafeArea: true,
    builder: (context) => SingleChildScrollView(
      padding: const EdgeInsets.fromLTRB(24, 8, 24, 24),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('语音输入', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 12),
          Text(
            available?.bundledOffline == true
                ? '本机 Whisper 语音识别'
                : available?.preferredSystem == null
                ? '当前设备没有可用的系统语音服务。'
                : '系统语音服务：${available!.preferredSystem!.name}',
          ),
          const SizedBox(height: 8),
          Text(
            available?.bundledOffline == true
                ? '声音只在本机处理，不上传。录音仅保存在内存中，处理完成或取消后清除。录音最多30秒，可随时停止并转成可编辑文字，由你点击发送。模型未准备好时可继续使用键盘。'
                : '对助手说话会转成可编辑文字，由你点击发送。优先使用已安装的本机识别；切换到可能联网的系统语音服务前会说明。系统服务如何处理音频，请查看该服务的数据政策。',
          ),
          const SizedBox(height: 8),
          const Text('尝试自动识别中文、英文及混合表达，实际能力取决于设备和识别服务。识别结果可修改，不会自动翻译或发送。'),
          const SizedBox(height: 8),
          const Text('好友语音与助手转写分开；只有你明确发送后，对方才可播放语音消息。'),
        ],
      ),
    ),
  );
}
