import 'dart:convert';

import 'package:birdtie_client/src/auth/session_vault.dart';
import 'package:birdtie_client/src/city/exploration_city_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('探索城市平台store只保存闭合city schema，不携身份位置或会话', () async {
    FlutterSecureStorage.setMockInitialValues({});
    const store = SecureExplorationCityStore();
    expect(await store.readCityID(), isNull);
    await store.writeCityID('aberdeen-gb');
    final rows = await const FlutterSecureStorage().readAll();
    expect(rows.keys, [SecureExplorationCityStore.storageKey]);
    expect(jsonDecode(rows.values.single), {
      'schema': 'birdtie.exploration.city.v1',
      'cityId': 'aberdeen-gb',
    });
    expect(
      await const SecureExplorationCityStore().readCityID(),
      'aberdeen-gb',
    );
  });

  test('退出只清auth scoped key，不清已选探索城市', () async {
    FlutterSecureStorage.setMockInitialValues({});
    const store = SecureExplorationCityStore(), vault = SecureSessionVault();
    await store.writeCityID('edinburgh-gb');
    await vault.write(
      const StoredSession(token: 'synthetic-token', method: 'dev_phone'),
    );
    await vault.delete();
    expect(await vault.read(), isNull);
    expect(await store.readCityID(), 'edinburgh-gb');
  });

  test('损坏或越界本机schema不作为探索选择，不尝试恢复私人内容', () async {
    for (final raw in [
      'broken',
      'null',
      '[]',
      jsonEncode({'schema': 'birdtie.exploration.city.v1', 'cityId': ''}),
      jsonEncode({'schema': 'birdtie.session.v1', 'cityId': 'aberdeen-gb'}),
      jsonEncode({'schema': 'birdtie.exploration.city.v1', 'cityId': 7}),
      jsonEncode({'schema': 'birdtie.exploration.city.v1', 'cityId': ' city '}),
      jsonEncode({
        'schema': 'birdtie.exploration.city.v1',
        'cityId': 'a\ncity',
      }),
      jsonEncode({
        'schema': 'birdtie.exploration.city.v1',
        'cityId': 'x' * 161,
      }),
      jsonEncode({
        'schema': 'birdtie.exploration.city.v1',
        'cityId': 'aberdeen-gb',
        'accountId': 'private-canary',
      }),
    ]) {
      FlutterSecureStorage.setMockInitialValues({
        SecureExplorationCityStore.storageKey: raw,
      });
      expect(await const SecureExplorationCityStore().readCityID(), isNull);
    }
    FlutterSecureStorage.setMockInitialValues({});
    await expectLater(
      const SecureExplorationCityStore().writeCityID(''),
      throwsArgumentError,
    );
    expect(await const FlutterSecureStorage().readAll(), isEmpty);
  });
}
