import 'dart:convert';
import 'package:birdtie_client/src/workspace/connection_request_review_pending_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';

const _owner = '22222222-2222-4222-8222-222222222222';
const _request = '11111111-1111-4111-8111-111111111111';
const _ref = '55555555-5555-4555-8555-555555555555';
PendingConnectionReview _value() => PendingConnectionReview(
  ownerID: _owner,
  requestID: _request,
  referenceID: _ref,
  action: 'accept',
  scope: 'friend',
  direction: 'incoming',
  observedAt: DateTime.utc(2026, 10, 6, 9),
  requestCreatedAt: DateTime.utc(2026, 10, 6, 8),
  requestExpiresAt: DateTime.utc(2026, 10, 7),
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  test('申请引用存储：真实SecureStorage adapter插件mock重建按环境本人Request隔离且无私文或批准', () async {
    const store = SecureConnectionReviewPendingStore();
    await store.write('https://original.fixture/', _value());
    const reopened = SecureConnectionReviewPendingStore();
    expect(
      (await reopened.read(
        'https://original.fixture',
        _owner,
        _request,
      ))!.toJson(),
      _value().toJson(),
    );
    expect(
      await reopened.read('https://other.fixture', _owner, _request),
      null,
    );
    expect(
      await reopened.read('https://original.fixture', _request, _request),
      null,
    );
    expect(await reopened.read('https://original.fixture', _owner, _ref), null);
    final raw = await const FlutterSecureStorage().readAll();
    expect(raw.length, 1);
    expect(jsonDecode(raw.values.single), _value().toJson());
    expect(
      _value().toJson().keys,
      isNot(
        contains(
          anyOf(
            'token',
            'session',
            'note',
            'otherName',
            'approval',
            'confirmed',
            'operationId',
          ),
        ),
      ),
    );
    expect(
      await reopened.compareDelete('https://original.fixture/', _value()),
      true,
    );
    expect(
      await store.read('https://original.fixture', _owner, _request),
      null,
    );
  });
  for (final store in ['secure', 'memory']) {
    test(
      '申请引用存储：$store同Request换action/reference拒绝且迟到compareDelete不清新引用',
      () async {
        final ConnectionReviewPendingStore data = store == 'secure'
            ? const SecureConnectionReviewPendingStore()
            : MemoryConnectionReviewPendingStore();
        final old = _value(),
            changed = PendingConnectionReview.fromJson(
              _value().toJson()
                ..['referenceId'] = _request
                ..['action'] = 'decline',
            );
        await data.write('https://original.fixture', old);
        await data.write('https://original.fixture', old);
        await expectLater(
          data.write('https://original.fixture', changed),
          throwsStateError,
        );
        expect(
          await data.compareDelete('https://original.fixture', changed),
          false,
        );
        expect(await data.compareDelete('https://original.fixture', old), true);
        await data.write('https://original.fixture', changed);
        expect(
          await data.compareDelete('https://original.fixture', old),
          false,
        );
        expect(
          (await data.read(
            'https://original.fixture',
            _owner,
            _request,
          ))!.referenceID,
          _request,
        );
      },
    );
  }
  test('申请引用存储：未知过期引用可恢复但损坏不能静默清除', () async {
    const store = SecureConnectionReviewPendingStore();
    await store.write('https://original.fixture', _value());
    final raw = await const FlutterSecureStorage().readAll(),
        key = raw.keys.single;
    await const FlutterSecureStorage().write(key: key, value: '{broken');
    await expectLater(
      store.read('https://original.fixture', _owner, _request),
      throwsFormatException,
    );
    await expectLater(
      store.compareDelete('https://original.fixture', _value()),
      throwsFormatException,
    );
    expect(await const FlutterSecureStorage().read(key: key), '{broken');
  });
  final changes = <String, dynamic>{
    'extra': true,
    'ownerId': 'A2222222-2222-4222-8222-222222222222',
    'referenceId': 'bad',
    'action': 'confirmed',
    'direction': 'agent',
    'scope': 'broadcast',
    'observedAt': '2026-10-06T09:00:00',
    'requestCreatedAt': '2500-01-01T00:00:00.000Z',
    'requestExpiresAt': '2026-10-06T08:00:00.000Z',
  };
  for (final entry in changes.entries) {
    test('申请引用存储：严格closed metadata拒绝${entry.key}', () {
      final raw = _value().toJson()..[entry.key] = entry.value;
      expect(
        () => PendingConnectionReview.fromJson(raw),
        throwsFormatException,
      );
    });
  }
  for (final environment in [
    'relative',
    'https://user:secret@api.test',
    'https://api.test?token=secret',
    'https://api.test#secret',
    'file:///private',
  ]) {
    test('申请引用存储：不保存无效或含凭据环境$environment', () async {
      final store = MemoryConnectionReviewPendingStore();
      await expectLater(
        store.write(environment, _value()),
        throwsFormatException,
      );
      expect(store.values, isEmpty);
    });
  }
  test('申请引用存储：同isolate同键多个adapter串行而非宣称平台CAS', () async {
    const a = SecureConnectionReviewPendingStore(),
        b = SecureConnectionReviewPendingStore();
    final changed = PendingConnectionReview.fromJson(
      _value().toJson()..['referenceId'] = _request,
    );
    final results = await Future.wait([
      a
          .write('https://original.fixture', _value())
          .then((_) => true, onError: (_) => false),
      b
          .write('https://original.fixture', changed)
          .then((_) => true, onError: (_) => false),
    ]);
    expect(results, [true, false]);
    expect(
      (await b.read('https://original.fixture', _owner, _request))!.referenceID,
      _ref,
    );
  });
}
