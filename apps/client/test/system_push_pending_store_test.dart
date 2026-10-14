import 'dart:convert';
import 'package:birdtie_client/src/workspace/system_push_pending_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'system_push_api_test.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  test('跨实例并发安装凭证只创建一个并持久恢复，不暴露proof', () async {
    final a = SecureSystemPushPendingStore(),
        b = SecureSystemPushPendingStore();
    final installations = await Future.wait(
      List.generate(20, (i) => (i.isEven ? a : b).installation()),
    );
    expect(installations.map((i) => i.id).toSet().length, 1);
    expect(installations.map((i) => i.proof).toSet().length, 1);
    expect(
      (await SecureSystemPushPendingStore().installation()).id,
      installations.first.id,
    );
    expect(
      installations.first.toString(),
      isNot(contains(installations.first.proof)),
    );
    expect(
      installations.first.toString(),
      isNot(contains(installations.first.id)),
    );
  });
  test('同operation的类型/版本不可改写，owner与origin隔离', () async {
    final a = SecureSystemPushPendingStore(),
        b = SecureSystemPushPendingStore();
    final op = PushPending(
      owner: pushOwner,
      id: pushOpID,
      operation: 'REGISTER',
      expectedVersion: 0,
    );
    await a.write('https://one.invalid', op);
    expect(await b.read('https://two.invalid', pushOwner), isNull);
    expect(await b.read('https://one.invalid', pushInstallID), isNull);
    await expectLater(
      b.write(
        'https://one.invalid',
        PushPending(
          owner: pushOwner,
          id: pushOpID,
          operation: 'REVOKE',
          expectedVersion: 0,
        ),
      ),
      throwsFormatException,
    );
    await expectLater(
      b.write(
        'https://one.invalid',
        PushPending(
          owner: pushOwner,
          id: pushOpID,
          operation: 'REGISTER',
          expectedVersion: 1,
        ),
      ),
      throwsFormatException,
    );
    expect(
      (await b.read('https://one.invalid', pushOwner))!.expectedVersion,
      0,
    );
    await b.delete(
      'https://one.invalid',
      PushPending(
        owner: pushOwner,
        id: pushOpID,
        operation: 'REGISTER',
        expectedVersion: 1,
      ),
    );
    expect(await b.read('https://one.invalid', pushOwner), isNotNull);
    await a.delete('https://one.invalid', op);
    expect(await b.read('https://one.invalid', pushOwner), isNull);
  });
  test('并发不同operation只有一个写入，失败不会卡住安全账本', () async {
    final a = SecureSystemPushPendingStore(),
        b = SecureSystemPushPendingStore();
    final original = PushPending(
      owner: pushOwner,
      id: pushOpID,
      operation: 'REGISTER',
      expectedVersion: 0,
    );
    final other = PushPending(
      owner: pushOwner,
      id: pushEventID,
      operation: 'REVOKE',
      expectedVersion: 1,
    );
    final first = a.write('https://one.invalid', original);
    final second = expectLater(
      b.write('https://one.invalid', other),
      throwsFormatException,
    );
    await first;
    await second;
    expect((await b.read('https://one.invalid', pushOwner))!.id, original.id);
    await a.delete('https://one.invalid', original);
    await b.write('https://one.invalid', other);
    expect((await a.read('https://one.invalid', pushOwner))!.id, other.id);
  });
  test('恢复数据闭合且不存SDK token，JS不安全版本与损坏凭证拒绝', () async {
    expect(
      () => PushPending(
        owner: pushOwner,
        id: pushOpID,
        operation: 'REGISTER',
        expectedVersion: 9007199254740991,
      ),
      throwsFormatException,
    );
    final value = PushPending(
      owner: pushOwner,
      id: pushOpID,
      operation: 'REGISTER',
      expectedVersion: 0,
    );
    expect(
      () => PushPending.fromJson({
        ...value.toJson(),
        'address': 'private-sdk-token',
      }),
      throwsFormatException,
    );
    FlutterSecureStorage.setMockInitialValues({
      'birdtie.push.installation.v1': jsonEncode({
        'id': pushInstallID,
        'proof': List.filled(43, 'a').join(),
        'token': 'forbidden-extra',
      }),
    });
    await expectLater(
      SecureSystemPushPendingStore().installation(),
      throwsFormatException,
    );
  });
}
