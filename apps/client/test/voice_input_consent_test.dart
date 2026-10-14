import 'package:birdtie_client/src/workspace/voice_input.dart';
import 'package:birdtie_client/src/workspace/voice_input_consent.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('本机Whisper处理说明有独立认可范围，不能复用系统或旧本地说明', () async {
    final store = MemoryVoiceInputConsentStore();
    const bundled = VoiceBundledEngine();
    const sameNameService = VoiceSystemService(
      id: 'birdtie.bundled.whisper.base-q5_1.v1',
      name: '本机 Whisper 语音识别',
    );
    await store.acknowledge(null);
    await store.acknowledge(sameNameService);
    expect(await store.acknowledged(bundled), isFalse);
    await store.acknowledge(bundled);
    expect(await store.acknowledged(const VoiceBundledEngine()), isTrue);
    expect(await MemoryVoiceInputConsentStore().acknowledged(bundled), isFalse);
  });
  test('系统界面说明绑定实际Activity与模式，不能复用服务认可', () async {
    final store = MemoryVoiceInputConsentStore();
    const activity = VoiceSystemActivity(id: 'a/.Speech', name: '系统语音');
    const service = VoiceSystemService(id: 'a/.Speech', name: '系统语音');
    const changed = VoiceSystemActivity(id: 'b/.Speech', name: '系统语音');
    expect(await store.acknowledged(activity), isFalse);
    await store.acknowledge(activity);
    expect(await store.acknowledged(activity), isTrue);
    expect(await store.acknowledged(service), isFalse);
    expect(await store.acknowledged(changed), isFalse);
    expect(await store.acknowledged(null), isFalse);
    expect(
      await MemoryVoiceInputConsentStore().acknowledged(activity),
      isFalse,
    );
  });
  test('首次说明绑定实际服务及处理方式，改变后重新说明', () async {
    final store = MemoryVoiceInputConsentStore();
    const a = VoiceSystemService(id: 'a/.Voice', name: '系统语音');
    const changedName = VoiceSystemService(id: 'a/.Voice', name: '另一个服务');
    const b = VoiceSystemService(id: 'b/.Voice', name: '系统语音');
    expect(await store.acknowledged(a), isFalse);
    await store.acknowledge(a);
    expect(await store.acknowledged(a), isTrue);
    expect(await store.acknowledged(changedName), isFalse);
    expect(await store.acknowledged(b), isFalse);
    expect(await store.acknowledged(null), isFalse);
    await store.acknowledge(null);
    expect(await store.acknowledged(null), isTrue);
    expect(await MemoryVoiceInputConsentStore().acknowledged(a), isFalse);
  });
}
