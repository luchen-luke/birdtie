import 'dart:convert';
import 'dart:io';
import 'package:crypto/crypto.dart';
import 'package:birdtie_client/src/workspace/close_connections_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const closeOwner = '81000000-0000-4000-8000-000000000001';
const closePeer = '81000000-0000-4000-8000-000000000002';
const closeTie = '81000000-0000-4000-8000-000000000003';
const closeRequest = '81000000-0000-4000-8000-000000000004';
final closeNow = DateTime.utc(2026, 10, 10, 10);
Map<String, dynamic> closeEntry({
  bool selected = false,
  int? revision,
  String? name,
}) => {
  'personId': closePeer,
  'tieId': closeTie,
  'requestId': closeRequest,
  'isClose': selected,
  'nativeRevision': revision ?? (selected ? 1 : 0),
  'sourceVersion': 'a' * 64,
  'displayName': name ?? '合成好友林同学 Aberdeen',
};
Map<String, dynamic> closeEnvelope({
  DateTime? now,
  String owner = closeOwner,
}) => {
  'schemaVersion': 'close-connections-v1',
  'ownerId': owner,
  'observedAt': (now ?? closeNow).toUtc().toIso8601String(),
  'validUntil': (now ?? closeNow)
      .toUtc()
      .add(const Duration(seconds: 30))
      .toIso8601String(),
  'modelAccess': false,
};
Map<String, dynamic> closeList({
  DateTime? now,
  String owner = closeOwner,
  List<Map<String, dynamic>>? entries,
}) => {
  ...closeEnvelope(now: now, owner: owner),
  'connections': entries ?? [closeEntry()],
  'limit': 100,
  'truncated': false,
};
Map<String, dynamic> closeResult({
  DateTime? now,
  Map<String, dynamic>? entry,
}) => {
  ...closeEnvelope(now: now),
  'connection': entry ?? closeEntry(selected: true),
};
http.Response closeJSON(Object data, {int status = 200}) => http.Response(
  jsonEncode({'data': data}),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
CloseConnectionsAPI closeAPI(
  Future<http.Response> Function(http.Request) reply, {
  DateTime Function()? now,
}) => CloseConnectionsAPI(
  client: MockClient(reply),
  apiBaseUrl: 'http://synthetic.test/',
  now: now ?? () => closeNow,
);

class CloseBorrowedClient extends MockClient {
  CloseBorrowedClient(super.fn);
  int closes = 0;
  @override
  void close() {
    closes++;
    super.close();
  }
}

const nativeCloseHashes = {
  'own-eligible.body.json':
      'ee7a14bb3df165e0d05ddd19594693bbcf6646b1e3069ab12ea015abfdfaa2d6',
  'own-eligible.request.json':
      '88bbe16203ab1afeee40293a3423742cefbe3f67f328aef79569ea6529a4a999',
  'own-empty.body.json':
      '3bc0be422614fbbe2b4b933b4beaefb317797699b888eec1fc70a84d2be0aafb',
  'own-empty.request.json':
      '3c76c13dbffcdddc388b7f5999122879010b2c5aa1f29b91452d031bdc67bb20',
  'own-hidden-name.body.json':
      '48326f2cc8f62f92ed7f2a8d6d4f8d205f38156befd2849ed7e969f436e879cf',
  'own-hidden-name.request.json':
      '270ea1417096b4cf22b045b504d1f0419e1c58914bbe2b5d95d7c48c233cd3f0',
  'own-retired.body.json':
      '4e54d5595a0d9fca46a86f44b73d3c00147ffc51c74278c441228d0270aa164a',
  'own-retired.request.json':
      '72d84cfd57984c449d8ffee435ee62a558ff41348c284311101ae479465d2284',
  'own-revoked.body.json':
      'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
  'own-revoked.request.json':
      '7bcd61016b2967b3b3b79709d26e7620cdfbcca918447a6a012c0fe1ed2e87dc',
  'own-selected.body.json':
      'f1c763b139efbb135c65016401c3148e97b87cd74b720066ec2a44af09cd3e32',
  'own-selected.request.json':
      'b3099f7b4ea25d8da014b99de049d98b4370733e8cffE567a3fa140f4ea9c487',
};

class NativeClosePacket {
  NativeClosePacket(String directory, String name) {
    final bytes = File('$directory/$name.body.json').readAsBytesSync();
    final metadata = File('$directory/$name.request.json').readAsBytesSync();
    expect(
      sha256.convert(bytes).toString(),
      nativeCloseHashes['$name.body.json']!.toLowerCase(),
    );
    expect(
      sha256.convert(metadata).toString(),
      nativeCloseHashes['$name.request.json']!.toLowerCase(),
    );
    body = List.unmodifiable(bytes);
    request = jsonDecode(utf8.decode(metadata)) as Map<String, dynamic>;
    expect(
      request['transport'],
      'original registered HTTP + native PG synthetic owned fixtures',
    );
    expect(request['modelAccess'], false);
    data = body.isEmpty
        ? null
        : (jsonDecode(utf8.decode(body)) as Map<String, dynamic>)['data']
              as Map<String, dynamic>;
    clock = DateTime.parse(
      data?['observedAt'] as String? ?? request['observedAt'] as String,
    ).toUtc().add(const Duration(microseconds: 1));
  }
  late final List<int> body;
  late final Map<String, dynamic> request;
  late final Map<String, dynamic>? data;
  late final DateTime clock;
  String get owner => request['ownerId'] as String;
  http.Response respond(http.Request call) {
    expect(call.method, request['method']);
    expect(call.url.path, request['path']);
    expect(call.url.query, isEmpty);
    expect(call.body, request['requestBody']);
    expect(call.headers['X-Birdtie-Organization-Workspace'], isNull);
    return http.Response.bytes(
      body,
      request['httpStatus'] as int,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );
  }
}

void main() {
  final nativeDirectory = Platform.environment['BIRDTIE_CLOSE_NATIVE_WIRE_DIR'];
  if (nativeDirectory == null) {
    test(
      '实际原生Close字节消费需要已取证packet目录',
      () {},
      skip: 'BIRDTIE_CLOSE_NATIVE_WIRE_DIR 未提供；不冒充实际接口消费',
    );
  } else {
    for (final name in [
      'own-empty',
      'own-eligible',
      'own-retired',
      'own-hidden-name',
    ]) {
      test('实际原生Close GET原字节 $name 与原metadata一一对齐', () async {
        final packet = NativeClosePacket(nativeDirectory, name);
        var reads = 0;
        final api = CloseConnectionsAPI(
          client: MockClient((r) async {
            reads++;
            return packet.respond(r);
          }),
          apiBaseUrl: 'http://synthetic-native.test',
          now: () => packet.clock,
        );
        addTearDown(api.dispose);
        final current = await api.list('Bearer SYNTHETIC_NATIVE', packet.owner);
        expect(reads, 1);
        expect(
          current.connections.length,
          (packet.data!['connections'] as List).length,
        );
        expect(current.validUntil, DateTime.parse(packet.data!['validUntil']));
        for (var i = 0; i < current.connections.length; i++) {
          final expected =
                  packet.data!['connections'][i] as Map<String, dynamic>,
              actual = current.connections[i];
          expect(actual.personID, expected['personId']);
          expect(actual.tieID, expected['tieId']);
          expect(actual.requestID, expected['requestId']);
          expect(actual.isClose, expected['isClose']);
          expect(actual.revision, expected['nativeRevision']);
          expect(actual.sourceVersion, expected['sourceVersion']);
          expect(actual.displayName, expected['displayName']);
        }
        if (name == 'own-hidden-name') {
          expect(current.connections.single.displayName, 'Birdtie 成员');
        }
        if (name == 'own-retired') {
          expect(current.connections.single.isClose, false);
          expect(current.connections.single.revision, 2);
        }
      });
    }
    test('实际原生Close GET→PUT保持原请求所有值并解码本人同对象回执', () async {
      final read = NativeClosePacket(nativeDirectory, 'own-eligible'),
          put = NativeClosePacket(nativeDirectory, 'own-selected');
      var clock = read.clock;
      final methods = <String>[];
      final api = CloseConnectionsAPI(
        client: MockClient((r) async {
          methods.add(r.method);
          if (r.method == 'GET') return read.respond(r);
          clock = put.clock;
          return put.respond(r);
        }),
        apiBaseUrl: 'http://synthetic-native.test',
        now: () => clock,
      );
      addTearDown(api.dispose);
      final list = await api.list('Bearer SYNTHETIC_NATIVE', read.owner);
      final result = await api.add(
        'Bearer SYNTHETIC_NATIVE',
        read.owner,
        list.connections.single,
        list.validUntil,
      );
      expect(methods, ['GET', 'PUT']);
      expect(result.connection.isClose, true);
      expect(result.connection.revision, 1);
      expect(result.connection.personID, put.request['personId']);
      expect(result.validUntil, DateTime.parse(put.data!['validUntil']));
      expect(
        result.validUntil.difference(result.observedAt),
        lessThan(const Duration(seconds: 30)),
        reason: '真实原生写回应沿用更短的原来源期限，不能重新扩大至30秒',
      );
    });
    test('实际原生Close DELETE204原空体→GET当前已撤销，revision不复活', () async {
      final selected = NativeClosePacket(nativeDirectory, 'own-selected'),
          removed = NativeClosePacket(nativeDirectory, 'own-revoked'),
          current = NativeClosePacket(nativeDirectory, 'own-retired');
      expect(removed.body, isEmpty);
      expect(removed.request['httpStatus'], 204);
      var clock = selected.clock;
      final methods = <String>[];
      final api = CloseConnectionsAPI(
        client: MockClient((r) async {
          methods.add(r.method);
          if (r.method == 'DELETE') {
            clock = removed.clock;
            return removed.respond(r);
          }
          clock = current.clock;
          return current.respond(r);
        }),
        apiBaseUrl: 'http://synthetic-native.test',
        now: () => clock,
      );
      addTearDown(api.dispose);
      final entry = CloseConnection.decode(selected.data!['connection']);
      await api.remove('Bearer SYNTHETIC_NATIVE', selected.owner, entry);
      final list = await api.list('Bearer SYNTHETIC_NATIVE', selected.owner);
      expect(methods, ['DELETE', 'GET']);
      expect(list.connections.single.isClose, false);
      expect(list.connections.single.revision, 2);
    });
    test('实际原生Close原字节错owner与204空体错误冒充200都failclosed', () async {
      final read = NativeClosePacket(nativeDirectory, 'own-eligible'),
          empty = NativeClosePacket(nativeDirectory, 'own-revoked');
      final api = CloseConnectionsAPI(
        client: MockClient((r) async => read.respond(r)),
        apiBaseUrl: 'http://synthetic-native.test',
        now: () => read.clock,
      );
      await expectLater(
        api.list('Bearer SYNTHETIC_NATIVE', read.request['personId']),
        throwsFormatException,
      );
      api.dispose();
      final wrong = CloseConnectionsAPI(
        client: MockClient((_) async => http.Response.bytes(empty.body, 200)),
        apiBaseUrl: 'http://synthetic-native.test',
        now: () => read.clock,
      );
      await expectLater(
        wrong.list('Bearer SYNTHETIC_NATIVE', read.owner),
        throwsFormatException,
      );
      wrong.dispose();
    });
  }
  test('列表使用本人原GET，没有model/Memory权限或UUID输入', () async {
    final calls = <http.Request>[];
    final api = closeAPI((r) async {
      calls.add(r);
      return closeJSON(closeList());
    });
    addTearDown(api.dispose);
    final result = await api.list('Bearer SYNTHETIC', closeOwner);
    expect(result.connections.single.displayName, '合成好友林同学 Aberdeen');
    expect(result.connections.single.isClose, false);
    expect(result.validUntil, closeNow.add(const Duration(seconds: 30)));
    expect(calls.single.method, 'GET');
    expect(calls.single.url.path, '/v1/me/close-connections');
    expect(calls.single.url.query, isEmpty);
    expect(calls.single.body, isEmpty);
    expect(calls.single.headers['X-Birdtie-Organization-Workspace'], isNull);
  });
  for (final mutation in <void Function(Map<String, dynamic>)>[
    (v) => v['ownerId'] = closePeer,
    (v) => v['modelAccess'] = true,
    (v) => v['agentMemory'] = 'PRIVATE',
    (v) => v['limit'] = 99,
    (v) => v['truncated'] = true,
    (v) => v['connections'] = [closeEntry(), closeEntry()],
    (v) => v['connections'] = [
      {...closeEntry(), 'personId': closeOwner},
    ],
    (v) => v['connections'] = [
      {...closeEntry(), 'personId': '00000000-0000-0000-0000-000000000000'},
    ],
    (v) => v['connections'] = [
      {...closeEntry(), 'isClose': true, 'nativeRevision': 0},
    ],
    (v) => v['connections'] = [
      {...closeEntry(), 'nativeRevision': -1},
    ],
    (v) => v['connections'] = [
      {...closeEntry(), 'displayName': 'a' * 81},
    ],
    (v) => v['connections'] = [
      {...closeEntry(), 'sourceVersion': 'UNKNOWN'},
    ],
    (v) => v['connections'] = [
      {...closeEntry(), 'privateNote': 'hidden'},
    ],
    (v) => v['validUntil'] = closeNow
        .add(const Duration(seconds: 31))
        .toIso8601String(),
    (v) => v['validUntil'] = v['observedAt'],
    (v) => v['observedAt'] = '2026-02-30T10:00:00Z',
    (v) => v['observedAt'] = '2026-10-10T10:00:00+08:00',
    (v) => v['observedAt'] = '2026-10-10T10:00:00.123456789Z',
  ]) {
    test('闭集名单无效项 ${mutation.hashCode} failclosed', () async {
      final raw = closeList();
      mutation(raw);
      final api = closeAPI((_) async => closeJSON(raw));
      addTearDown(api.dispose);
      await expectLater(
        api.list('Bearer synthetic', closeOwner),
        throwsFormatException,
      );
    });
  }
  test('重复与转义重复绑定字段不能被jsonDecode覆盖', () async {
    for (final key in ['ownerId', r'owner\u0049d']) {
      final json = jsonEncode(closeList());
      final raw =
          '{"data":${json.substring(0, json.length - 1)},"$key":"$closeOwner"}}';
      final api = closeAPI(
        (_) async => http.Response(
          raw,
          200,
          headers: {'content-type': 'application/json; charset=utf-8'},
        ),
      );
      await expectLater(
        api.list('Bearer synthetic', closeOwner),
        throwsFormatException,
      );
      api.dispose();
    }
  });
  test('明确PUT只提交捕获当前tie/request/hash/deadline且核对同对象回执', () async {
    final calls = <http.Request>[];
    final api = closeAPI((r) async {
      calls.add(r);
      return closeJSON(closeResult());
    });
    addTearDown(api.dispose);
    final before = CloseConnection.decode(closeEntry()),
        deadline = closeNow.add(const Duration(seconds: 30));
    final receipt = await api.add(
      'Bearer synthetic',
      closeOwner,
      before,
      deadline,
    );
    expect(receipt.connection.isClose, true);
    expect(calls.single.method, 'PUT');
    expect(calls.single.url.path, '/v1/me/close-connections/$closePeer');
    expect(jsonDecode(calls.single.body), {
      'tieId': closeTie,
      'requestId': closeRequest,
      'sourceVersion': 'a' * 64,
      'validUntil': deadline.toIso8601String(),
      'confirmed': true,
    });
  });
  test('过期具体来源不发PUT，错对象回执不冒充成功', () async {
    var calls = 0;
    final api = closeAPI((r) async {
      calls++;
      return closeJSON(
        closeResult(
          entry: {...closeEntry(selected: true), 'requestId': closeTie},
        ),
      );
    });
    addTearDown(api.dispose);
    final entry = CloseConnection.decode(closeEntry());
    await expectLater(
      api.add('Bearer synthetic', closeOwner, entry, closeNow),
      throwsFormatException,
    );
    expect(calls, 0);
    await expectLater(
      api.add(
        'Bearer synthetic',
        closeOwner,
        entry,
        closeNow.add(const Duration(seconds: 30)),
      ),
      throwsFormatException,
    );
    expect(calls, 1);
  });
  test('DELETE绑定当前nativeRevision且204无虚构名单', () async {
    final calls = <http.Request>[];
    final api = closeAPI((r) async {
      calls.add(r);
      return http.Response('', 204);
    });
    addTearDown(api.dispose);
    await api.remove(
      'Bearer synthetic',
      closeOwner,
      CloseConnection.decode(closeEntry(selected: true, revision: 7)),
    );
    expect(calls.single.method, 'DELETE');
    expect(jsonDecode(calls.single.body), {
      'tieId': closeTie,
      'requestId': closeRequest,
      'expectedRevision': 7,
      'confirmed': true,
    });
  });
  test('响应错误保留真实409，不关闭借用transport', () async {
    final c = CloseBorrowedClient((_) async => http.Response('', 409));
    final api = CloseConnectionsAPI(
      client: c,
      apiBaseUrl: 'http://local',
      now: () => closeNow,
    );
    await expectLater(
      api.list('Bearer synthetic', closeOwner),
      throwsA(
        isA<CloseConnectionsException>().having((v) => v.status, 'status', 409),
      ),
    );
    api.dispose();
    expect(c.closes, 0);
    c.close();
  });
}
