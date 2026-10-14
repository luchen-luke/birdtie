import 'package:birdtie_client/src/content/video_material_pending_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'video_material_controller_test.dart';

VideoMaterialPending pendingVideo({
  String op = videoOperation,
  int size = 100,
}) => VideoMaterialPending(videoOwner, {
  'operationId': op,
  'mimeType': 'video/mp4',
  'byteSize': size,
  'sha256': List.filled(64, 'a').join(),
  'durationMs': 1000,
  'purpose': 'HUMAN_PRIVATE_VIDEO_MATERIAL',
  'confirmedPrivateSave': true,
  'storagePolicyId': videoPolicy,
});
void main() {
  test('视频恢复记录隔离owner与API来源，禁止同operation改版本', () async {
    final s = MemoryVideoMaterialPendingStore();
    final p = pendingVideo();
    await s.write('one', p);
    expect((await s.read('one', videoOwner)).single.operationID, p.operationID);
    expect(await s.read('two', videoOwner), isEmpty);
    expect(await s.read('one', videoID), isEmpty);
    await expectLater(
      s.write('one', pendingVideo(size: 101)),
      throwsFormatException,
    );
    expect((await s.read('one', videoOwner)).single.input['byteSize'], 100);
    await s.remove('one', videoOwner, p.operationID);
    expect(await s.read('one', videoOwner), isEmpty);
  });
  test('视频恢复只保存闭合摘要字段，拒绝媒体路径和无限积累', () async {
    final p = pendingVideo();
    expect(
      () => VideoMaterialPending(videoOwner, {
        ...p.input,
        'localPath': 'private-file.mp4',
      }),
      throwsFormatException,
    );
    final s = MemoryVideoMaterialPendingStore();
    for (var i = 0; i < 12; i++) {
      await s.write(
        'one',
        pendingVideo(
          op: '${i.toRadixString(16).padLeft(8, '0')}-1111-4111-8111-111111111111',
        ),
      );
    }
    await expectLater(
      s.write('one', pendingVideo(op: 'ffffffff-1111-4111-8111-111111111111')),
      throwsFormatException,
    );
    expect((await s.read('one', videoOwner)).length, 12);
  });
}
