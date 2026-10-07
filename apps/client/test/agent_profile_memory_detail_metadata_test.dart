import 'package:birdtie_client/src/workspace/agent_profile_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'agent_memory_correction_api_test.dart' show correctionMemoryRaw;
import 'agent_memory_field_evidence_test.dart' show memoryDetailWire;
import 'agent_profile_api_test.dart';

// Synthetic transport projection of the registered single-record detail GET.
// This original Profile view reads the original summary, not evidence claims.
Map<String, dynamic> profileMetadataDetail({DateTime? at}) =>
    memoryDetailWire(at: at ?? DateTime.now().toUtc());

void main() {
  test('当前详情元数据兼容：原DTO读取当前单条投影', () {
    final wire = profileMetadataDetail();
    final view = AgentMemoryDetailView(wire, profileOwner, profileMemory);
    expect(view.agentID, profileAgent);
    expect(view.id, profileMemory);
    expect(view.status, 'ACTIVE');
    expect(view.memory!.summary, '我偏好徒步活动');
    expect(view.memory!.version, 1);
    expect(view.observedAt, DateTime.parse(wire['observedAt']));
    expect(view.expiresAt, DateTime.parse(wire['expiresAt']));
  });

  test('当前详情元数据兼容：旧无元数据详情保持原读取', () {
    final view = AgentMemoryDetailView(
      detailRaw(),
      profileOwner,
      profileMemory,
    );
    expect(view.memory!.summary, '我偏好徒步活动');
    expect(view.memory!.sourceType, 'EXPLICIT');
  });

  test('当前详情元数据兼容：待审推断仍是待审，不变成明确声明', () {
    final at = DateTime.now().toUtc();
    final memory = correctionMemoryRaw(at: at)
      ..['sourceType'] = 'INFERRED'
      ..['status'] = 'PENDING_REVIEW'
      ..['confidence'] = .4;
    final view = AgentMemoryDetailView(
      memoryDetailWire(at: at, memory: memory, status: 'PENDING_REVIEW'),
      profileOwner,
      profileMemory,
    );
    expect(view.status, 'PENDING_REVIEW');
    expect(view.memory!.sourceType, 'INFERRED');
    expect(view.memory!.raw['confidence'], .4);
  });

  for (final status in ['EXPIRED', 'DELETED']) {
    test('当前详情元数据兼容：$status元数据不能复活正文', () {
      final wire = memoryDetailWire(at: DateTime.now().toUtc(), status: status);
      wire['fieldEvidenceSet']['summary'] = '元数据不能替代正文';
      final view = AgentMemoryDetailView(wire, profileOwner, profileMemory);
      expect(view.status, status);
      expect(view.memory, isNull);
    });
  }

  test('当前详情元数据兼容：不消费可选字段中的身份正文或批准', () {
    final wire = profileMetadataDetail();
    wire['fieldEvidenceSet'] = {
      'owner': {'type': 'PERSON', 'id': '不是当前本人'},
      'memory': {'summary': '不能作为原正文'},
      'modelAccess': true,
      'grantsAuthority': true,
      'approved': true,
    };
    final view = AgentMemoryDetailView(wire, profileOwner, profileMemory);
    expect(view.agentID, profileAgent);
    expect(view.memory!.raw['ownerId'], profileOwner);
    expect(view.memory!.summary, '我偏好徒步活动');
    expect(view.status, 'ACTIVE');
  });

  final invalid = <String, void Function(Map<String, dynamic>)>{
    '未知顶层字段': (w) => w['anotherEvidenceSet'] = {},
    '错误本人': (w) => w['owner']['id'] = '82000000-0000-4000-8000-000000000099',
    '错误agent': (w) => w['agentId'] = '82000000-0000-4000-8000-000000000099',
    '错误目标ID': (w) => w['target']['id'] = '82000000-0000-4000-8000-000000000099',
    '错误目标版本': (w) => w['target']['version'] = 2,
    '错误目标状态': (w) => w['target']['status'] = 'PENDING_REVIEW',
    '正文缺失': (w) => w['memory'] = null,
    '正文未知字段': (w) => w['memory']['fieldEvidenceSet'] = {},
    '无效期限': (w) => w['expiresAt'] = w['observedAt'],
    '过长读取期限': (w) => w['expiresAt'] = DateTime.parse(
      w['observedAt'],
    ).add(const Duration(minutes: 3)).toIso8601String(),
    '正文已过期': (w) => w['memory']['validUntil'] = w['observedAt'],
    '正文来自未来': (w) => w['memory']['updatedAt'] = w['expiresAt'],
    '模型许可不能开启': (w) => w['modelAccess'] = true,
    '错误解释': (w) => w['explanation'] = '已确认真实事实',
  };
  for (final entry in invalid.entries) {
    test('当前详情元数据兼容：保留${entry.key}拒绝', () {
      final wire = profileMetadataDetail();
      entry.value(wire);
      expect(
        () => AgentMemoryDetailView(wire, profileOwner, profileMemory),
        throwsA(isA<FormatException>()),
      );
    });
  }

  test('当前详情元数据兼容：原API仍仅本人GET，不写入且不关闭借用client', () async {
    final requests = <String>[];
    final client = ProfileTrackedClient((r) async {
      requests.add('${r.method} ${r.url.path}');
      expect(r.headers['Authorization'], 'Bearer synthetic-owner');
      return profileResponse(profileMetadataDetail());
    });
    addTearDown(client.close);
    final api = AgentProfileAPI(client: client, apiBaseUrl: 'http://local');
    final d = await api.detail(
      'Bearer synthetic-owner',
      profileOwner,
      profileMemory,
    );
    expect(d.memory!.summary, '我偏好徒步活动');
    expect(requests, ['GET /v1/me/agent-memories/$profileMemory']);
    api.close();
    expect(client.closeCount, 0);
  });
}
