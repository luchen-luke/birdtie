import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/content/collection_publication_controller.dart';
import 'package:birdtie_client/src/content/personal_collection_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'personal_collection_controller_test.dart'
    show
        collectionOwner,
        collectionID,
        collectionPlaceID,
        collectionReceipt,
        collectionResponse,
        collectionController;

Map<String, dynamic> operationRecord(
  Map<String, dynamic> pending, {
  String? wrongOperation,
}) =>
    collectionReceipt(
        revision: (pending['revision'] as int) + 1,
        status: pending['action'] == 'PUBLISH' ? 'published' : 'draft',
      )
      ..['visibility'] = pending['action'] == 'PUBLISH' ? 'public' : 'private'
      ..['publicationOperation'] = {
        'operationId': wrongOperation ?? pending['operationId'],
        'action': pending['action'],
        'expectedRevision': pending['revision'],
        'resultRevision': (pending['revision'] as int) + 1,
      };

Future<PersonalCollectionController> source(
  http.Client client, {
  Listenable? changes,
  String? Function()? owner,
}) async {
  final c = collectionController(client, changes: changes, owner: owner);
  await c.open(PersonalCollection.fromJson(collectionReceipt()));
  return c;
}

CollectionPublicationController publication(PersonalCollectionController c) =>
    CollectionPublicationController(
      source: c,
      authorizationHeader: c.authorizationHeader,
      ownerID: c.ownerID,
      client: c.transport,
      apiBaseUrl: c.apiBaseUrl,
    );

Map<String, dynamic> publicRecord({String precision = 'hidden'}) => {
  'id': collectionID,
  'kind': 'journey',
  'cityId': 'aberdeen-gb',
  'title': '周末路线',
  'summary': '自己安排的路线',
  'revision': 2,
  'sourceExpiresAt': DateTime.now()
      .toUtc()
      .add(const Duration(seconds: 60))
      .toIso8601String(),
  'entries': [
    <String, dynamic>{
      'note': '公开备注',
      'locationVisibility': precision,
      if (precision == 'place') 'placeId': collectionPlaceID,
      if (precision == 'place') 'placeName': '海边公园',
    },
  ],
};

void main() {
  for (final allowed in [false, true]) {
    test(
      'C3 publication copy permission $allowed requires explicit current author choice',
      () async {
        final writes = <Map<String, dynamic>>[];
        final client = MockClient((r) async {
          if (r.method == 'POST') {
            final body = jsonDecode(r.body) as Map<String, dynamic>;
            writes.add(body);
            return collectionResponse(
              operationRecord({
                'revision': body['revision'],
                'action': 'PUBLISH',
                'operationId': body['operationId'],
              }),
            );
          }
          return collectionResponse(collectionReceipt());
        });
        final c = await source(client), p = publication(c);
        try {
          expect(p.allowPrivatePlaceCopy, isFalse);
          if (allowed) p.setAllowPrivatePlaceCopy(true);
          expect(await p.publish(), isTrue);
          expect(writes.single['allowPrivatePlaceCopy'], allowed);
          expect(
            writes.single.containsKey('privatePlaceCopyRestricted'),
            isFalse,
          );
        } finally {
          p.dispose();
          c.dispose();
          client.close();
        }
      },
    );
  }
  test(
    'C3 authoritative borrowed sidecar read disables publish even after own text edits and restart',
    () async {
      var writes = 0;
      final client = MockClient((r) async {
        if (r.method != 'GET') writes++;
        return collectionResponse({
          ...collectionReceipt(),
          'privatePlaceCopyRestricted': true,
        });
      });
      for (var restart = 0; restart < 2; restart++) {
        final c = await source(client), p = publication(c);
        expect(c.editing!.privatePlaceCopyRestricted, isTrue);
        expect(p.canPublish, isFalse);
        p.setAllowPrivatePlaceCopy(true);
        expect(p.allowPrivatePlaceCopy, isFalse);
        expect(await p.publish(), isFalse);
        expect(writes, 0);
        p.dispose();
        c.dispose();
      }
      client.close();
    },
  );
  test('冷开已读取新版2，结束旧操作1不会倒退本机编辑基准', () async {
    final client = MockClient((r) async {
      final current = collectionReceipt(revision: 2);
      if (r.method == 'GET') return collectionResponse(current);
      expect(r.url.path.endsWith('/close'), true);
      current['publicationOperation'] = {
        'operationId': collectionID,
        'action': 'PUBLISH',
        'expectedRevision': 1,
        'outcome': 'NO_EFFECT',
      };
      return collectionResponse(current);
    });
    final c = await source(client), p = publication(c);
    try {
      c.holdPublication({
        'id': collectionID,
        'kind': 'journey',
        'operationId': collectionID,
        'revision': 1,
        'action': 'PUBLISH',
      });
      expect(c.editing!.revision, 2);
      expect(await p.closeOriginal(), true);
      expect(c.editing!.revision, 2);
      expect(c.retainedLocalDraft!.editingRevision, 2);
      expect(p.canPublish, false);
      expect(await c.revalidateRetainedEditing(), true);
      expect(p.canPublish, true);
    } finally {
      p.dispose();
      c.dispose();
      client.close();
    }
  });
  test('NO_EFFECT同版本干净草稿仍须原GET重验后才能新公开', () async {
    var published = 0;
    late PersonalCollectionController c;
    final client = MockClient((r) async {
      if (r.method == 'GET') return collectionResponse(collectionReceipt());
      if (r.url.path.endsWith('/close')) {
        return collectionResponse(
          collectionReceipt()
            ..['publicationOperation'] = {
              'operationId': collectionID,
              'action': 'PUBLISH',
              'expectedRevision': 1,
              'outcome': 'NO_EFFECT',
            },
        );
      }
      published++;
      expect(c.publicationPending!['operationId'], isNot(collectionID));
      return collectionResponse(operationRecord(c.publicationPending!));
    });
    c = await source(client);
    final p = publication(c);
    try {
      expect(p.canPublish, true);
      c.holdPublication({
        'id': collectionID,
        'kind': 'journey',
        'operationId': collectionID,
        'revision': 1,
        'action': 'PUBLISH',
      });
      expect(await p.closeOriginal(), true);
      expect(c.dirty, false);
      expect(c.editable, true);
      expect(await p.publish(), false);
      expect(published, 0);
      expect(await c.revalidateRetainedEditing(), true);
      expect(p.canPublish, true);
      expect(await p.publish(), true);
      expect(published, 1);
    } finally {
      p.dispose();
      c.dispose();
      client.close();
    }
  });
  test('普通404不能解锁；显式close NO_EFFECT只清原hold并保留本机修改稿', () async {
    final pending = <String, dynamic>{
      'id': collectionID,
      'kind': 'journey',
      'operationId': collectionID,
      'revision': 1,
      'action': 'PUBLISH',
    };
    var closes = 0;
    var closeFinished = false;
    var originalReads = 0;
    final client = MockClient((r) async {
      if (r.url.path.endsWith('/receipt'))
        return collectionResponse({}, code: 404);
      if (r.method == 'GET') {
        originalReads++;
        return collectionResponse(
          closeFinished
              ? (collectionReceipt(revision: 2)..['summary'] = '其他设备当前说明')
              : collectionReceipt(),
        );
      }
      expect(
        r.url.path,
        '/v1/me/personal-collections/publication-operations/$collectionID/close',
      );
      expect(jsonDecode(r.body), {
        'id': collectionID,
        'kind': 'journey',
        'revision': 1,
        'action': 'PUBLISH',
      });
      closes++;
      closeFinished = true;
      final result = collectionReceipt(revision: 2)..['summary'] = '其他设备当前说明';
      result['publicationOperation'] = {
        'operationId': collectionID,
        'action': 'PUBLISH',
        'expectedRevision': 1,
        'outcome': 'NO_EFFECT',
      };
      return collectionResponse(result);
    });
    final c = await source(client), p = publication(c);
    try {
      c.setText(summary: '原本机未保存说明');
      c.holdPublication(pending);
      expect(await p.recover(), false);
      expect(c.publicationPending, isNotNull);
      expect(await p.closeOriginal(), true);
      expect(closes, 1);
      expect(c.publicationPending, isNull);
      expect(c.summary, '原本机未保存说明');
      expect(c.editing!.revision, 1);
      expect(c.retainedLocalDraft!.editingRevision, 1);
      expect(p.record!.revision, 2);
      expect(c.editable, true);
      expect(c.publicationVersionCurrent, false);
      expect(p.canPublish, false);
      expect(await c.save(), false);
      expect(originalReads, 2);
      expect(closes, 1);
      expect(c.summary, '原本机未保存说明');
      expect(c.error, contains('版本已变化'));
    } finally {
      p.dispose();
      c.dispose();
      client.close();
    }
  });
  test('close取回已提交原回执，不伪装无副作用或再次发布', () async {
    var closes = 0;
    final client = MockClient((r) async {
      if (r.method == 'GET') return collectionResponse(collectionReceipt());
      closes++;
      expect(r.url.path.endsWith('/close'), true);
      return collectionResponse(
        operationRecord({
          'operationId': collectionID,
          'revision': 1,
          'action': 'PUBLISH',
        })..['publicationOperation']['outcome'] = 'COMMITTED',
      );
    });
    final c = await source(client), p = publication(c);
    try {
      c.holdPublication({
        'id': collectionID,
        'kind': 'journey',
        'operationId': collectionID,
        'revision': 1,
        'action': 'PUBLISH',
      });
      expect(await p.closeOriginal(), true);
      expect(c.editing!.status, 'published');
      expect(c.publicationPending, isNull);
      expect(closes, 1);
    } finally {
      p.dispose();
      c.dispose();
      client.close();
    }
  });
  test('伪造NO_EFFECT resultRevision不释放原pending', () async {
    final client = MockClient((r) async {
      final result = collectionReceipt()
        ..['publicationOperation'] = {
          'operationId': collectionID,
          'action': 'PUBLISH',
          'expectedRevision': 1,
          'outcome': 'NO_EFFECT',
          'resultRevision': 2,
        };
      return collectionResponse(result);
    });
    final c = await source(client), p = publication(c);
    try {
      c.holdPublication({
        'id': collectionID,
        'kind': 'journey',
        'operationId': collectionID,
        'revision': 1,
        'action': 'PUBLISH',
      });
      expect(await p.closeOriginal(), false);
      expect(c.publicationPending, isNotNull);
    } finally {
      p.dispose();
      c.dispose();
      client.close();
    }
  });
  test('只发布已保存具体版本，默认隐藏结构位置；重复点击仅一次POST', () async {
    final gate = Completer<http.Response>();
    var writes = 0;
    late PersonalCollectionController c;
    final client = MockClient((r) async {
      if (r.method == 'GET') return collectionResponse(collectionReceipt());
      writes++;
      final p = jsonDecode(r.body) as Map<String, dynamic>;
      expect(p['revision'], 1);
      expect(p['entries'][0]['locationVisibility'], 'hidden');
      expect(p.containsKey('owner'), false);
      expect(c.editable, false);
      return gate.future;
    });
    c = await source(client);
    final p = publication(c);
    try {
      expect(p.canPublish, true);
      final result = p.publish();
      expect(await p.publish(), false);
      gate.complete(collectionResponse(operationRecord(c.publicationPending!)));
      expect(await result, true);
      expect(writes, 1);
      expect(c.editing!.status, 'published');
      expect(c.editable, false);
      expect(await c.save(), false);
      expect(await c.withdraw(), false);
      c.newDraft();
      expect(c.editing, null);
      expect(p.canUnpublish, false);
    } finally {
      p.dispose();
      c.dispose();
      client.close();
    }
  });

  test('未保存文字不自动保存或发布', () async {
    var writes = 0;
    final client = MockClient((r) async {
      if (r.method != 'GET') writes++;
      return collectionResponse(collectionReceipt());
    });
    final c = await source(client);
    final p = publication(c);
    try {
      c.setText(summary: '还没保存');
      expect(p.canPublish, false);
      expect(await p.publish(), false);
      expect(writes, 0);
    } finally {
      p.dispose();
      c.dispose();
      client.close();
    }
  });

  test('未知结果跨页面和本机草稿恢复只GET原因果回执', () async {
    var writes = 0;
    Map<String, dynamic>? pending;
    final client = MockClient((r) async {
      if (r.method == 'POST') {
        writes++;
        throw http.ClientException('lost receipt');
      }
      if (r.url.path.contains('publication-operations'))
        return collectionResponse(operationRecord(pending!));
      return collectionResponse(collectionReceipt());
    });
    final c = await source(client);
    final p = publication(c);
    expect(await p.publish(), false);
    pending = c.publicationPending;
    expect(await p.publish(), false);
    expect(await c.save(), false);
    final retained = c.retainedLocalDraft!;
    p.dispose();
    c.dispose();
    final restored = collectionController(client, retained: retained);
    final next = publication(restored);
    try {
      expect(next.uncertain, true);
      expect(await next.publish(), false);
      expect(await next.recover(), true);
      expect(writes, 1);
      expect(restored.editing!.status, 'published');
      expect(next.uncertain, false);
    } finally {
      next.dispose();
      restored.dispose();
      client.close();
    }
  });

  test('不匹配原operation的200不冒称成功且仍只核实', () async {
    late PersonalCollectionController c;
    final client = MockClient(
      (r) async => r.method == 'GET'
          ? collectionResponse(collectionReceipt())
          : collectionResponse(
              operationRecord(
                c.publicationPending!,
                wrongOperation: collectionID,
              ),
            ),
    );
    c = await source(client);
    final p = publication(c);
    try {
      expect(await p.publish(), false);
      expect(p.uncertain, true);
      expect(c.editing!.status, 'draft');
    } finally {
      p.dispose();
      c.dispose();
      client.close();
    }
  });

  test('明确409保留草稿，撤回公开有独立当前版本动作', () async {
    var fail = true;
    late PersonalCollectionController c;
    final client = MockClient((r) async {
      if (r.method == 'GET') return collectionResponse(collectionReceipt());
      if (fail) return collectionResponse({}, code: 409);
      expect(
        r.url.path,
        '/v1/me/personal-collections/journey/$collectionID${c.publicationPending!['action'] == 'UNPUBLISH' ? '/unpublish' : '/publication'}',
      );
      return collectionResponse(operationRecord(c.publicationPending!));
    });
    c = await source(client);
    final p = publication(c);
    try {
      expect(await p.publish(), false);
      expect(p.uncertain, false);
      expect(c.title, '周末路线');
      fail = false;
      expect(await p.publish(), true);
      expect(p.canUnpublish, true);
      expect(await p.unpublish(), true);
      expect(c.editing!.revision, 3);
      expect(c.editable, true);
    } finally {
      p.dispose();
      c.dispose();
      client.close();
    }
  });

  test('最终鉴权401可能晚于native提交，不丢弃原因果selector', () async {
    final client = MockClient(
      (r) async => r.method == 'GET'
          ? collectionResponse(collectionReceipt())
          : collectionResponse({}, code: 401),
    );
    final c = await source(client);
    final p = publication(c);
    try {
      expect(await p.publish(), false);
      expect(p.uncertain, true);
      expect(c.editable, false);
      expect(await p.publish(), false);
    } finally {
      p.dispose();
      c.dispose();
      client.close();
    }
  });

  test('账号ABA退休迟到公开结果，原owner保留UNKNOWN selectors', () async {
    var owner = collectionOwner;
    final changes = ChangeNotifier();
    final gate = Completer<http.Response>();
    final client = MockClient(
      (r) async => r.method == 'GET'
          ? collectionResponse(collectionReceipt())
          : gate.future,
    );
    final c = await source(client, changes: changes, owner: () => owner);
    final p = publication(c);
    final result = p.publish();
    final pending = c.publicationPending!;
    owner = collectionID;
    changes.notifyListeners();
    owner = collectionOwner;
    changes.notifyListeners();
    gate.complete(collectionResponse(operationRecord(pending)));
    try {
      expect(await result, false);
      expect(p.current, false);
      expect(p.record, null);
    } finally {
      p.dispose();
      c.dispose();
      client.close();
      changes.dispose();
    }
  });

  test('公开隐藏/城市DTO拒绝泄漏ID、标签或坐标，过期不能展示', () {
    const ref = CollectionShareReference('journey', collectionID);
    for (final precision in ['hidden', 'city']) {
      expect(
        PublicCollectionRecord.fromJson(
          publicRecord(precision: precision),
          ref,
        ).entries.length,
        1,
      );
      for (final field in ['placeId', 'placeName', 'latitude', 'longitude']) {
        final j = publicRecord(precision: precision);
        (j['entries'] as List)[0][field] = field == 'latitude'
            ? 57.1
            : collectionPlaceID;
        expect(
          () => PublicCollectionRecord.fromJson(j, ref),
          throwsFormatException,
        );
      }
    }
    final expired = publicRecord()
      ..['sourceExpiresAt'] = DateTime.now()
          .toUtc()
          .subtract(const Duration(seconds: 1))
          .toIso8601String();
    expect(
      () => PublicCollectionRecord.fromJson(expired, ref),
      throwsFormatException,
    );
  });

  test('公开只读身份ABA清空旧数据；分享代码只选择闭合原ID', () async {
    var token = 'Bearer old';
    final signal = ChangeNotifier();
    final gate = Completer<http.Response>();
    final client = MockClient((r) async => gate.future);
    final c = PublicCollectionController(
      reference: const CollectionShareReference('journey', collectionID),
      authorizationHeader: () => token,
      identityChanges: signal,
      client: client,
      apiBaseUrl: 'https://synthetic.invalid',
    );
    final result = c.load();
    token = 'Bearer new';
    signal.notifyListeners();
    token = 'Bearer old';
    signal.notifyListeners();
    gate.complete(collectionResponse(publicRecord()));
    await result;
    try {
      expect(c.current, false);
      expect(c.record, null);
      expect(
        CollectionShareReference.parse(
          'birdtie:collection/journey/$collectionID',
        )!.id,
        collectionID,
      );
      expect(
        CollectionShareReference.parse(
          'https://other.invalid/v1/public-collections/journey/$collectionID',
        ),
        null,
      );
      expect(
        CollectionShareReference.parse(
          'birdtie:collection/journey/$collectionID?owner=$collectionOwner',
        ),
        null,
      );
    } finally {
      c.dispose();
      client.close();
      signal.dispose();
    }
  });
}
