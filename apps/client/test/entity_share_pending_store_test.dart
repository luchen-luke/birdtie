import 'dart:convert';
import 'package:birdtie_client/src/workspace/entity_share_pending_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';

const shareOwner = '11111111-1111-4111-8111-111111111111';
const sharePeer = '22222222-2222-4222-8222-222222222222';
const shareConversation = '33333333-3333-4333-8333-333333333333';
const shareTarget = '44444444-4444-4444-8444-444444444444';
const shareOperation = '55555555-5555-4555-8555-555555555555';
const shareMessage = '66666666-6666-4666-8666-666666666666';
const pendingShare = PendingEntityShare(
  operationID: shareOperation,
  conversationID: shareConversation,
  type: 'moment',
  entityID: shareTarget,
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test('安全持久存储重建后仅原环境与本人能恢复稳定引用', () async {
    FlutterSecureStorage.setMockInitialValues({});
    const first = SecureEntitySharePendingStore();
    await first.write('https://api.example', shareOwner, pendingShare);
    const restarted = SecureEntitySharePendingStore();
    expect(
      (await restarted.read('https://api.example', shareOwner)).single.toJson(),
      pendingShare.toJson(),
    );
    expect(await restarted.read('https://other.example', shareOwner), isEmpty);
    expect(await restarted.read('https://api.example', sharePeer), isEmpty);
    final raw = await const FlutterSecureStorage().readAll();
    expect(raw.length, 1);
    expect(raw.values.single, jsonEncode(pendingShare.toJson()));
    expect(raw.values.single, isNot(contains('Bearer')));
    await restarted.delete('https://api.example', shareOwner, shareOperation);
    expect(await first.read('https://api.example', shareOwner), isEmpty);
  });
  test('原操作键不能改受众、实体或类型', () async {
    FlutterSecureStorage.setMockInitialValues({});
    const store = SecureEntitySharePendingStore();
    await store.write('https://api.example', shareOwner, pendingShare);
    await expectLater(
      store.write(
        'https://api.example',
        shareOwner,
        const PendingEntityShare(
          operationID: shareOperation,
          conversationID: shareConversation,
          type: 'place',
          entityID: shareTarget,
        ),
      ),
      throwsStateError,
    );
    expect(
      (await store.read('https://api.example', shareOwner)).single.type,
      'moment',
    );
  });
  test('损坏恢复记录不能静默删除再产生新操作', () async {
    FlutterSecureStorage.setMockInitialValues({});
    const store = SecureEntitySharePendingStore();
    await store.write('https://api.example', shareOwner, pendingShare);
    final raw = await const FlutterSecureStorage().readAll();
    await const FlutterSecureStorage().write(
      key: raw.keys.single,
      value: '{"token":"not-allowed"}',
    );
    await expectLater(
      store.read('https://api.example', shareOwner),
      throwsFormatException,
    );
    expect((await const FlutterSecureStorage().readAll()).length, 1);
  });
  test('操作键为独立安全随机UUID而非类型、标题或时间匹配', () {
    final ids = List.generate(1000, (_) => newEntityShareOperationID());
    expect(ids.toSet().length, 1000);
    for (final id in ids) {
      expect(chatEntityUUID.hasMatch(id), isTrue);
      expect(id[14], '4');
    }
  });
  for (final change in ['extra', 'kind', 'id']) {
    test('恢复格式闭合拒绝$change', () {
      final value = pendingShare.toJson();
      if (change == 'extra') value['approved'] = true;
      if (change == 'kind') value['type'] = 'private_memory';
      if (change == 'id') value['entityId'] = 'https://example.invalid';
      expect(() => PendingEntityShare.fromJson(value), throwsFormatException);
    });
  }
}
