import 'dart:convert';

import 'package:birdtie_client/src/workspace/agent_answer_sources.dart';
import 'package:birdtie_client/src/workspace/agent_reply_membership.dart';
import 'package:birdtie_client/src/workspace/agent_workspace_controller.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'reply_history_restore_test.dart' as history;

const _evidenceA = '017b6131-2b54-4e09-b55b-66ec2d7a8e01';
const _evidenceB = '017b6131-2b54-4e09-b55b-66ec2d7a8e02';
const _permission = '5be7afad-e474-42f9-acec-0b40e7be952c';
const _memory = 'dfdfc105-91d5-4d38-b0ad-4772203f97d7';
const _titleA = 'Aberdeen · 第一轮原始来源';
const _titleB = 'Aberdeen · 第二轮原始来源';

// Exact synthetic Task DTO captured from API19 native-http-03 / call 19.
// Auth headers and private credentials are excluded. This only replays the
// original parser contract; it does not turn this expired fixture into a grant.
const _capturedNativeTask = r'''
{
  "id": "cc0173ec-c318-4703-b666-5dd369b5575c",
  "principalType": "person",
  "principalId": "8da3554c-5ace-4136-b90a-b86be84ad797",
  "actingUserId": "8da3554c-5ace-4136-b90a-b86be84ad797",
  "cityContext": "aberdeen-gb", "contextType": "CITY",
  "contextId": "08755d32-57c3-49c3-b256-807b364d7e21",
  "query": "PG合成丁短时资料的地址是什么？",
  "intent": "FIND_PLACE", "status": "COMPLETED",
  "filters": {"category":"", "currentQuery":"PG合成丁短时资料的地址是什么？",
    "locationPreference":"city", "searchTerm":"PG合成丁短时资料", "targetIntent":"FIND_PLACE"},
  "conversation": [
    {"role":"user", "text":"PG合成丁短时资料的地址是什么？"},
    {
      "role":"assistant",
      "text":"PG合成丁短时资料的地址是：SYNTHETIC TEMPORARY ADDRESS, NOT REAL [source-1]。",
      "sources":[{"id":"source-1", "title":"Birdtie synthetic fixture, NOT REAL",
        "url":"https://birdtie.example/isolated-fixtures/pg-short-lived"}],
      "sourceEvidenceDigest":"74ce6c4c17bbabf51a0779727b92f5aec0e48a8c409a97d61344e2468da0e2dc",
      "resultMembership":{
        "schema":"agent-reply-membership-v1",
        "taskId":"cc0173ec-c318-4703-b666-5dd369b5575c", "cityId":"aberdeen-gb", "kind":"place",
        "turnDigest":"18274dfcde038fb78cc688a8272ec3aab0129f2c4e925dc93491f59d3ec7d3da",
        "resultSetId":"reply:cc0173ec-c318-4703-b666-5dd369b5575c:18274dfcde038fb78cc688a8",
        "refs":[{"type":"place", "id":"b8d65896-4805-4a34-8c83-d88938d229c5"}]
      },
      "localReplyEvidence":{
        "schema":"birdtie.local-place-reply.v1",
        "id":"local-native:cc0173ec-c318-4703-b666-5dd369b5575c:1",
        "sourceEvidenceDigest":"74ce6c4c17bbabf51a0779727b92f5aec0e48a8c409a97d61344e2468da0e2dc",
        "validUntil":"2026-10-09T01:02:03.250643+08:00",
        "facts":[
          {"evidenceId":"fe460fa4-cb31-4cc8-960d-31b680bfc87e", "placeId":"b8d65896-4805-4a34-8c83-d88938d229c5",
            "field":"address", "version":1, "permissionId":"5616cc25-4241-4493-a696-bde76e431a5f"},
          {"evidenceId":"eb366d09-523d-44cd-b2e3-4cd49bbb7b25", "placeId":"b8d65896-4805-4a34-8c83-d88938d229c5",
            "field":"coordinates", "version":1, "permissionId":"5616cc25-4241-4493-a696-bde76e431a5f"},
          {"evidenceId":"5bfc54b8-6b60-4211-bcb5-05ea218b8676", "placeId":"b8d65896-4805-4a34-8c83-d88938d229c5",
            "field":"name", "version":1, "permissionId":"5616cc25-4241-4493-a696-bde76e431a5f"}
        ],
        "memoryUses":[], "reason":"LOCAL_FIELDS_SUFFICIENT", "searchCalls":0, "modelCalls":0
      }
    }
  ],
  "createdAt":"2026-10-09T01:01:51.293949+08:00",
  "updatedAt":"2026-10-09T01:01:51.31221+08:00"
}
''';

Map<String, dynamic> _copy(Map<String, dynamic> data) =>
    jsonDecode(jsonEncode(data)) as Map<String, dynamic>;
Map<String, dynamic> _task(Map<String, dynamic> wire) => wire['task'];
Map<String, dynamic> _message(Map<String, dynamic> wire, [int index = 1]) =>
    _task(wire)['conversation'][index];
Map<String, dynamic> _proof(Map<String, dynamic> wire, [int index = 1]) =>
    _message(wire, index)['localReplyEvidence'];

// Synthetic protocol fixtures only. These do not assert native licensing,
// actual restaurant facts, a provider call, an import or a model Run.
Map<String, dynamic> _localWire(
  DateTime now, {
  bool oldObservation = false,
  bool liveBinding = false,
}) {
  final wire = history.historyWire(now);
  final messages = _task(wire)['conversation'] as List;
  final rounds = wire['messageResults'] as List;
  for (final (turn, place) in [history.placeA, history.placeB].indexed) {
    final index = turn * 2 + 1;
    final message = messages[index] as Map<String, dynamic>;
    message['text'] = '第 ${turn + 1} 轮站内许可地址。[source-1]';
    final digest = (turn == 0 ? 'a' : 'b') * 64;
    message['sources'] = [
      {
        'id': 'source-1',
        'title': turn == 0 ? _titleA : _titleB,
        'url': 'https://example.org/local-place-${turn + 1}',
      },
    ];
    message['sourceEvidenceDigest'] = digest;
    message['localReplyEvidence'] = {
      'schema': 'birdtie.local-place-reply.v1',
      'id': 'local-native:${history.taskID}:$index',
      'sourceEvidenceDigest': digest,
      'validUntil': now
          .add(Duration(minutes: oldObservation ? -10 : 1))
          .toIso8601String(),
      'facts': [
        {
          'evidenceId': turn == 0 ? _evidenceA : _evidenceB,
          'placeId': place,
          'field': 'address',
          'version': 1,
          'permissionId': _permission,
        },
      ],
      'memoryUses': <dynamic>[],
      'reason': 'LOCAL_FIELDS_SUFFICIENT',
      'searchCalls': 0,
      'modelCalls': 0,
    };
    final turnDigest = agentReplyTurnDigest(
      messages
          .take(index + 1)
          .map((m) => (role: m['role'] as String, text: m['text'] as String)),
    );
    final setID = 'reply:${history.taskID}:${turnDigest.substring(0, 24)}';
    message['resultMembership']['turnDigest'] = turnDigest;
    message['resultMembership']['resultSetId'] = setID;
    rounds[turn]['turnDigest'] = turnDigest;
    rounds[turn]['resultSet']['id'] = setID;
  }
  wire['message'] = messages.last['text'];
  if (liveBinding) {
    final set = _copy(wire['resultSet']);
    final last = _message(wire, 3);
    set['sources'] = last['sources'];
    set['answerBinding'] = {
      'taskId': history.taskID,
      'requestId': wire['requestId'],
      'currentQueryDigest': agentAnswerQueryDigest('找地点 B'),
      'taskSnapshotDigest': 'c' * 64,
      'sourceEvidenceDigest': last['sourceEvidenceDigest'],
      'runId': last['localReplyEvidence']['id'],
      'generatedAt': now.toIso8601String(),
      'validUntil': now.add(const Duration(minutes: 1)).toIso8601String(),
    };
    wire['resultSet'] = set;
  }
  return _copy(wire);
}

Map<String, dynamic> _memoryUse() => {
  'memoryId': _memory,
  'version': 2,
  'topic': 'spicy',
  'value': 'DISLIKE',
};

http.Response _response(Object data) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  200,
  headers: {'content-type': 'application/json'},
);

void main() {
  final observed = DateTime.utc(2026, 10, 9, 2);

  test('API19原始合成Task DTO重放接受+08:00观察时间，默认无历史授权', () {
    final raw = jsonDecode(_capturedNativeTask) as Map<String, dynamic>;
    final task = AgentTask.fromJson(raw, sourceCurrent: () => true);
    final refs = task.messages.last.sourceReferences!;
    expect(
      refs.localObservedValidUntil,
      DateTime.utc(2026, 10, 8, 17, 2, 3, 250, 643),
    );
    expect(refs.sources.single.title, 'Birdtie synthetic fixture, NOT REAL');
    expect(
      refs.sources.single.uri.toString(),
      'https://birdtie.example/isolated-fixtures/pg-short-lived',
    );
    expect(refs.runID, isEmpty);
    expect(refs.current, isFalse);
    expect(
      task.messages.last.resultMembership!.turnDigest,
      agentReplyTurnDigest(
        task.messages.map(
          (message) => (role: message.role, text: message.text),
        ),
      ),
    );
  });

  for (final date in [
    '2026-10-09T10:01:00+08:00',
    '2026-10-08T21:01:00-05:00',
    '2026-10-09T02:01:00+00:00',
    '2026-10-09T02:01:00Z',
  ]) {
    test('本地严格RFC3339观察时间 $date 规范化UTC，不授予读许可', () {
      final wire = _localWire(observed);
      _proof(wire)['validUntil'] = date;
      final refs = AgentTask.fromJson(
        _task(wire),
        sourceCurrent: () => true,
      ).messages[1].sourceReferences!;
      expect(
        refs.localObservedValidUntil,
        observed.add(const Duration(minutes: 1)),
      );
      expect(refs.current, isFalse);
    });
  }

  for (final date in [
    '2026-10-09T10:01:00+24:00',
    '2026-10-09T10:01:00-24:01',
    '2026-10-09T10:01:00+08:60',
    '2026-10-09T10:01:00+0800',
    '2026-10-09T10:01:00+8:00',
    '2026-10-09T10:01:00-00:00',
    '2026-10-09T10:01:00',
    '2026-10-32T10:01:00+08:00',
    '2026-10-09T24:01:00+08:00',
    '2026-10-09T10:60:00+08:00',
    '2026-10-09T10:01:61+08:00',
  ]) {
    test('本地拒绝异常civil或offset时间 $date', () {
      final wire = _localWire(observed);
      _proof(wire)['validUntil'] = date;
      expect(
        () => AgentTask.fromJson(
          _task(wire),
          sourceCurrent: () => true,
          localRoundCurrent: (_) => true,
        ),
        throwsFormatException,
      );
    });
  }

  test('offset观察时间不放宽live answer binding的原strictZ要求', () {
    final wire = _localWire(observed, liveBinding: true);
    _proof(wire, 3)['validUntil'] = '2026-10-09T10:01:00+08:00';
    wire['resultSet']['answerBinding']['validUntil'] =
        '2026-10-09T10:01:00+08:00';
    expect(
      () => AgentAnswerSources.read(
        wire,
        current: () => true,
        now: () => observed,
      ),
      throwsFormatException,
    );
  });

  test('本地来源保留原始标题URL，无回调默认关闭且不伪造Run', () {
    final wire = _localWire(observed);
    final task = AgentTask.fromJson(_task(wire), sourceCurrent: () => true);
    final source = task.messages[1].sourceReferences!;
    expect(source.runID, isEmpty);
    expect(source.localReplyID, 'local-native:${history.taskID}:1');
    expect(
      source.localObservedValidUntil,
      observed.add(const Duration(minutes: 1)),
    );
    expect(source.sources.single.title, _titleA);
    expect(
      source.sources.single.uri.toString(),
      'https://example.org/local-place-1',
    );
    expect(source.current, isFalse);
    expect(() => source.sources.clear(), throwsUnsupportedError);
    _message(wire)['sources'][0]['title'] = '调用方改写';
    expect(source.sources.single.title, _titleA);
    expect(task.messages[1].withText('展示文字').sourceReferences, same(source));
  });

  test('local回调仅对应指定轮，缺少任务索引不能单独激活来源', () {
    final rounds = <int>{3};
    final wire = _localWire(observed);
    final task = AgentTask.fromJson(
      _task(wire),
      sourceCurrent: () => true,
      localRoundCurrent: rounds.contains,
    );
    expect(task.messages[1].sourceReferences!.current, isFalse);
    final latest = task.messages[3].sourceReferences!;
    expect(latest.current, isTrue);
    rounds.clear();
    expect(latest.current, isFalse);
    rounds.add(3);
    expect(latest.current, isFalse);
    final direct = AgentMessageSources.read(
      _message(wire),
      current: () => true,
      localRoundCurrent: () => true,
    )!;
    expect(direct.current, isFalse);
  });

  test('remaining与持久未来时间不能单独授予本地来源读许可', () {
    final wire = _localWire(observed);
    _proof(wire)['validUntil'] = observed
        .add(const Duration(days: 1))
        .toIso8601String();
    _message(wire)['sourceRunId'] = '';
    final task = AgentTask.fromJson(
      _task(wire),
      sourceCurrent: () => true,
      localRoundRemaining: (_) => const Duration(days: 1),
    );
    expect(task.messages[1].sourceReferences!.runID, isEmpty);
    expect(task.messages[1].sourceReferences!.current, isFalse);
  });

  test('原GET恢复两轮独立local来源与地点lease，current binding沿用原解析', () async {
    final wire = _localWire(observed, liveBinding: true);
    final methods = <String>[];
    final source = history.historyRemote(() => wire, methods, () => observed);
    addTearDown(source.dispose);
    final result = await source.restore(
      AgentTask.fromJson(_task(wire)),
      [],
      [],
    );
    final first = result.task!.messages[1].sourceReferences!;
    final last = result.task!.messages[3].sourceReferences!;
    expect(first.current, isTrue);
    expect(last.current, isTrue);
    expect(first.sources.single.title, _titleA);
    expect(last.sources.single.title, _titleB);
    expect(first.runID, isEmpty);
    expect(last.runID, isEmpty);
    expect(result.messageResults![1]!.messageSources, same(first));
    expect(result.messageResults![3]!.messageSources, same(last));
    expect(
      result.messageResults![1]!.entities.single.id,
      'place:${history.placeA}',
    );
    expect(result.resultSet!.answerSources!.runID, last.localReplyID);
    expect(result.resultSet!.answerSources!.current, isTrue);
    expect(methods, ['GET']);
  });

  test('Recent单独读取没有local轮次许可；随后GET独立核验后才可点', () async {
    final wire = _localWire(observed);
    final methods = <String>[];
    final source = RemoteAgentTaskSource(
      cityID: () => history.cityID,
      authorizationHeader: () => 'Bearer synthetic-original',
      publicEvidenceOwnerID: () => history.ownerID,
      publicEvidenceNow: () => observed,
      apiBaseUrl: 'http://localhost:8080',
      client: MockClient((request) async {
        methods.add(request.method);
        return _response(
          request.url.path.endsWith('/agent-tasks') ? [_task(wire)] : wire,
        );
      }),
    );
    addTearDown(source.dispose);
    final recent = await source.loadRecent();
    expect(recent.single.messages[1].sourceReferences!.current, isFalse);
    final result = await source.restore(recent.single, [], []);
    expect(result.task!.messages[1].sourceReferences!.current, isTrue);
    expect(recent.single.messages[1].sourceReferences!.current, isFalse);
    expect(methods, ['GET', 'GET']);
  });

  for (final missing in ['omitted', 'empty', 'first', 'last']) {
    test('history $missing 不能将另一轮或持久时间嫁接为local来源许可', () async {
      final wire = _localWire(observed);
      final entries = wire['messageResults'] as List;
      switch (missing) {
        case 'omitted':
          wire.remove('messageResults');
        case 'empty':
          wire['messageResults'] = <dynamic>[];
        case 'first':
          entries.removeAt(0);
        case 'last':
          entries.removeAt(1);
      }
      final methods = <String>[];
      final source = history.historyRemote(() => wire, methods, () => observed);
      addTearDown(source.dispose);
      final result = await source.restore(
        AgentTask.fromJson(_task(wire)),
        [],
        [],
      );
      expect(
        result.task!.messages[1].sourceReferences!.current,
        missing == 'last',
      );
      expect(
        result.task!.messages[3].sourceReferences!.current,
        missing == 'first',
      );
      if (missing == 'empty' || missing == 'last') {
        expect(result.replyProjectionCurrent, isFalse);
        expect(result.entities, isEmpty);
      }
      expect(methods, ['GET']);
    });
  }

  test('持久观察时间不授予新lease也不阻止同版字段的独立history读取', () async {
    var now = observed;
    final wire = _localWire(observed, oldObservation: true);
    final source = history.historyRemote(() => wire, [], () => now);
    addTearDown(source.dispose);
    final result = await source.restore(
      AgentTask.fromJson(_task(wire)),
      [],
      [],
    );
    final refs = result.task!.messages[1].sourceReferences!;
    expect(refs.localObservedValidUntil!.isBefore(now), isTrue);
    expect(refs.current, isTrue);
    now = now.add(const Duration(minutes: 1));
    expect(refs.current, isFalse);
    now = observed;
    expect(refs.current, isFalse);
  });

  for (final changed in [
    'account',
    'organization',
    'city',
    'online',
    'owner',
    'epoch',
    'dispose',
  ]) {
    test('local来源在 $changed 改变后永久关闭，恢复旧值不复活', () async {
      var token = 'Bearer synthetic-original';
      String? organization, online;
      var city = history.cityID, owner = history.ownerID, epoch = 1;
      final source = RemoteAgentTaskSource(
        cityID: () => city,
        authorizationHeader: () => token,
        organizationWorkspaceID: () => organization,
        onlineContextID: () => online,
        publicEvidenceOwnerID: () => owner,
        publicEvidenceEpoch: () => epoch,
        publicEvidenceNow: () => observed,
        apiBaseUrl: 'http://localhost:8080',
        client: MockClient((_) async => _response(_localWire(observed))),
      );
      addTearDown(source.dispose);
      final result = await source.restore(
        const AgentTask(id: history.taskID, query: '原问题', status: 'COMPLETED'),
        [],
        [],
      );
      final refs = result.task!.messages[1].sourceReferences!;
      expect(refs.current, isTrue);
      switch (changed) {
        case 'account':
          token = 'Bearer another';
        case 'organization':
          organization = 'another';
        case 'city':
          city = 'another';
        case 'online':
          online = 'another';
        case 'owner':
          owner = 'another';
        case 'epoch':
          epoch++;
        case 'dispose':
          source.dispose();
      }
      expect(refs.current, isFalse);
      token = 'Bearer synthetic-original';
      organization = online = null;
      city = history.cityID;
      owner = history.ownerID;
      epoch = 1;
      expect(refs.current, isFalse);
    });
  }

  test('无SourceRunID的旧未绑定消息保持无可点来源，模型消息契约不变', () {
    final wire = _localWire(observed);
    _message(wire).remove('localReplyEvidence');
    expect(
      AgentTask.fromJson(
        _task(wire),
        sourceCurrent: () => true,
      ).messages[1].sourceReferences,
      isNull,
    );
    final message = _message(wire)..['sourceRunId'] = 'actual-model-run';
    final parsed = AgentMessage.fromJson(
      message,
      sourceCurrent: () => true,
    ).sourceReferences!;
    expect(parsed.runID, 'actual-model-run');
    expect(parsed.localReplyID, isNull);
    expect(parsed.current, isTrue);
  });

  test('排除的候选可以保留独立许可fact，不将facts当选中地点或授权', () {
    final wire = _localWire(observed);
    _proof(wire)['facts'].add({
      'evidenceId': _evidenceB,
      'placeId': history.placeB,
      'field': 'food_spicy',
      'version': 3,
      'permissionId': _permission,
    });
    _proof(wire)['memoryUses'] = [_memoryUse()];
    final task = AgentTask.fromJson(_task(wire), sourceCurrent: () => true);
    expect(task.messages[1].sourceReferences!.current, isFalse);
    expect(task.messages[1].resultMembership!.refs.single.id, history.placeA);
  });

  test('同地点字段的等值独立许可保留完整proof，不误判为重复', () {
    final wire = _localWire(observed);
    final fact = _copy(_proof(wire)['facts'][0])
      ..['evidenceId'] = _evidenceB
      ..['permissionId'] = '5be7afad-e474-42f9-acec-0b40e7be952d';
    _proof(wire)['facts'].add(fact);
    final task = AgentTask.fromJson(
      _task(wire),
      sourceCurrent: () => true,
      localRoundCurrent: (index) => index == 1,
    );
    expect(task.messages[1].sourceReferences!.current, isTrue);
    expect(
      task.messages[1].sourceReferences!.localReplyID,
      'local-native:${history.taskID}:1',
    );
  });

  final mutations = <String, void Function(Map<String, dynamic>)>{
    '未知schema': (w) => _proof(w)['schema'] = 'another',
    '未知证明字段': (w) => _proof(w)['grant'] = true,
    '缺少证明字段': (w) => _proof(w).remove('reason'),
    '理由不是本地字段': (w) => _proof(w)['reason'] = 'MODEL_ANSWER',
    '摘要不匹配': (w) => _proof(w)['sourceEvidenceDigest'] = 'c' * 64,
    '零摘要': (w) {
      _proof(w)['sourceEvidenceDigest'] = '0' * 64;
      _message(w)['sourceEvidenceDigest'] = '0' * 64;
    },
    '跨Task correlation': (w) => _proof(w)['id'] = 'local-native:another:1',
    '跨消息 correlation': (w) =>
        _proof(w)['id'] = 'local-native:${history.taskID}:3',
    '负索引': (w) => _proof(w)['id'] = 'local-native:${history.taskID}:-1',
    '有搜索调用': (w) => _proof(w)['searchCalls'] = 1,
    '有模型调用': (w) => _proof(w)['modelCalls'] = 1,
    '计数错误类型': (w) => _proof(w)['modelCalls'] = '0',
    '混入模型Run': (w) => _message(w)['sourceRunId'] = 'actual-model-run',
    '缺失membership': (w) => _message(w).remove('resultMembership'),
    '不是地点membership': (w) {
      _message(w)['resultMembership']['kind'] = 'activity';
      _message(w)['resultMembership']['refs'][0]['type'] = 'activity';
    },
    '用户挂local证明': (w) => _message(w)['role'] = 'user',
    '错误时间格式': (w) => _proof(w)['validUntil'] = '2026-10-09T02:01:00+08:99',
    '溢出日期': (w) => _proof(w)['validUntil'] = '2026-10-32T02:01:00Z',
    '空facts': (w) => _proof(w)['facts'] = <dynamic>[],
    '过多facts': (w) =>
        _proof(w)['facts'] = List.filled(101, _proof(w)['facts'][0]),
    '重复evidence': (w) => _proof(w)['facts'].add(_copy(_proof(w)['facts'][0])),
    '重复place field': (w) {
      final f = _copy(_proof(w)['facts'][0])..['evidenceId'] = _evidenceB;
      _proof(w)['facts'].add(f);
    },
    '无效evidenceID': (w) => _proof(w)['facts'][0]['evidenceId'] = 'made-up',
    '无效placeID': (w) => _proof(w)['facts'][0]['placeId'] = 'made-up',
    '零permissionID': (w) => _proof(w)['facts'][0]['permissionId'] =
        '00000000-0000-0000-0000-000000000000',
    '未知fact字段': (w) => _proof(w)['facts'][0]['field'] = 'private_note',
    'fact扩展grant': (w) => _proof(w)['facts'][0]['grant'] = true,
    'fact版本零': (w) => _proof(w)['facts'][0]['version'] = 0,
    'fact版本浮点': (w) => _proof(w)['facts'][0]['version'] = 1.0,
    'memory未知topic': (w) =>
        _proof(w)['memoryUses'] = [_memoryUse()..['topic'] = 'private'],
    'memory未知value': (w) =>
        _proof(w)['memoryUses'] = [_memoryUse()..['value'] = 'GUESS'],
    'memory无效版本': (w) =>
        _proof(w)['memoryUses'] = [_memoryUse()..['version'] = 0],
    'memory未知字段': (w) =>
        _proof(w)['memoryUses'] = [_memoryUse()..['grant'] = true],
    '重复memory': (w) => _proof(w)['memoryUses'] = [_memoryUse(), _memoryUse()],
    '过多memory': (w) =>
        _proof(w)['memoryUses'] = List.generate(3, (_) => _memoryUse()),
    '空来源': (w) => _message(w)['sources'] = <dynamic>[],
    '来源伪URL': (w) => _message(w)['sources'][0]['url'] = 'javascript:alert(1)',
    '来源含凭据': (w) =>
        _message(w)['sources'][0]['url'] = 'https://user:secret@example.org/a',
    '重复来源': (w) => _message(w)['sources'].add(_copy(_message(w)['sources'][0])),
  };
  for (final entry in mutations.entries) {
    test('local来源拒绝 ${entry.key}，不因回调true晋升错误证明', () {
      final wire = _localWire(observed);
      entry.value(wire);
      expect(
        () => AgentTask.fromJson(
          _task(wire),
          sourceCurrent: () => true,
          localRoundCurrent: (_) => true,
        ),
        throwsFormatException,
      );
    });
  }

  test('local桥接拒绝已过期的history lease', () async {
    final wire = _localWire(observed);
    wire['messageResults'][0]['validUntil'] = observed.toIso8601String();
    final source = history.historyRemote(() => wire, [], () => observed);
    addTearDown(source.dispose);
    await expectLater(
      source.restore(AgentTask.fromJson(_task(wire)), [], []),
      throwsFormatException,
    );
  });

  testWidgets('独立local来源面板可展开完整原文，轮次失效立即停止打开', (tester) async {
    var current = true;
    final task = AgentTask.fromJson(
      _task(_localWire(observed)),
      sourceCurrent: () => current,
      localRoundCurrent: (_) => true,
    );
    final sources = task.messages[1].sourceReferences!;
    final changes = ChangeNotifier();
    addTearDown(changes.dispose);
    final opened = <Uri>[];
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: AgentAnswerSourcesPanel.persisted(
              sources: sources,
              replyCurrent: () => current,
              changes: changes,
              openSource: (uri) async {
                opened.add(uri);
                return true;
              },
            ),
          ),
        ),
      ),
    );
    expect(find.text(_titleA), findsNothing);
    await tester.tap(find.text('全部来源 1'));
    await tester.pumpAndSettle();
    expect(find.text(_titleA), findsOneWidget);
    expect(find.text('https://example.org/local-place-1'), findsOneWidget);
    await tester.tap(find.text('打开原文'));
    await tester.pump();
    expect(opened, [Uri.parse('https://example.org/local-place-1')]);
    current = false;
    changes.notifyListeners();
    await tester.pump();
    final open = find.widgetWithText(TextButton, '打开原文');
    expect(open, findsNothing);
    expect(find.text(_titleA), findsNothing);
    expect(find.text('https://example.org/local-place-1'), findsNothing);
    expect(opened, hasLength(1));
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('local面板采用本次history lease自动停用，原观察到期和回退均不续期', (tester) async {
    var now = observed;
    final wire = _localWire(observed, oldObservation: true);
    _proof(wire)['validUntil'] = '2026-10-09T09:50:00+08:00';
    for (final entry in wire['messageResults'] as List) {
      entry['validUntil'] = observed
          .add(const Duration(seconds: 2))
          .toIso8601String();
    }
    final source = history.historyRemote(() => wire, [], () => now);
    addTearDown(source.dispose);
    final result = await source.restore(
      AgentTask.fromJson(_task(wire)),
      [],
      [],
    );
    final refs = result.task!.messages[1].sourceReferences!;
    expect(refs.remaining, const Duration(seconds: 2));
    expect(refs.localObservedValidUntil!.isBefore(observed), isTrue);
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: AgentAnswerSourcesPanel.persisted(
              sources: refs,
              replyCurrent: () => true,
              openSource: (_) async => true,
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('全部来源 1'));
    await tester.pump();
    expect(find.text(_titleA), findsOneWidget);
    now = observed.add(const Duration(seconds: 2));
    await tester.pump(const Duration(seconds: 2));
    expect(find.text(_titleA), findsNothing);
    expect(find.text('https://example.org/local-place-1'), findsNothing);
    final open = find.widgetWithText(TextButton, '打开原文');
    expect(open, findsNothing);
    expect(refs.remaining, Duration.zero);
    now = observed;
    await tester.pump();
    expect(refs.current, isFalse);
    expect(open, findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
