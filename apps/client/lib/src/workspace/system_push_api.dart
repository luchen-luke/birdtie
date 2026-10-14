import 'dart:convert';
import 'package:http/http.dart' as http;
import 'system_push_pending_store.dart';
import 'remote_inbox_source.dart';

void requirePush(bool value) {
  if (!value) throw const FormatException('系统通知回执无法核验');
}

DateTime? pushDate(Object? value) {
  if (value == null) return null;
  requirePush(value is String && RegExp(r'(Z|[+-]\d\d:\d\d)$').hasMatch(value));
  return DateTime.parse(value as String).toUtc();
}

class PushEvent {
  PushEvent._(this.id, this.bindingID, this.version);
  factory PushEvent.fromJson(Map<String, dynamic> j) {
    requirePush(
      j.length == 4 &&
          j.keys.every(
            {'schema', 'eventId', 'bindingId', 'bindingVersion'}.contains,
          ) &&
          j['schema'] == 'birdtie-push-v1' &&
          j['eventId'] is String &&
          j['bindingId'] is String &&
          j['bindingVersion'] is String,
    );
    final version = int.tryParse(j['bindingVersion']);
    requirePush(
      pushUUID.hasMatch(j['eventId']) &&
          pushUUID.hasMatch(j['bindingId']) &&
          version != null &&
          version > 0 &&
          version <= 9007199254740991 &&
          j['bindingVersion'] == version.toString(),
    );
    return PushEvent._(j['eventId'], j['bindingId'], version!);
  }
  final String id, bindingID;
  final int version;
}

class PushView {
  PushView._(
    this.installationID,
    this.version,
    this.bindingID,
    this.enabled,
    this.kind,
    this.validUntil,
  );
  factory PushView.fromJson(Map<String, dynamic> j, String id) {
    requirePush(
      j.keys.every(
            {
              'schema',
              'installationId',
              'version',
              'bindingId',
              'enabled',
              'registrationKind',
              'registeredAt',
              'validUntil',
            }.contains,
          ) &&
          j['schema'] == 'system-push-installation-v1' &&
          j['installationId'] == id &&
          pushUUID.hasMatch(id) &&
          j['version'] is int &&
          j['enabled'] is bool,
    );
    final int version = j['version'];
    final bool enabled = j['enabled'];
    final binding = j['bindingId'], kind = j['registrationKind'];
    final until = pushDate(j['validUntil']);
    requirePush(
      version >= 0 &&
          version <= 9007199254740991 &&
          (binding == null ||
              binding is String && pushUUID.hasMatch(binding)) &&
          (kind == null || ['FCM_TOKEN', 'APNS_TOKEN'].contains(kind)) &&
          (!enabled || (binding != null && kind != null && until != null)),
    );
    pushDate(j['registeredAt']);
    return PushView._(id, version, binding, enabled, kind, until);
  }
  final String installationID;
  final int version;
  final String? bindingID, kind;
  final bool enabled;
  final DateTime? validUntil;
  bool matches(PushEvent e) =>
      enabled &&
      bindingID == e.bindingID &&
      version == e.version &&
      validUntil?.isAfter(DateTime.now().toUtc()) == true;
}

class SystemPushAPI {
  SystemPushAPI({
    required this.authorizationHeader,
    required this.current,
    required this.baseUrl,
    http.Client? client,
    this.onDenied,
  }) : _client = client ?? http.Client(),
       _owns = client == null;
  final String? Function() authorizationHeader;
  final bool Function() current;
  final String baseUrl;
  final http.Client _client;
  final bool _owns;
  final void Function()? onDenied;
  String get origin => Uri.parse(baseUrl).origin;
  Future<Map<String, dynamic>> _request(
    String method,
    String path, {
    PushInstallation? installation,
    Object? input,
    Map<String, String>? query,
  }) async {
    final token = authorizationHeader();
    if (!current() || token == null)
      throw const FormatException('当前账号暂不可读取系统通知');
    final uri = Uri.parse(
      baseUrl,
    ).resolve(path).replace(queryParameters: query);
    final request = http.Request(method, uri)
      ..followRedirects = false
      ..headers['Authorization'] = token;
    if (installation != null) {
      request.headers['X-Birdtie-Push-Installation-Proof'] = installation.proof;
      request.headers['X-Birdtie-Push-Installation'] = installation.id;
    }
    if (input != null) {
      request.headers['Content-Type'] = 'application/json';
      request.body = jsonEncode(input);
    }
    final response = await _client
        .send(request)
        .timeout(const Duration(seconds: 15));
    final bytes = <int>[];
    await for (final chunk in response.stream.timeout(
      const Duration(seconds: 15),
    )) {
      if (bytes.length + chunk.length > 256 * 1024)
        throw const FormatException('系统通知响应超限');
      bytes.addAll(chunk);
    }
    if (!current() || authorizationHeader() != token)
      throw const FormatException('当前系统通知来源已变化');
    if (response.statusCode == 401 || response.statusCode == 403) {
      onDenied?.call();
      throw const FormatException('当前系统通知授权已变化，请重新打开设置。');
    }
    if (response.statusCode == 503) throw const FormatException('系统通知服务尚未配置。');
    if (response.statusCode < 200 || response.statusCode >= 300)
      throw const FormatException('当前通知或操作暂不可读取，请核实后重试。');
    final j = jsonDecode(utf8.decode(bytes));
    requirePush(j is Map<String, dynamic> && j['data'] is Map<String, dynamic>);
    return j['data'];
  }

  Future<List<String>> options() async {
    final j = await _request('GET', '/v1/me/system-push/options');
    requirePush(
      j['schema'] == 'system-push-options-v1' &&
          j['available'] is bool &&
          j['registrationKinds'] is List,
    );
    final kinds = List<String>.from(j['registrationKinds']);
    requirePush(
      kinds.toSet().length == kinds.length &&
          kinds.every(['FCM_TOKEN', 'APNS_TOKEN'].contains) &&
          (j['available'] == true || kinds.isEmpty),
    );
    return j['available'] == true ? kinds : [];
  }

  Future<PushView> view(PushInstallation i) async => PushView.fromJson(
    await _request(
      'GET',
      '/v1/me/system-push/installations/${i.id}',
      installation: i,
    ),
    i.id,
  );
  Future<bool> addressMatches(
    PushInstallation i,
    PushView binding, {
    required String kind,
    required String address,
  }) async {
    requirePush(
      binding.enabled &&
          binding.bindingID != null &&
          binding.installationID == i.id &&
          binding.kind == kind,
    );
    final j = await _request(
      'POST',
      '/v1/me/system-push/installations/${i.id}/address-check',
      installation: i,
      input: {
        'schema': 'system-push-address-check-v1',
        'expectedVersion': binding.version,
        'registrationKind': kind,
        'address': address,
      },
    );
    requirePush(
      j.length == 5 &&
          j.keys.every(
            {
              'schema',
              'installationId',
              'bindingId',
              'version',
              'matches',
            }.contains,
          ) &&
          j['schema'] == 'system-push-address-match-v1' &&
          j['installationId'] == i.id &&
          j['bindingId'] == binding.bindingID &&
          j['version'] == binding.version &&
          j['matches'] is bool,
    );
    return j['matches'];
  }

  Future<void> register(
    PushInstallation i,
    PushPending op, {
    required String kind,
    required String platform,
    required String address,
    required String permission,
  }) async {
    final j = await _request(
      'PUT',
      '/v1/me/system-push/installations/${i.id}',
      installation: i,
      input: {
        'schema': 'system-push-registration-v1',
        'operationId': op.id,
        'expectedVersion': op.expectedVersion,
        'registrationKind': kind,
        'platform': platform,
        'address': address,
        'permission': permission,
      },
    );
    validateOperation(j, op);
  }

  Future<void> revoke(PushInstallation i, PushPending op) async {
    final j = await _request(
      'DELETE',
      '/v1/me/system-push/installations/${i.id}',
      installation: i,
      input: {
        'schema': 'system-push-revoke-v1',
        'operationId': op.id,
        'expectedVersion': op.expectedVersion,
      },
    );
    validateOperation(j, op);
  }

  static void validateOperation(Map<String, dynamic> j, PushPending op) {
    requirePush(
      j['schema'] == 'system-push-operation-v1' &&
          j['operationId'] == op.id &&
          j['operation'] == op.operation &&
          j['expectedVersion'] == op.expectedVersion &&
          j['resultVersion'] == op.expectedVersion + 1 &&
          j['state'] == 'COMMITTED' &&
          (j['bindingId'] == null ||
              j['bindingId'] is String && pushUUID.hasMatch(j['bindingId'])) &&
          (op.operation != 'REGISTER' || j['bindingId'] != null) &&
          pushDate(j['committedAt']) != null,
    );
  }

  Future<void> recover(PushInstallation i, PushPending op) async =>
      validateOperation(
        await _request(
          'GET',
          '/v1/me/system-push/installations/${i.id}/operations/${op.id}',
          installation: i,
        ),
        op,
      );
  Future<InboxItem> open(PushInstallation i, PushEvent event) async {
    final j = await _request(
      'GET',
      '/v1/me/system-push/events/${event.id}',
      installation: i,
      query: {
        'bindingId': event.bindingID,
        'bindingVersion': event.version.toString(),
      },
    );
    requirePush(
      j['schema'] == 'system-push-open-v1' &&
          j['eventId'] == event.id &&
          j['bindingId'] == event.bindingID &&
          j['bindingVersion'] == event.version &&
          j['item'] is Map<String, dynamic>,
    );
    return InboxItem.fromJson(j['item']);
  }

  void dispose() {
    if (_owns) _client.close();
  }
}
