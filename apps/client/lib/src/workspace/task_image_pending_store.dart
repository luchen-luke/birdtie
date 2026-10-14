import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

final _uuid = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);
final _digest = RegExp(r'^[0-9a-f]{64}$');
const _purpose = 'HUMAN_TASK_MESSAGE_ATTACHMENT';
const _maxRecords = 12;
const _maxBytes = 4096;
const _inputKeys = {
  'operationId',
  'messageIndex',
  'prefixDigest',
  'expectedTaskUpdatedAt',
  'mimeType',
  'byteSize',
  'sha256',
  'pixelRisk',
  'purpose',
};

void _require(bool accepted) {
  if (!accepted) throw const FormatException('会话图片恢复记录无法核验');
}

bool _validDate(Object? value) {
  if (value is! String) return false;
  final parts = RegExp(
    r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,6})?Z$',
  ).firstMatch(value);
  if (parts == null) return false;
  final date = DateTime.tryParse(value);
  if (date == null || !date.isUtc || date.year < 2020 || date.year > 9999) {
    return false;
  }
  final actual = [
    date.year,
    date.month,
    date.day,
    date.hour,
    date.minute,
    date.second,
  ];
  return List.generate(
    6,
    (i) => int.parse(parts.group(i + 1)!),
  ).asMap().entries.every((entry) => entry.value == actual[entry.key]);
}

Map<String, dynamic> _validatedInput(Map<String, dynamic> input) {
  _require(
    input.length == _inputKeys.length &&
        input.keys.every(_inputKeys.contains) &&
        input['operationId'] is String &&
        _uuid.hasMatch(input['operationId']) &&
        input['messageIndex'] is int &&
        input['messageIndex'] >= 0 &&
        input['messageIndex'] <= 255 &&
        input['prefixDigest'] is String &&
        _digest.hasMatch(input['prefixDigest']) &&
        _validDate(input['expectedTaskUpdatedAt']) &&
        const ['image/png', 'image/jpeg'].contains(input['mimeType']) &&
        input['byteSize'] is int &&
        input['byteSize'] >= 1 &&
        input['byteSize'] <= 10 * 1024 * 1024 &&
        input['sha256'] is String &&
        _digest.hasMatch(input['sha256']) &&
        const ['UNKNOWN', 'USER_MASKED'].contains(input['pixelRisk']) &&
        input['purpose'] == _purpose,
  );
  // All values are closed primitives. Copy in a fixed order so equivalent
  // callers produce one immutable operation payload, independent of map order.
  return Map<String, dynamic>.unmodifiable({
    for (final key in _inputKeys) key: input[key],
  });
}

/// Human-only recovery metadata. It contains no pixels, text, credential,
/// approval or permission to export a message or image to a model.
class TaskImagePendingOperation {
  TaskImagePendingOperation({
    required this.ownerID,
    required this.taskID,
    required this.contextID,
    required Map<String, dynamic> input,
  }) : _input = _validatedInput(input) {
    _require(
      _uuid.hasMatch(ownerID) &&
          _uuid.hasMatch(taskID) &&
          _uuid.hasMatch(contextID),
    );
  }

  final String ownerID, taskID, contextID;
  final Map<String, dynamic> _input;
  String get operationID => _input['operationId'] as String;
  Map<String, dynamic> get input => _input;

  Map<String, dynamic> toJson() => {
    'ownerId': ownerID,
    'taskId': taskID,
    'contextId': contextID,
    'input': _input,
  };

  factory TaskImagePendingOperation.fromJson(Map<String, dynamic> value) {
    _require(
      value.length == 4 &&
          value.keys.every(
            const {'ownerId', 'taskId', 'contextId', 'input'}.contains,
          ) &&
          value['ownerId'] is String &&
          value['taskId'] is String &&
          value['contextId'] is String &&
          value['input'] is Map<String, dynamic>,
    );
    return TaskImagePendingOperation(
      ownerID: value['ownerId'],
      taskID: value['taskId'],
      contextID: value['contextId'],
      input: value['input'],
    );
  }
}

abstract interface class TaskImagePendingStore {
  Future<List<TaskImagePendingOperation>> read(
    String environment,
    String owner,
    String task,
  );
  Future<void> write(String environment, TaskImagePendingOperation operation);
  Future<void> delete(String environment, TaskImagePendingOperation operation);
}

String _prefix(String environment, String owner, String task) {
  final uri = Uri.tryParse(environment);
  _require(
    environment.trim() == environment &&
        !RegExp(r'[\x00-\x20\x7f]').hasMatch(environment) &&
        uri != null &&
        uri.hasAuthority &&
        const ['http', 'https'].contains(uri.scheme) &&
        uri.host.isNotEmpty &&
        uri.userInfo.isEmpty &&
        !uri.hasQuery &&
        !uri.hasFragment &&
        (uri.path.isEmpty || uri.path == '/') &&
        _uuid.hasMatch(owner) &&
        _uuid.hasMatch(task),
  );
  final origin = uri!.origin;
  _require(uri.port > 0 && uri.port <= 65535);
  return 'birdtie.task-image.pending.v1.'
      '${sha256.convert(utf8.encode('$origin\n$owner\n$task'))}.';
}

String _raw(TaskImagePendingOperation operation) {
  final raw = jsonEncode(operation.toJson());
  _require(utf8.encode(raw).length <= _maxBytes);
  return raw;
}

List<TaskImagePendingOperation> _readRecords(
  Map<String, String> records,
  String prefix,
  String owner,
  String task,
) {
  final entries = records.entries.where(
    (entry) => entry.key.startsWith(prefix),
  );
  final result = <TaskImagePendingOperation>[];
  final ids = <String>{};
  for (final entry in entries) {
    _require(result.length < _maxRecords);
    if (utf8.encode(entry.value).length > _maxBytes) {
      throw const FormatException('会话图片恢复记录过大');
    }
    final value = jsonDecode(entry.value);
    _require(value is Map<String, dynamic>);
    final operation = TaskImagePendingOperation.fromJson(value);
    _require(
      operation.ownerID == owner &&
          operation.taskID == task &&
          entry.key == '$prefix${operation.operationID}' &&
          ids.add(operation.operationID),
    );
    result.add(operation);
  }
  result.sort((a, b) => a.operationID.compareTo(b.operationID));
  return List.unmodifiable(result);
}

// Secure storage has no compare-and-set. Serialize local read/check/write and
// conditional delete across store instances; this adds no server authority.
Future<void> _storageTail = Future<void>.value();
Future<T> _serialized<T>(Future<T> Function() action) {
  final result = _storageTail.then((_) => action());
  _storageTail = result.then<void>(
    (_) {},
    onError: (Object _, StackTrace __) {},
  );
  return result;
}

class SecureTaskImagePendingStore implements TaskImagePendingStore {
  const SecureTaskImagePendingStore({
    this.storage = const FlutterSecureStorage(),
  });
  final FlutterSecureStorage storage;

  @override
  Future<List<TaskImagePendingOperation>> read(
    String environment,
    String owner,
    String task,
  ) async {
    final prefix = _prefix(environment, owner, task);
    return _serialized(() async {
      return _readRecords(await storage.readAll(), prefix, owner, task);
    });
  }

  @override
  Future<void> write(
    String environment,
    TaskImagePendingOperation operation,
  ) async {
    final prefix = _prefix(environment, operation.ownerID, operation.taskID),
        key = '$prefix${operation.operationID}',
        raw = _raw(operation);
    await _serialized(() async {
      final all = await storage.readAll();
      final records = _readRecords(
        all,
        prefix,
        operation.ownerID,
        operation.taskID,
      );
      final old = all[key];
      if (old != null && old != raw) {
        throw StateError('原会话图片操作不能更换内容或归属');
      }
      if (old == null && records.length >= _maxRecords) {
        throw StateError('待核实会话图片操作过多');
      }
      if (old == null) await storage.write(key: key, value: raw);
    });
  }

  @override
  Future<void> delete(
    String environment,
    TaskImagePendingOperation operation,
  ) async {
    final key =
            '${_prefix(environment, operation.ownerID, operation.taskID)}'
            '${operation.operationID}',
        raw = _raw(operation);
    await _serialized(() async {
      final old = await storage.read(key: key);
      if (old != null && old != raw) {
        throw StateError('原会话图片操作不能更换内容或归属');
      }
      if (old != null) await storage.delete(key: key);
    });
  }
}

class MemoryTaskImagePendingStore implements TaskImagePendingStore {
  final Map<String, String> _values = {};

  @override
  Future<List<TaskImagePendingOperation>> read(
    String environment,
    String owner,
    String task,
  ) async =>
      _readRecords(_values, _prefix(environment, owner, task), owner, task);

  @override
  Future<void> write(
    String environment,
    TaskImagePendingOperation operation,
  ) async {
    final prefix = _prefix(environment, operation.ownerID, operation.taskID),
        key = '$prefix${operation.operationID}',
        raw = _raw(operation),
        records = _readRecords(
          _values,
          prefix,
          operation.ownerID,
          operation.taskID,
        ),
        old = _values[key];
    if (old != null && old != raw) {
      throw StateError('原会话图片操作不能更换内容或归属');
    }
    if (old == null && records.length >= _maxRecords) {
      throw StateError('待核实会话图片操作过多');
    }
    _values[key] = raw;
  }

  @override
  Future<void> delete(
    String environment,
    TaskImagePendingOperation operation,
  ) async {
    final key =
            '${_prefix(environment, operation.ownerID, operation.taskID)}'
            '${operation.operationID}',
        old = _values[key];
    if (old != null && old != _raw(operation)) {
      throw StateError('原会话图片操作不能更换内容或归属');
    }
    _values.remove(key);
  }
}
