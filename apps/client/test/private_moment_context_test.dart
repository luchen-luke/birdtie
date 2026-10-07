import 'dart:convert';

import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/content/moment_context_choice.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  testWidgets('私人草稿情境选择显示真实名称和可移除的旧关联', (tester) async {
    String? selected;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: MomentContextChoice(
            label: '社群',
            value: 'old-community',
            available: const {'active-community': '羽毛球社群'},
            onChanged: (value) => selected = value,
          ),
        ),
      ),
    );
    expect(find.text('此前关联的社群'), findsOneWidget);
    await tester.tap(find.text('此前关联的社群'));
    await tester.pumpAndSettle();
    expect(find.text('羽毛球社群'), findsOneWidget);
    await tester.tap(find.text('不关联').last);
    await tester.pumpAndSettle();
    expect(selected, '');
  });

  test('私人动态草稿保留稳定情境引用，切换登录后立即隐藏旧数据', () async {
    var token = 'Bearer owner';
    final sent = <Map<String, dynamic>>[];
    var revision = 1;
    final record = <String, dynamic>{
      'id': '11111111-1111-4111-8111-111111111111',
      'cityId': 'aberdeen-gb',
      'placeId': '22222222-2222-4222-8222-222222222222',
      'title': '周末记录',
      'body': '只给自己看',
      'timePrecision': 'unknown',
      'locationPrecision': 'place',
      'activityIds': ['33333333-3333-4333-8333-333333333333'],
      'communityId': '44444444-4444-4444-8444-444444444444',
      'organizationId': '55555555-5555-4555-8555-555555555555',
      'status': 'draft',
      'revision': revision,
    };
    final client = MockClient((request) async {
      expect(request.headers.values, contains('Bearer owner'));
      if (request.method == 'GET') {
        return http.Response.bytes(
          utf8.encode(
            jsonEncode({
              'data': [record],
            }),
          ),
          200,
        );
      }
      sent.add(jsonDecode(request.body) as Map<String, dynamic>);
      if (request.method == 'PUT') {
        revision += 1;
        record['revision'] = revision;
      }
      return http.Response.bytes(
        utf8.encode(jsonEncode({'data': record})),
        request.method == 'POST' ? 201 : 200,
      );
    });
    final moments = PrivateMomentController(
      authorizationHeader: () => token,
      client: client,
      apiBaseUrl: 'https://birdtie.example',
    );
    final created = await moments.create(
      cityID: 'aberdeen-gb',
      title: '周末记录',
      body: '只给自己看',
      placeID: record['placeId'] as String,
      activityID: (record['activityIds'] as List<String>).first,
      communityID: record['communityId'] as String,
      organizationID: record['organizationId'] as String,
    );
    expect(created, isTrue, reason: 'error=${moments.error} sent=$sent');
    expect(sent.first['placeId'], record['placeId']);
    expect(
      sent.first['activityId'],
      (record['activityIds'] as List<String>).first,
    );
    expect(sent.first['communityId'], record['communityId']);
    expect(sent.first['organizationId'], record['organizationId']);
    final draft = moments.moments.single;
    expect(draft.activityIDs, hasLength(1));
    expect(draft.isEditableDraft, isTrue);
    expect(
      await moments.update(
        moment: draft,
        title: '修改标题',
        body: '',
        placeID: draft.placeID,
      ),
      isTrue,
    );
    expect(sent[1].containsKey('activityId'), isFalse);
    expect(sent[1].containsKey('communityId'), isFalse);
    token = 'Bearer other';
    expect(moments.moments, isEmpty);
    moments.dispose();
  });
}
