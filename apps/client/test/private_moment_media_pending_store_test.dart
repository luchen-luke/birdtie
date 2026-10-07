import 'dart:convert';
import 'package:birdtie_client/src/content/private_moment_media_pending_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';

const owner = '22222222-2222-4222-8222-222222222222';
const moment = '11111111-1111-4111-8111-111111111111';
const operation = '55555555-5555-4555-8555-555555555555';
PendingPrivateImageOperation reference() => PendingPrivateImageOperation(
  ownerID: owner,
  momentID: moment,
  operationID: operation,
  phase: 'preview',
  momentRevision: 1,
  mime: 'image/png',
  byteSize: 32,
  inputHash: List.filled(64, 'a').join(),
  pixelRisk: 'UNKNOWN',
  observedAt: DateTime.utc(2026, 10, 6),
  deadlineAt: DateTime.utc(2026, 10, 6, 0, 5),
);
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  test('当前实际安全存储adapter重建按环境本人记录恢复严格metadata不存照片令牌文字批准', () async {
    const store = SecurePrivateImagePendingStore();
    final v = reference();
    await store.write('https://api.example/', v);
    const reopened = SecurePrivateImagePendingStore();
    expect(
      (await reopened.read(
        'https://api.example',
        owner,
        moment,
      )).single.toJson(),
      v.toJson(),
    );
    expect(
      await reopened.read('https://other.example', owner, moment),
      isEmpty,
    );
    expect(await reopened.read('https://api.example', moment, moment), isEmpty);
    expect(
      await reopened.read('https://api.example', owner, operation),
      isEmpty,
    );
    final raw = await const FlutterSecureStorage().readAll();
    expect(raw.values.single, jsonEncode(v.toJson()));
    for (final word in [
      'token',
      'photo',
      'bytes',
      'title',
      'body',
      'approved',
      'confirmed',
      'session',
    ]) {
      expect(v.toJson().keys, isNot(contains(word)));
    }
    await reopened.delete('https://api.example', v);
    expect(await store.read('https://api.example', owner, moment), isEmpty);
  });
  test('同操作phase不能偷换hash版本或图片ID且损坏不能静默清除', () async {
    const store = SecurePrivateImagePendingStore();
    await store.write('https://api.example', reference());
    final changed = reference().toJson()
      ..['inputSha256'] = List.filled(64, 'b').join();
    await expectLater(
      store.write(
        'https://api.example',
        PendingPrivateImageOperation.fromJson(changed),
      ),
      throwsStateError,
    );
    final raw = await const FlutterSecureStorage().readAll();
    await const FlutterSecureStorage().write(
      key: raw.keys.single,
      value: '{"approved":true}',
    );
    await expectLater(
      store.read('https://api.example', owner, moment),
      throwsFormatException,
    );
    expect((await const FlutterSecureStorage().readAll()).length, 1);
  });
  test('期限是有限UTC历史引用而不是失效后禁止核实或批准', () {
    final j = reference().toJson()
      ..['observedAt'] = '2026-10-06T01:00:00.000Z'
      ..['deadlineAt'] = '2026-10-06T00:05:00.000Z';
    expect(
      PendingPrivateImageOperation.fromJson(
        j,
      ).deadlineAt.isBefore(DateTime.utc(2026, 10, 6, 1)),
      isTrue,
    );
    for (final invalid in [
      '2026-02-31T00:00:00.000Z',
      '2026-10-06T24:00:00Z',
      '2026-10-06T00:00:60Z',
      '+010000-10-06T00:00:00.000Z',
      '2026-10-06T00:00:00+00:00',
    ]) {
      expect(
        () => PendingPrivateImageOperation.fromJson({
          ...j,
          'deadlineAt': invalid,
        }),
        throwsFormatException,
      );
    }
  });
  for (final field in [
    'photo',
    'token',
    'approved',
    'ownerId',
    'momentRevision',
    'byteSize',
    'inputSha256',
    'phase',
    'assetId',
    'deadlineAt',
  ]) {
    test('闭合恢复记录拒绝$field', () {
      final j = reference().toJson();
      j[field] = switch (field) {
        'ownerId' => 'wrong',
        'momentRevision' => 0,
        'byteSize' => 11 * 1024 * 1024,
        'inputSha256' => 'wrong',
        'phase' => 'upload',
        'assetId' => operation,
        'deadlineAt' => '2026-10-06',
        _ => 'forbidden',
      };
      expect(
        () => PendingPrivateImageOperation.fromJson(j),
        throwsFormatException,
      );
    });
  }
}
