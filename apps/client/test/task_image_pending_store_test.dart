import 'dart:convert';

import 'package:birdtie_client/src/workspace/task_image_pending_store.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';

const _owner = '22222222-2222-4222-8222-222222222222';
const _task = '11111111-1111-4111-8111-111111111111';
const _context = '33333333-3333-4333-8333-333333333333';
const _operation = '55555555-5555-4555-8555-555555555555';
const _environment = 'https://api.example';

Map<String, dynamic> _input({String operation = _operation}) => {
  'operationId': operation,
  'messageIndex': 2,
  'prefixDigest': List.filled(64, 'a').join(),
  'expectedTaskUpdatedAt': '2026-10-08T03:04:05.123456Z',
  'mimeType': 'image/png',
  'byteSize': 128,
  'sha256': List.filled(64, 'b').join(),
  'pixelRisk': 'UNKNOWN',
  'purpose': 'HUMAN_TASK_MESSAGE_ATTACHMENT',
};

TaskImagePendingOperation _reference({String operation = _operation}) =>
    TaskImagePendingOperation(
      ownerID: _owner,
      taskID: _task,
      contextID: _context,
      input: _input(operation: operation),
    );

String _operationAt(int index) =>
    '55555555-5555-4555-8555-${index.toRadixString(16).padLeft(12, '0')}';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));

  test(
    'closed metadata is copied and immutable without text pixels or grants',
    () {
      final input = _input();
      final value = TaskImagePendingOperation(
        ownerID: _owner,
        taskID: _task,
        contextID: _context,
        input: input,
      );
      input['sha256'] = List.filled(64, 'c').join();
      expect(value.input['sha256'], List.filled(64, 'b').join());
      expect(() => value.input['byteSize'] = 9, throwsUnsupportedError);
      expect(
        () =>
            (value.toJson()['input'] as Map<String, dynamic>)['purpose'] = 'AI',
        throwsUnsupportedError,
      );
      final json = value.toJson();
      expect(json.keys.toSet(), {'ownerId', 'taskId', 'contextId', 'input'});
      expect((json['input'] as Map).keys.toSet(), _input().keys.toSet());
      expect(jsonEncode(json), isNot(contains('PRIVATE_TEXT_CANARY')));
      expect(value.operationID, _operation);
      expect(TaskImagePendingOperation.fromJson(json).toJson(), json);
    },
  );

  for (final entry in <String, Object>{
    'operationId': 'wrong',
    'messageIndex': -1,
    'prefixDigest': 'invalid',
    'expectedTaskUpdatedAt': '2026-02-31T03:04:05Z',
    'mimeType': 'image/svg+xml',
    'byteSize': 10 * 1024 * 1024 + 1,
    'sha256': 'invalid',
    'pixelRisk': 'CLEAN',
    'purpose': 'PRIVATE_MOMENT_ATTACHMENT',
    'text': 'PRIVATE_TEXT_CANARY',
    'pixels': 'PRIVATE_PIXELS_CANARY',
    'authorization': 'PRIVATE_CREDENTIAL_CANARY',
    'approved': true,
  }.entries) {
    test('closed input rejects ${entry.key}', () {
      final input = _input()..[entry.key] = entry.value;
      expect(
        () => TaskImagePendingOperation(
          ownerID: _owner,
          taskID: _task,
          contextID: _context,
          input: input,
        ),
        throwsFormatException,
      );
    });
  }

  test('message size and UTC timestamp bounds are exact', () {
    for (final input in [
      _input()..['messageIndex'] = 256,
      _input()..['messageIndex'] = 1.0,
      _input()..['byteSize'] = 0,
      _input()..['byteSize'] = 1.0,
      _input()..remove('pixelRisk'),
      for (final date in [
        '2019-10-08T03:04:05Z',
        '2026-10-08T24:04:05Z',
        '2026-10-08T03:04:60Z',
        '2026-10-08T03:04:05+00:00',
        '2026-10-08T03:04:05.1234567Z',
        '2026-10-08',
      ])
        _input()..['expectedTaskUpdatedAt'] = date,
    ]) {
      expect(
        () => TaskImagePendingOperation(
          ownerID: _owner,
          taskID: _task,
          contextID: _context,
          input: input,
        ),
        throwsFormatException,
      );
    }
    expect(
      TaskImagePendingOperation(
        ownerID: _owner,
        taskID: _task,
        contextID: _context,
        input: _input()
          ..['messageIndex'] = 255
          ..['byteSize'] = 10 * 1024 * 1024
          ..['pixelRisk'] = 'USER_MASKED',
      ).input['messageIndex'],
      255,
    );
  });

  test('top keys IDs and input shape reject forged metadata', () {
    for (final field in ['ownerId', 'taskId', 'contextId']) {
      final json = _reference().toJson()..[field] = 'wrong';
      expect(
        () => TaskImagePendingOperation.fromJson(json),
        throwsFormatException,
      );
    }
    for (final json in [
      _reference().toJson()..['session'] = 'PRIVATE_CREDENTIAL_CANARY',
      _reference().toJson()..remove('contextId'),
      _reference().toJson()..['input'] = [],
      _reference().toJson()..['input'] = {'purpose': 'MODEL_CONTEXT_EGRESS'},
    ]) {
      expect(
        () => TaskImagePendingOperation.fromJson(json),
        throwsFormatException,
      );
    }
  });

  for (final secure in [false, true]) {
    TaskImagePendingStore store() => secure
        ? const SecureTaskImagePendingStore()
        : MemoryTaskImagePendingStore();
    final name = secure ? 'secure adapter' : 'memory adapter';

    test(
      '$name isolates normalized origin owner task and preserves raw',
      () async {
        final original = store(), value = _reference();
        await original.write('$_environment/', value);
        await original.write(_environment, value);
        final reopened = secure ? store() : original;
        expect(
          (await reopened.read(_environment, _owner, _task)).single.toJson(),
          value.toJson(),
        );
        expect(
          await reopened.read('https://other.example', _owner, _task),
          isEmpty,
        );
        expect(
          await reopened.read('http://api.example', _owner, _task),
          isEmpty,
        );
        expect(
          await reopened.read('https://api.example:444', _owner, _task),
          isEmpty,
        );
        expect(await reopened.read(_environment, _task, _task), isEmpty);
        expect(await reopened.read(_environment, _owner, _context), isEmpty);
        if (secure) {
          final raw = await const FlutterSecureStorage().readAll();
          expect(raw.keys.single, startsWith('birdtie.task-image.pending.v1.'));
          expect(raw.values.single, jsonEncode(value.toJson()));
          expect(raw.keys.single, endsWith('.$_operation'));
        }
        await reopened.delete(_environment, value);
        await reopened.delete(_environment, value);
        expect(await original.read(_environment, _owner, _task), isEmpty);
      },
    );

    test(
      '$name rejects changed writes deletes and concurrent replacement',
      () async {
        final original = store(), value = _reference();
        final changed = TaskImagePendingOperation.fromJson(
          value.toJson()
            ..['input'] = (_input()..['sha256'] = List.filled(64, 'c').join()),
        );
        await expectLater(
          Future.wait([
            original.write(_environment, value),
            original.write(_environment, changed),
          ]),
          throwsStateError,
        );
        await expectLater(
          original.delete(_environment, changed),
          throwsStateError,
        );
        final changedContext = TaskImagePendingOperation(
          ownerID: _owner,
          taskID: _task,
          contextID: _operation,
          input: _input(),
        );
        await expectLater(
          original.write(_environment, changedContext),
          throwsStateError,
        );
        expect(
          (await original.read(_environment, _owner, _task)).single.toJson(),
          value.toJson(),
        );
        final values = await original.read(_environment, _owner, _task);
        expect(() => values.clear(), throwsUnsupportedError);
      },
    );

    test(
      '$name allows twelve refs and rejects a thirteenth without losing them',
      () async {
        final original = store();
        for (var index = 0; index < 12; index++) {
          await original.write(
            _environment,
            _reference(operation: _operationAt(index)),
          );
        }
        await expectLater(
          original.write(_environment, _reference(operation: _operationAt(12))),
          throwsStateError,
        );
        final records = await original.read(_environment, _owner, _task);
        expect(records, hasLength(12));
        expect(
          records.map((record) => record.operationID).toSet(),
          hasLength(12),
        );
        await original.write(
          _environment,
          _reference(operation: _operationAt(0)),
        );
        expect(await original.read(_environment, _owner, _task), hasLength(12));
      },
    );

    test(
      '$name rejects credentials paths malformed origins and invalid IDs',
      () async {
        final original = store();
        for (final environment in [
          'https://user:PRIVATE_CREDENTIAL_CANARY@api.example',
          'https://api.example?token=PRIVATE_CREDENTIAL_CANARY',
          'https://api.example#fragment',
          'https://api.example/base',
          'file:///private/path',
          'https://',
          'https://api.example:0',
          'https://api.example:65536',
          ' $_environment',
          'https://api.example\n',
        ]) {
          await expectLater(
            original.write(environment, _reference()),
            throwsFormatException,
          );
          await expectLater(
            original.read(environment, _owner, _task),
            throwsFormatException,
          );
        }
        await expectLater(
          original.read(_environment, 'wrong', _task),
          throwsFormatException,
        );
        await expectLater(
          original.read(_environment, _owner, 'wrong'),
          throwsFormatException,
        );
        expect(await original.read(_environment, _owner, _task), isEmpty);
      },
    );
  }

  test(
    'secure corruption key mismatch oversized UTF8 and duplicate IDs fail closed',
    () async {
      const store = SecureTaskImagePendingStore(),
          storage = FlutterSecureStorage();
      await store.write(_environment, _reference());
      final records = await storage.readAll(), key = records.keys.single;
      await storage.write(key: key, value: '{"approved":true}');
      await expectLater(
        store.read(_environment, _owner, _task),
        throwsFormatException,
      );
      expect((await storage.readAll())[key], '{"approved":true}');
      final changed = _reference().toJson()
        ..['input'] = _input(operation: _operationAt(1));
      await storage.write(key: key, value: jsonEncode(changed));
      await expectLater(
        store.read(_environment, _owner, _task),
        throwsFormatException,
      );
      await storage.write(key: key, value: records[key]!);
      await storage.write(key: '$key.duplicate', value: records[key]!);
      await expectLater(
        store.read(_environment, _owner, _task),
        throwsFormatException,
      );
      await storage.delete(key: '$key.duplicate');
      final oversized = jsonEncode({'data': List.filled(1400, '汉').join()});
      expect(oversized.length, lessThan(4096));
      expect(utf8.encode(oversized).length, greaterThan(4096));
      await storage.write(key: key, value: oversized);
      await expectLater(
        store.read(_environment, _owner, _task),
        throwsA(
          isA<FormatException>().having(
            (error) => error.message,
            'fixed size rejection',
            '会话图片恢复记录过大',
          ),
        ),
      );
      expect((await storage.readAll())[key], oversized);
    },
  );

  test(
    'secure read rejects thirteen records and does not silently remove one',
    () async {
      const store = SecureTaskImagePendingStore(),
          storage = FlutterSecureStorage();
      await store.write(_environment, _reference());
      final key = (await storage.readAll()).keys.single;
      final prefix = key.substring(0, key.length - _operation.length);
      for (var index = 0; index < 13; index++) {
        final value = _reference(operation: _operationAt(index));
        await storage.write(
          key: '$prefix${value.operationID}',
          value: jsonEncode(value.toJson()),
        );
      }
      await storage.delete(key: key);
      await expectLater(
        store.read(_environment, _owner, _task),
        throwsFormatException,
      );
      expect((await storage.readAll()).length, 13);
    },
  );
}
