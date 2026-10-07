import 'dart:convert';
import 'package:birdtie_client/src/workspace/chat_message_operation.dart';
import 'package:birdtie_client/src/workspace/chat_message_pending_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'entity_share_pending_store_test.dart';
import 'human_message_operation_recovery_test.dart' show receipt;

final pendingMessage = PendingHumanMessage(
  operationID: shareOperation,
  conversationID: shareConversation,
  payloadDigest: humanMessagePayloadDigest(shareConversation, '测试正文'),
);
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'secure journal reconstruction separates owner and environment without private body',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      final a = SecureHumanMessagePendingStore();
      await a.write('https://api.example', shareOwner, pendingMessage);
      final b = SecureHumanMessagePendingStore();
      expect(
        (await b.read('https://api.example', shareOwner)).single.toJson(),
        pendingMessage.toJson(),
      );
      expect(await b.read('https://api.other', shareOwner), isEmpty);
      expect(await b.read('https://api.example', sharePeer), isEmpty);
      final values = await const FlutterSecureStorage().readAll();
      expect(values.length, 1);
      expect(values.values.single, isNot(contains('测试正文')));
      expect(values.values.single, isNot(contains('Bearer')));
      await b.delete('https://api.example', shareOwner, pendingMessage);
      expect(await b.read('https://api.example', shareOwner), isEmpty);
    },
  );
  test('same operation cannot change conversation or payload', () async {
    FlutterSecureStorage.setMockInitialValues({});
    final s = SecureHumanMessagePendingStore();
    await s.write('https://api.example', shareOwner, pendingMessage);
    await expectLater(
      s.write(
        'https://api.example',
        shareOwner,
        PendingHumanMessage(
          operationID: shareOperation,
          conversationID: sharePeer,
          payloadDigest: pendingMessage.payloadDigest,
        ),
      ),
      throwsStateError,
    );
    expect(
      (await s.read('https://api.example', shareOwner)).single.toJson(),
      pendingMessage.toJson(),
    );
  });
  test('bad persistent metadata blocks reads and new writes', () async {
    FlutterSecureStorage.setMockInitialValues({});
    final s = SecureHumanMessagePendingStore();
    await s.write('https://api.example', shareOwner, pendingMessage);
    final values = await const FlutterSecureStorage().readAll();
    await const FlutterSecureStorage().write(
      key: values.keys.single,
      value: '{}',
    );
    await expectLater(
      s.read('https://api.example', shareOwner),
      throwsFormatException,
    );
    await expectLater(
      s.write('https://api.example', shareOwner, pendingMessage),
      throwsFormatException,
    );
  });
  test('receipt matches exact owner operation target digest version only', () {
    final raw = receipt(pendingMessage);
    expect(
      HumanMessageOperationReceipt.decode(
        raw,
        shareOwner,
        pendingMessage,
      ).messageID,
      shareMessage,
    );
    for (final key in [
      'ownerId',
      'operationId',
      'conversationId',
      'effectKey',
      'payloadDigest',
      'toolVersion',
    ]) {
      final altered = {...raw, key: 'changed'};
      expect(
        () => HumanMessageOperationReceipt.decode(
          altered,
          shareOwner,
          pendingMessage,
        ),
        throwsFormatException,
      );
    }
    expect(
      () => HumanMessageOperationReceipt.decode(
        {...raw, 'body': 'private'},
        shareOwner,
        pendingMessage,
      ),
      throwsFormatException,
    );
    expect(jsonEncode(pendingMessage.toJson()), isNot(contains('测试正文')));
  });
  test(
    'two in-process store instances serialize immutable operation and exact cleanup',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      final a = SecureHumanMessagePendingStore(),
          b = SecureHumanMessagePendingStore();
      final changed = PendingHumanMessage(
        operationID: shareOperation,
        conversationID: sharePeer,
        payloadDigest: pendingMessage.payloadDigest,
      );
      final first = a.write('https://api.example', shareOwner, pendingMessage);
      final other = b.write('https://api.example', shareOwner, changed);
      await expectLater(other, throwsStateError);
      await first;
      expect(
        (await b.read('https://api.example', shareOwner)).single.toJson(),
        pendingMessage.toJson(),
      );
      await expectLater(
        b.delete('https://api.example', shareOwner, changed),
        throwsStateError,
      );
      expect(
        (await a.read('https://api.example', shareOwner)).single.toJson(),
        pendingMessage.toJson(),
      );
      await b.delete('https://api.example', shareOwner, pendingMessage);
      expect(await a.read('https://api.example', shareOwner), isEmpty);
    },
  );
}
